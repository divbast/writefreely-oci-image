// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/divbast/ci/harness"
)

// settleWithin is how long a test waits for what the blog may still be
// doing, such as a fetch it started, to reach its log.
const settleWithin = 3 * time.Second

// logLacks checks that c's log holds no needle, and shows each line that
// holds it when one does. An empty log shows nothing, so it fails.
func logLacks(t *testing.T, c *harness.Container, what, needle string) {
	t.Helper()
	checks := harness.NewChecks(t)
	logs, err := c.Logs(t.Context())
	if err != nil {
		checks.True(what, false, err.Error())
		return
	}
	var holding []string
	for line := range strings.Lines(logs) {
		if strings.Contains(line, needle) {
			holding = append(holding, strings.TrimSpace(line))
		}
	}
	switch {
	case logs == "":
		checks.True(what, false, "the log is empty, which shows nothing")
	case len(holding) > 0:
		checks.True(what, false, fmt.Sprintf("%q is in %d line(s): %s", needle, len(holding), harness.Clip(strings.Join(holding, "\n"))))
	default:
		checks.True(what, true, fmt.Sprintf("no %q in %d bytes", needle, len(logs)))
	}
}

// awaitLog waits until c's log holds every one of needles, for up to
// harness.ServeWithin, and fails t with the log when it does not.
func awaitLog(t *testing.T, c *harness.Container, what string, needles ...string) {
	t.Helper()
	ctx := t.Context()
	err := harness.Poll(ctx, harness.ServeWithin, 250*time.Millisecond, func(ctx context.Context) error {
		logs, err := c.Logs(ctx)
		if err != nil {
			return err
		}
		for _, needle := range needles {
			if !strings.Contains(logs, needle) {
				return harness.NotYet(fmt.Errorf("no %q in the log yet", needle))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("waiting for %s: %v", what, harness.WithLog(ctx, c, err))
	}
}

// requestLine is how the blog's request log begins a request's line: the
// method and the path, after a quote.
func requestLine(method, path string) string {
	return `"` + method + " " + path
}

// TestProfileLookupStaysOffLoopback asks the blog for the profile of the
// lab's canary account, whose WebFinger self link points at the blog's
// own loopback address, with a token in its path. Once the lab has
// answered the blog's WebFinger lookup, so the blog holds that link and
// has decided whether to fetch it, and a little after, the blog's request
// log holds no request for the link's path: the address check kept the
// fetch off loopback. The token may be in the lines where the blog names
// the fetch it refused; only a request line shows a fetch that reached the
// blog.
func TestProfileLookupStaysOffLoopback(t *testing.T) {
	t.Parallel()
	run := federated(t)
	canary := "/loopback-canary-" + string(hostFile(t, run.dir, "canary.txt"))
	handle := "canary@" + labIP
	profile := "/@/" + handle

	harness.RunAppRecords(t, run.probe, "get", blogBase+profile, "any")
	awaitLog(t, run.lab, "the lab to answer the blog's WebFinger lookup of "+handle,
		"GET /.well-known/webfinger?resource=acct:"+handle+" 200")
	// In the request log's own format, so a change to it fails here rather
	// than pass the check below unseen
	awaitLog(t, run.blog, "the blog to log the profile lookup", requestLine(http.MethodGet, profile))
	if err := harness.Pause(t.Context(), settleWithin); err != nil {
		t.Fatal(err)
	}
	logLacks(t, run.blog, "the blog's log holds no request for "+canary, requestLine(http.MethodGet, canary))
}

// crossSiteOrigin is the Origin of a page on another site.
const crossSiteOrigin = "https://other.example"

// TestCrossSiteWriteRefused logs the trial's admin in through the login
// form, then publishes with the session's cookie, as a form any page can
// send. A write from another site, with no CSRF token, is refused; so is
// one from the blog's own origin with no token; with the token the blog's
// pages carry, the same write is published. So both the origin and the
// token are checked.
func TestCrossSiteWriteRefused(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	s := serveWF(t, e, image, nil, dataVolume(t, e))
	client := webLogin(t, s, "writer", trialPassword(t, s))
	const posts = "/api/collections/writer/posts"
	form := []byte(url.Values{"body": {postBody}}.Encode())
	write := func(origin, token string) (harness.Response, error) {
		header := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}, "Origin": {origin}}
		if token != "" {
			header.Set("X-Csrf-Token", token)
		}
		return s.Do(ctx, client, http.MethodPost, posts, header, form)
	}

	resp, err := write(crossSiteOrigin, "")
	harness.Answers(t, "a write from "+crossSiteOrigin+" with the session's cookie and no CSRF token answers 403", http.StatusForbidden, resp, err)
	resp, err = write(s.Base, "")
	harness.Answers(t, "a write from the blog's own origin with the session's cookie and no CSRF token answers 403", http.StatusForbidden, resp, err)
	token := csrfToken(t, s, client, "/me/settings", csrfMeta)
	resp, err = write(s.Base, token)
	harness.Answers(t, "the same write from the blog's own page, with its CSRF token, answers 201", http.StatusCreated, resp, err)
}

// TestTokensStayOutOfLog sends the blog requests that carry a token in
// the query string: a password reset's, a one-time login's and an access
// token, the last as the POST that reads it. Once the log shows each
// request, it holds none of the tokens.
func TestTokensStayOutOfLog(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	s := serveWF(t, e, image, nil, dataVolume(t, e))
	checks := harness.NewChecks(t)
	requests := []struct{ method, path, query, token string }{
		{method: http.MethodGet, path: "/reset", query: "t=SENTINEL1", token: "SENTINEL1"},
		{method: http.MethodGet, path: "/login", query: "to=/&with=SENTINEL2", token: "SENTINEL2"},
		{method: http.MethodPost, path: "/api/posts", query: "access_token=SENTINEL3", token: "SENTINEL3"},
	}

	logged := make([]string, 0, len(requests))
	for _, r := range requests {
		request := r.method + " " + r.path
		resp, err := s.Do(ctx, nil, r.method, r.path+"?"+r.query, nil, nil)
		answer := fmt.Sprint(err)
		if err == nil {
			answer = strconv.Itoa(resp.Status)
		}
		checks.True(request+"?"+r.query+" is answered", err == nil, answer)
		logged = append(logged, requestLine(r.method, r.path))
	}
	// Every readiness check logs "GET /login too, so for the login that
	// line shows only that the log is current; the other two are this
	// test's own requests
	awaitLog(t, s.C, "the blog to log each request", logged...)
	for _, r := range requests {
		logLacks(t, s.C, "the log holds no "+r.token, r.token)
	}
}
