FROM mcr.microsoft.com/devcontainers/go:1.26-bookworm

ARG GOLANGCI_LINT_VERSION=v2.11.4
ARG GCL_VERSION=4.72.0

RUN apt-get update \
	&& apt-get install -y --no-install-recommends bash ca-certificates curl git rsync \
	&& curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
	&& apt-get install -y --no-install-recommends nodejs \
	&& rm -rf /var/lib/apt/lists/*

RUN curl -sSfL "https://github.com/golangci/golangci-lint/releases/download/${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION#v}-linux-amd64.tar.gz" \
	| tar -xz -C /tmp \
	&& mv "/tmp/golangci-lint-${GOLANGCI_LINT_VERSION#v}-linux-amd64/golangci-lint" /usr/local/bin/golangci-lint \
	&& rm -rf "/tmp/golangci-lint-${GOLANGCI_LINT_VERSION#v}-linux-amd64"

RUN npm install -g "gitlab-ci-local@${GCL_VERSION}"
