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
	Layout    conf.Layout
	Mounted   string    // the operator's config.ini, when it exists
	Secret    string    // the first admin's password, when it exists
	AdminUser string    // the first admin's name
	Out       io.Writer // where a trial's one-time password is printed
}

// admin is who Run creates first, if anyone. A trial's password is
// printed once, since nobody chose it.
type admin struct {
	name, password string
	trial          bool
}

// Run bootstraps the layout's WriteFreely and returns its Apper, with its
// database closed: Initialize opens it again.
func Run(ctx context.Context, o *Options) (*wfapp.Apper, error) {
	a, isMounted, err := load(o)
	if err != nil {
		return nil, err
	}
	first, err := prepare(ctx, a, o, isMounted)
	if err != nil {
		return nil, err
	}
	if first != nil {
		err = first.create(a, o.Out)
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// load checks the volume, loads the working file and writes any missing
// key file.
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
	err = keys.Ensure(l.KeysDir())
	if err != nil {
		return nil, false, fmt.Errorf("writing the key files: %w", err)
	}
	return a, isMounted, nil
}

// prepare decides the first admin, when the database has no users, and
// takes the schema to the image's migration.
func prepare(ctx context.Context, a *wfapp.Apper, o *Options, isMounted bool) (*admin, error) {
	state, err := readState(ctx, a)
	if err != nil {
		return nil, err
	}
	var first *admin
	if state.Users == 0 {
		// Before the schema: a refusal here leaves the database as it was
		first, err = o.firstAdmin(isMounted)
		if err != nil {
			return nil, err
		}
	}
	return first, migrate(a, state)
}

// migrate creates the schema in an empty database, migrates one behind
// the image, and refuses one ahead of it.
func migrate(a *wfapp.Apper, state dbstate.State) error {
	switch current := migrations.CurrentVer(); {
	case state.Empty:
		return writefreely.CreateSchema(a)
	case state.Version < current:
		return writefreely.Migrate(a)
	case state.Version > current:
		return fmt.Errorf("the database is at migration %d, newer than this image's %d: run an image at least as new as the one that last migrated it", state.Version, current)
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
// operator's admin from the secret and the name, given together, or with
// neither and nothing mounted, a trial's.
func (o *Options) firstAdmin(isMounted bool) (*admin, error) {
	b, err := os.ReadFile(o.Secret)
	hasSecret := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	switch {
	case hasSecret && o.AdminUser != "":
		password := strings.TrimRight(string(b), "\r\n")
		if password == "" {
			return nil, fmt.Errorf("%s is empty", o.Secret)
		}
		return &admin{name: o.AdminUser, password: password}, nil
	case hasSecret || o.AdminUser != "":
		return nil, fmt.Errorf("%s and %s go together: give both, or neither", conf.AdminUserEnv, o.Secret)
	case isMounted:
		return nil, fmt.Errorf("the database has no users and no admin to create: set %s and mount %s", conf.AdminUserEnv, o.Secret)
	default:
		return &admin{name: conf.TrialUser, password: rand.Text(), trial: true}, nil
	}
}
