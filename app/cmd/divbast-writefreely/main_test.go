// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package main

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/writefreely/writefreely"

	"github.com/divbast/writefreely-oci-image/app/internal/bootstrap"
	"github.com/divbast/writefreely-oci-image/app/internal/conf"
)

// options are Options on a fresh, empty layout, writing to out.
func options(t *testing.T, out *bytes.Buffer) bootstrap.Options {
	t.Helper()
	dir := t.TempDir()
	return bootstrap.Options{
		Layout:  conf.Layout{Data: filepath.Join(dir, "data"), Assets: filepath.Join(dir, "assets")},
		Mounted: filepath.Join(dir, "etc", "config.ini"),
		Secret:  filepath.Join(dir, "secrets", "writefreely_admin_password"),
		Out:     out,
	}
}

func TestRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string   // a substring of stdout; empty wants none
		wantErr  []string // substrings of stderr; none wants it empty
	}{
		// With no fork build set, as in tests, WriteFreely's own version
		{name: "version", args: []string{"version"}, wantOut: writefreely.FormatVersion() + "\n"},
		{name: "none", wantCode: 2, wantErr: []string{"usage: divbast-writefreely"}},
		{name: "unknown", args: []string{"nope"}, wantCode: 2, wantErr: []string{"usage: divbast-writefreely"}},
		// The ENTRYPOINT is serve, so docker run <image> version runs
		// "serve version": it must not start a server, or bootstrap a trial
		{name: "serve with more", args: []string{"serve", "version"}, wantCode: 2, wantErr: []string{"usage: divbast-writefreely"}},
		{name: "version with more", args: []string{"version", "x"}, wantCode: 2, wantErr: []string{"usage: divbast-writefreely"}},
		{name: "health with more", args: []string{"health", "x"}, wantCode: 2, wantErr: []string{"usage: divbast-writefreely"}},
		{name: "user alone", args: []string{"user"}, wantCode: 2, wantErr: []string{"usage: divbast-writefreely user"}},
		{name: "user unknown", args: []string{"user", "nope", "x"}, wantCode: 2, wantErr: []string{"usage: divbast-writefreely user"}},
		{name: "user create without a name", args: []string{"user", "create"}, wantCode: 2, wantErr: []string{"usage: divbast-writefreely user"}},
		// Before the first serve there is no working file, and user must
		// not make one in the wrong place, nor ask for a password first
		{name: "user before serve", args: []string{"user", "create", "a"}, wantCode: 1, wantErr: []string{"no working config yet: start the server once with serve first"}},
		// Nothing listens on either port in a test
		{name: "health", args: []string{"health"}, wantCode: 1, wantErr: []string{"healthz: ", "app: "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out, errb bytes.Buffer
			o := options(t, &out)
			if code := run(tc.args, &o, strings.NewReader(""), &errb); code != tc.wantCode {
				t.Errorf("run(%q) = %d, want %d; stderr %q", tc.args, code, tc.wantCode, errb.String())
			}
			switch {
			case tc.wantOut == "" && out.Len() != 0:
				t.Errorf("stdout = %q, want nothing", out.String())
			case !strings.Contains(out.String(), tc.wantOut):
				t.Errorf("stdout = %q, want it to contain %q", out.String(), tc.wantOut)
			}
			if len(tc.wantErr) == 0 && errb.Len() != 0 {
				t.Errorf("stderr = %q, want nothing", errb.String())
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(errb.String(), want) {
					t.Errorf("stderr = %q, want it to contain %q", errb.String(), want)
				}
			}
			if _, err := os.Stat(o.Layout.DataConfig()); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("run(%q) left %s behind (stat: %v)", tc.args, o.Layout.DataConfig(), err)
			}
		})
	}
}

// version names the fork build the image's build sets, in WriteFreely's
// own form, and falls back to WriteFreely's version without one.
func TestVersionLine(t *testing.T) {
	t.Parallel()
	if got, want := versionLine("0.17.3-divbast.7"), "WriteFreely 0.17.3-divbast.7"; got != want {
		t.Errorf("versionLine(fork build) = %q, want %q", got, want)
	}
	if got, want := versionLine(""), writefreely.FormatVersion(); got != want {
		t.Errorf("versionLine(\"\") = %q, want %q", got, want)
	}
}

// served is options on a layout serve has bootstrapped, as a trial: the
// working config.ini and the admin, writer, exist.
func served(t *testing.T) bootstrap.Options {
	t.Helper()
	var out bytes.Buffer
	o := options(t, &out)
	// Upstream's username check walks the pages tree, which the image
	// ships, and refuses every name when it is missing
	if err := os.MkdirAll(filepath.Join(o.Layout.Assets, "pages"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Run(t.Context(), &o); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return o
}

// users counts the users named name in o's database.
func users(t *testing.T, o *bootstrap.Options, name string) int {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(o.Layout.Data, "writefreely.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM users WHERE username = ?", name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// WriteFreely keeps package-level state, so tests that touch its database
// do not run in parallel.
func TestUserCreateReadsStdin(t *testing.T) { //nolint:paralleltest // WriteFreely's package-level state
	o := served(t)
	var errb bytes.Buffer
	if code := run([]string{"user", "create", "bob"}, &o, strings.NewReader("pw\n"), &errb); code != 0 {
		t.Fatalf("user create bob = %d, want 0; stderr %q", code, errb.String())
	}
	if n := users(t, &o, "bob"); n != 1 {
		t.Errorf("%d users named bob, want 1", n)
	}

	// No line at all is no password, which is refused rather than set
	errb.Reset()
	if code := run([]string{"user", "create", "carol"}, &o, strings.NewReader(""), &errb); code != 1 || !strings.Contains(errb.String(), "no password") {
		t.Errorf("user create carol with nothing on stdin = %d, stderr %q; want 1 and no password refused", code, errb.String())
	}
	if n := users(t, &o, "carol"); n != 0 {
		t.Errorf("%d users named carol, want none", n)
	}
}

// watched is stdin that records whether anything read from it.
type watched struct {
	r    io.Reader
	read bool
}

func (w *watched) Read(p []byte) (int, error) {
	w.read = true
	return w.r.Read(p)
}

// userCreate runs user create with args and a password on stdin, and
// returns its code, its stderr and whether it read the password.
func userCreate(t *testing.T, o *bootstrap.Options, args ...string) (code int, stderr string, read bool) {
	t.Helper()
	stdin := &watched{r: strings.NewReader("pw\n")}
	var errb bytes.Buffer
	code = run(append([]string{"user", "create"}, args...), o, stdin, &errb)
	return code, errb.String(), stdin.read
}

// WriteFreely's admin is its first user, so user create --admin needs
// there to be none, and user create without it needs one. user create
// refuses either before it asks for the password, saying what to run in
// this image's own form.
func TestUserCreateChecksTheAdminFirst(t *testing.T) { //nolint:paralleltest // WriteFreely's package-level state
	o := served(t)
	code, stderr, read := userCreate(t, &o, "--admin", "dave")
	if code != 1 || !strings.Contains(stderr, `an admin already exists, "writer": create other users with user create <name>, without --admin`) {
		t.Errorf("user create --admin dave beside an admin = %d, stderr %q; want 1 and the image's refusal", code, stderr)
	}
	if read {
		t.Error("user create --admin dave beside an admin read the password before it refused")
	}
	if n := users(t, &o, "dave"); n != 0 {
		t.Errorf("%d users named dave, want none", n)
	}

	db, err := sql.Open("sqlite3", filepath.Join(o.Layout.Data, "writefreely.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), "DELETE FROM users WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	code, stderr, read = userCreate(t, &o, "bob")
	if code != 1 || !strings.Contains(stderr, "there is no admin yet: create one first with user create --admin <name>") {
		t.Errorf("user create bob with no admin = %d, stderr %q; want 1 and the image's refusal", code, stderr)
	}
	if read {
		t.Error("user create bob with no admin read the password before it refused")
	}
	if n := users(t, &o, "bob"); n != 0 {
		t.Errorf("%d users named bob, want none", n)
	}

	if code, stderr, _ = userCreate(t, &o, "--admin", "dave"); code != 0 {
		t.Errorf("user create --admin dave with no admin = %d, stderr %q; want 0", code, stderr)
	}
	if n := users(t, &o, "dave"); n != 1 {
		t.Errorf("%d users named dave, want 1", n)
	}
}

func TestUserCreateRefusesColonForm(t *testing.T) { //nolint:paralleltest // WriteFreely's package-level state
	o := served(t)
	var errb bytes.Buffer
	if code := run([]string{"user", "create", "bob:pw"}, &o, strings.NewReader("pw\n"), &errb); code != 2 {
		t.Errorf("user create bob:pw = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "usage: divbast-writefreely user create [--admin] <name> ") {
		t.Errorf("stderr = %q, want the usage", errb.String())
	}
	for _, name := range []string{"bob", "bobpw", "bob:pw"} {
		if n := users(t, &o, name); n != 0 {
			t.Errorf("%d users named %s, want none", n, name)
		}
	}
}
