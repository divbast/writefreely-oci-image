// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package health_test

import (
	"bufio"
	"bytes"
	"database/sql"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/writefreely/writefreely"
	"github.com/writefreely/writefreely/migrations"

	"github.com/divbast/writefreely-oci-image/app/internal/conf"
	"github.com/divbast/writefreely-oci-image/app/internal/health"
	"github.com/divbast/writefreely-oci-image/app/internal/keys"
	"github.com/divbast/writefreely-oci-image/app/internal/wfapp"
)

// healthy is a database at the image's migration and its key files.
func healthy(t *testing.T) (db *sql.DB, keysDir string) {
	t.Helper()
	dir := t.TempDir()
	l := conf.Layout{Data: dir, Assets: filepath.Join(dir, "assets")}
	file, _, err := conf.Working(l, filepath.Join(dir, "etc", "config.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if err = writefreely.CreateSchema(wfapp.New(file, l)); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}
	if err = keys.Ensure(l.KeysDir()); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite3", filepath.Join(dir, "writefreely.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, l.KeysDir()
}

// get asks h for path and returns the status and body.
func get(t *testing.T, h http.Handler, method, path string) (status int, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, http.NoBody))
	return rec.Code, rec.Body.String()
}

// WriteFreely keeps package-level state, so CreateSchema runs one test at
// a time.
func TestHandler(t *testing.T) { //nolint:paralleltest // WriteFreely's package-level state
	db, keysDir := healthy(t)
	want := migrations.CurrentVer()
	h := health.Handler(db, "sqlite3", want, keysDir)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if code, body := get(t, h, method, "/healthz"); code != http.StatusOK || body != "" {
			t.Errorf("healthy %s /healthz = %d %q, want 200 and no body", method, code, body)
		}
	}
	if code, body := get(t, h, http.MethodGet, "/"); code != http.StatusNotFound || body != "" {
		t.Errorf("GET / = %d %q, want 404 and no body", code, body)
	}
	if code, body := get(t, h, http.MethodPost, "/healthz"); code != http.StatusNotFound || body != "" {
		t.Errorf("POST /healthz = %d %q, want 404 and no body", code, body)
	}
	if code, body := get(t, health.Handler(db, "sqlite3", want+1, keysDir), http.MethodGet, "/healthz"); code != http.StatusServiceUnavailable || !strings.HasPrefix(body, "schema: ") {
		t.Errorf("a schema behind = %d %q, want 503 and a schema: line", code, body)
	}

	if err := os.Remove(filepath.Join(keysDir, keys.Names[2])); err != nil {
		t.Fatal(err)
	}
	if code, body := get(t, h, http.MethodGet, "/healthz"); code != http.StatusServiceUnavailable || !strings.HasPrefix(body, "keys: ") || !strings.Contains(body, keys.Names[2]) {
		t.Errorf("a key missing = %d %q, want 503 and a keys: line naming it", code, body)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if code, body := get(t, h, http.MethodGet, "/healthz"); code != http.StatusServiceUnavailable || !strings.Contains(body, "database: ") {
		t.Errorf("a closed database = %d %q, want 503 and a database: line", code, body)
	}
}

// answering is a server that answers every request with status.
func answering(t *testing.T, status int) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(s.Close)
	return s.URL
}

// stalled is a URL whose listener accepts and never answers.
func stalled(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	return "http://" + ln.Addr().String() + "/"
}

func TestProbe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, app, healthz string
		wantCode           int
		wantLines          []string // each line's prefix, in order
	}{
		{name: "healthy", app: answering(t, http.StatusOK), healthz: answering(t, http.StatusOK)},
		{name: "app 404", app: answering(t, http.StatusNotFound), healthz: answering(t, http.StatusOK), wantCode: 1, wantLines: []string{"app: "}},
		{name: "healthz 503", app: answering(t, http.StatusOK), healthz: answering(t, http.StatusServiceUnavailable), wantCode: 1, wantLines: []string{"healthz: "}},
		{name: "both stalled", app: stalled(t), healthz: stalled(t), wantCode: 1, wantLines: []string{"healthz: ", "app: "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			start := time.Now()
			code := health.Probe(t.Context(), tc.app, tc.healthz, "", &stderr)
			if took, limit := time.Since(start), 2*health.HalfWithin+time.Second; took > limit {
				t.Errorf("Probe took %v, want at most %v", took, limit)
			}
			if code != tc.wantCode {
				t.Errorf("Probe = %d, want %d; stderr %q", code, tc.wantCode, stderr.String())
			}
			if tc.wantCode == 0 {
				if stderr.Len() != 0 {
					t.Errorf("a healthy Probe printed %q", stderr.String())
				}
				return
			}
			lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
			if len(lines) != len(tc.wantLines) {
				t.Fatalf("Probe printed %q, want a line for each failed half: %q", lines, tc.wantLines)
			}
			for i, want := range tc.wantLines {
				if !strings.HasPrefix(lines[i], want) {
					t.Errorf("line %d = %q, want it to start %q", i+1, lines[i], want)
				}
			}
		})
	}
}

// gopherServer is the address of a Gopher server that reads a selector
// and writes answer, then closes the connection, or with hold, keeps it
// open, saying nothing more, until the test ends.
func gopherServer(t *testing.T, answer []byte, hold bool) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { close(done); _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				if _, err := bufio.NewReader(c).ReadString('\n'); err != nil {
					return
				}
				// A probe that stops reading closes its end, which ends
				// a write it left unread
				if _, err := c.Write(answer); err != nil {
					return
				}
				if hold {
					<-done
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// item is a well-formed Gopher menu line.
const item = "0Jan 2, 2026 - A post\t/a-post\tblog.example.com\t70\r\n"

// hugeMenu is a root menu past what the probe reads of an answer, as a
// single-user blog with many thousands of posts answers.
func hugeMenu() []byte {
	menu := []byte("iA blog\t\terror.host\t1\r\n")
	for len(menu) <= 2*health.MaxAnswer {
		menu = append(menu, item...)
	}
	return append(menu, ".\r\n"...)
}

func TestProbeGopherHalf(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		answer   []byte
		hold     bool // the server keeps the connection open after answer
		wantCode int
		// The probe decides before the half's budget runs out
		quick bool
	}{
		{name: "empty menu", answer: []byte(".\r\n"), quick: true},
		{name: "menu", answer: []byte("iWelcome\t\tblog.example.com\t70\r\n" + item + ".\r\n"), quick: true},
		// The probe reads no further than the menu's first line, which
		// a large blog's root menu answers at once
		{name: "first line, then nothing", answer: []byte(item), hold: true, quick: true},
		{name: "first line, then closed", answer: []byte(item), quick: true},
		{name: "a menu past the read limit", answer: hugeMenu(), quick: true},
		{name: "never answers", hold: true, wantCode: 1},
		{name: "closed at once", wantCode: 1, quick: true},
		{name: "part of a line, then closed", answer: []byte("0Jan 2"), wantCode: 1, quick: true},
		{name: "garbage, then nothing", answer: []byte("HTTP/1.1 400 Bad Request\r\n"), hold: true, wantCode: 1, quick: true},
		{name: "too few fields", answer: []byte("iWelcome\t\tblog.example.com\r\n.\r\n"), wantCode: 1, quick: true},
		{name: "no item type", answer: []byte("\tWelcome\tblog.example.com\t70\r\n.\r\n"), wantCode: 1, quick: true},
		{name: "a port that is no number", answer: []byte("iWelcome\t\tblog.example.com\tseventy\r\n.\r\n"), wantCode: 1, quick: true},
		{name: "a bare LF", answer: []byte("iWelcome\t\tblog.example.com\t70\n.\n"), wantCode: 1, quick: true},
		{name: "a line past the read limit", answer: bytes.Repeat([]byte("i"), 2*health.MaxAnswer), hold: true, wantCode: 1, quick: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			start := time.Now()
			code := health.Probe(t.Context(), answering(t, http.StatusOK), answering(t, http.StatusOK), gopherServer(t, tc.answer, tc.hold), &stderr)
			limit := 3*health.HalfWithin + time.Second
			if tc.quick {
				limit = health.HalfWithin / 2
			}
			if took := time.Since(start); took > limit {
				t.Errorf("Probe took %v, want at most %v", took, limit)
			}
			if code != tc.wantCode {
				t.Errorf("Probe = %d, want %d; stderr %q", code, tc.wantCode, stderr.String())
			}
			switch {
			case tc.wantCode == 0 && stderr.Len() != 0:
				t.Errorf("a healthy Probe printed %q", stderr.String())
			case tc.wantCode != 0 && (!strings.HasPrefix(stderr.String(), "gopher: ") || strings.Count(stderr.String(), "\n") != 1):
				t.Errorf("Probe printed %q, want one gopher: line", stderr.String())
			}
		})
	}
	// Nothing listening is a failed half too
	var stderr bytes.Buffer
	if code := health.Probe(t.Context(), answering(t, http.StatusOK), answering(t, http.StatusOK), closedAddr(t), &stderr); code != 1 || !strings.HasPrefix(stderr.String(), "gopher: ") {
		t.Errorf("Probe with nothing on the Gopher port = %d, %q; want 1 and a gopher: line", code, stderr.String())
	}
}

// closedAddr is an address nothing listens on: a listener's, closed.
func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
