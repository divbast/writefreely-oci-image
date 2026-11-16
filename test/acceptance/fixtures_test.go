// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/divbast/ci/harness"
)

// trialMarker opens the trial's block in the log; the password is on the
// line after it.
const trialMarker = `created the admin "writer" with this one-time password:`

// The image's mounted paths, as the README gives them.
const (
	configDir  = "/etc/writefreely"
	secretsDir = "/run/secrets"
	secretFile = "writefreely_admin_password"
	adminUser  = "WRITEFREELY_ADMIN_USER"
)

// dataVolume is a fresh named volume at /data, as a user mounts one.
func dataVolume(tb testing.TB, e harness.Engine) harness.Mount {
	tb.Helper()
	name, err := e.Volume(tb.Context(), tb, "")
	if err != nil {
		tb.Fatal(err)
	}
	return harness.Mount{Source: name, Target: "/data", Volume: true}
}

// serveWF serves the image under the contract with env and mounts, and
// waits until /login answers: it renders for any visitor, whatever the
// instance's configuration.
func serveWF(t *testing.T, e harness.Engine, image string, env map[string]string, mounts ...harness.Mount) *harness.Serving {
	t.Helper()
	s, err := e.Serve(t.Context(), t, &harness.ServeSpec{
		Run: harness.RunSpec{
			Image:     image,
			Confine:   harness.Contract(tmpfsSize),
			Env:       env,
			Mounts:    mounts,
			HealthCmd: healthCmd,
		},
		AppPort:    appPort,
		HealthPort: healthPort,
		ReadyPath:  "/login",
	})
	if err != nil {
		t.Fatalf("serving %s: %v", image, err)
	}
	return s
}

// hostDir is a fresh host directory holding files, the directory at mode
// and each file at fileMode, for a bind mount that uid 65532 reads, or
// writes, as the modes allow.
func hostDir(tb testing.TB, mode os.FileMode, files map[string]string, fileMode os.FileMode) string {
	tb.Helper()
	dir := tb.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	for name, body := range files {
		if err := root.WriteFile(name, []byte(body), fileMode); err != nil {
			tb.Fatal(err)
		}
		// WriteFile's mode passes through the umask; the container's user
		// needs exactly fileMode
		if err := root.Chmod(name, fileMode); err != nil {
			tb.Fatal(err)
		}
	}
	if err := os.Chmod(dir, mode); err != nil {
		tb.Fatal(err)
	}
	return dir
}

// configMount mounts ini as /etc/writefreely/config.ini, read-only, or
// writable by the container's user. The directory is what is mounted,
// since the harness binds host directories.
func configMount(tb testing.TB, ini string, writable bool) harness.Mount {
	tb.Helper()
	dirMode, fileMode := os.FileMode(0o755), os.FileMode(0o644)
	if writable {
		dirMode, fileMode = 0o777, 0o666
	}
	return harness.Mount{
		Source:   hostDir(tb, dirMode, map[string]string{"config.ini": ini}, fileMode),
		Target:   configDir,
		ReadOnly: !writable,
	}
}

// secretMount mounts password as the admin's secret, read-only. It always
// ends the file with a newline, as echo writes one and as Docker and
// Podman secrets made from it keep it, so every test that logs in with the
// bare password proves the newline is trimmed.
func secretMount(tb testing.TB, password string) harness.Mount {
	tb.Helper()
	return harness.Mount{
		Source:   hostDir(tb, 0o755, map[string]string{secretFile: password + "\n"}, 0o644),
		Target:   secretsDir,
		ReadOnly: true,
	}
}

// trialPassword is the one-time password the trial printed.
func trialPassword(t *testing.T, s *harness.Serving) string {
	t.Helper()
	log, err := s.C.Logs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(log, trialMarker+"\n")
	line, _, _ := strings.Cut(after, "\n")
	password := strings.TrimSpace(strings.TrimPrefix(line, "divbast-writefreely:"))
	if !ok || password == "" {
		t.Fatalf("no trial password in the log:\n%s", log)
	}
	return password
}

// webLogin logs user in through the login form, as a browser does, and
// returns a client holding the session's cookie. WriteFreely answers a
// login with a 302 to the user's next page, and a failed one with a 307
// back to /login.
func webLogin(t *testing.T, s *harness.Serving, user, pass string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar:           jar,
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	form := url.Values{"alias": {user}, "pass": {pass}}.Encode()
	resp, err := s.Do(t.Context(), client, http.MethodPost, "/auth/login",
		http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, []byte(form))
	harness.Answers(t, "logging in as "+user+" redirects onwards", http.StatusFound, resp, err)
	if err == nil {
		harness.Lacks(t, "logging in as "+user+" does not go back to the login page", "/login", resp.Header.Get("Location"))
	}
	return client
}
