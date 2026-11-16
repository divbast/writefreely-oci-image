// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"strconv"
	"strings"
	"testing"

	"github.com/divbast/ci/harness"
)

// TestTrial checks the trial's admin: its one-time password is printed
// once, logs in, and is never printed again, since after a restart the
// user is still there.
func TestTrial(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	s := serveWF(t, e, image, nil, dataVolume(t, e))
	checks := harness.NewChecks(t)

	harness.LogContains(t, s.C, "the trial prints its admin's password", trialMarker)
	password := trialPassword(t, s)
	webLogin(t, s, "writer", password)

	if err := s.Restart(ctx); err != nil {
		t.Fatalf("restart: %v", err)
	}
	log, err := s.C.Logs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n := strings.Count(log, trialMarker)
	checks.True("after a restart, the password has been printed once in all", n == 1, strconv.Itoa(n)+" times")
	webLogin(t, s, "writer", password)
}
