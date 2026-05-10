# Image Build Example

This example tests a job that writes an image reference to an artifact.

```yaml
stages:
  - build

build-image:
  stage: build
  script:
    - mkdir -p dist
    - echo "registry.example.com/app:${CI_COMMIT_REF_SLUG}" > dist/image.txt
---
.glut:
  name: "image build"
  setup:
    branch: "feature/image"
    pipeline_source: "push"
  assert:
    job:
      build-image:
        present: true
        exit-status: 0
        stderr:
          not:
            contain-substring: "error"
    artifacts:
      "dist/image.txt":
        exists: true
        filetype: "file"
        contents:
          - "/registry.example.com\\/app:feature-image/"
```

The test sets `branch` to `feature/image`. GLUT derives `CI_COMMIT_REF_SLUG`
from that branch, so the job writes `feature-image` to the artifact.

The artifact assert checks that the file exists and contains the expected image
name.
