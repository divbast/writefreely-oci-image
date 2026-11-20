// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"maps"
	"strings"
	"testing"
	"time"

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
		// A configuration directory mounted with a misnamed file: never
		// taken for the trial
		{name: "config.ini missing", mounts: func(t *testing.T) []harness.Mount {
			t.Helper()
			return []harness.Mount{dataVolume(t, e), {
				Source:   hostDir(t, 0o755, map[string]string{"writefreely.ini": configuredINI}, 0o644),
				Target:   configDir,
				ReadOnly: true,
			}}
		}, needles: []string{configDir + " holds no config.ini"}},
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

// firstStart is a first start of image, with the admin ada and env, on a
// fresh volume, under a read-only multi-user config.ini, with the secret
// file holding secret.
func firstStart(t *testing.T, e harness.Engine, image string, env map[string]string, secret string) *harness.RunSpec {
	t.Helper()
	runEnv := map[string]string{adminUser: "ada"}
	maps.Copy(runEnv, env)
	return &harness.RunSpec{
		Image:   image,
		Confine: harness.Contract(tmpfsSize),
		Network: "none",
		Env:     runEnv,
		Mounts:  []harness.Mount{dataVolume(t, e), configMount(t, configuredINI, false), secretMount(t, secret)},
	}
}

// TestRefusesTwoAdminPasswordSources gives the first admin's password both
// in WRITEFREELY_ADMIN_PASSWORD and in the secret file: the start is
// refused, naming both, rather than one taken over the other.
func TestRefusesTwoAdminPasswordSources(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	spec := firstStart(t, e, image, map[string]string{adminPassword: cliAdminPassword}, cliAdminPassword)
	harness.Refuses(t, e, spec, 1, "two password sources", adminPassword+" and "+secretsDir+"/"+secretFile)
}

// TestRefusesLongAdminPassword gives the first admin a 73-byte password,
// one past bcrypt's limit, in the secret file: the start is refused,
// naming the limit, before any table of the schema is made, which would
// leave a database with no admin.
func TestRefusesLongAdminPassword(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	c, err := e.Start(t.Context(), t, firstStart(t, e, image, nil, strings.Repeat("p", 73)))
	if err != nil {
		t.Fatalf("starting %s: %v", image, err)
	}
	if exitsOne(t, c, "a 73-byte password", harness.ServeWithin, "at most 72") {
		logLacks(t, c, "a 73-byte password is refused before the schema is made", "Creating table")
	}
}

// exitsOne checks that c stops within the time given, with exit status
// 1, and that its log holds needle, each check named for what: what makes
// it stop. It reports whether c stopped at all, its log saying why when it
// did not.
func exitsOne(t *testing.T, c *harness.Container, what string, within time.Duration, needle string) bool {
	t.Helper()
	ctx := t.Context()
	checks := harness.NewChecks(t)
	code, err := c.WaitExit(ctx, within)
	if err != nil {
		checks.True(what+": the container stops within "+within.String(), false, harness.WithLog(ctx, c, err).Error())
		return false
	}
	harness.Equal(checks, what+": the container exits with status 1", 1, code)
	harness.LogContains(t, c, what+": the log says '"+needle+"'", needle)
	return true
}
