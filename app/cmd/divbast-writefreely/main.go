// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Command divbast-writefreely is the WriteFreely image's one binary: it
// runs WriteFreely, as a library, under the image's confinement.
//
// Usage:
//
//	divbast-writefreely serve
//	divbast-writefreely health
//	divbast-writefreely version
//	divbast-writefreely user create [--admin] <name>:<password>
//	divbast-writefreely user delete <name>
//	divbast-writefreely user reset-pass <name>
//
// serve bootstraps /data, then serves WriteFreely on 8080 and /healthz on
// 8081, as PID 1, until SIGTERM or SIGINT. health is the HEALTHCHECK's
// probe. version prints the build of WriteFreely it links: the fork build
// the image's build names. user manages users through
// the working config.ini, once serve has made it; delete and reset-pass
// ask at the terminal, as upstream's do. Exit codes: 0 for success, 1 for
// a refusal or a failure, 2 for a wrong command line.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/writefreely/writefreely"
	"github.com/writefreely/writefreely/migrations"

	"github.com/divbast/writefreely-oci-image/app/internal/bootstrap"
	"github.com/divbast/writefreely-oci-image/app/internal/conf"
	"github.com/divbast/writefreely-oci-image/app/internal/health"
	"github.com/divbast/writefreely-oci-image/app/internal/wfapp"
)

const (
	usage     = "usage: divbast-writefreely serve|health|version|user …"
	userUsage = "usage: divbast-writefreely user create [--admin] <name>:<password> | delete <name> | reset-pass <name>"
)

// forkVersion is the build of divbast's fork of WriteFreely the binary
// links, which the image's build sets with -X from the Containerfile's
// WRITEFREELY_FORK_VERSION. WriteFreely's own version stays the upstream
// release the fork builds on: its update check and release-notes links
// compare and name it as an upstream release.
var forkVersion string

// versionLine is what version prints: the fork build fork, or with none,
// as in tests, WriteFreely's own version.
func versionLine(fork string) string {
	if fork == "" {
		return writefreely.FormatVersion()
	}
	return "WriteFreely " + fork
}

func main() {
	o := bootstrap.Options{
		Layout:    conf.Image,
		Mounted:   conf.MountedPath,
		Secret:    conf.SecretPath,
		AdminUser: os.Getenv(conf.AdminUserEnv),
		Out:       os.Stdout,
	}
	os.Exit(run(os.Args[1:], &o, os.Stderr))
}

// run dispatches one subcommand and returns the exit code. o.Out is
// stdout.
func run(args []string, o *bootstrap.Options, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr, usage)
	}
	// serve, health and version take nothing more. The ENTRYPOINT is
	// serve, so docker run <image> version arrives as "serve version", and
	// must not start a server, or bootstrap a trial, in its place
	if args[0] != "user" && len(args) > 1 {
		return usageError(stderr, usage)
	}
	ctx := context.Background()
	switch args[0] {
	case "serve":
		return serve(ctx, o, stderr)
	case "health":
		return health.Probe(ctx,
			"http://127.0.0.1:"+strconv.Itoa(conf.AppPort)+"/login",
			"http://127.0.0.1:"+strconv.Itoa(conf.HealthPort)+"/healthz", stderr)
	case "version":
		if _, err := fmt.Fprintln(o.Out, versionLine(forkVersion)); err != nil {
			return 1
		}
		return 0
	case "user":
		return user(args[1:], o, stderr)
	default:
		return usageError(stderr, usage)
	}
}

// serve bootstraps, starts /healthz, and hands the process to WriteFreely's
// Serve, which exits 0 on SIGTERM or SIGINT. It returns only on a failure
// before serving.
func serve(ctx context.Context, o *bootstrap.Options, stderr io.Writer) int {
	a, err := bootstrap.Run(ctx, o)
	if err != nil {
		return fail(stderr, err)
	}
	if _, err = writefreely.Initialize(a, false); err != nil {
		return fail(stderr, err)
	}
	r := mux.NewRouter()
	writefreely.InitRoutes(a, r)
	a.App().InitStaticRoutes(r)

	// Bound before WriteFreely serves, so a port that cannot be had fails
	// the start rather than a health check minutes later
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", ":"+strconv.Itoa(conf.HealthPort))
	if err != nil {
		return fail(stderr, fmt.Errorf("the health listener: %w", err))
	}
	cfg := a.App().Config()
	hs := &http.Server{
		Handler:           health.Handler(a.App().DB().DB, cfg.Database.Type, migrations.CurrentVer(), o.Layout.KeysDir()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		err := hs.Serve(ln)
		os.Exit(fail(stderr, fmt.Errorf("the health listener stopped: %w", err)))
	}()
	writefreely.Serve(a.App(), r)
	return 0
}

// user runs one user subcommand against the working config.ini, which
// must already exist: before the first serve, it would be made in the
// wrong place, or with nothing to manage.
func user(args []string, o *bootstrap.Options, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr, userUsage)
	}
	fl := flag.NewFlagSet("user "+args[0], flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	admin := fl.Bool("admin", false, "create the admin")
	if err := fl.Parse(args[1:]); err != nil || fl.NArg() != 1 || (args[0] != "create" && *admin) {
		return usageError(stderr, userUsage)
	}
	name := fl.Arg(0)
	var do func(*wfapp.Apper) error
	switch args[0] {
	case "create":
		// Cut at the first colon, so a password may hold one
		n, password, ok := strings.Cut(name, ":")
		if !ok {
			return usageError(stderr, userUsage)
		}
		do = func(a *wfapp.Apper) error { return writefreely.CreateUser(a, n, password, *admin) }
	case "delete":
		do = func(a *wfapp.Apper) error { return writefreely.DoDeleteAccount(a, name) }
	case "reset-pass":
		do = func(a *wfapp.Apper) error { return writefreely.ResetPassword(a, name) }
	default:
		return usageError(stderr, userUsage)
	}
	path, err := existing(o)
	if err != nil {
		return fail(stderr, err)
	}
	a := wfapp.New(path, o.Layout)
	if err = a.LoadConfig(); err != nil {
		return fail(stderr, err)
	}
	if err = do(a); err != nil {
		return fail(stderr, err)
	}
	return 0
}

// existing returns the working file if there is one yet.
func existing(o *bootstrap.Options) (string, error) {
	for _, path := range []string{o.Mounted, o.Layout.DataConfig()} {
		switch _, err := os.Stat(path); {
		case err == nil:
			return path, nil
		case !errors.Is(err, fs.ErrNotExist):
			return "", err
		}
	}
	return "", errors.New("no working config yet: start the server once with serve first")
}

// fail prints err, a line per line of it, and returns the failure code.
func fail(stderr io.Writer, err error) int {
	for line := range strings.SplitSeq(err.Error(), "\n") {
		// A failed write to stderr leaves nowhere to report it
		_, _ = fmt.Fprintln(stderr, "divbast-writefreely:", line)
	}
	return 1
}

// usageError prints a usage line and returns the wrong-command-line code.
func usageError(stderr io.Writer, line string) int {
	// A failed write to stderr leaves nowhere to report it
	_, _ = fmt.Fprintln(stderr, line)
	return 2
}
