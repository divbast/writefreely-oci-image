# SPDX-License-Identifier: AGPL-3.0-only
# SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

# WriteFreely's release. Renovate moves these three in one PR, with
# app/go.mod's github.com/writefreely/writefreely; its regexes need them
# adjacent and in this order. The version tags the image: app-publish.yml
# reads it from this line, by the name build.yml gives as version-arg. The
# tag and the sha256 name the release's amd64 tarball, which the prose
# stage takes the editor's bundle from: the release publishes no checksum
# file, so the sha256 is the one GitHub's API gives the asset.
ARG WRITEFREELY_VERSION=0.17.3
ARG WRITEFREELY_TAG=v0.17.3
ARG WRITEFREELY_TARBALL_SHA256=207d1e4b7f8629c6b45c1da00b37ada910e28f1331ecce60dd82cb2ec78cf8b1
# The newest stable Go, by digest, with the gcc WriteFreely's CGO SQLite
# needs. Only the binary and upstream's trees leave its stage.
ARG GO_IMAGE=golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9
# Node's LTS, by digest, for compiling WriteFreely's LESS. Only the CSS
# leaves its stage.
ARG NODE_IMAGE=node:26-trixie-slim@sha256:e6bedef217677b276c591795e169840edd9096dbf13f29df8ef8d1856b8820bc
# The runtime: static-debian13, since the binary is static, with nothing in
# it but CA certificates, time zones and /etc/passwd. CI verifies its
# signature by the distroless project before anything is built.
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931

# divbast's binary, with WriteFreely linked in as a library from the module
# app/go.mod pins, and upstream's templates, pages, static files and LESS
# from that module's own tree.
FROM ${GO_IMAGE} AS go
ARG WRITEFREELY_VERSION
ARG WRITEFREELY_TAG
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
ENV GOTOOLCHAIN=local GOFLAGS=-mod=readonly
WORKDIR /src
COPY app/go.mod app/go.sum ./
# The version the image is tagged with must be the one it links: a bump
# that moved the Containerfile's pins and not go.mod's, or the other way,
# fails here rather than ship a wrong tag
RUN go mod download \
    && mod=$(go list -m -f '{{.Version}}' github.com/writefreely/writefreely) \
    && if [ "${mod}" != "${WRITEFREELY_TAG}" ] || [ "${WRITEFREELY_TAG#v}" != "${WRITEFREELY_VERSION}" ]; then \
        echo "app/go.mod links WriteFreely ${mod}; the Containerfile pins ${WRITEFREELY_TAG} and ${WRITEFREELY_VERSION}" >&2; \
        exit 1; \
    fi \
    && dir=$(go list -m -f '{{.Dir}}' github.com/writefreely/writefreely) \
    && mkdir -p /out/upstream /out/data \
    && cp -R "${dir}/templates" "${dir}/pages" "${dir}/static" "${dir}/less" /out/upstream/
COPY app/ ./
# Static, so the distroless static base serves: CGO for SQLite, linked
# with -static, and netgo and osusergo for the resolver and user lookups
# that would otherwise reach for glibc's NSS at run time
RUN CGO_ENABLED=1 go build -trimpath -tags sqlite,netgo,osusergo \
    -ldflags="-s -w -X github.com/writefreely/writefreely.softwareVer=${WRITEFREELY_VERSION} -extldflags -static" \
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
# JavaScript, so the amd64 tarball's serves every architecture. Three
# retries, 10s, 30s and 90s apart, so one dropped connection does not cost
# the daily slot.
FROM ${GO_IMAGE} AS prose
ARG WRITEFREELY_VERSION
ARG WRITEFREELY_TAG
ARG WRITEFREELY_TARBALL_SHA256
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
RUN retry() { \
        for delay in 10 30 90; do "$@" && return 0; sleep "${delay}"; done; \
        "$@"; \
    } \
    && retry curl -fsSL -o /tmp/writefreely.tar.gz \
        "https://github.com/writefreely/writefreely/releases/download/${WRITEFREELY_TAG}/writefreely_${WRITEFREELY_VERSION}_linux_amd64.tar.gz" \
    && echo "${WRITEFREELY_TARBALL_SHA256}  /tmp/writefreely.tar.gz" | sha256sum -c - \
    && bundle=$(tar -tzf /tmp/writefreely.tar.gz | grep -E '(^|/)static/js/prose\.bundle\.js$') \
    && if [ "$(printf '%s\n' "${bundle}" | wc -l)" -ne 1 ]; then \
        echo "the tarball holds no one prose.bundle.js: '${bundle}'" >&2; \
        exit 1; \
    fi \
    && mkdir -p /out \
    && tar -xzf /tmp/writefreely.tar.gz -O "${bundle}" >/out/prose.bundle.js \
    && test -s /out/prose.bundle.js

FROM ${RUNTIME_IMAGE}
ARG WRITEFREELY_VERSION

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
    org.opencontainers.image.version="${WRITEFREELY_VERSION}" \
    org.opencontainers.image.licenses="AGPL-3.0-only"

EXPOSE 8080 8081
# Exec form: the shell form would need /bin/sh, which the image lacks.
# --timeout must clear divbast-writefreely health's own budget: it allows
# each of its two exchanges 4 s and runs them one after the other, so a
# listener that accepts and never answers costs 8 s. A shorter timeout
# kills the probe mid-flight and records a bare engine timeout instead of
# the probe's own line, which is the only diagnostic there is. Podman reads
# no HEALTHCHECK from an image published as an OCI index: its users pass
# the same array as --health-cmd.
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD ["/usr/local/bin/divbast-writefreely", "health"]
USER 65532:65532
# The binary, as PID 1: it bootstraps /data in-process, then serves
ENTRYPOINT ["/usr/local/bin/divbast-writefreely", "serve"]
