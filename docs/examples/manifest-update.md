# Manifest Update Example

This example tests a job that updates a manifest and pushes it to the fake
origin.

```yaml
stages:
  - update

update-manifest:
  stage: update
  script:
    - git config user.name "Test User"
    - git config user.email "test@example.com"
    - sed -i 's/old/new/' manifest.yaml
    - git add manifest.yaml
    - git commit -m "chore: update manifest"
    - git push origin HEAD:main
---
.glut:
  name: "manifest update"
  setup:
    branch: "main"
    git:
      origin:
        branch: "main"
        files:
          "manifest.yaml": "image: old\n"
  assert:
    job:
      update-manifest:
        exit-status: 0
    git:
      origin:
        commits:
          ge: 2
        last-commit:
          message:
            contain-substring: "update manifest"
        file:
          "manifest.yaml":
            exists: true
            contents:
              contain-substring: "image: new"
```

`setup.git.origin.files` creates the first manifest in the fake origin. The job
updates it and pushes back to `main`.

The git assert checks that the origin has a new commit and that the file content
changed.
