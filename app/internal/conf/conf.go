// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Package conf holds the image's paths and the config.ini keys the image
// owns: WriteFreely's configuration is the operator's, but where it listens
// and where its files live are the image's.
package conf

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/writefreely/writefreely/config"
)

// Layout is where the image keeps WriteFreely's files: Data, the volume,
// and Assets, upstream's templates, pages and static trees.
type Layout struct{ Data, Assets string }

// Image is the layout in the image. Tests use their own.
var Image = Layout{Data: "/data", Assets: "/usr/local/share/writefreely"}

// The image's fixed paths, names and ports.
const (
	// MountedPath is the operator's configuration, when mounted.
	MountedPath = "/etc/writefreely/config.ini"
	// SecretPath holds the first admin's password, when mounted.
	SecretPath = "/run/secrets/writefreely_admin_password" //nolint:gosec // G101: the path to the secret's file, not a credential
	// AdminUserEnv names the first admin, beside SecretPath.
	AdminUserEnv = "WRITEFREELY_ADMIN_USER"
	// TrialUser is the admin a trial creates: upstream reserves "admin".
	TrialUser = "writer"
	// AppPort serves WriteFreely.
	AppPort = 8080
	// HealthPort serves /healthz alone.
	HealthPort = 8081
)

const sqlite = "sqlite3"

// DataConfig is the working file when none is mounted.
func (l Layout) DataConfig() string { return filepath.Join(l.Data, "config.ini") }

// KeysDir holds WriteFreely's key files: keys_parent_dir is Data, and
// upstream adds keys/ to it.
func (l Layout) KeysDir() string { return filepath.Join(l.Data, "keys") }

// Defaults is upstream's own new configuration on SQLite, with the
// image-owned keys at the image's values.
func Defaults(l Layout) *config.Config {
	cfg := config.New()
	cfg.UseSQLite(true)
	// Upstream's defaults for these, localhost and a relative file, are
	// exactly what Enforce refuses; unset, Enforce fills them
	cfg.Server.Bind, cfg.Server.Port, cfg.Database.FileName = "", 0, ""
	if err := Enforce(cfg, "defaults", l); err != nil {
		panic("conf: the image's defaults conflict with its own keys: " + err.Error())
	}
	return cfg
}

// Working returns the working file: mounted when it exists, and otherwise
// l.DataConfig(), written from Defaults when absent. mounted's directory
// existing without it, as a directory mounted with a misnamed file, is
// refused: the image does not create that directory, so an operator
// mounted it, and taking the trial instead would ignore their
// configuration without a word.
func Working(l Layout, mounted string) (path string, isMounted bool, err error) {
	switch _, err := os.Stat(mounted); {
	case err == nil:
		return mounted, true, nil
	case !errors.Is(err, fs.ErrNotExist):
		return "", false, err
	}
	dir := filepath.Dir(mounted)
	switch _, err := os.Stat(dir); {
	case err == nil:
		return "", false, fmt.Errorf("%s holds no %s: mount the directory with your %s in it, or mount nothing there", dir, filepath.Base(mounted), filepath.Base(mounted))
	case !errors.Is(err, fs.ErrNotExist):
		return "", false, err
	}
	path = l.DataConfig()
	switch _, err := os.Stat(path); {
	case err == nil:
		return path, false, nil
	case !errors.Is(err, fs.ErrNotExist):
		return "", false, err
	}
	if err := config.Save(Defaults(l), path); err != nil {
		return "", false, fmt.Errorf("writing the default configuration: %w", err)
	}
	return path, false, nil
}

// Enforce fills each unset image-owned key with the image's value, and
// refuses any other value, a host that is not a URL, and anything else the
// image cannot honour: one error per conflict, each naming file and what
// to change.
func Enforce(cfg *config.Config, file string, l Layout) error {
	var errs []error
	// config.Load turns a host without a scheme, or none, into "://", and
	// a save writes that back, after which the file never loads again
	if u, err := url.Parse(cfg.App.Host); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("%s: [app] host must be the blog's public URL with its scheme, such as https://blog.example.com", file))
	}
	s := &cfg.Server
	own(&errs, file, "server", "bind", &s.Bind, "0.0.0.0")
	own(&errs, file, "server", "port", &s.Port, AppPort)
	own(&errs, file, "server", "templates_parent_dir", &s.TemplatesParentDir, l.Assets)
	own(&errs, file, "server", "static_parent_dir", &s.StaticParentDir, l.Assets)
	own(&errs, file, "server", "pages_parent_dir", &s.PagesParentDir, l.Assets)
	own(&errs, file, "server", "keys_parent_dir", &s.KeysParentDir, l.Data)
	for _, tls := range []struct {
		key string
		set bool
	}{{"tls_cert_path", s.TLSCertPath != ""}, {"tls_key_path", s.TLSKeyPath != ""}, {"autocert", s.Autocert}} {
		if tls.set {
			errs = append(errs, fmt.Errorf("%s: [server] %s is set: TLS is a reverse proxy's job", file, tls.key))
		}
	}
	switch g := s.GopherPort; {
	case g == 0:
	case g < 1024:
		errs = append(errs, fmt.Errorf("%s: [server] gopher_port = %d: below 1024, which needs a capability the image never has", file, g))
	case g == AppPort || g == HealthPort:
		errs = append(errs, fmt.Errorf("%s: [server] gopher_port = %d: the image's own port; choose another, at 1024 or above", file, g))
	}
	d := &cfg.Database
	if d.Type == "" {
		d.Type = sqlite
	}
	if d.Type == sqlite {
		own(&errs, file, "database", "filename", &d.FileName, filepath.Join(l.Data, "writefreely.db"))
	}
	return errors.Join(errs...)
}

// own sets an unset *value to image, and adds a conflict to errs for any
// value but image.
func own[T comparable](errs *[]error, file, section, key string, value *T, image T) {
	var unset T
	switch *value {
	case unset:
		*value = image
	case image:
	default:
		*errs = append(*errs, fmt.Errorf("%s: [%s] %s = %v: the image sets this to %v; remove the line", file, section, key, *value, image))
	}
}
