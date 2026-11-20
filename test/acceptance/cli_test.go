// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"strconv"
	"strings"
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
// ends in a newline; user create, run in the container, makes a user who
// logs in with the password it read from stdin, once; and the
// <name>:<password> form is refused.
func TestOperatorCLI(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	s := serveWF(t, e, image, map[string]string{adminUser: "ada"},
		dataVolume(t, e), configMount(t, configuredINI, false), secretMount(t, cliAdminPassword))
	checks := harness.NewChecks(t)

	webLogin(t, s, "ada", cliAdminPassword)

	res, err := s.C.Exec(ctx, strings.NewReader(cliUserPassword+"\n"), binary, "user", "create", "grace")
	checks.True("user create grace exits 0", err == nil && res.Code == 0, string(res.Stderr))
	webLogin(t, s, "grace", cliUserPassword)

	res, err = s.C.Exec(ctx, strings.NewReader("x\n"), binary, "user", "create", "grace")
	checks.True("user create of a user who exists fails", err == nil && res.Code != 0, strconv.Itoa(res.Code)+" "+string(res.Stderr))

	res, err = s.C.Exec(ctx, strings.NewReader("x\n"), binary, "user", "create", "hopper:"+cliUserPassword)
	checks.True("user create of name:password is refused as a wrong command line", err == nil && res.Code == 2, strconv.Itoa(res.Code)+" "+string(res.Stderr))
}
