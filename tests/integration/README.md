# Integration tests (real GitLab)

These tests verify **composite components** — components that `include:` other
components — end to end. They use `setup.components.fetch: real`, so
gitlab-ci-local fetches the included components from a **real GitLab** over HTTPS
using the real `CI_JOB_TOKEN`. That cannot work in GitHub CI, so they run on
GitLab instead.

> Do **not** add these to `tests/passing/` — the default `glut run
> ./tests/passing/` (Makefile / GitHub CI) would try to run them without a real
> GitLab and fail. They run only via the GitLab pipeline below.

## How it runs

1. `.github/workflows/integration-gitlab.yml` mirrors the current commit to
   `gitlab.com/glut-test/glut`, triggers its pipeline, and waits for the result.
2. `.gitlab-ci.yml` builds glut and runs `glut run ./tests/integration/` on
   GitLab. There `CI_SERVER_HOST=gitlab.com`, `CI_PROJECT_NAMESPACE=glut-test`
   and a real `CI_JOB_TOKEN` are present, so `fetch: real` resolves the fixture
   components from the `glut-test` group.

## Fixtures

`fixtures/<name>/templates/<name>.yml` are the component sources, published as
separate projects under `glut-test`:

- `comp-greet` — leaf; prints a greeting (component inputs + execution).
- `comp-artifact` — leaf; writes an artifact (artifact assertions).
- `comp-suite` — composite; `include:`s the two leaves and adds its own job.

`composite.yml` includes `comp-suite` and asserts the nested leaf jobs and the
composite's own job all run.

## One-time setup

Create the fixture projects and the runner project in the `glut-test` group:

```sh
GITLAB_TOKEN=<glut-test group access token: api + write_repository> \
  ./scripts/glut-test-bootstrap.sh
```

The script is idempotent — re-run it after changing a fixture to force-update its
content and tag (`1.0.0`, so `@1` refs resolve).

Add the same token as the GitHub repository secret `GITLAB_TOKEN` so the workflow
can push, trigger and poll.

Fixtures are created **public** by default (`VISIBILITY=public`) so the runner
job can read them without component-token-scope configuration. Set
`VISIBILITY=internal`/`private` if your group requires it (then allow the runner
project in each fixture's CI/CD job-token allowlist).
