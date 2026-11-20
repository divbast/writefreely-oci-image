// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/divbast/ci/harness/probekit"
)

// gopherTimeout bounds the connection, and then the whole exchange.
const gopherTimeout = 10 * time.Second

// gopher sends selector to the Gopher server at addr and records whether
// the raw answer, to the server's close, is exactly the bytes of
// expectFile.
func gopher(ctx context.Context, out *probekit.Output, args []string) error {
	addr, selector, expectFile := args[0], args[1], args[2]
	want, err := readFile(expectFile)
	if err != nil {
		return err
	}
	what := "gopher " + addr + " " + selector + " answers " + expectFile + " exactly"
	expected := fmt.Sprintf("%d bytes", len(want))
	got, err := gopherFetch(ctx, addr, selector)
	if err != nil {
		out.Record(false, what, expected, err.Error())
		return nil
	}
	out.Record(bytes.Equal(got, want), what, expected, difference(got, want))
	return nil
}

// gopherFetch sends selector, as one line, and reads the answer until
// the server closes the connection.
func gopherFetch(ctx context.Context, addr, selector string) ([]byte, error) {
	d := net.Dialer{Timeout: gopherTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	return gopherExchange(conn, selector)
}

// gopherExchange sends selector over conn and reads the answer, all
// within gopherTimeout.
func gopherExchange(conn net.Conn, selector string) ([]byte, error) {
	if err := conn.SetDeadline(time.Now().Add(gopherTimeout)); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(conn, selector+"\r\n"); err != nil {
		return nil, err
	}
	answer, err := io.ReadAll(io.LimitReader(conn, maxBody))
	if err != nil {
		return nil, fmt.Errorf("after %d bytes: %w", len(answer), err)
	}
	return answer, nil
}

// difference says how got differs from want: "equal", or where the two
// first part, with what each holds from there.
func difference(got, want []byte) string {
	if bytes.Equal(got, want) {
		return fmt.Sprintf("%d bytes, equal", len(got))
	}
	at := 0
	for at < len(got) && at < len(want) && got[at] == want[at] {
		at++
	}
	from := max(at-20, 0)
	return fmt.Sprintf("%d bytes, first unlike at byte %d: got %s, want %s",
		len(got), at, clip(got[from:min(len(got), at+60)]), clip(want[from:min(len(want), at+60)]))
}
