// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package wfapp_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-ini/ini"
	"github.com/writefreely/writefreely"
	"github.com/writefreely/writefreely/config"

	"github.com/divbast/writefreely-oci-image/app/internal/conf"
	"github.com/divbast/writefreely-oci-image/app/internal/wfapp"
)

var _ writefreely.Apper = (*wfapp.Apper)(nil)

// base is the smallest config.ini the image accepts, with a setting of
// the operator's own.
const base = "[app]\nhost = https://blog.example.com\nsite_name = x\n"

// fixture writes body to a config.ini in a fresh directory and returns its
// path and a layout beside it.
func fixture(t *testing.T, body string) (string, conf.Layout) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "config.ini")
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return file, conf.Layout{Data: filepath.Join(dir, "data"), Assets: filepath.Join(dir, "assets")}
}

// readFile returns the file at path, through an os.Root on its directory.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLoadConfigEnforces(t *testing.T) {
	t.Parallel()
	file, l := fixture(t, base)
	a := wfapp.New(file, l)
	if err := a.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg := a.App().Config()
	if cfg.Server.Port != conf.AppPort || cfg.App.SiteName != "x" || cfg.Server.KeysParentDir != l.Data {
		t.Errorf("loaded port %d, site_name %q, keys_parent_dir %q; want %d, x, %s",
			cfg.Server.Port, cfg.App.SiteName, cfg.Server.KeysParentDir, conf.AppPort, l.Data)
	}
}

func TestLoadConfigRefuses(t *testing.T) {
	t.Parallel()
	file, l := fixture(t, base+"[server]\nport = 80\n")
	err := wfapp.New(file, l).LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "[server] port = 80") {
		t.Fatalf("LoadConfig = %v, want the port refused", err)
	}
}

func TestSaveConfigWritesImageValues(t *testing.T) {
	t.Parallel()
	file, l := fixture(t, base)
	a := wfapp.New(file, l)
	if err := a.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	a.App().Config().App.SiteName = "saved"
	if err := a.SaveConfig(a.App().Config()); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	cfg, err := config.Load(file)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.App.SiteName != "saved" || cfg.Server.Port != conf.AppPort {
		t.Errorf("saved site_name %q, port %d; want saved, %d", cfg.App.SiteName, cfg.Server.Port, conf.AppPort)
	}
}

func TestSaveConfigRefusesConflicts(t *testing.T) {
	t.Parallel()
	file, l := fixture(t, base)
	before := readFile(t, file)
	bad := config.New()
	if err := wfapp.New(file, l).SaveConfig(bad); err == nil || !strings.Contains(err.Error(), "[server] bind = localhost") {
		t.Fatalf("SaveConfig(bind localhost) = %v, want it refused", err)
	}
	if after := readFile(t, file); !bytes.Equal(after, before) {
		t.Errorf("a refused save rewrote the file: %q", after)
	}
}

func TestSaveConfigReadOnly(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only file anyway")
	}
	file, l := fixture(t, base)
	if err := os.Chmod(file, 0o400); err != nil {
		t.Fatal(err)
	}
	a := wfapp.New(file, l)
	if err := a.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	a.App().Config().App.SiteName = "saved"
	if err := a.SaveConfig(a.App().Config()); err == nil {
		t.Fatal("SaveConfig on a read-only file = nil, want an error")
	}
	if got := readFile(t, file); string(got) != base {
		t.Errorf("read-only file now %q", got)
	}
}

// A secret given through the environment runs the server but is never
// written into the working file: an admin-panel save keeps the file's own
// value, so the secret stays wherever the operator put it.
func TestSaveConfigKeepsEnvSecretsOut(t *testing.T) {
	t.Setenv("WRITEFREELY_SMTP_PASSWORD", "env")
	file, l := fixture(t, base+"[email]\nsmtp_password = file\n")
	a := wfapp.New(file, l)
	if err := a.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := a.App().Config().Email.Password; got != "env" {
		t.Fatalf("loaded smtp_password %q, want env: the variable takes over the file's value", got)
	}
	a.App().Config().App.SiteName = "saved"
	if err := a.SaveConfig(a.App().Config()); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	saved, err := ini.Load(readFile(t, file))
	if err != nil {
		t.Fatalf("reading the saved file: %v", err)
	}
	site, pass := saved.Section("app").Key("site_name").String(), saved.Section("email").Key("smtp_password").String()
	if site != "saved" || pass != "file" {
		t.Errorf("saved site_name %q, smtp_password %q; want saved, file", site, pass)
	}
	if got := a.App().Config().Email.Password; got != "env" {
		t.Errorf("after a save the server runs with smtp_password %q, want env", got)
	}
}

// A read-write mount is the operator's file, often not the container's:
// upstream's config.Save would chmod it to 0600 after writing, which fails
// for a file uid 65532 does not own, so a save never touches the mode.
func TestSaveConfigKeepsMode(t *testing.T) {
	t.Parallel()
	file, l := fixture(t, base)
	if err := os.Chmod(file, 0o640); err != nil { //nolint:gosec // G302: an operator's file is often group-readable, and a save must keep that
		t.Fatal(err)
	}
	a := wfapp.New(file, l)
	if err := a.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if err := a.SaveConfig(a.App().Config()); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode after a save = %v, want 0640 kept", info.Mode().Perm())
	}
}
