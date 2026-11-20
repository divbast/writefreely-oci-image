# SPDX-License-Identifier: AGPL-3.0-only
# SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

# WriteFreely's release, after which divbast's fork branches (the fork is
# upstream's development branch after it, plus fixes), with app/go.mod's
# require of github.com/writefreely/writefreely. The version tags the image:
# app-publish.yml reads it from this line, by the name build.yml gives as
# version-arg. The tag and the sha256 name the release's amd64 tarball,
# which the prose stage takes the editor's bundle from: the release
# publishes no checksum file, so the sha256 is the one GitHub's API gives
# the asset. Renovate leaves these three alone while the image is on the
# fork; its regexes need the tag directly above the sha256.
#
# The fork's build, which app/go.mod's replace links and which names the
# image's version: what divbast-writefreely version prints and the version
# label say, and the extra tag app-publish.yml reads from this line, by the
# name build.yml gives as extra-tag-arg. Renovate moves it with the replace
# in one PR.
ARG WRITEFREELY_VERSION=0.17.3
ARG WRITEFREELY_FORK_VERSION=0.17.3-divbast.21
ARG WRITEFREELY_TAG=v0.17.3
ARG WRITEFREELY_TARBALL_SHA256=207d1e4b7f8629c6b45c1da00b37ada910e28f1331ecce60dd82cb2ec78cf8b1
# Every image below is a Docker Hardened Image on Debian 13, pinned by
# digest, and CI verifies Docker's signature on each before anything is
# built.
#
# The newest stable Go, in Docker Hardened Images' -dev variant, which has
# the gcc and static glibc WriteFreely's CGO SQLite links against. Only the
# binary and upstream's trees leave its stage.
ARG GO_IMAGE=dhi.io/golang:1.27.4-debian13-dev@sha256:2f58c6928a851f0b6059f1a63514828a788a748d88b218a1bc9ae23c3b540140
# Node's LTS, in the -dev variant, for compiling WriteFreely's LESS and
# fetching the editor's bundle. Only the CSS and the bundle leave its
# stages.
ARG NODE_IMAGE=dhi.io/node:26.13.1-debian13-dev@sha256:c60298e2cbf6ca2f82bb25e827b1c361491c30e92dec7f8804dc6f6dede6c639
# The runtime: Docker Hardened Images' static, since the binary is static,
# with nothing in it but CA certificates, time zones and the nonroot user
# 65532: no shell and no package manager.
ARG RUNTIME_IMAGE=dhi.io/static:20250419-debian13@sha256:4749130e822c145dbdf9ca11ec0cc2f694b5fe687fd74268a2b62160e6726f39

# divbast's binary, with WriteFreely linked in as a library from the module
# app/go.mod pins, the fork's build its replace names, and the templates,
# pages, static files and LESS from that module's own tree.
FROM ${GO_IMAGE} AS go
ARG WRITEFREELY_VERSION
ARG WRITEFREELY_FORK_VERSION
ARG WRITEFREELY_TAG
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
ENV GOTOOLCHAIN=local GOFLAGS=-mod=readonly
WORKDIR /src
COPY app/go.mod app/go.sum ./
# The versions the image is tagged with must be the ones it links: a bump
# that moved the Containerfile's pins and not go.mod's, or the other way,
# fails here rather than ship a wrong tag. That holds for the release
# go.mod requires and for the fork build its replace names
RUN go mod download \
    && mod=$(go list -m -f '{{.Version}}' github.com/writefreely/writefreely) \
    && if [ "${mod}" != "${WRITEFREELY_TAG}" ] || [ "${WRITEFREELY_TAG#v}" != "${WRITEFREELY_VERSION}" ]; then \
        echo "app/go.mod links WriteFreely ${mod}; the Containerfile pins ${WRITEFREELY_TAG} and ${WRITEFREELY_VERSION}" >&2; \
        exit 1; \
    fi \
    && fork=$(go list -m -f '{{if .Replace}}{{.Replace.Version}}{{end}}' github.com/writefreely/writefreely) \
    && if [ "${fork}" != "v${WRITEFREELY_FORK_VERSION}" ]; then \
        echo "app/go.mod links the fork build ${fork:-none}; the Containerfile pins v${WRITEFREELY_FORK_VERSION}" >&2; \
        exit 1; \
    fi \
    && dir=$(go list -m -f '{{.Dir}}' github.com/writefreely/writefreely) \
    && mkdir -p /out/upstream /out/data \
    && cp -R "${dir}/templates" "${dir}/pages" "${dir}/static" "${dir}/less" /out/upstream/
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

# The editor's bundle, from the release tarball, checked against its pinned
# sha256: its webpack 4 build needs Node's legacy OpenSSL provider and
# forks fetched over git+ssh, so it is not built here. The bundle is
# JavaScript, so the amd64 tarball's serves every architecture. Neither
# -dev image has curl or gzip, so Node fetches the tarball, with three
# retries, 10s, 30s and 90s apart, so one dropped connection does not cost
# the daily slot; and Node's zlib unpacks it, once its sha256 has matched,
# for tar to read.
FROM ${NODE_IMAGE} AS prose
ARG WRITEFREELY_VERSION
ARG WRITEFREELY_TAG
ARG WRITEFREELY_TARBALL_SHA256
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
RUN node --input-type=module -e ' \
        import { writeFile } from "node:fs/promises"; \
        import { setTimeout as sleep } from "node:timers/promises"; \
        const [url, out] = process.argv.slice(1); \
        const delays = [10, 30, 90]; \
        for (let attempt = 0; ; attempt++) { \
            try { \
                const res = await fetch(url); \
                if (!res.ok) throw new Error("HTTP " + res.status); \
                await writeFile(out, new Uint8Array(await res.arrayBuffer())); \
                break; \
            } catch (err) { \
                if (attempt === delays.length) throw err; \
                console.error("fetch " + url + ": " + err.message + "; again in " + delays[attempt] + "s"); \
                await sleep(delays[attempt] * 1000); \
            } \
        }' \
        "https://github.com/writefreely/writefreely/releases/download/${WRITEFREELY_TAG}/writefreely_${WRITEFREELY_VERSION}_linux_amd64.tar.gz" \
        /tmp/writefreely.tar.gz \
    && echo "${WRITEFREELY_TARBALL_SHA256}  /tmp/writefreely.tar.gz" | sha256sum -c - \
    && node --input-type=module -e ' \
        import { createReadStream, createWriteStream } from "node:fs"; \
        import { pipeline } from "node:stream/promises"; \
        import { createGunzip } from "node:zlib"; \
        await pipeline(createReadStream(process.argv[1]), createGunzip(), createWriteStream(process.argv[2]));' \
        /tmp/writefreely.tar.gz /tmp/writefreely.tar \
    && bundle=$(tar -tf /tmp/writefreely.tar | grep -E '(^|/)static/js/prose\.bundle\.js$') \
    && if [ "$(printf '%s\n' "${bundle}" | wc -l)" -ne 1 ]; then \
        echo "the tarball holds no one prose.bundle.js: '${bundle}'" >&2; \
        exit 1; \
    fi \
    && mkdir -p /out \
    && tar -xf /tmp/writefreely.tar -O "${bundle}" >/out/prose.bundle.js \
    && test -s /out/prose.bundle.js

FROM ${RUNTIME_IMAGE}
ARG WRITEFREELY_FORK_VERSION

# Root-owned, and never written
COPY --from=go /out/divbast-writefreely /usr/local/bin/divbast-writefreely
COPY --from=go /out/upstream/templates /usr/local/share/writefreely/templates
COPY --from=go /out/upstream/pages /usr/local/share/writefreely/pages
COPY --from=go /out/upstream/static /usr/local/share/writefreely/static
COPY --from=css /out/css/ /usr/local/share/writefreely/static/css/
COPY --from=prose /out/prose.bundle.js /usr/local/share/writefreely/static/js/prose.bundle.js
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
