# Contributing

Thank you for helping GLUT.

Keep repository text in simple English. Use short sentences and direct names.
Run tests in the devcontainer before you send a change.

## Add A Mock API Endpoint

The `internal/mockserver` package owns HTTP behavior. Other packages may read
recorded calls or prepared state, but they should not implement HTTP routes.

Mock API work uses two groups. Tier 2 means a standard stored resource with
create, read, update, and delete behavior. Tier 1 means a special endpoint with
custom behavior.

## Add A Tier 2 CRUD Resource

Use Tier 2 for normal GitLab API resources that follow simple CRUD behavior.

1. Add the resource state to the mock server store.
2. Add route handling in `internal/mockserver`.
3. Record each request with method, path, query, and body.
4. Return GitLab-like JSON and status codes.
5. Add tests in `internal/mockserver`.
6. Add assertion coverage only when the new behavior changes assert data.

Keep the handler small. Put common store logic in store helpers.

## Add A Tier 1 Special Endpoint

Use Tier 1 for endpoints with special behavior. Examples are endpoints that
create releases, run side effects, or need GitLab-specific validation.

1. Add a dedicated handler in `internal/mockserver`.
2. Keep the request and response shape close to GitLab.
3. Record the call before returning the response.
4. Add focused tests in `internal/mockserver`.
5. Add runner or asserter tests only when the endpoint changes their contract.

Do not hide special behavior inside a generic CRUD helper.

## Where Tests Belong

- HTTP routing and response tests belong in `internal/mockserver`.
- Recorded call checks belong in `internal/mockserver` or `internal/asserter`.
- Setup wiring belongs in `internal/runner`.
- YAML parsing belongs in `internal/parser`.
- Schema checks belong in `schema`.

Do not add empty passing tests.
