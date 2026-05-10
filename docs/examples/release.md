# Release Example

This example tests a release job without calling the real `release-cli`.

```yaml
stages:
  - release

release:
  stage: release
  script:
    - release-cli create --tag-name "$CI_COMMIT_TAG" --name "$CI_COMMIT_TAG"
---
.glut:
  name: "release from tag"
  setup:
    tag: "v1.2.0"
    pipeline_source: "push"
    mocks:
      binaries:
        release-cli:
          executable: |
            #!/bin/sh
            echo "release-cli $*"
  assert:
    job:
      release:
        present: true
        exit-status: 0
        stdout:
          - "release-cli create"
    binary:
      release-cli:
        called: true
        times: 1
        calls:
          - args:
              contain-elements:
                - "create"
                - "--tag-name"
                - "v1.2.0"
        never-called-with:
          args:
            contain-element: "--dry-run"
```

`setup.tag` sets `CI_COMMIT_TAG`. The mock binary replaces `release-cli` in
`PATH`, so the test can record the call without creating a real release.

The binary assert checks that the command used the expected tag.
