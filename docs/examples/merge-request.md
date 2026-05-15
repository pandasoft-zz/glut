# Merge Request Pipeline Example

This example tests a job that checks an MR for the `ready` label, posts a
comment, and approves the MR.

## Happy Path

The MR has the `ready` label. The job comments and approves.

```yaml
stages:
  - check

check-mr:
  stage: check
  script:
    - |
      labels=$(curl -s \
        -H "PRIVATE-TOKEN: $CI_JOB_TOKEN" \
        "$CI_API_V4_URL/projects/1/merge_requests/$CI_MERGE_REQUEST_IID" \
        | grep -o '"ready"' || true)
      if [ -z "$labels" ]; then
        echo "MR is not ready" >&2
        exit 1
      fi
    - |
      curl -s -X POST \
        -H "PRIVATE-TOKEN: $CI_JOB_TOKEN" \
        -H "Content-Type: application/json" \
        -d "{\"body\":\"MR #$CI_MERGE_REQUEST_IID is approved and ready to merge\"}" \
        "$CI_API_V4_URL/projects/1/merge_requests/$CI_MERGE_REQUEST_IID/notes"
    - |
      curl -s -X POST \
        -H "PRIVATE-TOKEN: $CI_JOB_TOKEN" \
        "$CI_API_V4_URL/projects/1/merge_requests/$CI_MERGE_REQUEST_IID/approve"
    - echo "MR approved"
---
.glut:
  name: "check-mr approves a ready MR"
  setup:
    branch: "feature/my-change"
    pipeline_source: "merge_request_event"
    merge_request:
      iid: 42
      title: "Add new feature"
      target_branch: "main"
      draft: false
      labels: "ready"
    api:
      token:
        valid: true
        scopes:
          - "api"
      project:
        path: "test-group/test-project"
        default_branch: "main"
    git:
      origin:
        branch: "feature/my-change"
        files:
          "Dockerfile": "FROM scratch\n"
  assert:
    job:
      check-mr:
        exit-status: 0
        stdout:
          - "MR approved"
    api:
      "GET /api/v4/projects/*/merge_requests/*":
        called: true
      "POST /api/v4/projects/*/merge_requests/*/notes":
        called: true
        times: 1
        body:
          body:
            contain-substring: "approved and ready"
      "POST /api/v4/projects/*/merge_requests/*/approve":
        called: true
        times: 1
```

Key points:

- Use `branch`, not `tag`, for MR pipelines.
- `CI_COMMIT_BRANCH` is **not set** in MR pipelines. Use
  `CI_MERGE_REQUEST_SOURCE_BRANCH_NAME` or `CI_COMMIT_REF_NAME` if you need the
  branch name inside the job.
- `CI_MERGE_REQUEST_IID` comes from `setup.merge_request.iid`. It is a string
  (`"42"`) in the environment.
- Always use `*` in `assert.api` paths. Never hard-code a project ID or MR IID.
- `git.origin.branch` matches `setup.branch`. This ensures any `git checkout`
  inside the pipeline finds the expected branch name.

## Draft MR Path

When the MR is in draft state, the job exits early without approving.

```yaml
stages:
  - check

check-mr:
  stage: check
  script:
    - |
      if [ "$CI_MERGE_REQUEST_DRAFT" = "true" ]; then
        echo "MR is in draft state, skipping approval" >&2
        exit 1
      fi
    - echo "not reached"
---
.glut:
  name: "check-mr rejects a draft MR"
  setup:
    branch: "feature/my-change"
    pipeline_source: "merge_request_event"
    merge_request:
      iid: 42
      title: "Draft: Add new feature"
      target_branch: "main"
      draft: true
    api:
      token:
        valid: true
        scopes:
          - "api"
  assert:
    job:
      check-mr:
        exit-status: 1
        stderr:
          - "MR is in draft state"
    api:
      "POST /api/v4/projects/*/merge_requests/*/approve":
        called: false
```

`setup.merge_request.draft: true` sets `CI_MERGE_REQUEST_DRAFT=true` in the
environment. The job reads this variable and exits before calling the approve
endpoint. The `called: false` assert confirms no approve request was made.
