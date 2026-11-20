// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/divbast/ci/harness/probekit"
)

// Where the lab listens: HTTPS for its actor and WebFinger, and SMTPS,
// TLS from the first byte, for its mail relay.
const (
	httpsAddr = ":443"
	smtpsAddr = ":465"
)

// stopWithin bounds the lab's shutdown once it is told to stop.
const stopWithin = 5 * time.Second

// journalPoll is how often a wait reads a journal again.
const journalPoll = 250 * time.Millisecond

// lab is what lab-serve serves, loaded from its directory.
type lab struct {
	cert tls.Certificate
	// actorPEM is the lab actor's public key, as its actor publishes it.
	actorPEM string
	// canary is the token the canary's WebFinger link carries.
	canary      string
	inbox, mail *journal
	log         *log.Logger
}

// loadLab reads the lab's directory: cert.pem and key.pem, the TLS
// certificate both listeners serve; actor.pem, the lab actor's key; and
// canary.txt, a token.
func loadLab(dir string) (*lab, error) {
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
	if err != nil {
		return nil, fmt.Errorf("the lab's certificate: %w", err)
	}
	key, err := loadRSAKey(filepath.Join(dir, "actor.pem"))
	if err != nil {
		return nil, fmt.Errorf("the lab actor's key: %w", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	canary, err := readFile(filepath.Join(dir, "canary.txt"))
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(string(canary))
	if token == "" {
		return nil, fmt.Errorf("%s is empty", filepath.Join(dir, "canary.txt"))
	}
	return &lab{
		cert:     cert,
		actorPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})),
		canary:   token,
		inbox:    &journal{path: inboxJournal},
		mail:     &journal{path: mailJournal},
		log:      log.New(os.Stderr, "lab: ", 0),
	}, nil
}

// labServe serves the lab from dir until it is stopped: HTTPS on 443,
// and SMTPS on 465, each with dir's certificate. What its inbox and its
// relay take goes to the journals on /tmp/lab. It records one line when
// it stops, and fails when either listener does.
func labServe(ctx context.Context, out *probekit.Output, args []string) error {
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		return err
	}
	l, err := loadLab(args[0])
	if err != nil {
		return err
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{l.cert}, MinVersion: tls.VersionTLS12}
	var lc net.ListenConfig
	httpsLn, err := lc.Listen(ctx, "tcp", httpsAddr)
	if err != nil {
		return fmt.Errorf("listening for HTTPS: %w", err)
	}
	smtpsLn, err := lc.Listen(ctx, "tcp", smtpsAddr)
	if err != nil {
		return errors.Join(fmt.Errorf("listening for SMTPS: %w", err), httpsLn.Close())
	}

	srv := &http.Server{
		Handler:           l.routes(),
		ReadHeaderTimeout: httpTimeout,
		ErrorLog:          log.New(os.Stderr, "lab: https: ", 0),
	}
	failed := make(chan error, 2)
	go func() { failed <- fmt.Errorf("HTTPS: %w", srv.Serve(tls.NewListener(httpsLn, cfg))) }()
	go func() { failed <- fmt.Errorf("SMTPS: %w", l.serveSMTPS(ctx, tls.NewListener(smtpsLn, cfg))) }()
	l.log.Printf("serving HTTPS on %s and SMTPS on %s", httpsAddr, smtpsAddr)

	select {
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopWithin)
		defer cancel()
		err := errors.Join(srv.Shutdown(stopCtx), smtpsLn.Close())
		found := "stopped"
		if err != nil {
			found = err.Error()
		}
		out.Record(err == nil, "the lab serves until it is stopped", "stopped", found)
		return nil
	case err := <-failed:
		return errors.Join(fmt.Errorf("the lab stopped serving: %w", err), srv.Close(), smtpsLn.Close())
	}
}

// routes are the lab's HTTPS routes, each request logged.
func (l *lab) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/webfinger", l.webfinger)
	mux.HandleFunc("GET /users/lab", l.actor)
	mux.HandleFunc("POST /users/lab/inbox", l.takeInbox)
	mux.HandleFunc("/users/gone", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "this account is gone", http.StatusGone)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(rec, r)
		l.log.Printf("%s %s %s %d", r.RemoteAddr, r.Method, r.RequestURI, rec.status)
	})
}

// statusRecorder keeps the status a handler wrote, for the log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

// writeJSON answers v as JSON of contentType.
func (l *lab) writeJSON(w http.ResponseWriter, contentType string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if _, err := w.Write(body); err != nil {
		l.log.Printf("writing an answer: %v", err)
	}
}

// webfinger answers the lab's two accounts: lab, whose self link is the
// lab's actor, and canary, whose self link is a loopback URL on the
// blog's own port, carrying the canary token.
func (l *lab) webfinger(w http.ResponseWriter, r *http.Request) {
	resource := r.URL.Query().Get("resource")
	var self string
	switch resource {
	case "acct:lab@" + labIP:
		self = labActor
	case "acct:canary@" + labIP:
		self = "http://127.0.0.1:8080/loopback-canary-" + l.canary
	default:
		http.NotFound(w, r)
		return
	}
	l.writeJSON(w, "application/jrd+json", map[string]any{
		"subject": resource,
		"links":   []map[string]string{{"rel": "self", "type": activityJSON, "href": self}},
	})
}

// actor answers the lab's actor, with the public half of actor.pem.
func (l *lab) actor(w http.ResponseWriter, _ *http.Request) {
	l.writeJSON(w, activityJSON, map[string]any{
		"@context":          []string{asContext, "https://w3id.org/security/v1"},
		"id":                labActor,
		"type":              "Person",
		"preferredUsername": "lab",
		"name":              "Lab",
		"url":               labActor,
		"inbox":             labActor + "/inbox",
		"outbox":            labActor + "/outbox",
		"followers":         labActor + "/followers",
		"following":         labActor + "/following",
		"publicKey": map[string]string{
			"id":           labKeyID,
			"owner":        labActor,
			"publicKeyPem": l.actorPEM,
		},
	})
}

// inboxEntry is one POST the lab's inbox took, as it arrived.
type inboxEntry struct {
	Time   time.Time `json:"time"`
	Remote string    `json:"remote"`
	Method string    `json:"method"`
	// Target is the request target, as the request line carried it.
	Target string      `json:"target"`
	Host   string      `json:"host"`
	Header http.Header `json:"header"`
	Body   string      `json:"body"`
}

// takeInbox appends each POST to the inbox journal, with its headers,
// and answers 202.
func (l *lab) takeInbox(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	err = l.inbox.append(&inboxEntry{
		Time:   time.Now().UTC(),
		Remote: r.RemoteAddr,
		Method: r.Method,
		Target: r.RequestURI,
		Host:   r.Host,
		Header: r.Header,
		Body:   string(body),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// journal is a file of JSON lines, appended to by the lab and read by
// the waits, each in a process of its own.
type journal struct {
	mu   sync.Mutex
	path string
}

// append writes v as one line, in one write, so a reader never sees half
// of it but as a last line with no newline yet, which it skips.
func (j *journal) append(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	f, err := os.OpenFile(j.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	return errors.Join(werr, f.Close())
}

// readJournal returns every whole line of the journal at path, none when
// there is no journal yet.
func readJournal[T any](path string) ([]T, error) {
	b, err := readFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	whole := b[:bytes.LastIndexByte(b, '\n')+1]
	var entries []T
	for line := range bytes.Lines(whole) {
		var e T
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// waitJournal reads the journal at path until an entry matches, or
// within has passed, and returns the first that matches. The count is
// of the entries it read that did not.
func waitJournal[T any](ctx context.Context, path string, within time.Duration, match func(*T) bool) (entry *T, others int, err error) {
	deadline := time.Now().Add(within)
	for {
		entries, err := readJournal[T](path)
		if err != nil {
			return nil, 0, err
		}
		for i := range entries {
			if match(&entries[i]) {
				return &entries[i], i, nil
			}
		}
		if !time.Now().Before(deadline) {
			return nil, len(entries), fmt.Errorf("none within %s", within)
		}
		select {
		case <-ctx.Done():
			return nil, len(entries), ctx.Err()
		case <-time.After(journalPoll):
		}
	}
}

// parseSeconds reads a wait's seconds argument.
func parseSeconds(arg string) (time.Duration, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("the wait is '%s' seconds, and must be a whole number above 0", arg)
	}
	return time.Duration(n) * time.Second, nil
}
