// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package keys_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/writefreely/writefreely/key"

	"github.com/divbast/writefreely-oci-image/app/internal/keys"
)

// read returns each key file's bytes, failing on a wrong mode or length.
func read(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	got := make(map[string][]byte, len(keys.Names))
	for _, name := range keys.Names {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v, want 0600", name, info.Mode().Perm())
		}
		b := readFile(t, path)
		if len(b) != key.EncKeysBytes {
			t.Errorf("%s holds %d bytes, want %d", name, len(b), key.EncKeysBytes)
		}
		got[name] = b
	}
	return got
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

func TestEnsure(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "keys")
	if err := keys.Ensure(dir); err != nil {
		t.Fatalf("Ensure(empty): %v", err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("keys dir: %v, %v; want mode 0700", info, err)
	}
	first := read(t, dir)
	if err := keys.Ensure(dir); err != nil {
		t.Fatalf("Ensure(again): %v", err)
	}
	for name, b := range read(t, dir) {
		if !bytes.Equal(b, first[name]) {
			t.Errorf("a second Ensure rewrote %s", name)
		}
	}
	gone := keys.Names[1]
	if err := os.Remove(filepath.Join(dir, gone)); err != nil {
		t.Fatal(err)
	}
	if err := keys.Ensure(dir); err != nil {
		t.Fatalf("Ensure(one missing): %v", err)
	}
	for name, b := range read(t, dir) {
		if same := bytes.Equal(b, first[name]); same == (name == gone) {
			t.Errorf("%s: rewritten %t, want rewritten only for %s", name, !same, gone)
		}
	}
}

func TestNames(t *testing.T) {
	t.Parallel()
	want := []string{"email.aes256", "cookies_auth.aes256", "cookies_enc.aes256", "csrf.aes256"}
	if len(keys.Names) != len(want) {
		t.Fatalf("Names = %q, want %q", keys.Names, want)
	}
	for i := range want {
		if keys.Names[i] != want[i] {
			t.Errorf("Names[%d] = %q, want %q", i, keys.Names[i], want[i])
		}
	}
}
