// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/divbast/ci/harness"
)

const (
	// compiledSelector is a selector only lessc writes: core.less, which
	// app.less imports, nests &#me #official-writing inside body, and the
	// compiled write.css spells it out.
	compiledSelector = "body#me #official-writing"
	// proseMarker is a class the ProseMirror editor in prose.bundle.js
	// gives the node it selects.
	proseMarker = "ProseMirror-selectednode"
	// proseAtLeast is a floor under the bundle's size: v0.17.3's is 424
	// KiB, and a truncated or empty one is far below it.
	proseAtLeast = 100 << 10
)

// TestServes checks the trial from a client outside the container: the
// blog and the login page, the stylesheet compiled from upstream's LESS,
// the editor's bundle from the release tarball, and /healthz only on the
// health port.
func TestServes(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	s := serveWF(t, e, image, nil, dataVolume(t, e))
	checks := harness.NewChecks(t)

	for _, path := range []string{"/", "/login"} {
		resp, err := s.Get(ctx, path, nil)
		harness.Answers(t, "GET "+path+" answers 200", http.StatusOK, resp, err)
		if err == nil {
			harness.Contains(t, "GET "+path+" is WriteFreely's", "WriteFreely", string(resp.Body))
		}
	}

	resp, err := s.Get(ctx, "/css/write.css", nil)
	harness.Answers(t, "GET /css/write.css answers 200", http.StatusOK, resp, err)
	if err == nil {
		harness.Contains(t, "write.css is compiled from upstream's LESS", compiledSelector, string(resp.Body))
	}

	resp, err = s.Get(ctx, "/js/prose.bundle.js", nil)
	harness.Answers(t, "GET /js/prose.bundle.js answers 200", http.StatusOK, resp, err)
	if err == nil {
		checks.True("prose.bundle.js is the whole bundle", len(resp.Body) >= proseAtLeast, strconv.Itoa(len(resp.Body))+" bytes")
		harness.Contains(t, "prose.bundle.js is ProseMirror's editor", proseMarker, string(resp.Body))
	}

	harness.StatusIs(t, s, "GET /healthz on the app port answers 404", http.StatusNotFound, "/healthz")
}
