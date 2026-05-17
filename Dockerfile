FROM golang:1.26.2-bookworm AS builder
ARG VERSION=v0.0.0-dev
ARG COMMIT=unknown
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o /glut ./cmd/glut

FROM node:22-slim
ARG GCL_VERSION=4.72.0
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash ca-certificates curl docker.io git rsync \
    && npm install -g gitlab-ci-local@${GCL_VERSION} \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /glut /usr/local/bin/glut
ENTRYPOINT ["/usr/local/bin/glut"]
