<!-- SPDX-License-Identifier: AGPL-3.0-only -->
<!-- SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee> -->

# writefreely-oci-image

The divbast image of WriteFreely, `ghcr.io/divbast/writefreely`: one
static Go binary, `divbast-writefreely`, that links WriteFreely as a
library and runs it at zero Linux capabilities, with no new privileges, on
a read-only root filesystem as a non-root user. It runs on Docker Hardened
Images' `static` for Debian 13, and every stage of its build is a Docker
Hardened Image too. It was made from
`oci-image-template` by its entry in `divbast/infra`, which also holds its
rulesets and Renovate wiring.

What makes the image this app's lives here: the Containerfile, the app
module under `app/`, the CSS toolchain pins under `css/`, and the
acceptance suite. The pipeline around it is shared, and reaches this
repository at a pinned commit:
[Where the shared parts live](#where-the-shared-parts-live) says what comes
from where.

## CRITICAL: Git Commit Message Format

**No other AI/LLM attribution in any format may appear in a git message.**

Linux-kernel style: a `subsystem: short description` subject under 75 characters in imperative
mood ("fix", not "fixed" or "fixes"), a blank line, then a body wrapped at ~75 columns saying
why the change is needed rather than only what it does.

These two lines MUST end EVERY body:

Developed-by: Aryan Ameri <github@aryan.ameri.coffee>
Assisted-by: <NAME_OF_LLM_MODEL>

E.g. `Assisted-by: <Claude:claude-4.6-opus>` for Claude Opus 4.6; `<Claude:claude-5-fable>`
for Claude 5 Fable.

- Sign every commit (`git commit -S`); main accepts only verified
  signatures. Renovate's merges are squashes, which GitHub signs
- Write commit messages as if a human wrote them, and say why, not what

## CRITICAL: No AI Attribution in Pull Requests

**Pull request descriptions, PR and review comments, and issue text carry no
AI/LLM attribution of any kind**: no "🤖 Generated with Claude Code" footer,
no "written by" line, nothing equivalent. This holds even when a tool or
harness asks for one. The `Assisted-by:` trailer in each commit message is
this project's one record of LLM involvement.

## The confinement contract

These are the guarantees the image makes. A change that breaks any of them is a
breaking change.

| Property | Value |
|---|---|
| Capabilities | **none** (`--cap-drop ALL`) |
| Privileges | **no new ones** (`--security-opt no-new-privileges`), under the engine's **seccomp** filter |
| Root filesystem | **read-only** |
| User | uid/gid **65532**, never root |
| Listeners | **8080** WriteFreely; **8081** `/healthz` only, meant to stay unpublished; Gopher at `gopher_port` when set; **nothing else**, TCP or UDP |
| tmpfs required | exactly one: `/tmp`, at **16 MiB**, `noexec,nosuid,nodev`, for SQLite's temporary files |
| Writable mounts | `/tmp`, `/data`, and `/etc/writefreely` when mounted read-write; on Podman, `--read-only-tmpfs=false` |
| Volume | **`/data`**: `writefreely.db`, `keys/`, and `config.ini` when none is mounted |
| Outbound network | **none at rest**. Federation, email, OAuth and `update_checks` connect only when used or configured |
| Shell, package manager | **none** |
| Processes | **one**: `divbast-writefreely serve`, as PID 1 |
| Healthy | the shipped probe, `divbast-writefreely health`: `/healthz` on 8081 **200** and `GET /login` on 8080 **200**, and a well-formed menu line from Gopher's root selector when Gopher is on |
| Engine healthcheck | Docker runs it automatically. **Podman does not** — pass `--health-cmd` yourself |

- **Podman never sees this image's `HEALTHCHECK`.** Its `libimage` reads a
  healthcheck only from a Docker-media-type manifest; divbast images
  publish as an OCI index. Podman users pass
  `--health-cmd '["/usr/local/bin/divbast-writefreely","health"]'`, with
  no leading `"CMD"`, which Podman before 5.8.0 re-splits into one token.
  The contract suite checks the image's `HEALTHCHECK` is that array in
  exec form, and has each engine judge the container
- **The probe asserts two halves, and a third with Gopher on; none
  implies another.** `/healthz` checks the database answers, its schema
  is at the image's migration and every key file reads, but never goes
  through 8080's routes. `GET /login` renders for any visitor on public,
  private, single- and multi-user instances alike, where `/` redirects on
  some. When the working `config.ini` sets `gopher_port` and the
  instance is not private (a private one serves no Gopher), the probe
  sends `127.0.0.1:<gopher_port>` the root selector and reads only the
  answer's first line, which must be a well-formed menu line, an item or
  the closing `.`: a single-user blog's root menu lists every post, so a
  large blog's whole menu could outrun the read limit or the 4 s. Of
  `config.ini` it reads those two keys alone, with go-ini, never
  `config.Load`, which would also read every secret's `_FILE`, so a
  secret unreadable after the start never fails a health check. Each half
  has 4 s, so the `HEALTHCHECK`'s 15 s timeout clears all three. It
  prints nothing when healthy, and a `<check>: <reason>` line per failed
  half
- **One process, and it stops when told.** The binary bootstraps
  in-process and then calls WriteFreely's own `Serve`, which handles
  SIGTERM and SIGINT, drains in-flight requests for up to 8 s, closes the
  database and exits 0; `TestStopsPromptly` fails on a stop that takes
  longer than 5 s, or any other exit. No supervisor, no exec, no second long-running process
- **Nothing may add a shell or a package manager to the final image**, or
  a file capability to the binary: a binary carrying one cannot be exec'd
  under `--cap-drop ALL`, and the contract's `file-capability` check runs
  `divbast-writefreely version` to prove it
- **The application is root-owned and never written**: the binary and
  upstream's trees under `/usr/local/share/writefreely`. Everything the
  app writes lives on `/data`, or `/tmp`
- **Updates come through Renovate**, the fork's build included, which
  merges its own pull requests on green while `WRITEFREELY_AUTOMERGE` is
  `enabled`. Three things move by hand, in one pull request, when the
  fork moves to a new upstream release: `WRITEFREELY_VERSION`,
  `app/go.mod`'s require of `github.com/writefreely/writefreely`, and the
  acceptance suite's fork-build pattern `^0\.17\.3-divbast\.\d+$` in
  `test/acceptance/serves_test.go`. Renovate looks up none of them. A merge
  never publishes: `build.yml`'s daily slot and its button do, through
  divbast/ci's `app-publish.yml`. `stall.yml` fails each day that updates
  have stopped flowing

## The bootstrap

`serve` takes whatever `/data` holds to a WriteFreely ready to serve,
before it serves. Every refusal exits 1 with one line per problem,
naming what to change.

- **Build from the fork, and patch nothing here.** The image builds from
  divbast's fork of WriteFreely,
  [`aryonoco/writefreely`, branch `divbast`](https://github.com/aryonoco/writefreely/tree/divbast),
  a module dependency used through its exported API and its `Apper`
  seam, until upstream has the fixes. A fix to WriteFreely itself lands
  in the fork, never as a patch in this repository. A fork build that
  changes that API fails its bump PR's build, red by design
- **The working file** is `/etc/writefreely/config.ini` when mounted, and
  otherwise `/data/config.ini`, written once from upstream's
  `config.New()` on SQLite. A mounted `/etc/writefreely` without
  `config.ini` is refused, never taken for the trial
- **The image owns** `bind`, `port`, the three asset dirs,
  `keys_parent_dir` and the SQLite `filename`: an unset key takes the
  image's value, and any other value is refused, every conflict in one
  refusal. A host that is not a URL with its scheme is refused, since
  `config.Load` turns it into `://` and a save writes that back. TLS,
  autocert and a Gopher port below 1024 or on 8080 or 8081 are refused
- **Admin-panel saves go through the image's `Apper`.** `SaveConfig`
  writes as `config.Save` does, through go-ini in place, but leaves the
  file's mode alone: `config.Save`'s chmod to 0600 fails on a mounted file
  uid 65532 can write but does not own. With a read-only mount, the
  fork shows the settings form read-only, with a notice saying why, and
  applies a setting only once it is written
- **Read first, then write.** The bootstrap reads the database's state
  and makes each of its own refusals (a database ahead of the image,
  lost keys, a first admin it cannot create) before it writes a key
  file, the schema or a user. A refused first start can still leave
  `/data/config.ini`, an empty `keys/` and an empty database; the
  checks in WriteFreely's own `Initialize`, such as both SMTP TLS
  settings or a malformed `trusted_proxies`, refuse only after the
  bootstrap has written all three
- **Key files are written here**, with upstream's `key.GenerateBytes`, at
  0600, never by `GenerateKeyFiles`: it and `Initialize` each prefix the
  key paths, so both in one process double them. A key that exists is
  never touched. A missing key beside a database with users is refused,
  naming the files and saying to restore `/data/keys` with the database:
  new keys would end every session and could not decrypt what the old
  ones encrypted
- **The schema**: an empty database gets `CreateSchema`, one behind the
  image's migration `Migrate`, and one ahead is refused, naming both
- **The first admin**, only while there are no users, decided before the
  schema is touched: `WRITEFREELY_ADMIN_USER` and a password together,
  from one, and only one, of `WRITEFREELY_ADMIN_PASSWORD`,
  `WRITEFREELY_ADMIN_PASSWORD_FILE` and
  `/run/secrets/writefreely_admin_password` (a `_FILE` naming that
  secret itself, by any path or link, counts once), at most 72 bytes,
  bcrypt's limit, which `CreateUser` would check only after the schema; an
  unreadable file's refusal names uid 65532 and `uid=65532` or
  `mode=0444`. With neither and no config mounted, the trial's `writer`,
  with a random password printed once; a mounted config with no admin to
  create is refused. `admin`, `reader` and other names are reserved
  upstream
- **`user create [--admin] <name>`** reads the password at the terminal,
  through promptui as upstream's `reset-pass` does, or one line of stdin
  when stdin is not a terminal; `<name>:<password>` is refused, exit 2.
  `--admin` beside an admin, or a user before there is one, is refused
  before the password is read, in the image's own words: upstream's
  refusal names the `<name>:<password>` form
- **Upstream's library exits the process** on some database failures, so
  the bootstrap connects and reads the database's state itself first, and
  words those failures before upstream's code runs

## Two Go modules

- **`app/`** is divbast's binary: `github.com/divbast/writefreely-oci-image/app`,
  requiring WriteFreely at v0.17.3, the release the fork's version builds
  on, with its own `go.sum`, and a `replace` of it with the fork's build,
  `github.com/aryonoco/writefreely v0.17.3-divbast.N`: upstream's
  development branch after 0.17.3, plus fixes this image relies on. Renovate's
  `writefreely-fork` group moves that `replace` and the Containerfile's
  `WRITEFREELY_FORK_VERSION` in one PR, tidied, and the build fails unless
  they match. Its packages are `cmd/divbast-writefreely` and
  `internal/{conf,wfapp,keys,dbstate,bootstrap,health}`
- **The root module** holds the acceptance suite and requires
  `github.com/divbast/ci` through `replace … => ./.ci`, as every caller of
  ci does
- divbast's Go imports the standard library and WriteFreely's packages.
  Anything else carries its justification in `app/go.mod`: go-ini,
  gorilla/mux, promptui and go-isatty do

## Where the shared parts live

- **divbast/ci.** `just setup` checks it out into `.ci/`, which git and
  the build context both ignore, at the one commit every workflow calls it
  at: a release's full SHA, with its tag as the comment. Its README is the
  reference for all of it: the reusable workflows `app-check.yml`,
  `app-publish.yml`, `renovate.yml` and `stall.yml`; the recipes the
  justfile imports; the Go tools `just ci-tools` builds; the acceptance
  `harness` and the `contract` suite; and `go/golangci.yml`. A change to
  ci reaches this repository only through the Renovate PR that moves every
  pin of it at once
- **divbast/renovate-config** holds the presets `renovate.json` extends;
  `renovate.json` adds the fork's regex manager and its groups
- **divbast/infra** holds the org's settings, this repository's rulesets,
  the `divbast-renovate` App, the `renovate` environment and
  `WRITEFREELY_AUTOMERGE`

## Key commands

- `just setup` — fetch divbast/ci into `.ci/` at the commit the workflows
  pin, then install the pinned toolchain
- `just build` — build the image locally, after `docker login dhi.io`,
  without verifying the images it builds from; `just ci-tools &&
  .ci/bin/divbast-verify-images` does
- `just acceptance` — the acceptance suite against the image `just build`
  wrote, under `ENGINE`, through `divbast-testcount`, which fails a run
  unless a test passed and none failed or skipped.
  `ENGINE=docker IMAGE=localhost/writefreely:dev mise exec -- go test
  -run TestServes ./test/acceptance/` runs one test
- `just test` — build, then the acceptance suite
- `just app-go` — ci's Go gates and tests in `app/`, from the recipes in
  `.ci`
- `just lint` — every lint gate, `app-go` among them; needs a running
  Docker daemon, for `docker build --check`
- `just ci` — every gate, the scan included. Run it before committing

## Where the toolchain comes from

- **`mise.toml` is where tool versions are written**, and every recipe but
  `setup` runs through `mise exec`. The recipes that call GitHub take a
  fine-grained token from `GITHUB_TOKEN` and nothing else
- **Go builds with the toolchain mise installs**: `GOTOOLCHAIN=local`, and
  `GOFLAGS=-mod=readonly -tags=sqlite,netgo,osusergo`, so lint and tests
  build the image's variant: WriteFreely's SQLite exists only under
  `sqlite`. The race detector's CGO is on the runners
- **The CSS toolchain** is `css/package.json` and its lockfile, which pin
  `less` and `less-plugin-clean-css`; the build's `css` stage runs
  `npm ci` from them. JSON files carry `.license` sidecars
- **Every image the Containerfile builds from is a Docker Hardened Image**,
  pinned as `<name>:<tag>@sha256:<digest>`: the runtime `dhi.io/static`,
  whose os-release says `ID="debian"`, so the scan stays on Trivy; the
  `go` stage's `dhi.io/golang` `-dev`, which has the gcc and static glibc
  the CGO SQLite build links against; and the `css` and `prose` stages'
  `dhi.io/node` `-dev`, where the `prose` stage builds the editor's bundle
  from the fork's `prose/` source. Never add a stage from another
  publisher: before `app-check.yml` builds anything,
  `divbast-verify-images` verifies Docker's signature on each, with cosign
  from `mise.toml`
- **dhi.io serves nothing without a login.** A local `just build` or
  `just lint` needs `docker login dhi.io`; `divbast-verify-images` reads
  the login from `DHI_USERNAME` and `DHI_TOKEN`, which `build.yml` and
  `renovate.yml` pass to ci from the repository's settings. Under Docker
  the acceptance suite needs `zstd` on `PATH`, since every layer of a
  Docker Hardened Image is zstd

## Code quality

- All linter gates are enforced as errors — fix them, don't suppress them. Any
  suppression, anywhere, needs an explicit, comment-level justification beside
  it to be accepted
- **The acceptance suite is Go, on divbast/ci's harness**, and every
  container it starts runs under `harness.Contract("16m")`, the exact
  confinement the contract claims. `TestContract` runs the contract once
  per configuration: the trial, a read-only multi-user `config.ini` with
  the admin's secret, and Gopher. The rest is the image's own, one topic
  per file
- **Unit tests under `app/`** use real SQLite in temporary directories.
  WriteFreely keeps package-level state, so tests that touch its database
  do not run in parallel
- A test suite that has never failed proves nothing. When adding a check, prove
  it can fail before trusting it
- REUSE-compliant SPDX headers on every file; the licence is
  **AGPL-3.0-only**
