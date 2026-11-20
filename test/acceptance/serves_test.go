// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
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
	// proseForkMarker is an entry only the fork's editor source has, as
	// the minified bundle spells it: prose/markdownParser.js maps a single
	// newline to a line break, so the editor keeps a post's line breaks
	// when it saves.
	proseForkMarker = `softbreak:{node:"hard_break"}`
	// proseAtLeast is a floor under the bundle's size: the fork's build is
	// 359 KiB, and a truncated or empty one is far below it.
	proseAtLeast = 100 << 10
)

// TestServes checks the trial from a client outside the container: the
// blog and the login page, the stylesheet compiled from upstream's LESS,
// the editor's bundle built from the fork's source, and /healthz only on
// the health port.
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
		harness.Contains(t, "prose.bundle.js is built from the fork's editor source", proseForkMarker, string(resp.Body))
	}

	harness.StatusIs(t, s, "GET /healthz on the app port answers 404", http.StatusNotFound, "/healthz")
}

// forkBuild is how a build of divbast's fork of WriteFreely is versioned.
var forkBuild = regexp.MustCompile(`^0\.17\.3-divbast\.\d+$`)

// containerfileArgs returns the value of every ARG name=value line in the
// Containerfile. The pins are read from there, where Renovate moves them,
// since Renovate's recommended preset ignores every path under test/.
func containerfileArgs(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile("../../Containerfile")
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`(?m)^ARG `+regexp.QuoteMeta(name)+`=(\S*)$`).FindAllSubmatch(b, -1)
	values := make([]string, 0, len(found))
	for _, m := range found {
		values = append(values, string(m[1]))
	}
	return values
}

// TestVersionNamesForkBuild checks that the image names the fork build it
// links, the Containerfile's WRITEFREELY_FORK_VERSION, wherever it gives
// its version: in what version prints, and in its version label.
func TestVersionNamesForkBuild(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	checks := harness.NewChecks(t)

	res, err := e.Run(ctx, t, &harness.RunSpec{
		Image:      image,
		Confine:    harness.Contract(tmpfsSize),
		Network:    "none",
		Entrypoint: []string{binary},
		Args:       []string{"version"},
	})
	if err != nil {
		t.Fatalf("running version: %v", err)
	}
	checks.True("version exits 0", res.Code == 0, strconv.Itoa(res.Code)+" "+string(res.Stderr))
	printed, isFork := strings.CutPrefix(strings.TrimSpace(string(res.Stdout)), "WriteFreely ")
	checks.True("version names a build of the fork", isFork && forkBuild.MatchString(printed), string(res.Stdout))

	out, err := exec.CommandContext(ctx, e.Name(), "image", "inspect", //nolint:gosec // G204: the engine and the image are the harness's own
		"--format", `{{index .Config.Labels "org.opencontainers.image.version"}}`, image).Output()
	label := strings.TrimSpace(string(out))
	checks.True("the version label names a build of the fork", err == nil && forkBuild.MatchString(label), label)

	found := containerfileArgs(t, "WRITEFREELY_FORK_VERSION")
	if len(found) != 1 {
		checks.True("the Containerfile pins the fork build once", false, strconv.Itoa(len(found))+" ARG WRITEFREELY_FORK_VERSION lines")
		return
	}
	harness.Equal(checks, "version prints the Containerfile's fork build", found[0], printed)
	harness.Equal(checks, "the version label is the Containerfile's fork build", found[0], label)
}

// TestWriteFreelyReportsRelease checks that WriteFreely itself reports the
// upstream release the fork builds on, the Containerfile's
// WRITEFREELY_VERSION, and not the fork build. Its update check and its
// release-notes links compare and name that version as an upstream
// release, and NodeInfo, where this reads it, and the User-Agent of its
// requests carry it.
func TestWriteFreelyReportsRelease(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s := serveWF(t, e, image, nil, dataVolume(t, e))
	checks := harness.NewChecks(t)

	found := containerfileArgs(t, "WRITEFREELY_VERSION")
	if len(found) != 1 {
		t.Fatalf("the Containerfile has %d ARG WRITEFREELY_VERSION lines, want 1", len(found))
	}
	resp, err := s.Get(t.Context(), "/api/nodeinfo", nil)
	harness.Answers(t, "GET /api/nodeinfo answers 200", http.StatusOK, resp, err)
	if err != nil {
		return
	}
	var info struct {
		Software struct {
			Version string `json:"version"`
		} `json:"software"`
	}
	if err := json.Unmarshal(resp.Body, &info); err != nil {
		checks.True("NodeInfo is JSON", false, err.Error()+": "+harness.Clip(string(resp.Body)))
		return
	}
	harness.Equal(checks, "NodeInfo's software.version is the Containerfile's WriteFreely release", found[0], info.Software.Version)
}
