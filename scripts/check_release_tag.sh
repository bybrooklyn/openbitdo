#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <release-tag>" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXPECTED_TAG="v0.1.0-rc.1"
tag="$1"
version="$(tr -d '\r\n' <"$ROOT/VERSION")"

if [[ "$tag" != "$EXPECTED_TAG" ]]; then
  echo "this release workflow accepts only ${EXPECTED_TAG}; received ${tag}" >&2
  exit 1
fi
if [[ "$version" != "$EXPECTED_TAG" ]]; then
  echo "VERSION must equal ${EXPECTED_TAG} for this release; found ${version:-<empty>}" >&2
  exit 1
fi

echo "release tag and VERSION both equal ${EXPECTED_TAG}"
