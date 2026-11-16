// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"testing"

	"github.com/divbast/ci/harness"
)

// hostINI is a config.ini with a usable host, which every accepted one
// has, followed by extra.
func hostINI(extra string) string {
	return "[app]\nhost = http://localhost:8080\n" + extra
}

// TestRefusals checks every start the image refuses: each exits 1 before
// serving, with one line naming what to change, never a stack trace or
// SQLite's own error.
func TestRefusals(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	type mounts func(t *testing.T) []harness.Mount
	config := func(ini string) mounts {
		return func(t *testing.T) []harness.Mount {
			t.Helper()
			return []harness.Mount{dataVolume(t, e), configMount(t, ini, false)}
		}
	}
	withSecret := func(ini string) mounts {
		return func(t *testing.T) []harness.Mount {
			t.Helper()
			return []harness.Mount{dataVolume(t, e), configMount(t, ini, false), secretMount(t, contractPassword)}
		}
	}
	ada := map[string]string{adminUser: "ada"}
	for _, tc := range []struct {
		name    string
		mounts  mounts
		env     map[string]string
		needles []string
	}{
		{name: "bind", mounts: config(hostINI("[server]\nbind = localhost\n")), needles: []string{"[server] bind = localhost"}},
		{name: "port", mounts: config(hostINI("[server]\nport = 80\n")), needles: []string{"[server] port = 80"}},
		{name: "templates dir", mounts: config(hostINI("[server]\ntemplates_parent_dir = /srv\n")), needles: []string{"[server] templates_parent_dir"}},
		{name: "keys dir", mounts: config(hostINI("[server]\nkeys_parent_dir = /srv\n")), needles: []string{"[server] keys_parent_dir"}},
		{name: "sqlite file", mounts: config(hostINI("[database]\ntype = sqlite3\nfilename = writefreely.db\n")), needles: []string{"[database] filename"}},
		// Upstream's own config.ini, converted: every conflict at once
		{name: "upstream-style config", mounts: config(hostINI("[server]\nbind = localhost\n[database]\ntype = sqlite3\nfilename = writefreely.db\n")), needles: []string{"[server] bind", "[database] filename"}},
		{name: "no host", mounts: config("[app]\nsingle_user = false\n"), needles: []string{"[app] host must be the blog's public URL"}},
		{name: "tls", mounts: config(hostINI("[server]\ntls_cert_path = /x\n")), needles: []string{"TLS is a reverse proxy's job"}},
		{name: "autocert", mounts: config(hostINI("[server]\nautocert = true\n")), needles: []string{"[server] autocert"}},
		{name: "gopher low", mounts: config(hostINI("[server]\ngopher_port = 70\n")), needles: []string{"below 1024"}},
		{name: "gopher clash", mounts: config(hostINI("[server]\ngopher_port = 8081\n")), needles: []string{"gopher_port = 8081"}},
		{name: "user without secret", mounts: config(configuredINI), env: ada, needles: []string{"go together"}},
		{name: "secret without user", mounts: withSecret(configuredINI), needles: []string{"go together"}},
		{name: "no admin", mounts: config(configuredINI), needles: []string{"no users and no admin to create"}},
		{name: "reserved user", mounts: withSecret(configuredINI), env: map[string]string{adminUser: "admin"}, needles: []string{"invalid, reserved"}},
		// No volume on a read-only root
		{name: "no volume", mounts: func(*testing.T) []harness.Mount { return nil }, needles: []string{"/data is not writable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := harness.RunSpec{
				Image:   image,
				Confine: harness.Contract(tmpfsSize),
				Network: "none",
				Env:     tc.env,
				Mounts:  tc.mounts(t),
			}
			for _, needle := range tc.needles {
				harness.Refuses(t, e, &spec, 1, tc.name, needle)
			}
		})
	}
}
