// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/divbast/ci/harness"
)

// postBody is what the post says, and what its page must show.
const postBody = "posted by the acceptance suite"

// publishViaAPI logs the trial's admin in through WriteFreely's API and
// publishes a post with the body to the blog. It returns the post's slug,
// or "" after recording the failure when the login or the publish fails.
func publishViaAPI(t *testing.T, s *harness.Serving, body string) string {
	t.Helper()
	ctx := t.Context()
	checks := harness.NewChecks(t)
	jsonHeader := http.Header{"Content-Type": {"application/json"}}

	creds, err := json.Marshal(map[string]string{"alias": "writer", "pass": trialPassword(t, s)})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := s.Do(ctx, nil, http.MethodPost, "/api/auth/login", jsonHeader, creds)
	harness.Answers(t, "POST /api/auth/login answers 200", http.StatusOK, resp, err)
	var login struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(resp.Body, &login) != nil || login.Data.AccessToken == "" {
		checks.True("the API login gives an access token", false, string(resp.Body))
		return ""
	}

	post, err := json.Marshal(map[string]string{"title": "divbast check", "body": body})
	if err != nil {
		t.Fatal(err)
	}
	header := jsonHeader.Clone()
	header.Set("Authorization", login.Data.AccessToken)
	resp, err = s.Do(ctx, nil, http.MethodPost, "/api/collections/writer/posts", header, post)
	harness.Answers(t, "POST /api/collections/writer/posts answers 201", http.StatusCreated, resp, err)
	var created struct {
		Data struct {
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(resp.Body, &created) != nil || created.Data.Slug == "" {
		checks.True("the post has a slug", false, string(resp.Body))
		return ""
	}
	return created.Data.Slug
}

// TestLoginAndPost logs the trial's admin in through WriteFreely's API,
// publishes a post to the blog, and finds it on the blog.
func TestLoginAndPost(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s := serveWF(t, e, image, nil, dataVolume(t, e))

	slug := publishViaAPI(t, s, postBody)
	if slug == "" {
		return
	}
	resp, err := s.Get(t.Context(), "/"+slug, nil)
	harness.Answers(t, "GET /"+slug+" answers 200", http.StatusOK, resp, err)
	if err == nil {
		harness.Contains(t, "the post is on the blog", postBody, string(resp.Body))
	}
}
