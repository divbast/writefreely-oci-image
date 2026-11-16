// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Package wfapp is WriteFreely's seam into the image: an Apper that loads
// and saves the working config.ini with the image-owned keys enforced.
package wfapp

import (
	"net/http"
	"time"

	"github.com/go-ini/ini"
	"github.com/writefreely/writefreely"
	"github.com/writefreely/writefreely/config"

	"github.com/divbast/writefreely-oci-image/app/internal/conf"
)

// Apper implements writefreely.Apper. It wraps rather than embeds
// *writefreely.App: an embedded field named App would clash with the App
// method the interface needs.
type Apper struct {
	app    *writefreely.App
	file   string
	layout conf.Layout
}

// New returns an Apper for the working file under layout l.
func New(file string, l conf.Layout) *Apper {
	return &Apper{app: writefreely.NewApp(file), file: file, layout: l}
}

// App returns the wrapped WriteFreely app.
func (a *Apper) App() *writefreely.App { return a.app }

// LoadConfig reads the working file, enforces the image-owned keys, and
// hands the result to WriteFreely.
func (a *Apper) LoadConfig() error {
	cfg, err := config.Load(a.file)
	if err != nil {
		return err
	}
	if err := conf.Enforce(cfg, a.file, a.layout); err != nil {
		return err
	}
	a.app.SetConfig(cfg)
	return nil
}

// SaveConfig writes c to the working file, with the image-owned keys at
// the image's values. The admin panel's settings form calls it and shows
// its error.
//
// It writes as config.Save does, through go-ini in place, but leaves the
// file's mode alone: config.Save chmods it to 0600 afterwards, which fails
// on a mounted file uid 65532 can write but does not own, and reports a
// failure for a save that landed.
func (a *Apper) SaveConfig(c *config.Config) error {
	if err := conf.Enforce(c, a.file, a.layout); err != nil {
		return err
	}
	f := ini.Empty()
	if err := ini.ReflectFrom(f, c); err != nil {
		return err
	}
	return f.SaveTo(a.file)
}

// LoadKeys reads WriteFreely's key files.
func (a *Apper) LoadKeys() error { return a.app.LoadKeys() }

// ReqLog formats one request's log line, as WriteFreely does.
func (a *Apper) ReqLog(r *http.Request, status int, d time.Duration) string {
	return a.app.ReqLog(r, status, d)
}
