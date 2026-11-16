// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Package health serves /healthz on the health port and holds the probe
// the image's HEALTHCHECK runs.
package health

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
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

// halfWithin bounds each half of Probe: the HEALTHCHECK's 10 s timeout
// clears both, one after the other.
const halfWithin = 4 * time.Second

// Probe checks the image is healthy, in two halves, neither of which
// implies the other: healthURL, the health port's /healthz, answers 200,
// and appURL, a WriteFreely page on the app port, answers 200. It prints
// nothing and returns 0 when both hold. Otherwise it prints one
// "<check>: <reason>" line for each half that failed, healthz or app, to
// stderr, which is the only record of why, and returns 1.
func Probe(ctx context.Context, appURL, healthURL string, stderr io.Writer) int {
	code := 0
	for _, h := range []struct{ name, url string }{{"healthz", healthURL}, {"app", appURL}} {
		if err := half(ctx, h.url); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: %v\n", h.name, err)
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
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s answered %d, want 200", req.URL.Path, resp.StatusCode)
	}
	return nil
}
