#!/usr/bin/env sh
set -eu

profile_path="${1:-coverage.out}"
min_coverage="${2:-90}"

summary="$(go tool cover -func="$profile_path")"
printf '%s\n' "$summary"

total_line="$(printf '%s\n' "$summary" | awk '/^total:/ {print $0}')"
if [ -z "$total_line" ]; then
  echo "coverage check failed: total line not found" >&2
  exit 1
fi

total_percent="$(printf '%s\n' "$total_line" | awk '{print $3}' | tr -d '%')"
if [ -z "$total_percent" ]; then
  echo "coverage check failed: total percent not found" >&2
  exit 1
fi

awk -v actual="$total_percent" -v min="$min_coverage" '
BEGIN {
  if (actual > min) {
    exit 0
  }
  exit 1
}
' || {
  echo "coverage check failed: total coverage ${total_percent}% is not above ${min_coverage}%" >&2
  exit 1
}

echo "coverage check passed: total coverage ${total_percent}% is above ${min_coverage}%"
