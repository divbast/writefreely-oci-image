// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Command divbast-writefreely is the WriteFreely image's one binary: it
// runs WriteFreely, as a library, under the image's confinement.
//
// Usage:
//
//	divbast-writefreely version
//
// version prints WriteFreely's version. Exit codes: 0 for success, 2 for
// a wrong command line.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/writefreely/writefreely"
)

const usage = "usage: divbast-writefreely serve|health|version|user …"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches one subcommand and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr)
	}
	switch args[0] {
	case "version":
		if _, err := fmt.Fprintln(stdout, writefreely.FormatVersion()); err != nil {
			return 1
		}
		return 0
	default:
		return usageError(stderr)
	}
}

// usageError prints the usage line and returns the wrong-command-line code.
func usageError(stderr io.Writer) int {
	// A failed write to stderr leaves nowhere to report it
	_, _ = fmt.Fprintln(stderr, usage)
	return 2
}
