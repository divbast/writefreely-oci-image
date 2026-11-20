// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Package bootstrap takes whatever /data holds to a WriteFreely ready to
// serve: the working config.ini, the key files, the schema at the image's
// migration, and the first admin.
package bootstrap

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/writefreely/writefreely"
	"github.com/writefreely/writefreely/migrations"

	"github.com/divbast/writefreely-oci-image/app/internal/conf"
	"github.com/divbast/writefreely-oci-image/app/internal/dbstate"
	"github.com/divbast/writefreely-oci-image/app/internal/keys"
	"github.com/divbast/writefreely-oci-image/app/internal/wfapp"
)

// Options are the paths and the environment Run reads.
type Options struct {
	Layout       conf.Layout
	Mounted      string    // the operator's config.ini, when it exists
	Secret       string    // a file holding the first admin's password, when it exists
	Password     string    // the first admin's password, from conf.AdminPasswordEnv
	PasswordFile string    // a file holding it, from conf.AdminPasswordFileEnv
	AdminUser    string    // the first admin's name
	Out          io.Writer // where a trial's one-time password is printed
}

// admin is who Run creates first, if anyone. A trial's password is
// printed once, since nobody chose it.
type admin struct {
	name, password string
	trial          bool
}

// Run bootstraps the layout's WriteFreely and returns its Apper, with its
// database closed: Initialize opens it again. It reads the database's
// state, and makes each of its own refusals before it writes a key file,
// the schema or a user, so none of its refusals writes any of the three.
func Run(ctx context.Context, o *Options) (*wfapp.Apper, error) {
	a, isMounted, err := load(o)
	if err != nil {
		return nil, err
	}
	state, err := readState(ctx, a)
	if err != nil {
		return nil, err
	}
	first, err := decide(o, state, isMounted)
	if err != nil {
		return nil, err
	}
	err = apply(a, o, state, first)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// apply writes what decide allowed: each missing key file, the schema at
// the image's migration, and the first admin, if any.
func apply(a *wfapp.Apper, o *Options, state dbstate.State, first *admin) error {
	if err := keys.Ensure(o.Layout.KeysDir()); err != nil {
		return fmt.Errorf("writing the key files: %w", err)
	}
	if err := migrate(a, state); err != nil {
		return err
	}
	if first == nil {
		return nil
	}
	return first.create(a, o.Out)
}

// load checks the volume and loads the working file.
func load(o *Options) (a *wfapp.Apper, isMounted bool, err error) {
	l := o.Layout
	err = writable(l)
	if err != nil {
		return nil, false, err
	}
	path, isMounted, err := conf.Working(l, o.Mounted)
	if err != nil {
		return nil, false, err
	}
	a = wfapp.New(path, l)
	err = a.LoadConfig()
	if err != nil {
		return nil, false, err
	}
	return a, isMounted, nil
}

// decide refuses a database newer than the image, key files lost from a
// database with users, and a first admin that cannot be created, every
// problem at once; otherwise it returns the first admin, when the
// database has no users.
func decide(o *Options, state dbstate.State, isMounted bool) (*admin, error) {
	var errs []error
	if current := migrations.CurrentVer(); state.Version > current {
		errs = append(errs, fmt.Errorf("the database is at migration %d, newer than this image's %d: run an image at least as new as the one that last migrated it", state.Version, current))
	}
	var first *admin
	if state.Users > 0 {
		errs = append(errs, lostKeys(o.Layout.KeysDir()))
	} else {
		var err error
		first, err = o.firstAdmin(isMounted)
		errs = append(errs, err)
	}
	return first, errors.Join(errs...)
}

// lostKeys refuses key files missing beside a database with users: new
// ones would end every session, and could not decrypt what the old ones
// encrypted.
func lostKeys(dir string) error {
	missing, err := keys.Missing(dir)
	if err != nil {
		return fmt.Errorf("reading the key files: %w", err)
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("the database has users, but %s is missing %s: restore %s from the same backup as the database; new keys would end every session and could not decrypt what the old ones encrypted", dir, and(missing), dir)
}

// migrate creates the schema in an empty database, and migrates one
// behind the image: decide has refused one ahead of it.
func migrate(a *wfapp.Apper, state dbstate.State) error {
	switch {
	case state.Empty:
		return writefreely.CreateSchema(a)
	case state.Version < migrations.CurrentVer():
		return writefreely.Migrate(a)
	}
	return nil
}

// create creates the admin, and prints a trial's password once.
func (first *admin) create(a *wfapp.Apper, out io.Writer) error {
	if err := writefreely.CreateUser(a, first.name, first.password, true); err != nil {
		return err
	}
	if !first.trial {
		return nil
	}
	_, err := fmt.Fprintf(out, "divbast-writefreely: created the admin %q with this one-time password:\ndivbast-writefreely:     %s\ndivbast-writefreely: it is shown once; log in and change it in your settings\n", first.name, first.password)
	return err
}

// writable checks that WriteFreely can write the volume, by creating the
// keys directory and a file beside it, so a missing or read-only volume
// fails here, naming it, rather than in SQLite.
func writable(l conf.Layout) error {
	fail := func(err error) error {
		return fmt.Errorf("%s is not writable by uid %d: %w", l.Data, os.Getuid(), err)
	}
	if err := os.MkdirAll(l.KeysDir(), 0o700); err != nil {
		return fail(err)
	}
	f, err := os.CreateTemp(l.Data, ".writable-")
	if err != nil {
		return fail(err)
	}
	return errors.Join(f.Close(), os.Remove(f.Name()))
}

// readState connects to the configured database, reads its state, and
// closes it again.
func readState(ctx context.Context, a *wfapp.Apper) (dbstate.State, error) {
	app := a.App()
	if err := writefreely.ConnectToDatabase(app); err != nil {
		return dbstate.State{}, fmt.Errorf("connecting to the database: %w", err)
	}
	state, err := dbstate.Read(ctx, app.DB().DB, app.Config().Database.Type)
	return state, errors.Join(err, app.DB().Close())
}

// firstAdmin decides who to create in a database with no users: the
// operator's admin, from the name and a password given together, or with
// neither and nothing mounted, a trial's.
func (o *Options) firstAdmin(isMounted bool) (*admin, error) {
	password, err := o.password()
	if err != nil {
		return nil, err
	}
	given := password != ""
	switch {
	case given && o.AdminUser != "":
		return &admin{name: o.AdminUser, password: password}, nil
	case given || o.AdminUser != "":
		return nil, fmt.Errorf("%s and the admin's password, from %s, go together: give both, or neither", conf.AdminUserEnv, o.sources())
	case isMounted:
		return nil, fmt.Errorf("the database has no users and no admin to create: set %s, and give the admin's password in %s", conf.AdminUserEnv, o.sources())
	default:
		return &admin{name: conf.TrialUser, password: rand.Text(), trial: true}, nil
	}
}

// sources names where the first admin's password may come from.
func (o *Options) sources() string {
	return conf.AdminPasswordEnv + ", " + conf.AdminPasswordFileEnv + " or " + o.Secret
}

// password returns the first admin's password, or "" when none is given.
// It refuses one given in more than one place, and one longer than
// WriteFreely takes, which CreateUser would refuse only once the schema
// was made.
func (o *Options) password() (string, error) {
	var given []string
	if o.Password != "" {
		given = append(given, conf.AdminPasswordEnv)
	}
	if o.PasswordFile != "" {
		given = append(given, conf.AdminPasswordFileEnv)
	}
	// A secret that exists counts, readable or not: reading it says why
	// not. The variable naming that same file is one source, not two
	if _, err := os.Stat(o.Secret); !errors.Is(err, fs.ErrNotExist) && (o.PasswordFile == "" || !sameFile(o.PasswordFile, o.Secret)) {
		given = append(given, o.Secret)
	}
	var password string
	switch len(given) {
	case 0:
		return "", nil
	case 1:
	default:
		return "", fmt.Errorf("the first admin's password is given in %s: give it in one of them only", and(given))
	}
	switch given[0] {
	case conf.AdminPasswordEnv:
		password = o.Password
	case conf.AdminPasswordFileEnv:
		p, err := readPassword(o.PasswordFile)
		if err != nil {
			return "", fmt.Errorf("%s: %w", conf.AdminPasswordFileEnv, err)
		}
		password = p
	default:
		p, err := readPassword(o.Secret)
		if err != nil {
			return "", err
		}
		password = p
	}
	if n := len(password); n > conf.MaxPasswordBytes {
		return "", fmt.Errorf("the first admin's password, from %s, is %d bytes: WriteFreely takes at most %d", given[0], n, conf.MaxPasswordBytes)
	}
	return password, nil
}

// sameFile reports whether a and b name one file: the same path once
// cleaned, or, when both exist, the same file by another path, such as a
// link.
func sameFile(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// readPassword reads the password in the file at path, without the line
// ending a newline puts at its end, and refuses an empty one.
func readPassword(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the operator names this file, by the image's secret path or its variable, for exactly this read
	if errors.Is(err, fs.ErrPermission) {
		return "", fmt.Errorf("%w: the image runs as uid %d, which must be able to read it: give the secret uid=%d or mode=0444, or chmod 0444 the file", err, conf.UID, conf.UID)
	}
	if err != nil {
		return "", err
	}
	password := strings.TrimRight(string(b), "\r\n")
	if password == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return password, nil
}

// and lists items as "a", "a and b", or "a, b and c".
func and(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
