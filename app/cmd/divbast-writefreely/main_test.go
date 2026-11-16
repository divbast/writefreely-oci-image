// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string // a substring of stdout; empty wants none
		wantErr  string // a substring of stderr; empty wants none
	}{
		{name: "version", args: []string{"version"}, wantCode: 0, wantOut: "WriteFreely 0.17.3"},
		{name: "none", args: nil, wantCode: 2, wantErr: "usage: divbast-writefreely"},
		{name: "unknown", args: []string{"nope"}, wantCode: 2, wantErr: "usage: divbast-writefreely"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out, errb bytes.Buffer
			if code := run(tc.args, &out, &errb); code != tc.wantCode {
				t.Errorf("run(%q) = %d, want %d; stderr %q", tc.args, code, tc.wantCode, errb.String())
			}
			checkStream(t, "stdout", out.String(), tc.wantOut)
			checkStream(t, "stderr", errb.String(), tc.wantErr)
		})
	}
}

// checkStream wants got to contain want, or to be empty when want is.
func checkStream(t *testing.T, name, got, want string) {
	t.Helper()
	switch {
	case want == "" && got != "":
		t.Errorf("%s = %q, want nothing", name, got)
	case !strings.Contains(got, want):
		t.Errorf("%s = %q, want it to contain %q", name, got, want)
	}
}
