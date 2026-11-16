// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package dbstate_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/writefreely/writefreely"
	"github.com/writefreely/writefreely/migrations"

	"github.com/divbast/writefreely-oci-image/app/internal/conf"
	"github.com/divbast/writefreely-oci-image/app/internal/dbstate"
	"github.com/divbast/writefreely-oci-image/app/internal/wfapp"
)

// WriteFreely keeps package-level state, so these tests run one at a time.
func TestReadState(t *testing.T) { //nolint:paralleltest // WriteFreely's package-level state
	dir := t.TempDir()
	l := conf.Layout{Data: dir, Assets: filepath.Join(dir, "assets")}
	file, _, err := conf.Working(l, filepath.Join(dir, "etc", "config.ini"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(dir, "writefreely.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	got, err := dbstate.Read(t.Context(), db, "sqlite3")
	if err != nil || got != (dbstate.State{Empty: true}) {
		t.Fatalf("Read(empty) = %+v, %v; want Empty and nothing else", got, err)
	}

	if err = writefreely.CreateSchema(wfapp.New(file, l)); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}
	got, err = dbstate.Read(t.Context(), db, "sqlite3")
	want := dbstate.State{Version: migrations.CurrentVer()}
	if err != nil || got != want {
		t.Fatalf("Read(schema) = %+v, %v; want %+v", got, err, want)
	}
}
