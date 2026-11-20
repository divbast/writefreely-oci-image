// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"testing"

	"github.com/divbast/ci/harness"
)

// TestAdminPasswordFromEnv creates the first admin from
// WRITEFREELY_ADMIN_PASSWORD, with no secret file mounted: the admin logs
// in with it.
func TestAdminPasswordFromEnv(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	const fromEnv = "pw-from-env"
	s := serveWF(t, e, image, map[string]string{adminUser: "ada", adminPassword: fromEnv},
		dataVolume(t, e), configMount(t, configuredINI, false))
	webLogin(t, s, "ada", fromEnv)
}

// lostKey is the key file taken from the volume of a blog with users.
const lostKey = "csrf.aes256"

// TestLostKeysRefused takes one key file away from the trial's volume,
// which has a user, and restarts it: the start is refused, with exit
// status 1 and a log that names the file, rather than made with a new key
// that would end every session.
func TestLostKeysRefused(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	s, err := e.Serve(ctx, t, &harness.ServeSpec{
		Run: harness.RunSpec{
			Image:     image,
			Confine:   harness.Contract(tmpfsSize),
			Mounts:    []harness.Mount{dataVolume(t, e)},
			HealthCmd: healthCmd,
		},
		AppPort:    appPort,
		HealthPort: healthPort,
		ReadyPath:  "/login",
		Probe:      harness.ProbeSpec{AppPackage: labProbe},
	})
	if err != nil {
		t.Fatalf("serving %s: %v", image, err)
	}
	harness.RunAppRecords(t, s.Probe, "remove", "/data/keys/"+lostKey)

	// The restart can find the container already stopped again, which
	// is what is checked next; any other failure leaves it stopped from
	// the first run, with status 0, which fails that check too
	if err = s.C.Restart(ctx); err != nil {
		t.Logf("restart: %v", err)
	}
	exitsOne(t, s.C, "a restart without "+lostKey, harness.ServeWithin, "missing "+lostKey)
}
