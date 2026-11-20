// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Package keys writes WriteFreely's key files. WriteFreely's own
// GenerateKeyFiles is never called: it and Initialize each prefix the key
// paths with keys_parent_dir, so calling both in one process doubles it.
package keys

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/writefreely/writefreely/key"
)

// Names are WriteFreely's key files, under upstream's names.
var Names = []string{"email.aes256", "cookies_auth.aes256", "cookies_enc.aes256", "csrf.aes256"}

// Missing returns the key files missing from dir, in Names' order: all of
// them when dir does not exist. It writes nothing.
func Missing(dir string) ([]string, error) {
	root, err := os.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return slices.Clone(Names), nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close() //nolint:errcheck // a read-only handle
	var missing []string
	for _, name := range Names {
		switch _, err := root.Stat(name); {
		case errors.Is(err, fs.ErrNotExist):
			missing = append(missing, name)
		case err != nil:
			return nil, err
		}
	}
	return missing, nil
}

// Ensure writes each key file missing from dir, which it creates, as
// WriteFreely would: key.EncKeysBytes random bytes, at 0600. A file that
// exists is never touched, since what it encrypts is lost without it.
func Ensure(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // a read-only handle; every write below is checked
	for _, name := range Names {
		switch _, err := root.Stat(name); {
		case err == nil:
			continue
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
		b, err := key.GenerateBytes(key.EncKeysBytes)
		if err != nil {
			return err
		}
		if err := root.WriteFile(name, b, 0o600); err != nil {
			return fmt.Errorf("%s: %w", filepath.Join(dir, name), err)
		}
	}
	return nil
}
