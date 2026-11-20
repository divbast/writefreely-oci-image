// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Command probe is the acceptance suite's own probe, which the harness
// builds and mounts beside its core probe: it runs the lab, the peers
// the blog meets on the lab network, and the checks the suite makes
// from inside a container there.
//
// Usage:
//
//	probe lab-serve <dir>
//	probe follow <inbox-url> <blog-actor-url>
//	probe inbox-post <inbox-url> <mode> <expect-status>
//	probe inbox-wait <type> <seconds> <blog-actor-url>
//	probe actor-key <blog-actor-url>
//	probe mail-wait <seconds> <to>
//	probe gopher <host:port> <selector> <expect-file>
//	probe get <url> <expect-status>
//	probe publish <base> <user> <pass> <alias> <body-file>
//	probe remove <path>
//
// Each prints its records as probekit's package comment gives them, and
// exits 0 when every one is ok, 1 when any is not, and 2 for a
// subcommand or arguments it does not know.
package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/divbast/ci/harness/probekit"
)

// The lab, as its peers and the blog reach it.
const (
	labIP   = "11.89.67.10"
	labBase = "https://" + labIP
	// labActor is the lab's ActivityPub actor, and labKeyID its key.
	labActor = labBase + "/users/lab"
	labKeyID = labActor + "#main-key"
	// goneActor answers 410, as an account deleted with its key does.
	goneActor = labBase + "/users/gone"
	// labMount is where the suite mounts the lab's directory in the lab.
	labMount = "/run/lab"
	// The lab's journals, on the container's /tmp: what its inbox and its
	// mail relay took, one JSON object a line.
	journalDir   = "/tmp/lab"
	inboxJournal = journalDir + "/inbox.jsonl"
	mailJournal  = journalDir + "/mail.jsonl"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := probekit.Main(ctx, map[string]probekit.Command{
		"lab-serve":  {Args: []string{"dir"}, Run: labServe},
		"follow":     {Args: []string{"inbox-url", "blog-actor-url"}, Run: follow},
		"inbox-post": {Args: []string{"inbox-url", "mode", "expect-status"}, Run: inboxPost},
		"inbox-wait": {Args: []string{"type", "seconds", "blog-actor-url"}, Run: inboxWait},
		"actor-key":  {Args: []string{"blog-actor-url"}, Run: actorKey},
		"mail-wait":  {Args: []string{"seconds", "to"}, Run: mailWait},
		"gopher":     {Args: []string{"host:port", "selector", "expect-file"}, Run: gopher},
		"get":        {Args: []string{"url", "expect-status"}, Run: get},
		"publish":    {Args: []string{"base", "user", "pass", "alias", "body-file"}, Run: publish},
		"remove":     {Args: []string{"path"}, Run: remove},
	}, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// remove deletes one file, as the blog's user: how a test takes a file
// away from the blog's /data.
func remove(_ context.Context, out *probekit.Output, args []string) error {
	path := filepath.Clean(args[0])
	err := os.Remove(path)
	found := "removed"
	if err != nil {
		found = err.Error()
	}
	out.Record(err == nil, path+" is removed", "removed", found)
	return nil
}

// readFile reads a file the suite named, cleaned, as the container's
// user.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Clean(path))
}
