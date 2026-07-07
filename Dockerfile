# Versions — single source of truth for all stages
ARG GO_VERSION=1.26.2
ARG NODE_VERSION=22.23
# Keep GCL_VERSION in sync with config.TestedGCLVersion (internal/config/constants.go):
# GLUT parses gitlab-ci-local's human-oriented output, so the tested version is
# part of the runtime contract.
ARG GCL_VERSION=4.72.0
ARG DOCKER_CLI_VERSION=29.6.1

# ── Go builder ────────────────────────────────────────────────────
FROM golang:${GO_VERSION}-bookworm AS builder
ARG VERSION=v0.0.0-dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o /glut ./cmd/glut

# ── Runtime base (node + GCL + system deps) ───────────────────────
# Runs as root: GLUT talks to the Docker daemon over a bind-mounted
# /var/run/docker.sock (see docs/getting-started/installation.md), and the
# socket's host-side group ownership varies by environment, so a fixed
# non-root UID/GID cannot reliably be granted access without extra
# host-specific setup at container run time.
FROM node:${NODE_VERSION}-slim AS runtime
ARG GCL_VERSION
ARG DOCKER_CLI_VERSION
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash ca-certificates curl git rsync \
    && set -eux; \
    case "${TARGETARCH}" in \
      amd64) DOCKER_ARCH=x86_64 ;; \
      arm64) DOCKER_ARCH=aarch64 ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://download.docker.com/linux/static/stable/${DOCKER_ARCH}/docker-${DOCKER_CLI_VERSION}.tgz" -o /tmp/docker-cli.tgz \
    && tar -xzC /usr/local/bin --strip-components=1 -f /tmp/docker-cli.tgz docker/docker \
    && rm /tmp/docker-cli.tgz \
    && npm install -g gitlab-ci-local@${GCL_VERSION} \
    && rm -rf /var/lib/apt/lists/*

# ── dev: used by `make docker` and integration tests ──────────────
# Built explicitly via `docker build --target dev`.
FROM runtime AS dev
COPY --from=builder /glut /usr/local/bin/glut
ENTRYPOINT ["/usr/local/bin/glut"]

# ── release: default build target used by goreleaser ──────────────
# Last stage — `docker build .` (no --target) produces this image.
# goreleaser supplies the pre-built binary via TARGETPLATFORM.
FROM runtime AS release
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/glut /usr/local/bin/glut
ENTRYPOINT ["/usr/local/bin/glut"]
