// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"testing"

	"github.com/divbast/ci/harness"
)

// dataVolume is a fresh named volume at /data, as a user mounts one.
func dataVolume(tb testing.TB, e harness.Engine) harness.Mount {
	tb.Helper()
	name, err := e.Volume(tb.Context(), tb, "")
	if err != nil {
		tb.Fatal(err)
	}
	return harness.Mount{Source: name, Target: "/data", Volume: true}
}

// serveWF serves the image under the contract with env and mounts, and
// waits until /login answers: it renders for any visitor, whatever the
// instance's configuration.
func serveWF(t *testing.T, e harness.Engine, image string, env map[string]string, mounts ...harness.Mount) *harness.Serving {
	t.Helper()
	s, err := e.Serve(t.Context(), t, &harness.ServeSpec{
		Run: harness.RunSpec{
			Image:     image,
			Confine:   harness.Contract(tmpfsSize),
			Env:       env,
			Mounts:    mounts,
			HealthCmd: healthCmd,
		},
		AppPort:    appPort,
		HealthPort: healthPort,
		ReadyPath:  "/login",
	})
	if err != nil {
		t.Fatalf("serving %s: %v", image, err)
	}
	return s
}
