// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Package health serves /healthz on the health port and holds the probe
// the image's HEALTHCHECK runs.
package health

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/divbast/writefreely-oci-image/app/internal/dbstate"
	"github.com/divbast/writefreely-oci-image/app/internal/keys"
)

// checkWithin bounds the database's half of one /healthz answer.
const checkWithin = 3 * time.Second

// Handler serves /healthz, and an empty 404 to anything else: GET or HEAD
// answers 200 with no body when the database answers a ping, its schema
// is at migration want, and every key file in keysDir is readable, and
// otherwise 503 with one "<check>: <reason>" line per failed check:
// database, schema, keys.
func Handler(db *sql.DB, driver string, want int, keysDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), checkWithin)
		defer cancel()
		var failed []string
		if err := db.PingContext(ctx); err != nil {
			failed = append(failed, "database: "+err.Error())
		} else if s, err := dbstate.Read(ctx, db, driver); err != nil {
			failed = append(failed, "schema: "+err.Error())
		} else if s.Version != want {
			failed = append(failed, fmt.Sprintf("schema: at migration %d, want %d", s.Version, want))
		}
		if err := readable(keysDir); err != nil {
			failed = append(failed, "keys: "+err.Error())
		}
		if len(failed) == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, strings.Join(failed, "\n")+"\n")
	})
}

// readable checks every key file in dir opens for reading.
func readable(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	var errs []error
	for _, name := range keys.Names {
		f, err := root.Open(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, f.Close())
	}
	if err := errors.Join(errs...); err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), "\n", "; "))
	}
	return nil
}

// halfWithin bounds each half of Probe: the HEALTHCHECK's 15 s timeout
// clears all three, one after the other.
const halfWithin = 4 * time.Second

// maxAnswer bounds what a half reads of an answer.
const maxAnswer = 1 << 20

// Probe checks the image is healthy, in halves, none of which implies
// another: healthURL, the health port's /healthz, answers 200; appURL, a
// WriteFreely page on the app port, answers 200; and when gopherAddr is
// not empty, the Gopher server there answers the root selector with a
// whole menu. It prints nothing and returns 0 when every half holds.
// Otherwise it prints one "<check>: <reason>" line for each half that
// failed, healthz, app or gopher, to stderr, which is the only record of
// why, and returns 1.
func Probe(ctx context.Context, appURL, healthURL, gopherAddr string, stderr io.Writer) int {
	type check struct {
		name string
		run  func(context.Context) error
	}
	checks := []check{
		{"healthz", func(ctx context.Context) error { return half(ctx, healthURL) }},
		{"app", func(ctx context.Context) error { return half(ctx, appURL) }},
	}
	if gopherAddr != "" {
		checks = append(checks, check{"gopher", func(ctx context.Context) error { return gopherHalf(ctx, gopherAddr) }})
	}
	code := 0
	for _, c := range checks {
		if err := c.run(ctx); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: %v\n", c.name, err)
			code = 1
		}
	}
	return code
}

// half sends GET url within halfWithin, and returns why it did not answer
// 200.
func half(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, halfWithin)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	// A redirect is an answer of its own: following one would let the
	// app half pass on a page it did not ask for
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswer)); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s answered %d, want 200", req.URL.Path, resp.StatusCode)
	}
	return nil
}

// gopherHalf sends the root selector, an empty line, to the Gopher server
// at addr, and reads its answer up to the lone "." line that ends a menu,
// all within halfWithin. It returns why the answer did not arrive whole.
func gopherHalf(ctx context.Context, addr string) error {
	ctx, cancel := context.WithTimeout(ctx, halfWithin)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	// The context bounds only the dial; the deadline bounds the exchange
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(conn, "\r\n"); err != nil {
		return err
	}
	var answer []byte
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		answer = append(answer, buf[:n]...)
		switch {
		case bytes.Equal(answer, []byte(".\r\n")) || bytes.HasSuffix(answer, []byte("\n.\r\n")):
			return nil
		case len(answer) > maxAnswer:
			return fmt.Errorf("the root selector's answer ran past %d bytes without its closing \".\" line", maxAnswer)
		case errors.Is(err, io.EOF):
			return fmt.Errorf("the root selector's answer ended after %d bytes without its closing \".\" line", len(answer))
		case err != nil:
			return fmt.Errorf("reading the root selector's answer, after %d bytes: %w", len(answer), err)
		}
	}
}
