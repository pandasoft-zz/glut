# Versions — single source of truth for all stages
ARG GO_VERSION=1.26.2
ARG NODE_VERSION=22
ARG GCL_VERSION=4.72.0

# ── Go builder ────────────────────────────────────────────────────
FROM golang:${GO_VERSION}-bookworm AS builder
ARG VERSION=v0.0.0-dev
ARG COMMIT=unknown
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o /glut ./cmd/glut

# ── Runtime base (node + GCL + system deps) ───────────────────────
FROM node:${NODE_VERSION}-slim AS runtime
ARG GCL_VERSION
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash ca-certificates curl docker.io git rsync \
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
