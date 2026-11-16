// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"strconv"
	"testing"

	"github.com/divbast/ci/harness"
)

// The operator's admin, from the secret, and the user the CLI creates:
// not "reader", which WriteFreely reserves.
const (
	cliAdminPassword = "ada-pass-word-1234"
	cliUserPassword  = "grace-pass-1234"
)

// TestOperatorCLI checks what the README tells an operator: the admin the
// secret names logs in with the bare password, though the secret's file
// ends in a newline, and user create, run in the container, makes a user
// who logs in, once.
func TestOperatorCLI(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	s := serveWF(t, e, image, map[string]string{adminUser: "ada"},
		dataVolume(t, e), configMount(t, configuredINI, false), secretMount(t, cliAdminPassword))
	checks := harness.NewChecks(t)

	webLogin(t, s, "ada", cliAdminPassword)

	res, err := s.C.Exec(ctx, nil, binary, "user", "create", "grace:"+cliUserPassword)
	checks.True("user create grace exits 0", err == nil && res.Code == 0, string(res.Stderr))
	webLogin(t, s, "grace", cliUserPassword)

	res, err = s.C.Exec(ctx, nil, binary, "user", "create", "grace:x")
	checks.True("user create of a user who exists fails", err == nil && res.Code != 0, strconv.Itoa(res.Code)+" "+string(res.Stderr))
}
