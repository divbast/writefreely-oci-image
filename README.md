<!-- SPDX-License-Identifier: AGPL-3.0-only -->
<!-- SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee> -->

# writefreely-oci-image

A security hardened [WriteFreely](https://github.com/writefreely/writefreely)
image: federated blogging, at zero Linux capabilities, with no new
privileges, on a read-only root filesystem as a non-root user.

Published as `ghcr.io/divbast/writefreely`.

## Base

The image is one static Go binary, `/usr/local/bin/divbast-writefreely`,
on [distroless](https://github.com/GoogleContainerTools/distroless)'s
`static-debian13`, which holds nothing else but CA certificates, time
zones and the `nonroot` user. The binary is divbast's own `main` with
WriteFreely linked in as a library, from the WriteFreely release
`app/go.mod` pins, so no upstream file is patched. Beside it are
upstream's templates, pages and static files from that same release; its
stylesheets, compiled from its LESS by a pinned `lessc`; and its editor's
bundle, `prose.bundle.js`, taken from the release's tarball and checked
against a pinned sha256. Every image the build uses is pinned by digest,
and CI builds nothing until it has checked that the distroless project
signed the runtime's digest.

## Container Features

| Property | Value |
|---|---|
| Capabilities | none (`--cap-drop ALL`) |
| Privileges | no new ones (`--security-opt no-new-privileges`), under the engine's seccomp filter |
| Root filesystem | read-only |
| User | uid/gid 65532 (`nonroot`) |
| Port | 8080 (WriteFreely), 8081 (health, do not publish), and Gopher's `gopher_port` when it is set; nothing else listens |
| tmpfs required | `/tmp`, 16 MiB, which nothing can run from (`--tmpfs /tmp:size=16m,noexec,nosuid,nodev`) |
| Volume required | `/data`: the database, the key files, and `config.ini` when none is mounted |
| Processes | one: `divbast-writefreely serve`, as PID 1 |
| Shell in image | none |
| Package manager | none |
| Outbound network | none at rest. Federation, email, OAuth and update checks connect out only when used or configured |
| Healthy | the shipped probe: `/healthz` 200 **and** `GET /login` 200 |

## Running

```bash
podman run -d --name writefreely \
  --cap-drop ALL --read-only --read-only-tmpfs=false \
  --security-opt no-new-privileges \
  --user 65532:65532 \
  --tmpfs /tmp:size=16m,noexec,nosuid,nodev \
  -v writefreely-data:/data \
  -p 8080:8080 \
  --health-cmd '["/usr/local/bin/divbast-writefreely","health"]' \
  ghcr.io/divbast/writefreely:0.17.3
```

`--read-only-tmpfs=false` and `--health-cmd` are Podman's alone, as
[Health](#health) says. Docker takes the same command without them.

`:0.17.3` names the WriteFreely version, and `:latest` the newest image.
Each new image of a version moves that version's tag. The
`:<version>-<arch>` and `:<version>-staging` tags are the publish's
working tags, not for use.

Under a Quadlet, the equivalents of the confinement, the tmpfs and the
health check are:

```
DropCapability=all
ReadOnly=true
ReadOnlyTmpfs=false
NoNewPrivileges=true
User=65532
Group=65532
Tmpfs=/tmp:size=16m,noexec,nosuid,nodev
HealthCmd=["/usr/local/bin/divbast-writefreely","health"]
```

Every start bootstraps `/data` before WriteFreely serves: it writes any
missing key file, creates the schema in an empty database, migrates one
an older image left behind, and creates the first admin. A start it
cannot honour exits 1 with a line naming what to change, and serves
nothing.

### The trial

With nothing but an empty volume, as above, the image runs a single-user
blog on WriteFreely's own defaults, and creates its admin, `writer`, with
a random password it prints once, to the container's log:

```
divbast-writefreely: created the admin "writer" with this one-time password:
divbast-writefreely:     QSWD4K2ZCHEOH5BPW3X6NFLM7A
divbast-writefreely: it is shown once; log in and change it in your settings
```

`podman logs writefreely` shows it. Log in at `/login` as `writer`, and
change the password under your settings: once a user exists, nothing is
printed again. WriteFreely's own `config.ini` for the trial is written to
`/data/config.ini`.

### Production

Mount a directory holding your `config.ini` at `/etc/writefreely`, and give
the first admin's name and password:

```bash
podman run -d --name writefreely \
  --cap-drop ALL --read-only --read-only-tmpfs=false \
  --security-opt no-new-privileges \
  --user 65532:65532 \
  --tmpfs /tmp:size=16m,noexec,nosuid,nodev \
  -v writefreely-data:/data \
  -v /srv/writefreely/etc:/etc/writefreely:ro \
  -v /srv/writefreely/secrets:/run/secrets:ro \
  -e WRITEFREELY_ADMIN_USER=ada \
  -p 127.0.0.1:8080:8080 \
  --health-cmd '["/usr/local/bin/divbast-writefreely","health"]' \
  ghcr.io/divbast/writefreely:0.17.3
```

The password is the file `writefreely_admin_password` in the secrets
directory; a newline at its end is not part of it. The admin is created
only while the database has no users, so the name and the password have
no effect after the first start, and are needed only for it. Give both or
neither: one without the other is refused, and so is a mounted
`config.ini` with no users and no admin to create. A mounted
`/etc/writefreely` with no `config.ini` in it is refused too, rather than
taken for the trial.

`config.ini` is WriteFreely's own, as
[its documentation](https://writefreely.org/docs/latest/admin/config)
describes, and every setting in it is yours but these, which the image
owns. Leave them out, and each takes the image's value; any other value
is refused, every conflict at once, a line each:

| Key | The image's value |
|---|---|
| `[server] bind` | `0.0.0.0` |
| `[server] port` | `8080` |
| `[server] templates_parent_dir`, `static_parent_dir`, `pages_parent_dir` | `/usr/local/share/writefreely` |
| `[server] keys_parent_dir` | `/data` |
| `[database] filename`, when `type = sqlite3` | `/data/writefreely.db` |

`[app] host` must be the blog's public URL with its scheme, such as
`https://blog.example.com`, and a `config.ini` without one is refused.
`tls_cert_path`, `tls_key_path` and `autocert` are refused too: TLS is the
reverse proxy's job. `type = mysql` is allowed, untested here, and needs
the network the image otherwise does without.

**The admin panel's settings** are saved as WriteFreely saves them, into
the `config.ini` in use, with the image's keys written at the image's
values:

- with no `config.ini` mounted, into `/data/config.ini`, so they persist
  with the volume;
- with the directory mounted read-write, and `config.ini` writable by uid
  65532, into your file, which WriteFreely rewrites whole, so its comments
  are lost;
- with it mounted read-only, as above, the save fails and the panel says
  why. The change holds until the next restart, and is gone after it.

### Gopher

WriteFreely's Gopher server listens on `[server] gopher_port`, never on a
private instance. The port must be 1024 or above, since the image holds no
capability to bind a lower one, and neither 8080 nor 8081. Publish it on
Gopher's own port with `-p 70:7070` for `gopher_port = 7070`.

### Reverse proxy and TLS

Put a reverse proxy in front, terminating TLS, and send it to 8080 over
plain HTTP. `[app] host` is the URL the proxy serves, and WriteFreely
builds every link and every federated identity from it, so it must be
right before the blog federates.

### The operator's commands

`divbast-writefreely user` manages users through the running container's
`config.ini`, so the database's path and the image's keys are always the
ones in force:

```bash
podman exec writefreely divbast-writefreely user create grace:a-long-password
podman exec -it writefreely divbast-writefreely user reset-pass grace
podman exec -it writefreely divbast-writefreely user delete grace
```

`user create --admin` creates the admin, while there is none. `reset-pass`
and `delete` ask at the terminal, as WriteFreely's own commands do, so
they need `-it`. Before the first start there is nothing to manage, and
`user` says to start the server first.

### Upgrades and backups

A newer image on an existing `/data` runs WriteFreely's migrations before
it serves. **Back up `/data` first.** An image refuses a database a newer
image migrated, naming both versions, rather than run older code on it.
To choose when your `/data` migrates, pin a version tag such as `:0.17.3`
rather than `:latest`, and back up before you move it.

The database, the key files and the trial's `config.ini` are one unit,
and the keys cannot be made again: without them, sessions end and
encrypted data is lost. To back up, stop the container, copy everything
in `/data`, and start it again:

```bash
podman stop writefreely
podman volume export writefreely-data --output writefreely-data.tar
podman start writefreely
```

## Health

A server that answers can still be broken, so the image ships its own probe
and declares a `HEALTHCHECK`. Docker runs it with no configuration.

Podman does not. Podman's `libimage` reads a healthcheck only out of a
Docker-media-type manifest, and this image is published as an OCI index, so
Podman's inspection path never looks at the config's healthcheck field —
[podman/podman#25454](https://github.com/containers/podman/issues/25454)
and [#18904](https://github.com/containers/podman/issues/18904) track it.
A Podman user passes it explicitly, with `--health-cmd` as in
[Running](#running), or a Quadlet's `HealthCmd=`. The array has no leading
`"CMD"` on purpose: Podman before 5.8.0 re-splits an array that starts with
`"CMD"` into one useless token.

| | |
|---|---|
| `/usr/local/bin/divbast-writefreely health` | The in-container probe `HEALTHCHECK` runs. Asserts `/healthz` is 200 **and** `GET /login` on 8080 is 200, which holds on public, private, single- and multi-user instances alike. Prints nothing when healthy; otherwise one line, `<check>: <reason>`, for each half that failed, and exits 1 |
| `http://127.0.0.1:8081/healthz` | 200 with no body, or 503 with one `<check>: <reason>` line per failure: `database`, which must answer a ping, `schema`, which must be at the image's migration, and `keys`, every key file readable |

Port 8081 serves nothing else and is meant to stay unpublished. On 8080,
`/healthz` is WriteFreely's 404, so a health check whose port is mistyped
fails rather than reporting a false healthy.

There is no shell in this image, so a health command given as a plain
string, which Docker and Podman both run through `/bin/sh -c`, can never
pass. Compose's `healthcheck.test` wants the array with a leading `CMD`:

    test: ["CMD", "/usr/local/bin/divbast-writefreely", "health"]

A Kubernetes `exec` probe takes the argv alone:

    exec:
      command: ["/usr/local/bin/divbast-writefreely", "health"]

The engine records why a check failed:

    docker inspect --format '{{json .State.Health}}' <container>

## Dependency updates

Every input the Containerfile names is pinned: the `golang`, `node` and
distroless images by digest, WriteFreely by version, by its release tag
and by its tarball's sha256, `lessc` and its clean-css plugin by
`css/package-lock.json`, and every Go module by `app/go.sum`.

A pin only moves through a pull request, and
[Renovate](https://docs.renovatebot.com/) opens those. It runs every
morning, after every merge to `main`, whenever CI finishes on one of its
branches and when the Dependency Dashboard issue is edited, from
`.github/workflows/renovate.yml`, which calls divbast/ci's shared Renovate
workflow, as the divbast org's `divbast-renovate` GitHub App. Its settings
are divbast/renovate-config's shared presets, and `renovate.json` adds
WriteFreely's managers and groups. The pull requests Renovate opens here
are:

- A WriteFreely release arrives as one pull request that moves the version,
  the tag and the tarball's sha256 in the Containerfile, and `app/go.mod`
  and `app/go.sum`, together: the build fails unless they name the same
  release.
- The other Go modules' minor and patch releases arrive grouped in one pull
  request, which tidies `app/go.mod`; a major arrives on its own.
- A new Go or Node image, or a new distroless digest, arrives as a pull
  request that moves its pin, and CI verifies the distroless signature
  before it builds. `lessc` and its plugin move by pull requests of their
  own.
- A release of divbast/ci arrives as one pull request that moves every
  workflow's pin of it together.

Renovate merges its own pull requests once every check on one has passed,
majors and WriteFreely releases included, while the repository variable
`WRITEFREELY_AUTOMERGE` is `enabled`. Two kinds first wait for someone to
approve them on the Dependency Dashboard: a new major version of
divbast/ci, and a raise of a `go.mod`'s `go` line. Any other value of the
variable, or none, stops merging from the next run, and Renovate goes on
opening and rebasing pull requests for a person to merge:

    gh variable set WRITEFREELY_AUTOMERGE --body disabled

After a merge it rebases every open update the merge left behind, so each is
tested again against the new `main` before it merges. It merges with a
squash, which GitHub signs, because `main` accepts only signed commits.

**At most one new image a day.** A merge does not publish. The daily `build`
run publishes `main` if anything merged since the last image, so however many
updates land in a day, users see one new image at most. An urgent fix need not
wait: a manual run of the `build` workflow from `main` (Actions, build, Run
workflow) publishes at once. Neither ever replaces `:latest` with an image of
an older commit.

A publish pushes each architecture, merges them into an index under
`:<version>-staging`, attests it and verifies the attestation, and only then
moves `:<version>` and `:latest`, before checking both as an anonymous client
pulls them. If the attestation or its verification fails, `:<version>` and
`:latest` stay where they were.

A daily stall check, `.github/workflows/stall.yml`, which calls divbast/ci's
shared one, fails, and GitHub emails you, when a Renovate pull request has
been open more than 14 days or red or pending for more than a day, when
Renovate has not succeeded in a day, or when two daily runs should have
published and did not.

[divbast/infra](https://github.com/divbast/infra) manages the `renovate`
environment, the App's client ID and private key in it,
`WRITEFREELY_AUTOMERGE` and this repository's rulesets. It creates
`WRITEFREELY_AUTOMERGE` and then leaves its value alone, so an apply never
turns merging back on.

## Verifying the image

Every image published from `main` carries an attestation of where it was
built, signed with Sigstore. With [gh](https://cli.github.com/) 2.102.0 or
newer:

    gh attestation verify oci://ghcr.io/divbast/writefreely:latest --repo divbast/writefreely-oci-image --signer-workflow divbast/ci/.github/workflows/app-publish.yml --source-ref refs/heads/main

`--repo` and `--source-ref` say the image was built from this repository's
`main` branch, and `--signer-workflow` that divbast/ci's publish workflow,
which this repository's `build` workflow calls, signed it. Each
attestation is on the index that `:<version>` and `:latest` name, so the
per-architecture tags, which are not for use, verify with none.

If gh reports that its token was denied, for an image anyone may pull, it
is sending a registry login from Docker's configuration, which GHCR
refuses. Point `DOCKER_CONFIG` at an empty directory for the command.

## Building it yourself

With [mise](https://mise.jdx.dev/) and Docker: `just setup` checks
divbast/ci out into `.ci/` at the commit the workflows pin and installs
the toolchain, and `just ci` runs every gate, the image build included.

- `just build` builds the image locally, without verifying the images it
  builds from; `just ci-tools && .ci/bin/divbast-verify-images` does, with
  cosign from `mise.toml`.
- `just test` builds, then runs the acceptance suite against the image,
  under `ENGINE`, docker unless it says podman.
- `just lint` runs every lint gate, `app-go` among them, which runs ci's Go
  gates and tests in `app/`, and needs a running Docker daemon, for
  `docker build --check`.
- `just scan` reads the published image with
  [skopeo](https://github.com/containers/skopeo), which must be on `PATH`.

The lint gates that call GitHub read a fine-grained personal access token
from `GITHUB_TOKEN` and nowhere else; divbast/ci's README says which, and
how to pass it, under
[Local GitHub token](https://github.com/divbast/ci#local-github-token).

## AI/LLM Disclosure

This project was developed with significant LLM involvement. Each git commit contains an `Assisted-by:` tag detailing the particular model/tool used.
