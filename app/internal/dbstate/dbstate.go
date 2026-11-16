// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Package dbstate reads what a WriteFreely database holds: whether it has
// a schema, the schema's migration version, and how many users it has.
package dbstate

import (
	"context"
	"database/sql"
	"fmt"
)

// State is what Read found. Empty is a database with no users table.
// Version is the highest migration in appmigrations, 0 without that
// table. Users counts the users.
type State struct {
	Empty          bool
	Version, Users int
}

// Read reads db's state. driver is the configuration's database type,
// "sqlite3" or "mysql".
func Read(ctx context.Context, db *sql.DB, driver string) (State, error) {
	users, err := hasTable(ctx, db, driver, "users")
	if err != nil {
		return State{}, err
	}
	if !users {
		return State{Empty: true}, nil
	}
	var s State
	migrated, err := hasTable(ctx, db, driver, "appmigrations")
	if err != nil {
		return State{}, err
	}
	if migrated {
		var v sql.NullInt64
		if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM appmigrations").Scan(&v); err != nil {
			return State{}, fmt.Errorf("reading the migration version: %w", err)
		}
		s.Version = int(v.Int64)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&s.Users); err != nil {
		return State{}, fmt.Errorf("counting users: %w", err)
	}
	return s, nil
}

// hasTable reports whether db has the table name.
func hasTable(ctx context.Context, db *sql.DB, driver, name string) (bool, error) {
	q := "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?"
	if driver == "mysql" {
		q = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?"
	}
	var n int
	if err := db.QueryRowContext(ctx, q, name).Scan(&n); err != nil {
		return false, fmt.Errorf("looking for table %s: %w", name, err)
	}
	return n > 0, nil
}
