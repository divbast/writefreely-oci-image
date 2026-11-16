// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package conf_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/writefreely/writefreely/config"

	"github.com/divbast/writefreely-oci-image/app/internal/conf"
)

const file = "/etc/writefreely/config.ini"

// withHost is an otherwise empty configuration with a usable host: every
// configuration Enforce accepts has one.
func withHost() *config.Config {
	return &config.Config{App: config.AppCfg{Host: "https://blog.example.com"}}
}

// testLayout is a layout under a fresh temporary directory.
func testLayout(t *testing.T) conf.Layout {
	t.Helper()
	dir := t.TempDir()
	return conf.Layout{Data: filepath.Join(dir, "data"), Assets: filepath.Join(dir, "assets")}
}

func TestEnforceFillsUnset(t *testing.T) {
	t.Parallel()
	l := conf.Image
	cfg := withHost()
	if err := conf.Enforce(cfg, file, l); err != nil {
		t.Fatalf("Enforce(empty) = %v, want nil", err)
	}
	for _, c := range []struct{ key, got, want string }{
		{"bind", cfg.Server.Bind, "0.0.0.0"},
		{"templates_parent_dir", cfg.Server.TemplatesParentDir, l.Assets},
		{"static_parent_dir", cfg.Server.StaticParentDir, l.Assets},
		{"pages_parent_dir", cfg.Server.PagesParentDir, l.Assets},
		{"keys_parent_dir", cfg.Server.KeysParentDir, l.Data},
		{"type", cfg.Database.Type, "sqlite3"},
		{"filename", cfg.Database.FileName, l.Data + "/writefreely.db"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.key, c.got, c.want)
		}
	}
	if cfg.Server.Port != conf.AppPort {
		t.Errorf("port = %d, want %d", cfg.Server.Port, conf.AppPort)
	}
}

func TestEnforce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		set    func(*config.Config)
		needle string // in the error; empty wants none
	}{
		{"image values", func(c *config.Config) {
			c.Server.Bind, c.Server.Port, c.Server.KeysParentDir = "0.0.0.0", 8080, "/data"
			c.Server.TemplatesParentDir = "/usr/local/share/writefreely"
			c.Database.Type, c.Database.FileName = "sqlite3", "/data/writefreely.db"
		}, ""},
		{"bind", func(c *config.Config) { c.Server.Bind = "localhost" }, file + ": [server] bind = localhost: the image sets this to 0.0.0.0; remove the line"},
		{"unix socket", func(c *config.Config) { c.Server.Bind = "/run/wf.sock" }, "[server] bind = /run/wf.sock"},
		{"port", func(c *config.Config) { c.Server.Port = 80 }, file + ": [server] port = 80: the image sets this to 8080; remove the line"},
		{"templates", func(c *config.Config) { c.Server.TemplatesParentDir = "/srv" }, "[server] templates_parent_dir = /srv: the image sets this to /usr/local/share/writefreely"},
		{"static", func(c *config.Config) { c.Server.StaticParentDir = "/srv" }, "[server] static_parent_dir = /srv"},
		{"pages", func(c *config.Config) { c.Server.PagesParentDir = "/srv" }, "[server] pages_parent_dir = /srv"},
		{"keys", func(c *config.Config) { c.Server.KeysParentDir = "/srv" }, "[server] keys_parent_dir = /srv: the image sets this to /data"},
		{"sqlite file", func(c *config.Config) { c.Database.Type, c.Database.FileName = "sqlite3", "writefreely.db" }, "[database] filename = writefreely.db: the image sets this to /data/writefreely.db"},
		{"tls cert", func(c *config.Config) { c.Server.TLSCertPath = "/x" }, file + ": [server] tls_cert_path is set: TLS is a reverse proxy's job"},
		{"tls key", func(c *config.Config) { c.Server.TLSKeyPath = "/x" }, "[server] tls_key_path is set: TLS is a reverse proxy's job"},
		{"autocert", func(c *config.Config) { c.Server.Autocert = true }, "[server] autocert is set: TLS is a reverse proxy's job"},
		{"gopher low", func(c *config.Config) { c.Server.GopherPort = 70 }, file + ": [server] gopher_port = 70: below 1024, which needs a capability the image never has"},
		{"gopher on app", func(c *config.Config) { c.Server.GopherPort = 8080 }, "[server] gopher_port = 8080: the image's own port"},
		{"gopher on health", func(c *config.Config) { c.Server.GopherPort = 8081 }, "[server] gopher_port = 8081: the image's own port"},
		{"gopher high", func(c *config.Config) { c.Server.GopherPort = 7070 }, ""},
		{"mysql keeps its filename", func(c *config.Config) { c.Database.Type, c.Database.FileName = "mysql", "x" }, ""},
		{"no host", func(c *config.Config) { c.App.Host = "" }, file + ": [app] host must be the blog's public URL with its scheme, such as https://blog.example.com"},
		{"host as config.Load leaves a bare name", func(c *config.Config) { c.App.Host = "://" }, "[app] host must be the blog's public URL"},
		{"host without a scheme", func(c *config.Config) { c.App.Host = "blog.example.com" }, "[app] host must be the blog's public URL"},
		{"host with a port", func(c *config.Config) { c.App.Host = "http://localhost:8080" }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := withHost()
			tc.set(cfg)
			before := *cfg
			err := conf.Enforce(cfg, file, conf.Image)
			switch {
			case tc.needle == "" && err != nil:
				t.Fatalf("Enforce = %v, want nil", err)
			case tc.needle != "" && err == nil:
				t.Fatalf("Enforce = nil, want an error containing %q", tc.needle)
			case tc.needle != "" && !strings.Contains(err.Error(), tc.needle):
				t.Fatalf("Enforce = %q, want it to contain %q", err, tc.needle)
			}
			if before.Database.Type == "mysql" && cfg.Database.FileName != before.Database.FileName {
				t.Errorf("mysql filename = %q, want %q kept", cfg.Database.FileName, before.Database.FileName)
			}
			if tc.name == "gopher high" && cfg.Server.GopherPort != 7070 {
				t.Errorf("gopher_port = %d, want 7070 kept", cfg.Server.GopherPort)
			}
		})
	}
}

// An operator converting upstream's own config.ini meets every conflict in
// one refusal, a line each, and fixes them in one pass.
func TestEnforceNamesEveryConflict(t *testing.T) {
	t.Parallel()
	cfg := config.New()
	cfg.UseSQLite(true)
	err := conf.Enforce(cfg, file, conf.Image)
	if err == nil {
		t.Fatal("Enforce(upstream's defaults) = nil, want the conflicts")
	}
	lines := strings.Split(err.Error(), "\n")
	for _, needle := range []string{"[server] bind = localhost", "[database] filename = writefreely.db"} {
		n := 0
		for _, line := range lines {
			if strings.Contains(line, needle) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%d lines of %q name %q, want exactly 1", n, err, needle)
		}
	}
	if len(lines) != 2 {
		t.Errorf("Enforce gave %d lines, want 2: %q", len(lines), err)
	}
}

func TestDefaultsEnforced(t *testing.T) {
	t.Parallel()
	l := testLayout(t)
	cfg := conf.Defaults(l)
	if err := conf.Enforce(cfg, "defaults", l); err != nil {
		t.Fatalf("Enforce(Defaults) = %v, want nil", err)
	}
	if cfg.Database.Type != "sqlite3" || cfg.Database.FileName != l.Data+"/writefreely.db" {
		t.Errorf("database = %q %q, want sqlite3 at %s/writefreely.db", cfg.Database.Type, cfg.Database.FileName, l.Data)
	}
}

func TestWorkingMounted(t *testing.T) {
	t.Parallel()
	l := testLayout(t)
	mounted := filepath.Join(t.TempDir(), "config.ini")
	if err := os.WriteFile(mounted, []byte("[app]\nsite_name = x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, isMounted, err := conf.Working(l, mounted)
	if err != nil || path != mounted || !isMounted {
		t.Fatalf("Working = %q, %t, %v; want %q, true, nil", path, isMounted, err, mounted)
	}
	if _, err := os.Stat(l.DataConfig()); !os.IsNotExist(err) {
		t.Errorf("Working wrote %s beside a mounted file (stat: %v)", l.DataConfig(), err)
	}
}

func TestWorkingCreates(t *testing.T) {
	t.Parallel()
	l := testLayout(t)
	if err := os.MkdirAll(l.Data, 0o700); err != nil {
		t.Fatal(err)
	}
	path, isMounted, err := conf.Working(l, filepath.Join(t.TempDir(), "absent.ini"))
	if err != nil || path != l.DataConfig() || isMounted {
		t.Fatalf("Working = %q, %t, %v; want %q, false, nil", path, isMounted, err, l.DataConfig())
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(%s): %v", path, err)
	}
	if !cfg.App.SingleUser || !cfg.App.Federation || cfg.Database.Type != "sqlite3" || cfg.App.Host != "http://localhost:8080" {
		t.Errorf("created config = single_user %t, federation %t, type %q, host %q; want upstream's defaults on SQLite",
			cfg.App.SingleUser, cfg.App.Federation, cfg.Database.Type, cfg.App.Host)
	}
}

func TestWorkingKeepsExisting(t *testing.T) {
	t.Parallel()
	l := testLayout(t)
	if err := os.MkdirAll(l.Data, 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("[app]\nsite_name = kept\n")
	if err := os.WriteFile(l.DataConfig(), want, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(l.DataConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, werr := conf.Working(l, filepath.Join(t.TempDir(), "absent.ini")); werr != nil {
		t.Fatalf("Working: %v", werr)
	}
	after, err := os.Stat(l.DataConfig())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(l.DataConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("Working rewrote %s: now %q, mtime %v (was %v)", l.DataConfig(), got, after.ModTime(), before.ModTime())
	}
}
