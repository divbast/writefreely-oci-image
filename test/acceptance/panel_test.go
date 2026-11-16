// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"testing"

	"github.com/divbast/ci/harness"
)

// panelSiteName is the setting the admin panel saves.
const panelSiteName = "divbast panel check"

// imagePort is the port line the image writes into a saved config.ini;
// go-ini pads keys to line up the equals signs.
var imagePort = regexp.MustCompile(`(?m)^port\s*=\s*8080$`)

// panelForm is the admin panel's whole settings form, as a browser posts
// it, with a new site name.
var panelForm = url.Values{
	"site_name": {panelSiteName}, "site_desc": {""}, "landing": {""},
	"open_registration": {""}, "open_deletion": {""}, "min_username_len": {"3"},
	"max_blogs": {"1"}, "federation": {"on"}, "public_stats": {"on"},
	"monetization": {""}, "private": {""}, "local_timeline": {""},
	"user_invites": {"none"}, "default_visibility": {""},
}.Encode()

// savePanel posts panelForm as the logged-in client, and returns where
// WriteFreely sent it: the settings page, with the save's result in cm.
func savePanel(t *testing.T, s *harness.Serving, client *http.Client) string {
	t.Helper()
	resp, err := s.Do(t.Context(), client, http.MethodPost, "/admin/update/config",
		http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, []byte(panelForm))
	harness.Answers(t, "the settings form answers 302", http.StatusFound, resp, err)
	return resp.Header.Get("Location")
}

// hostFile reads name in the host directory dir.
func hostFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestAdminPanelSaves checks the admin panel's settings in each way the
// configuration can be given: with none mounted they persist on /data,
// with a writable config.ini WriteFreely rewrites it, and with a read-only
// one the save fails and the panel says so.
func TestAdminPanelSaves(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)

	t.Run("no config", func(t *testing.T) {
		t.Parallel()
		s := serveWF(t, e, image, nil, dataVolume(t, e))
		where := savePanel(t, s, webLogin(t, s, "writer", trialPassword(t, s)))
		harness.Contains(t, "the panel says the configuration saved", "Configuration+saved", where)
		if err := s.Restart(t.Context()); err != nil {
			t.Fatalf("restart: %v", err)
		}
		resp, err := s.Get(t.Context(), "/login", nil)
		harness.Answers(t, "GET /login after a restart answers 200", http.StatusOK, resp, err)
		if err == nil {
			harness.Contains(t, "after a restart, the saved site name is in force", panelSiteName, string(resp.Body))
		}
	})

	t.Run("read-write", func(t *testing.T) {
		t.Parallel()
		config := configMount(t, configuredINI, true)
		s := serveWF(t, e, image, map[string]string{adminUser: "ada"},
			dataVolume(t, e), config, secretMount(t, cliAdminPassword))
		where := savePanel(t, s, webLogin(t, s, "ada", cliAdminPassword))
		harness.Contains(t, "the panel says the configuration saved", "Configuration+saved", where)
		saved := string(hostFile(t, config.Source, "config.ini"))
		harness.Contains(t, "the mounted config.ini holds the saved site name", panelSiteName, saved)
		checks := harness.NewChecks(t)
		checks.True("the mounted config.ini holds the image's port", imagePort.MatchString(saved), harness.Clip(saved))
	})

	t.Run("read-only", func(t *testing.T) {
		t.Parallel()
		config := configMount(t, configuredINI, false)
		s := serveWF(t, e, image, map[string]string{adminUser: "ada"},
			dataVolume(t, e), config, secretMount(t, cliAdminPassword))
		where := savePanel(t, s, webLogin(t, s, "ada", cliAdminPassword))
		harness.Contains(t, "the panel reports the save's result", "?cm=", where)
		harness.Lacks(t, "the panel does not say the configuration saved", "Configuration+saved", where)
		checks := harness.NewChecks(t)
		after := hostFile(t, config.Source, "config.ini")
		checks.True("the read-only config.ini is unchanged", bytes.Equal(after, []byte(configuredINI)), harness.Clip(string(after)))
	})
}
