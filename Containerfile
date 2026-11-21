# SPDX-License-Identifier: AGPL-3.0-only
# SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

# WriteFreely's release that divbast's fork builds on (the fork is
# upstream's development branch after it, plus fixes), with app/go.mod's
# require of github.com/writefreely/writefreely. The version tags the image:
# app-publish.yml reads it from this line, by the name build.yml gives as
# version-arg. It moves by hand when the fork moves to a new upstream
# release, together with the require and the acceptance suite's pattern for
# a fork build, ^0\.17\.3-divbast\.\d+$ in test/acceptance/serves_test.go;
# Renovate looks up none of the three. Everything else moves through
# Renovate, the fork's build below included.
#
# The fork's build, which app/go.mod's replace links and which names the
# image's version: what divbast-writefreely version prints and the version
# label say, and the extra tag app-publish.yml reads from this line, by the
# name build.yml gives as extra-tag-arg. Renovate moves it with the replace
# in one PR.
ARG WRITEFREELY_VERSION=0.17.3
ARG WRITEFREELY_FORK_VERSION=0.17.3-divbast.27
# Every image below is a Docker Hardened Image on Debian 13, pinned by
# digest, and CI verifies Docker's signature on each before anything is
# built.
#
# The newest stable Go, in Docker Hardened Images' -dev variant, which has
# the gcc and static glibc WriteFreely's CGO SQLite links against. Only the
# binary and the fork's trees, the editor's source among them, leave its
# stage.
ARG GO_IMAGE=dhi.io/golang:1.27.4-debian13-dev@sha256:c096bb340b7b9a205cb704c75d5ca877392f8aed70c818ae255b10bee1822b60
# Node's LTS, in the -dev variant, for compiling WriteFreely's LESS and
# building the editor's bundle. Only the CSS and the bundle leave its
# stages.
ARG NODE_IMAGE=dhi.io/node:26.13.1-debian13-dev@sha256:1e91de4150a2814738074722988b0d17e025a1f5fd615e5a678207466cfa9fb3
# The runtime: Docker Hardened Images' static, since the binary is static,
# with nothing in it but CA certificates, time zones and the nonroot user
# 65532: no shell and no package manager.
ARG RUNTIME_IMAGE=dhi.io/static:20250419-debian13@sha256:4749130e822c145dbdf9ca11ec0cc2f694b5fe687fd74268a2b62160e6726f39

# divbast's binary, with WriteFreely linked in as a library from the module
# app/go.mod pins, the fork's build its replace names, and the templates,
# pages, static files, LESS and the editor's source from that module's own
# tree.
FROM ${GO_IMAGE} AS go
ARG WRITEFREELY_VERSION
ARG WRITEFREELY_FORK_VERSION
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
ENV GOTOOLCHAIN=local GOFLAGS=-mod=readonly
WORKDIR /src
COPY app/go.mod app/go.sum ./
# The versions the image is tagged with must be the ones it links: a bump
# that moved the Containerfile's pins and not go.mod's, or the other way,
# fails here rather than ship a wrong tag. That holds for the release
# go.mod requires and for the fork build its replace names. The build
# takes WriteFreely only from the fork, so go.mod must replace the module
# with the fork's. The editor's source leaves with the module's trees,
# made writable, since the module cache is read-only and npm installs
# into it
RUN go mod download \
    && mod=$(go list -m -f '{{.Version}}' github.com/writefreely/writefreely) \
    && if [ "${mod}" != "v${WRITEFREELY_VERSION}" ]; then \
        echo "app/go.mod requires WriteFreely ${mod}; the Containerfile pins v${WRITEFREELY_VERSION}" >&2; \
        exit 1; \
    fi \
    && path=$(go list -m -f '{{if .Replace}}{{.Replace.Path}}{{end}}' github.com/writefreely/writefreely) \
    && if [ "${path}" != "github.com/aryonoco/writefreely" ]; then \
        echo "app/go.mod replaces WriteFreely with ${path:-nothing}; the build takes WriteFreely only from the fork, github.com/aryonoco/writefreely" >&2; \
        exit 1; \
    fi \
    && fork=$(go list -m -f '{{if .Replace}}{{.Replace.Version}}{{end}}' github.com/writefreely/writefreely) \
    && if [ "${fork}" != "v${WRITEFREELY_FORK_VERSION}" ]; then \
        echo "app/go.mod links the fork build ${fork:-none}; the Containerfile pins v${WRITEFREELY_FORK_VERSION}" >&2; \
        exit 1; \
    fi \
    && dir=$(go list -m -f '{{.Dir}}' github.com/writefreely/writefreely) \
    && mkdir -p /out/upstream /out/data \
    && cp -R "${dir}/templates" "${dir}/pages" "${dir}/static" "${dir}/less" /out/upstream/ \
    && cp -R "${dir}/prose" /out/prose \
    && chmod -R u+w /out/prose
COPY app/ ./
# Static, so the static runtime serves: CGO for SQLite, linked
# with -static, and netgo and osusergo for the resolver and user lookups
# that would otherwise reach for glibc's NSS at run time. WriteFreely reports
# the release its version builds on (0.17.3, since the fork is upstream's
# development branch after that release, plus fixes), because its update
# check and release-notes links compare and name it as an upstream
# release, and NodeInfo and its User-Agent carry it; version prints the fork
# build, from the binary's own forkVersion
RUN CGO_ENABLED=1 go build -trimpath -tags sqlite,netgo,osusergo \
    -ldflags="-s -w -X github.com/writefreely/writefreely.softwareVer=${WRITEFREELY_VERSION} -X main.forkVersion=${WRITEFREELY_FORK_VERSION} -extldflags -static" \
    -o /out/divbast-writefreely ./cmd/divbast-writefreely
# Proof that the build fetched no part of WriteFreely's own module: the
# replace fetches the fork's module in its place, so once the binary is
# built the module cache must hold nothing under upstream's path. The stage
# keeps the module cache in its layers: a --mount=type=cache on it would
# defeat the check, since an empty cache passes
RUN upstream="$(go env GOMODCACHE)/cache/download/github.com/writefreely/writefreely" \
    && if [ -e "${upstream}" ]; then \
        echo "the build fetched WriteFreely's own module, github.com/writefreely/writefreely; it takes WriteFreely only from the fork" >&2; \
        ls -R "${upstream}" >&2; \
        exit 1; \
    fi

# Upstream's make ui, for the stylesheets: its four LESS files, compiled
# with its flags, by the lessc and clean-css plugin css/package-lock.json
# pins.
FROM ${NODE_IMAGE} AS css
WORKDIR /css
COPY css/package.json css/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY --from=go /out/upstream/less ./less
WORKDIR /css/less
RUN set -eu; \
    mkdir -p /out/css; \
    for pair in app:write fonts:fonts icons:icons prose:prose; do \
        ../node_modules/.bin/lessc "${pair%%:*}.less" --clean-css="--s1 --advanced" "/out/css/${pair##*:}.css"; \
    done

# The editor's bundle, built here from the fork's own source, the prose/
# tree of the module the go stage linked, so the editor the image serves is
# the one the fork's tests cover, never a bundle built elsewhere. npm
# installs from the registry alone and runs no install script. The bundle
# is JavaScript, so the same source and lockfile build the same bundle on
# every architecture.
FROM ${NODE_IMAGE} AS prose
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
COPY --from=go /out/prose /build/prose
WORKDIR /build/prose
# Every lockfile entry but the root, links and bundled packages must
# resolve to registry.npmjs.org and carry an integrity hash, so npm ci
# can take none from git or GitHub
RUN node -e ' \
        const fs = require("node:fs"); \
        const lock = JSON.parse(fs.readFileSync("package-lock.json", "utf8")); \
        const prefix = "https://registry.npmjs.org/"; \
        let bad = 0; \
        for (const [name, p] of Object.entries(lock.packages)) { \
            if (name === "" || p.link === true || p.inBundle === true) continue; \
            if (typeof p.resolved !== "string" || !p.resolved.startsWith(prefix) || \
                    typeof p.integrity !== "string" || p.integrity === "") { \
                console.error("bad lockfile entry: " + name + " resolved=" + p.resolved + " integrity=" + p.integrity); \
                bad++; \
            } \
        } \
        process.exit(bad > 0 ? 1 : 0);'
# The build needs no install script: webpack and Babel do it all. They are
# devDependencies, so --include=dev installs them whatever NODE_ENV the
# base image sets. webpack writes the bundle to static/js beside prose/,
# where WriteFreely's tree serves it from
RUN npm ci --include=dev --ignore-scripts --no-audit --no-fund \
    && npm run build \
    && test -s /build/static/js/prose.bundle.js

FROM ${RUNTIME_IMAGE}
ARG WRITEFREELY_FORK_VERSION

# Root-owned, and never written
COPY --from=go /out/divbast-writefreely /usr/local/bin/divbast-writefreely
COPY --from=go /out/upstream/templates /usr/local/share/writefreely/templates
COPY --from=go /out/upstream/pages /usr/local/share/writefreely/pages
COPY --from=go /out/upstream/static /usr/local/share/writefreely/static
COPY --from=css /out/css/ /usr/local/share/writefreely/static/css/
COPY --from=prose /build/static/js/prose.bundle.js /usr/local/share/writefreely/static/js/prose.bundle.js
# The volume's mount point, owned by the user, so a new named volume takes
# that owner when the engine first mounts it
COPY --from=go --chown=65532:65532 /out/data /data

# SQLite's temporary files, on the /tmp tmpfs
ENV TMPDIR=/tmp

LABEL org.opencontainers.image.source="https://github.com/divbast/writefreely-oci-image" \
    org.opencontainers.image.url="https://github.com/divbast/writefreely-oci-image" \
    org.opencontainers.image.title="WriteFreely" \
    org.opencontainers.image.version="${WRITEFREELY_FORK_VERSION}" \
    org.opencontainers.image.licenses="AGPL-3.0-only"

EXPOSE 8080 8081
# Exec form: the shell form would need /bin/sh, which the image lacks.
# --timeout must clear divbast-writefreely health's own budget: it allows
# each of its exchanges 4 s and runs them one after the other, three with
# Gopher on, so listeners that accept and never answer cost 12 s. A
# shorter timeout kills the probe mid-flight and records a bare engine
# timeout instead of the probe's own lines, which are the only diagnostic
# there is. Podman reads no HEALTHCHECK from an image published as an OCI
# index: its users pass the same array as --health-cmd.
HEALTHCHECK --interval=30s --timeout=15s --start-period=60s --retries=3 \
    CMD ["/usr/local/bin/divbast-writefreely", "health"]
USER 65532:65532
# The binary, as PID 1: it bootstraps /data in-process, then serves
ENTRYPOINT ["/usr/local/bin/divbast-writefreely", "serve"]
