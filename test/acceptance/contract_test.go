// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"slices"
	"testing"
	"time"

	"github.com/divbast/ci/contract"
	"github.com/divbast/ci/harness"
)

// What the image's contract says of every container here.
const (
	// tmpfsSize is the size of /tmp the README states. Every container
	// gets exactly this, so no check passes on more room than the
	// contract grants.
	tmpfsSize = "16m"
	// appPort serves WriteFreely, and healthPort only /healthz.
	appPort    = 8080
	healthPort = 8081
)

// binary is divbast's one binary, which the image's ENTRYPOINT,
// HEALTHCHECK and the contract's Exec all run.
const binary = "/usr/local/bin/divbast-writefreely"

// serverArgv is PID 1's argv: the image's ENTRYPOINT, which bootstraps
// in-process and then serves.
var serverArgv = []string{binary, "serve"}

// healthCmd is the shipped health probe, as the image's HEALTHCHECK runs
// it and a Podman user passes it with --health-cmd.
var healthCmd = []string{binary, "health"}

// TestContract runs divbast's confinement contract against the image, on a
// network with no route out, once for each configuration it supports: no
// capability, no new privileges, a seccomp filter, a read-only root and no
// writable mount that can execute, uid 65532, the server as PID 1, no shell
// and no package manager, its listeners alone, no egress, the HEALTHCHECK
// and the health port.
func TestContract(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	for _, sc := range []struct {
		name   string
		env    map[string]string
		mounts func(tb testing.TB) []harness.Mount
	}{
		// No configuration, an empty volume: the trial
		{name: "trial", mounts: func(tb testing.TB) []harness.Mount {
			tb.Helper()
			return []harness.Mount{dataVolume(tb, e)}
		}},
	} {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			contract.Run(t, &contract.Config{
				Image:      image,
				Scenario:   sc.name,
				AppPort:    appPort,
				HealthPort: healthPort,
				TmpfsSize:  tmpfsSize,
				PID1:       serverArgv,
				Exec:       []string{binary, "version"},
				HealthCmd:  healthCmd,
				Healthcheck: contract.Healthcheck{
					Test:        slices.Concat([]string{"CMD"}, healthCmd),
					Interval:    30 * time.Second,
					Timeout:     10 * time.Second,
					StartPeriod: 60 * time.Second,
					Retries:     3,
				},
				Env:    sc.env,
				Mounts: sc.mounts,
				// WriteFreely's pages: neither may answer on the health port
				HealthOnly404: []string{"/", "/login"},
			})
		})
	}
}
