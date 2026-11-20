// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/divbast/ci/harness"
)

// The federated blog: single-user, its host the address the lab reaches
// it at, which its inbox checks each signature's host against, and its
// admin's blog the actor the lab follows.
const (
	fedINI   = "[app]\nhost = " + blogBase + "\nsingle_user = true\nfederation = true\n"
	fedAlias = "ada"
	fedActor = blogBase + "/api/collections/" + fedAlias
	fedInbox = fedActor + "/inbox"
	// adminPassword names the first admin's password in the environment.
	adminPassword = "WRITEFREELY_ADMIN_PASSWORD"
)

// earlierImage is this image's 0.17.3 as first published, by digest. It
// could not make an actor's keys: the first fetch of a blog's actor tried
// to, stored none, and served the actor with no key. It reads the admin's
// password from the secret file only.
const earlierImage = "ghcr.io/divbast/writefreely:0.17.3@sha256:cceb6c8585395793621234b27e1d5bb8e5b61de07049063d2e980a73ad2b9d40"

// noKey is what actor-key finds in an actor that publishes no key: an
// empty publicKeyPem.
const noKey = `no PEM block in ""`

// fedLab is the lab and, beside it on a lab network, the federated blog.
type fedLab struct {
	// lab is the lab's container, and probe the lab's probe, which runs
	// every check made from the lab.
	lab   *harness.Container
	probe *harness.Probe
	// dir is the lab's directory, on the host.
	dir  string
	blog *harness.Container
}

// federated starts the lab, and the federated blog from the image under
// test beside it on a lab network, its admin's password from the
// environment.
func federated(t *testing.T) fedLab {
	t.Helper()
	e, image := underTest(t)
	network := labNetwork(t, e)
	dir := labDir(t)
	lab, probe := startLab(t, e, image, network, dir)
	blog, _ := startBlog(t, e, image, network, fedINI, map[string]string{adminUser: fedAlias, adminPassword: cliAdminPassword})
	return fedLab{lab: lab, probe: probe, dir: dir, blog: blog}
}

// TestFederationFollowGetsAccept has the lab's actor follow the blog: the
// blog's actor publishes a key that parses, the blog takes the lab's
// signed Follow, and the Accept it sends back to the lab's inbox verifies
// against that key.
func TestFederationFollowGetsAccept(t *testing.T) {
	t.Parallel()
	lab := federated(t).probe

	harness.RunAppRecords(t, lab, "actor-key", fedActor)
	harness.RunAppRecords(t, lab, "follow", fedInbox, fedActor)
	harness.RunAppRecords(t, lab, "inbox-wait", "Accept", "30", fedActor)
}

// TestFederationRefusesUnsignedAndWrongKey posts the blog's inbox what it
// must not act on: a Follow with no signature, one signed by a key that is
// not the lab's, and one signed by the lab for another actor are each
// refused with 401. A Delete from an actor gone with its key is taken and
// dropped with 202, so its sender does not retry it, and a body past the
// inbox's size limit is refused with 413. Each is a subtest of its own,
// and all post to the one blog, one after another.
func TestFederationRefusesUnsignedAndWrongKey(t *testing.T) { //nolint:tparallel // the subtests post one at a time: see the loop
	t.Parallel()
	lab := federated(t).probe

	// One at a time, since what is checked is how each post is answered,
	// not how the blog answers posts that arrive at once
	for _, tc := range []struct{ mode, status string }{ //nolint:paralleltest // one at a time, as said above
		{mode: "unsigned", status: "401"},
		{mode: "wrong-key", status: "401"},
		{mode: "wrong-actor", status: "401"},
		{mode: "gone-delete", status: "202"},
		{mode: "oversize", status: "413"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			harness.RunAppRecords(t, lab, "inbox-post", fedInbox, tc.mode, tc.status)
		})
	}
}

// TestFederationKeysAfterUpgrade upgrades in place a volume that
// earlierImage served with federation on. Its blog's actor, fetched once,
// serves no key, since the earlier image could make none; then the image
// under test, on the same volume, serves the actor with a key that parses.
// The earlier image's attempt stored no key row at all, so a row left
// empty is not met here: the fork's unit test
// TestGetAPActorKeysRepairsEmptyRow proves that repair.
func TestFederationKeysAfterUpgrade(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	network := labNetwork(t, e)
	dir := labDir(t)
	_, lab := startLab(t, e, image, network, dir)
	data := dataVolume(t, e)
	admin := map[string]string{adminUser: fedAlias}
	checks := harness.NewChecks(t)

	old, _ := startBlog(t, e, earlierImage, network, fedINI, admin, data, secretMount(t, cliAdminPassword))
	records, err := lab.RunApp(ctx, "actor-key", fedActor)
	if err != nil {
		t.Fatal(err)
	}
	checks.True("the earlier image serves its actor with no key",
		len(records) == 1 && !records[0].OK && strings.Contains(records[0].Found, noKey),
		fmt.Sprintf("%+v", records))

	// Its log is read now, since once it is removed a failing test can no
	// longer show it
	if logs, err := old.Logs(ctx); err != nil {
		t.Logf("the log of the earlier image's container %s: %v", old, err)
	} else {
		t.Logf("the log of the earlier image's container %s:\n%s", old, logs)
	}
	// Stopped, so SQLite closes the database, and removed, so the blog's
	// fixed address is free for the next
	for _, verb := range []string{"stop", "rm"} {
		out, err := exec.CommandContext(ctx, e.Name(), verb, old.ID).CombinedOutput() //nolint:gosec // G204: the engine and the container are the harness's own
		if err != nil {
			t.Fatalf("%s %s %s: %v: %s", e.Name(), verb, old, err, out)
		}
	}

	startBlog(t, e, image, network, fedINI, admin, data, secretMount(t, cliAdminPassword))
	harness.RunAppRecords(t, lab, "actor-key", fedActor)
}
