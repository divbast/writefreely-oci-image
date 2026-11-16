// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package bootstrap_test

import (
	"bytes"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/writefreely/writefreely/migrations"

	"github.com/divbast/writefreely-oci-image/app/internal/bootstrap"
	"github.com/divbast/writefreely-oci-image/app/internal/conf"
	"github.com/divbast/writefreely-oci-image/app/internal/dbstate"
	"github.com/divbast/writefreely-oci-image/app/internal/keys"
)

// WriteFreely keeps package-level state, so no test here runs in parallel.

const (
	marker  = `created the admin "writer" with this one-time password:`
	mounted = "[app]\nhost = https://blog.example.com\nsingle_user = false\n"
)

var passwordLine = regexp.MustCompile(`(?m)^divbast-writefreely: {5}\S{26}$`)

// setup is a fresh layout and options for Run, writing a mounted
// config.ini and a secret only when given.
func setup(t *testing.T, ini, secret, user string) (bootstrap.Options, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	o := bootstrap.Options{
		Layout:    conf.Layout{Data: filepath.Join(dir, "data"), Assets: filepath.Join(dir, "assets")},
		Mounted:   filepath.Join(dir, "etc", "config.ini"),
		Secret:    filepath.Join(dir, "secrets", "writefreely_admin_password"),
		AdminUser: user,
	}
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Upstream's username check walks the pages tree, which the image
	// ships, and refuses every name when it is missing
	for _, d := range []string{o.Layout.Data, filepath.Join(o.Layout.Assets, "pages")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if ini != "" {
		write(o.Mounted, ini)
	}
	if secret != "" {
		write(o.Secret, secret)
	}
	out := &bytes.Buffer{}
	o.Out = out
	return o, out
}

// open opens the layout's SQLite database for a test's own queries.
func open(t *testing.T, l conf.Layout) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(l.Data, "writefreely.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRunTrial(t *testing.T) { //nolint:paralleltest // WriteFreely's package-level state
	o, out := setup(t, "", "", "")
	if _, err := bootstrap.Run(t.Context(), &o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n := strings.Count(out.String(), marker); n != 1 {
		t.Errorf("the marker appears %d times in %q, want once", n, out)
	}
	if !passwordLine.MatchString(out.String()) {
		t.Errorf("no password line in %q", out)
	}
	paths := make([]string, 0, 2+len(keys.Names))
	paths = append(paths, o.Layout.DataConfig(), filepath.Join(o.Layout.Data, "writefreely.db"))
	for _, name := range keys.Names {
		paths = append(paths, filepath.Join(o.Layout.KeysDir(), name))
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("after a trial Run: %v", err)
		}
	}

	out.Reset()
	if _, err := bootstrap.Run(t.Context(), &o); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("a second Run printed %q, want nothing", out)
	}
}

func TestRunSecretAdmin(t *testing.T) { //nolint:paralleltest // WriteFreely's package-level state
	o, out := setup(t, mounted, "s3cret-pass-word\n", "ada")
	if _, err := bootstrap.Run(t.Context(), &o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var name string
	if err := open(t, o.Layout).QueryRowContext(t.Context(), "SELECT username FROM users WHERE id = 1").Scan(&name); err != nil || name != "ada" {
		t.Errorf("user 1 = %q, %v; want ada", name, err)
	}
	if out.Len() != 0 {
		t.Errorf("Run printed %q, want nothing", out)
	}
}

func TestRunRefusals(t *testing.T) { //nolint:paralleltest // WriteFreely's package-level state
	for _, tc := range []struct { //nolint:paralleltest // WriteFreely's package-level state
		name, ini, secret, user, needle string
		dataMode                        os.FileMode
	}{
		{name: "user without secret", ini: mounted, user: "ada", needle: "go together"},
		{name: "secret without user", ini: mounted, secret: "pw\n", needle: "go together"},
		{name: "empty secret", ini: mounted, secret: "\n", user: "ada", needle: "is empty"},
		{name: "mounted, no admin", ini: mounted, needle: "no users and no admin to create"},
		{name: "reserved user", ini: mounted, secret: "a-long-password\n", user: "admin", needle: "invalid, reserved"},
		{name: "data not writable", dataMode: 0o555, needle: "is not writable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dataMode != 0 && os.Geteuid() == 0 {
				t.Skip("root writes a read-only directory anyway")
			}
			o, out := setup(t, tc.ini, tc.secret, tc.user)
			if tc.dataMode != 0 {
				if err := os.Chmod(o.Layout.Data, tc.dataMode); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(o.Layout.Data, 0o700) }) //nolint:gosec // G302: lets TempDir's cleanup remove it
			}
			_, err := bootstrap.Run(t.Context(), &o)
			if err == nil || !strings.Contains(err.Error(), tc.needle) {
				t.Fatalf("Run = %v, want an error containing %q", err, tc.needle)
			}
			if strings.Contains(out.String(), marker) {
				t.Errorf("a refused Run printed a password: %q", out)
			}
		})
	}
}

// v1 builds a database as WriteFreely's V1 schema left it, before every
// migration since.
func v1(t *testing.T, l conf.Layout) {
	t.Helper()
	dir, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Dir}}", "github.com/writefreely/writefreely").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	root, err := os.OpenRoot(strings.TrimSpace(string(dir)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	schema, err := root.ReadFile("sqlite.sql")
	if err != nil {
		t.Fatal(err)
	}
	db := open(t, l)
	for q := range strings.SplitSeq(string(schema), ";\n") {
		if strings.TrimSpace(q) == "" {
			continue
		}
		if _, err := db.ExecContext(t.Context(), q); err != nil {
			t.Fatalf("V1 schema: %v", err)
		}
	}
	if err := migrations.SetInitialMigrations(migrations.NewDatastore(db, "sqlite3")); err != nil {
		t.Fatal(err)
	}
}

func TestRunMigratesBehind(t *testing.T) { //nolint:paralleltest // WriteFreely's package-level state
	o, _ := setup(t, "", "", "")
	v1(t, o.Layout)
	if got, err := dbstate.Read(t.Context(), open(t, o.Layout), "sqlite3"); err != nil || got.Version != 1 {
		t.Fatalf("fixture state = %+v, %v; want version 1", got, err)
	}
	if _, err := bootstrap.Run(t.Context(), &o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, err := dbstate.Read(t.Context(), open(t, o.Layout), "sqlite3"); err != nil || got.Version != migrations.CurrentVer() {
		t.Errorf("after Run, state = %+v, %v; want version %d", got, err, migrations.CurrentVer())
	}
}

func TestRunRefusesAhead(t *testing.T) { //nolint:paralleltest // WriteFreely's package-level state
	o, _ := setup(t, "", "", "")
	if _, err := bootstrap.Run(t.Context(), &o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := open(t, o.Layout).ExecContext(t.Context(),
		"INSERT INTO appmigrations (version, migrated, result) VALUES (?, CURRENT_TIMESTAMP, '')", migrations.CurrentVer()+1); err != nil {
		t.Fatal(err)
	}
	_, err := bootstrap.Run(t.Context(), &o)
	if err == nil || !strings.Contains(err.Error(), "newer than this image's") {
		t.Fatalf("Run on a newer database = %v, want it refused", err)
	}
}
