// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package bootstrap

// FirstAdmin returns the password o's first admin would be created with,
// for the tests.
func FirstAdmin(o *Options, isMounted bool) (string, error) {
	first, err := o.firstAdmin(isMounted)
	if err != nil {
		return "", err
	}
	return first.password, nil
}
