// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/textproto"
	"slices"
	"strings"
	"time"

	"github.com/divbast/ci/harness/probekit"
)

// smtpIdle bounds how long the relay waits for a client's next line, or
// its TLS handshake.
const smtpIdle = 2 * time.Minute

// mailEntry is one message the relay took, with how it came.
type mailEntry struct {
	Time   time.Time `json:"time"`
	Remote string    `json:"remote"`
	// TLS is the connection's TLS version: every connection is TLS from
	// its first byte.
	TLS  string `json:"tls"`
	Helo string `json:"helo"`
	// AuthUser and AuthPass are what AUTH PLAIN sent, any of which the
	// relay takes; empty when the client sent no AUTH.
	AuthUser string   `json:"auth_user"`
	AuthPass string   `json:"auth_pass"`
	From     string   `json:"from"`
	To       []string `json:"to"`
	Data     string   `json:"data"`
}

// serveSMTPS takes connections on ln, each served on its own, until ln
// closes.
func (l *lab) serveSMTPS(ctx context.Context, ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		tlsConn, ok := conn.(*tls.Conn)
		if !ok {
			_ = conn.Close()
			return fmt.Errorf("the SMTPS listener gave a %T, not a TLS connection", conn)
		}
		go l.serveSMTP(ctx, tlsConn)
	}
}

// smtpSession is one connection to the relay.
type smtpSession struct {
	text *textproto.Conn
	// msg is the message being taken: how the connection came, kept
	// across messages, and the envelope and data, reset after each.
	msg mailEntry
}

// serveSMTP serves one connection: a line-oriented loop over EHLO, HELO,
// AUTH PLAIN, MAIL, RCPT, DATA, RSET, NOOP and QUIT, appending each
// message DATA ends to the mail journal.
func (l *lab) serveSMTP(ctx context.Context, conn *tls.Conn) {
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(smtpIdle)); err != nil {
		return
	}
	if err := conn.HandshakeContext(ctx); err != nil {
		l.log.Printf("smtps: %s: TLS handshake: %v", conn.RemoteAddr(), err)
		return
	}
	s := &smtpSession{
		text: textproto.NewConn(conn),
		msg: mailEntry{
			Remote: conn.RemoteAddr().String(),
			TLS:    tls.VersionName(conn.ConnectionState().Version),
		},
	}
	if s.text.PrintfLine("220 %s ESMTP lab relay", labIP) != nil {
		return
	}
	for {
		if err := conn.SetDeadline(time.Now().Add(smtpIdle)); err != nil {
			return
		}
		line, err := s.text.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		quit, err := s.handle(l, strings.ToUpper(verb), arg)
		if err != nil {
			l.log.Printf("smtps: %s: %s: %v", s.msg.Remote, strings.ToUpper(verb), err)
			return
		}
		if quit {
			return
		}
	}
}

// handle answers one command, and reports whether the client quit.
func (s *smtpSession) handle(l *lab, verb, arg string) (quit bool, err error) {
	switch verb {
	case "EHLO":
		s.msg.Helo = arg
		return false, s.lines("250-"+labIP, "250-8BITMIME", "250 AUTH PLAIN")
	case "HELO":
		s.msg.Helo = arg
		return false, s.lines("250 " + labIP)
	case "AUTH":
		return false, s.auth(arg)
	case "MAIL":
		from, ok := mailPath(arg, "FROM:")
		if !ok {
			return false, s.lines("501 5.5.4 MAIL takes FROM:<address>")
		}
		s.reset()
		s.msg.From = from
		return false, s.lines("250 2.1.0 ok")
	case "RCPT":
		to, ok := mailPath(arg, "TO:")
		switch {
		case !ok:
			return false, s.lines("501 5.5.4 RCPT takes TO:<address>")
		case s.msg.From == "":
			return false, s.lines("503 5.5.1 MAIL first")
		}
		s.msg.To = append(s.msg.To, to)
		return false, s.lines("250 2.1.5 ok")
	case "DATA":
		return false, s.data(l)
	case "RSET":
		s.reset()
		return false, s.lines("250 2.0.0 ok")
	case "NOOP":
		return false, s.lines("250 2.0.0 ok")
	case "QUIT":
		return true, s.lines("221 2.0.0 bye")
	default:
		return false, s.lines("502 5.5.2 not offered")
	}
}

// lines writes each line of a reply.
func (s *smtpSession) lines(lines ...string) error {
	for _, line := range lines {
		if err := s.text.PrintfLine("%s", line); err != nil {
			return err
		}
	}
	return nil
}

// reset clears the envelope and the data, and keeps how the connection
// came.
func (s *smtpSession) reset() {
	s.msg.From, s.msg.To, s.msg.Data = "", nil, ""
}

// auth takes AUTH PLAIN, with its response on the line or after a 334,
// and accepts any credentials, keeping them for the journal.
func (s *smtpSession) auth(arg string) error {
	mech, response, _ := strings.Cut(arg, " ")
	if !strings.EqualFold(mech, "PLAIN") {
		return s.lines("504 5.5.4 only AUTH PLAIN is offered")
	}
	if response == "" {
		if err := s.lines("334 "); err != nil {
			return err
		}
		line, err := s.text.ReadLine()
		if err != nil {
			return err
		}
		response = line
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(response))
	parts := strings.Split(string(raw), "\x00")
	if err != nil || len(parts) != 3 {
		return s.lines("501 5.5.2 not an AUTH PLAIN response")
	}
	s.msg.AuthUser, s.msg.AuthPass = parts[1], parts[2]
	return s.lines("235 2.7.0 authenticated")
}

// data takes a message's data, to the line holding a lone dot, and
// appends the message to the journal.
func (s *smtpSession) data(l *lab) error {
	if s.msg.From == "" || len(s.msg.To) == 0 {
		return s.lines("503 5.5.1 MAIL and RCPT first")
	}
	if err := s.lines("354 end with a line holding a lone dot"); err != nil {
		return err
	}
	data, err := s.text.ReadDotBytes()
	if err != nil {
		return err
	}
	s.msg.Data, s.msg.Time = string(data), time.Now().UTC()
	if err := l.mail.append(&s.msg); err != nil {
		l.log.Printf("smtps: journal: %v", err)
		return s.lines("451 4.3.0 the message could not be kept")
	}
	l.log.Printf("smtps: %s: a message from %s to %s, over %s", s.msg.Remote, s.msg.From, strings.Join(s.msg.To, ", "), s.msg.TLS)
	s.reset()
	return s.lines("250 2.0.0 taken")
}

// mailPath returns the address in a MAIL or RCPT argument, as
// "FROM:<address>" or "TO:<address>" with any parameters after it.
func mailPath(arg, prefix string) (string, bool) {
	if len(arg) < len(prefix) || !strings.EqualFold(arg[:len(prefix)], prefix) {
		return "", false
	}
	rest := strings.TrimSpace(arg[len(prefix):])
	if strings.HasPrefix(rest, "<") {
		addr, _, ok := strings.Cut(rest[1:], ">")
		return addr, ok
	}
	addr, _, _ := strings.Cut(rest, " ")
	return addr, addr != ""
}

// mailWait waits up to seconds for a message to to in the relay's
// journal, and records that it came, that it came over TLS, and that its
// sender logged in with exactly user and pass: the relay takes any.
func mailWait(ctx context.Context, out *probekit.Output, args []string) error {
	to, user, pass := args[1], args[2], args[3]
	within, err := parseSeconds(args[0])
	if err != nil {
		return err
	}
	arrives := "a message to " + to + " arrives within " + args[0] + " s"
	msg, others, err := waitJournal(ctx, mailJournal, within, func(m *mailEntry) bool {
		return slices.ContainsFunc(m.To, func(r string) bool { return strings.EqualFold(r, to) })
	})
	if err != nil {
		out.Record(false, arrives, "a message", fmt.Sprintf("%v; the relay took %d others", err, others))
		return nil
	}
	out.Record(true, arrives, "a message", fmt.Sprintf("from %s, %d bytes, AUTH PLAIN as %q", msg.From, len(msg.Data), msg.AuthUser))
	out.Record(strings.HasPrefix(msg.TLS, "TLS"), "the message to "+to+" came over TLS", "TLS", msg.TLS)
	out.Record(msg.AuthUser == user && msg.AuthPass == pass, "the message to "+to+" logged in as "+user+" with the password given",
		fmt.Sprintf("%q with %q", user, pass), fmt.Sprintf("%q with %q", msg.AuthUser, msg.AuthPass))
	return nil
}
