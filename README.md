<!-- SPDX-License-Identifier: AGPL-3.0-only -->
<!-- SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee> -->

# writefreely-oci-image

A security hardened [WriteFreely](https://github.com/writefreely/writefreely)
image: federated blogging, at zero Linux capabilities, with no new
privileges, on a read-only root filesystem as a non-root user.

Published as `ghcr.io/divbast/writefreely`.

## Base

The image is one static Go binary, `/usr/local/bin/divbast-writefreely`,
on Docker Hardened Images'
[`static`](https://hub.docker.com/hardened-images/catalog/dhi/static) for Debian 13,
which holds nothing else but CA certificates, time zones and the
`nonroot` user: no shell and no package manager. The binary is divbast's
own `main` with WriteFreely linked in as a library. It builds from
divbast's fork of WriteFreely,
[`aryonoco/writefreely`, branch `divbast`](https://github.com/aryonoco/writefreely/tree/divbast),
at the build `app/go.mod` pins: upstream WriteFreely's development
branch after the 0.17.3 release, plus fixes this image relies on. The
image uses it through its exported API and its `Apper` seam. Building
from upstream WriteFreely again would mean dropping `app/go.mod`'s
replace, and with it the build's two checks that it takes nothing from
WriteFreely's own repository. Beside the binary are the fork's
templates, pages and static files from that same build; its stylesheets,
compiled from its LESS by a pinned `lessc`; and the editor's bundle,
`prose.bundle.js`, built in the image's `node` `-dev` stage from the
fork's `prose/` source, with the packages its lockfile pins from the npm
registry and no install script run. No part of the image comes from
WriteFreely's own repository: the build fails if it fetches upstream's
module. Every stage of the build is also a Docker Hardened Image: the
binary is built on DHI's `golang` `-dev`, and the stylesheets and the
editor's bundle on its `node` `-dev`. Every image the build uses is
pinned by digest, and CI builds nothing until it has verified Docker's
signature on each.

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
| Healthy | the shipped probe: `/healthz` 200 **and** `GET /login` 200, **and** Gopher's root menu when Gopher is on |

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

`:0.17.3` names the WriteFreely release the fork's version builds on,
`:0.17.3-divbast.N` the build of divbast's fork the image links, and
`:latest` the newest image. `:0.17.3` and `:0.17.3-divbast.N` both move with the
image's own dependency updates: each new image moves them. A digest,
`ghcr.io/divbast/writefreely@sha256:…`, is the only reference that never
moves. The index's version annotation says the release, `0.17.3`, while
the image's `org.opencontainers.image.version` label and
`divbast-writefreely version` say the fork build. The `:<version>-<arch>`
and `:<version>-staging` tags are the publish's working tags, not for
use.

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

Every start bootstraps `/data` before WriteFreely serves: on a new
volume it writes the key files, it creates the schema in an empty
database, migrates one an older image left behind, and creates the first
admin. A start it cannot honour exits 1 with a line naming what to
change, and serves nothing. The bootstrap reads the database first, and
makes each of its own refusals before it writes a key file, the schema
or a user; a refused first start can still leave `/data/config.ini`, an
empty `/data/keys/` and an empty database behind. WriteFreely's own
checks, such as both SMTP TLS settings or a malformed
`trusted_proxies`, refuse the start only after the bootstrap has written
all three.

On SIGTERM, which `podman stop` and `docker stop` send, WriteFreely stops
taking requests, lets those in flight finish for up to 8 s, closes the
database and exits 0, inside the engines' default 10 s.

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

The admin's password comes from one of these, and only one:

- the file `writefreely_admin_password` in the secrets directory, as above;
- `WRITEFREELY_ADMIN_PASSWORD_FILE`, naming a file anywhere in the
  container;
- `WRITEFREELY_ADMIN_PASSWORD`, holding the password itself, which then
  sits in the container's environment for anyone who can inspect it.

A newline at the end of a file is not part of the password, and an empty
variable counts as unset; a `WRITEFREELY_ADMIN_PASSWORD_FILE` naming the
mounted secret itself is one source. More than one source is refused, and
so is a password over 72 bytes, the most WriteFreely takes, before the
bootstrap writes a key file, the schema or a user. The admin is created
only while the database has no users, so the name and the password have
no effect after the first start, and are needed only for it. Give both or
neither: one without the other is refused, and so is a mounted
`config.ini` with no users and no admin to create. A mounted
`/etc/writefreely` with no `config.ini` in it is refused too, rather than
taken for the trial.

The admin's name is also the blog's handle on the fediverse,
`@name@host`, which other servers keep: it is public, and cannot change
once the blog federates.

**Every file the image reads must be readable by uid 65532**, its user:
`config.ini`, the password file and any secret file below. On the host,
`chmod 0444` the file in a directory others may enter (`chmod 0755`), or
`chown 65532` it. A Podman secret takes `uid=65532` or `mode=0444`, as in
`--secret writefreely_admin_password,uid=65532`, which mounts it at
`/run/secrets/writefreely_admin_password`. An unreadable password file is
refused, naming the file and uid 65532.

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
- with it mounted read-only, as above, the settings form is shown
  read-only, with a notice saying why, and a save changes nothing.

A setting changes only once the file is written: a save that fails
leaves the running instance as it was. A secret given through the
environment, below, is never written into the file by a save.

### Secrets from the environment

Each of these secrets can come from a variable instead of `config.ini`,
or from a file the variable's `_FILE` twin names, and either takes the
place of the file's value:

| Secret | Variable, and its `_FILE` twin | `config.ini` key |
|---|---|---|
| SMTP password | `WRITEFREELY_SMTP_PASSWORD` | `[email] smtp_password` |
| Mailgun private key | `WRITEFREELY_MAILGUN_PRIVATE` | `[email] mailgun_private` |
| Slack OAuth client secret | `WRITEFREELY_OAUTH_SLACK_CLIENT_SECRET` | `[oauth.slack] client_secret` |
| Write.as OAuth client secret | `WRITEFREELY_OAUTH_WRITEAS_CLIENT_SECRET` | `[oauth.writeas] client_secret` |
| GitLab OAuth client secret | `WRITEFREELY_OAUTH_GITLAB_CLIENT_SECRET` | `[oauth.gitlab] client_secret` |
| Gitea OAuth client secret | `WRITEFREELY_OAUTH_GITEA_CLIENT_SECRET` | `[oauth.gitea] client_secret` |
| Generic OAuth client secret | `WRITEFREELY_OAUTH_GENERIC_CLIENT_SECRET` | `[oauth.generic] client_secret` |

`WRITEFREELY_SMTP_PASSWORD_FILE=/run/secrets/smtp_password`, for
instance, reads the SMTP password from that file, without one trailing
newline. An empty variable counts as unset. Setting a variable and its
`_FILE` twin both is refused at start, and an admin-panel save never
writes a secret from the environment back into `config.ini`.

### Email

`[email] smtp_enable_tls = true` sends mail over implicit TLS, with the
server's certificate verified, as port 465 expects; with
`smtp_port = 465` and neither it nor `smtp_enable_start_tls` set,
implicit TLS is used. Setting both `smtp_enable_tls` and
`smtp_enable_start_tls` is refused at start.

### Gopher

WriteFreely's Gopher server listens on `[server] gopher_port`, never on a
private instance. The port must be 1024 or above, since the image holds no
capability to bind a lower one, and neither 8080 nor 8081. Every link in
its menus carries `[server] gopher_advertised_port`, which is
`gopher_port` unless set. To serve Gopher on its own port, 70, listen
above 1024, publish that port as 70 with `-p 70:7070`, and advertise 70:

```ini
[server]
gopher_port = 7070
gopher_advertised_port = 70
```

A Gopher port that cannot be bound stops the start, naming the port,
rather than leaving the web serving alone. Private and password-protected
blogs are neither listed nor served over Gopher. Each connection has 10 s
to send its selector and 120 s for the whole exchange, and at most 64 are
served at once. With Gopher on, the health probe checks it too.

### Reverse proxy and TLS

Put a reverse proxy in front, terminating TLS, and send it to 8080 over
plain HTTP. `[app] host` is the URL the proxy serves, and WriteFreely
builds every link and every federated identity from it, so it must be
right before the blog federates.

**List the proxy in `[server] trusted_proxies`**, as IP addresses or CIDR
prefixes, such as the container network's. The fork also takes `unix`,
for a proxy that connects over a Unix socket, which has no effect here:
the image listens on TCP 8080, never on a socket. WriteFreely takes a
request from a listed proxy to come from the client its `X-Forwarded-For`
names. Left unset, every visitor seems to come from the proxy, and so
shares one login limit: after 10 failed logins from one client address in
15 minutes, that address's logins are refused for the rest of the window.
When an address reaches the limit, one line is logged, with a hint about
`trusted_proxies` when it is empty.

**Federation.** Every ActivityPub inbox request must carry a valid HTTP
signature, or it is refused. The signature is checked against
`[app] host`, but **the proxy must pass the original `Host` header**:
WriteFreely serves its instance actor and host-meta by it, and signs
every fetch it makes as that actor, the key lookups behind the inbox's
check included. With `Host` rewritten, servers that require signed
fetches, such as GoToSocial, or Mastodon with authorized fetch, cannot
verify the blog's fetches and refuse them, so the blog refuses their
deliveries for want of their keys, and lookups and mentions of their
users fail. Caddy and Traefik pass it by default, while nginx needs
`proxy_set_header Host $host;` and Apache `ProxyPreserveHost On` to pass it
through. `[server] inbox_max_bytes` caps an inbox request's body, at
1048576 bytes (1 MiB) unless set, and a larger one is answered 413. Remote
actor and WebFinger documents the blog fetches are limited to 1 MiB.

**Forms and the session cookie.** Every write made with the login cookie
carries a CSRF token, which WriteFreely's own pages send, and a write
without one is refused: a client of your own that writes with the session
cookie must send the token too. A page left open across an upgrade needs
a reload before its next save. Clients that authenticate with an API
token, in the `Authorization` header, are unaffected. Behind a reverse
proxy, a `Referrer-Policy` of `same-origin` or
`strict-origin-when-cross-origin` is safest; `no-referrer` works in
browsers that send `Sec-Fetch-Site`, which all current ones do.

### Accounts

- **Password reset by email is refused for admins**: WriteFreely sends
  no reset email for an admin's account. Reset an admin's password with
  `user reset-pass`, below.
- `[app] disable_password_auth = true` refuses every password login, the
  admin's included.
- `[app] open_registration = true` lets anyone create an account, even on
  a single-user instance.
- `[app] default_visibility` is read once for each blog, when it is
  created: changing it later leaves existing blogs as they are.

### The operator's commands

`divbast-writefreely user` manages users through the running container's
`config.ini`, so the database's path and the image's keys are always the
ones in force:

```bash
podman exec -it writefreely divbast-writefreely user create grace
printf '%s\n' "$password" | podman exec -i writefreely divbast-writefreely user create grace
podman exec -it writefreely divbast-writefreely user reset-pass grace
podman exec -it writefreely divbast-writefreely user delete grace
```

`user create` asks for the new user's password at the terminal, masked;
without a terminal, it reads one line of stdin instead, as the second
command does with `-i`. The password is never an argument, where `ps`
and the shell's history would show it, so the
`user create <name>:<password>` form of earlier images is refused, as a
wrong command line. `user create --admin` creates the admin, while there
is none, and `user create` without it needs the admin to exist; either
is refused before the password is asked for. `reset-pass` and `delete`
ask at the terminal, as WriteFreely's own commands do, so they need
`-it`. Before the first start there is nothing to manage, and `user`
says to start the server first.

### Upgrades and backups

A newer image on an existing `/data` runs WriteFreely's migrations before
it serves. **Back up `/data` first.** An image refuses a database a newer
image migrated, naming both versions, rather than run older code on it.
To choose when your `/data` migrates, pin a version tag such as `:0.17.3`
rather than `:latest`, and back up before you move it.

The database, the key files and the trial's `config.ini` are one unit,
and the keys cannot be made again: without them, sessions end and
encrypted data is lost. **Restore `/data/keys` with the database**: a
database with users beside a missing key file is refused at start,
naming the files, rather than given new keys. There is no flag to
override it: with `/data/keys` lost and no backup, the only way on is to
recreate each missing file yourself, as 32 random bytes owned by uid
65532 at mode 0600, which ends every session and, for `email.aes256`,
loses the email addresses stored for users and subscribers. To back up,
stop the container, copy everything in `/data`, and start it again:

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
| `/usr/local/bin/divbast-writefreely health` | The in-container probe `HEALTHCHECK` runs. Asserts `/healthz` is 200 **and** `GET /login` on 8080 is 200, which holds on public, private, single- and multi-user instances alike, **and**, when `config.ini` sets `gopher_port` on an instance that is not private, that Gopher starts its answer to the root selector with a well-formed menu line, an item or the lone `.` that ends the menu. It reads no further: a single-user blog's root menu lists every post, so a large blog's whole menu could outrun the check. Each check has 4 s, one after another, which the `HEALTHCHECK`'s 15 s timeout clears. Prints nothing when healthy; otherwise one line, `<check>: <reason>`, for each check that failed, `healthz`, `app` or `gopher`, and exits 1 |
| `http://127.0.0.1:8081/healthz` | 200 with no body, or 503 with one `<check>: <reason>` line per failure: `database`, which must answer a ping, `schema`, which must be at the image's migration, and `keys`, every key file readable |

Port 8081 serves nothing else and is meant to stay unpublished. On 8080,
`/healthz` is WriteFreely's 404, so a health check whose port is mistyped
fails rather than reporting a false healthy.

There is no shell in this image, so a health command given as a plain
string, which Docker and Podman both run through `/bin/sh -c`, can never
pass. Compose's `healthcheck.test` wants the array with a leading `CMD`:

    test: ["CMD", "/usr/local/bin/divbast-writefreely", "health"]

A Kubernetes `exec` probe takes the argv alone, and needs a timeout that
clears the probe's 12 s, since Kubernetes' default is 1 s:

    exec:
      command: ["/usr/local/bin/divbast-writefreely", "health"]
    timeoutSeconds: 15

The engine records why a check failed:

    docker inspect --format '{{json .State.Health}}' <container>

## Dependency updates

Every input the Containerfile names is pinned: the Docker Hardened
Images, `golang`, `node` and `static`, by digest, the WriteFreely release
the fork builds on by `WRITEFREELY_VERSION` and `app/go.mod`'s require,
the build of divbast's fork by `WRITEFREELY_FORK_VERSION` and
`app/go.mod`'s replace, the editor's packages by the fork's
`prose/package-lock.json`, `lessc` and its clean-css plugin by
`css/package-lock.json`, and every Go module by `app/go.sum`.

A pin only moves through a pull request. Three things move by hand, in
one pull request, when the fork moves to a new upstream release:
`WRITEFREELY_VERSION`, `app/go.mod`'s require of
`github.com/writefreely/writefreely`, and the acceptance suite's pattern
for a fork build, `^0\.17\.3-divbast\.\d+$` in
`test/acceptance/serves_test.go`. Renovate looks up none of them.
Everything else, the fork's build included, moves through pull requests
[Renovate](https://docs.renovatebot.com/) opens. It runs every
morning, after every merge to `main`, whenever CI finishes on one of its
branches and when the Dependency Dashboard issue is edited, from
`.github/workflows/renovate.yml`, which calls divbast/ci's shared Renovate
workflow, as the divbast org's `divbast-renovate` GitHub App. Its settings
are divbast/renovate-config's shared presets, and `renovate.json` adds
the fork's manager and this repository's groups. The pull requests
Renovate opens here are:

- A build of divbast's fork arrives as one `writefreely-fork` pull
  request that moves `WRITEFREELY_FORK_VERSION` in the Containerfile and
  `app/go.mod`'s replace, with `app/go.sum`, together: the build fails
  unless they name the same build. Upstream WriteFreely releases do not
  arrive here while the image is on the fork; they reach it through the
  fork.
- The other Go modules' minor and patch releases arrive grouped in one pull
  request, which tidies `app/go.mod`; a major arrives on its own, unless it
  moves a module only WriteFreely imports to a new path, which waits for
  a build of the fork that adopts it.
- A new Go, Node or `static` image, or a new digest of one, arrives as a
  pull request that moves its pin, and CI verifies Docker's signature on
  it before it builds. `lessc` and its plugin move by pull requests of
  their own.
- A release of divbast/ci arrives as one pull request that moves every
  workflow's pin of it together.

Renovate merges its own pull requests once every check on one has passed,
majors and fork builds included, while the repository variable
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
moves `:<version>`, the fork build's tag and `:latest`, before checking each
as an anonymous client pulls them. If the attestation or its verification
fails, all three stay where they were.

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
attestation is on the index that `:<version>`, the fork build's tag and
`:latest` name, so the per-architecture tags, which are not for use, verify
with none.

If gh reports that its token was denied, for an image anyone may pull, it
is sending a registry login from Docker's configuration, which GHCR
refuses. Point `DOCKER_CONFIG` at an empty directory for the command.

## Building it yourself

With [mise](https://mise.jdx.dev/) and Docker: `just setup` checks
divbast/ci out into `.ci/` at the commit the workflows pin and installs
the toolchain, and `just ci` runs every gate, the image build included.

- `just build` builds the image locally, without verifying the images it
  builds from, and needs `docker login dhi.io`, since dhi.io serves its
  images only to a Docker account; `just ci-tools &&
  .ci/bin/divbast-verify-images` does verify them, with cosign from
  `mise.toml`, and reads that login from `DHI_USERNAME` and `DHI_TOKEN`.
- `just test` builds, then runs the acceptance suite against the image,
  under `ENGINE`, docker unless it says podman. Under Docker it needs
  `zstd` on `PATH`, since every layer of a Docker Hardened Image is zstd
  and the suite reads the image's layers through it; no `mise.toml` entry
  provides it. CI runners already have it, and a Mac gets it with `brew
  install zstd`.
- `just lint` runs every lint gate, `app-go` among them, which runs ci's Go
  gates and tests in `app/`, and needs a running Docker daemon, for
  `docker build --check`, logged in to dhi.io.
- `just scan` reads the published image with
  [skopeo](https://github.com/containers/skopeo), which must be on `PATH`.

The lint gates that call GitHub read a fine-grained personal access token
from `GITHUB_TOKEN` and nowhere else; divbast/ci's README says which, and
how to pass it, under
[Local GitHub token](https://github.com/divbast/ci#local-github-token).

## AI/LLM Disclosure

This project was developed with significant LLM involvement. Each git commit contains an `Assisted-by:` tag detailing the particular model/tool used.
