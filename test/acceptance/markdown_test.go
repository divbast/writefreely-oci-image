// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"net/http"
	"testing"

	"github.com/divbast/ci/harness"
)

// markdownBody has a contraction, a hashtag, a mention and two paragraphs.
const markdownBody = "Don't stop #divbast and @alice@example.com\n\nsecond paragraph"

// TestPostRendersMarkdown publishes a post and checks its page shows what
// the fork's Markdown engine makes of it: a typographic apostrophe, a
// hashtag link and a mention link.
func TestPostRendersMarkdown(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s := serveWF(t, e, image, nil, dataVolume(t, e))

	slug := publishViaAPI(t, s, markdownBody)
	if slug == "" {
		return
	}
	resp, err := s.Get(t.Context(), "/"+slug, nil)
	harness.Answers(t, "GET /"+slug+" answers 200", http.StatusOK, resp, err)
	if err != nil {
		return
	}
	page := string(resp.Body)
	harness.Contains(t, "the hashtag is a link", `tag:divbast" class="hashtag"><span>#</span><span class="p-category">divbast</span></a>`, page)
	harness.Contains(t, "the mention is a link", `/@/alice@example.com" class="u-url mention">@<span>alice@example.com</span></a>`, page)
	harness.Contains(t, "the apostrophe is typographic", "Don’t", page)
}
