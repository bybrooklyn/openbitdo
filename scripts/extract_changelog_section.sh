#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <release-tag> [changelog]" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tag="$1"
changelog="${2:-$ROOT/CHANGELOG.md}"
heading="## ${tag}"

matches="$(grep -Fxc "$heading" "$changelog" || true)"
if [[ "$matches" != "1" ]]; then
  echo "expected exactly one changelog heading '${heading}', found ${matches}" >&2
  exit 1
fi

awk -v heading="$heading" '
  $0 == heading { in_section = 1 }
  in_section && $0 != heading && /^## / { exit }
  in_section {
    print
    if ($0 != heading && $0 !~ /^[[:space:]]*$/) {
      has_body = 1
    }
  }
  END {
    if (!has_body) {
      print "release changelog section has no body" > "/dev/stderr"
      exit 1
    }
  }
' "$changelog"
