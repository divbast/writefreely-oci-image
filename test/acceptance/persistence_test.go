// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"net/http"
	"testing"

	"github.com/divbast/ci/harness"
)

// TestRestartKeepsSessions checks that a session outlives a restart: the
// cookie keys live on /data, so a login stays valid, and so do the
// database and the user it names.
func TestRestartKeepsSessions(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	s := serveWF(t, e, image, nil, dataVolume(t, e))
	client := webLogin(t, s, "writer", trialPassword(t, s))

	resp, err := s.Do(ctx, client, http.MethodGet, "/me/settings", nil, nil)
	harness.Answers(t, "GET /me/settings, logged in, answers 200", http.StatusOK, resp, err)

	if err = s.Restart(ctx); err != nil {
		t.Fatalf("restart: %v", err)
	}
	resp, err = s.Do(ctx, client, http.MethodGet, "/me/settings", nil, nil)
	harness.Answers(t, "after a restart, the same session's GET /me/settings answers 200", http.StatusOK, resp, err)
}
