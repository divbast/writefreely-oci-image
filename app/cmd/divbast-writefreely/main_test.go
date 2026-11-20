// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package main

import (
	"bytes"
	"errors"
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
		{name: "user create without credentials", args: []string{"user", "create"}, wantCode: 2, wantErr: []string{"usage: divbast-writefreely user"}},
		// Before the first serve there is no working file, and user must
		// not make one in the wrong place
		{name: "user before serve", args: []string{"user", "create", "a:b"}, wantCode: 1, wantErr: []string{"no working config yet: start the server once with serve first"}},
		// Nothing listens on either port in a test
		{name: "health", args: []string{"health"}, wantCode: 1, wantErr: []string{"healthz: ", "app: "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out, errb bytes.Buffer
			o := options(t, &out)
			if code := run(tc.args, &o, &errb); code != tc.wantCode {
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
