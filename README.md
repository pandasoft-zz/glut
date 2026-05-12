<p align="center">
  <img src="docs/assets/logo.png" alt="GLUT" width="180">
</p>

# GLUT

GLUT is a CLI tool for testing GitLab CI components on your machine. It runs a
GitLab CI pipeline with `gitlab-ci-local` and adds isolated git state, a mock
GitLab API, mock binaries, and structured asserts.

Use GLUT when a component changes git state, calls the GitLab API, or calls
tools such as `release-cli`. It is useful for image build jobs, manifest update
jobs, and release jobs.

## For Users

The full manual is on **[GitHub Pages](https://pandasoft-zz.github.io/glut/)**
and covers everything you need to get started and use GLUT day to day:

- [Installation](https://pandasoft-zz.github.io/glut/getting-started/installation/) — Docker image and native binary
- [Getting Started](https://pandasoft-zz.github.io/glut/getting-started/) — quickstart and AI skill setup
- [Test Format](https://pandasoft-zz.github.io/glut/reference/test-format/) — how to write test files
- [Assert Syntax](https://pandasoft-zz.github.io/glut/reference/assert-syntax/) — all available assertions
- [Mock API](https://pandasoft-zz.github.io/glut/reference/mock-api/) — mock GitLab API reference
- [CLI Reference](https://pandasoft-zz.github.io/glut/reference/cli/) — all commands and flags
- [Examples](https://pandasoft-zz.github.io/glut/examples/) — image build, manifest update, release

## For Developers

### Repository Overview

| Path | Purpose |
|---|---|
| `cmd/glut/` | CLI entry point (Cobra, thin handlers) |
| `internal/parser/` | YAML parsing and schema validation |
| `internal/runner/` | Test orchestration |
| `internal/asserter/` | Assert evaluation |
| `internal/mockserver/` | Mock GitLab API |
| `schema/` | JSON Schema for the `.glut:` document |
| `docs/` | MkDocs source (published to GitHub Pages) |
| `tests/` | GLUT test files |
| `skill/` | AI skill definition |

Architecture and package boundary rules are in [docs/architecture.md](docs/architecture.md).

### Dev Container

All development happens inside the dev container. Open the repository in VS
Code and choose **Reopen in Container**. The container includes Go, Docker
access, and `gitlab-ci-local`.

If you prefer the CLI:

```bash
devcontainer open .
```

### Common Commands

```bash
make build          # build the glut binary
make test           # run all tests
make test-cover     # run tests with coverage report
make lint           # run golangci-lint
make docker         # build the Docker image locally
make release        # build a snapshot release with goreleaser
```

Docs are built with MkDocs:

```bash
pip install mkdocs mkdocs-readthedocs
mkdocs serve        # preview at http://localhost:8000
```

### Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to add mock API endpoints and
where tests belong. Read [AGENTS.md](AGENTS.md) before any implementation work.
