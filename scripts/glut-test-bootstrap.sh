#!/usr/bin/env bash
#
# glut-test-bootstrap.sh — set up the GitLab fixtures for the composite-component
# integration tests (see tests/integration/README.md).
#
# It is idempotent: re-running force-updates each fixture's content and tag.
#
# For every directory in tests/integration/fixtures/<name> it:
#   1. creates the project <group>/<name> on GitLab (if missing),
#   2. force-pushes the directory contents to the default branch,
#   3. force-creates the tag used by `@1` style component refs (default 1.0.0).
#
# It also creates the (empty) runner project that the GitHub workflow pushes the
# glut sources to before triggering the GitLab pipeline.
#
# Usage:
#   GITLAB_TOKEN=<group-access-token> ./scripts/glut-test-bootstrap.sh
#
# Environment:
#   GITLAB_TOKEN     required — group access token with api + write_repository
#   GITLAB_HOST      default: gitlab.com
#   GLUT_TEST_GROUP  default: glut-test
#   GLUT_TEST_RUNNER default: glut          (runner project the workflow pushes to)
#   FIXTURE_TAG      default: 1.0.0          (semver tag so numeric refs like @1 resolve)
#   VISIBILITY       default: public         (public avoids CI job-token scope friction)
set -euo pipefail

GITLAB_HOST="${GITLAB_HOST:-gitlab.com}"
GLUT_TEST_GROUP="${GLUT_TEST_GROUP:-glut-test}"
GLUT_TEST_RUNNER="${GLUT_TEST_RUNNER:-glut}"
FIXTURE_TAG="${FIXTURE_TAG:-1.0.0}"
VISIBILITY="${VISIBILITY:-public}"
API="https://${GITLAB_HOST}/api/v4"

: "${GITLAB_TOKEN:?set GITLAB_TOKEN to a group access token (api + write_repository)}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixtures_dir="${repo_root}/tests/integration/fixtures"

api() { # method path [data]
  local method="$1" path="$2" data="${3:-}"
  if [ -n "$data" ]; then
    curl -sS -X "$method" --header "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
      --header "Content-Type: application/json" --data "$data" "${API}${path}"
  else
    curl -sS -X "$method" --header "PRIVATE-TOKEN: ${GITLAB_TOKEN}" "${API}${path}"
  fi
}

urlenc() { # url-encode a path (group/name -> group%2Fname)
  local s="$1"
  s="${s//\//%2F}"
  printf '%s' "$s"
}

group_id="$(api GET "/groups/$(urlenc "${GLUT_TEST_GROUP}")" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -n1)"
if [ -z "${group_id}" ]; then
  echo "error: could not resolve group '${GLUT_TEST_GROUP}' (check token and group name)" >&2
  exit 1
fi
echo "group ${GLUT_TEST_GROUP} -> id ${group_id}"

ensure_project() { # name
  local name="$1"
  local full="${GLUT_TEST_GROUP}/${name}"
  if api GET "/projects/$(urlenc "${full}")" | grep -q "\"path_with_namespace\":\"${full}\""; then
    echo "project ${full} already exists"
    return 0
  fi
  echo "creating project ${full}"
  api POST "/projects" \
    "{\"name\":\"${name}\",\"path\":\"${name}\",\"namespace_id\":${group_id},\"visibility\":\"${VISIBILITY}\",\"initialize_with_readme\":false}" \
    >/dev/null
}

push_fixture() { # name dir
  local name="$1" dir="$2"
  local url="https://oauth2:${GITLAB_TOKEN}@${GITLAB_HOST}/${GLUT_TEST_GROUP}/${name}.git"
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "${tmp}"' RETURN
  cp -r "${dir}/." "${tmp}/"
  git -C "${tmp}" init -q
  git -C "${tmp}" config user.email "glut-bot@example.com"
  git -C "${tmp}" config user.name "glut-bot"
  git -C "${tmp}" config commit.gpgSign false
  git -C "${tmp}" add -A
  git -C "${tmp}" commit -q -m "glut integration fixture: ${name}"
  git -C "${tmp}" branch -M main
  git -C "${tmp}" tag -f "${FIXTURE_TAG}" >/dev/null
  git -C "${tmp}" push -q -f "${url}" main
  git -C "${tmp}" push -q -f "${url}" "refs/tags/${FIXTURE_TAG}"
  echo "pushed ${name} (main + tag ${FIXTURE_TAG})"
}

# Fixtures (leaf + composite components).
for dir in "${fixtures_dir}"/*/; do
  name="$(basename "${dir}")"
  ensure_project "${name}"
  push_fixture "${name}" "${dir}"
done

# Runner project: the GitHub workflow pushes glut here, then triggers a pipeline.
ensure_project "${GLUT_TEST_RUNNER}"

echo "done. fixtures and runner project are ready under ${GLUT_TEST_GROUP}."
