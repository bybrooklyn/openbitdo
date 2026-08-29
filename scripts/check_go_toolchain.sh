#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXPECTED_GO_VERSION="1.27.0"

declared_version="$(awk '$1 == "go" { print $2; exit }' "$ROOT/go.mod")"
if [[ "$declared_version" != "$EXPECTED_GO_VERSION" ]]; then
  echo "go.mod must declare Go ${EXPECTED_GO_VERSION}; found ${declared_version:-<missing>}" >&2
  exit 1
fi

actual_version="$(go env GOVERSION)"
actual_version="${actual_version#go}"
if [[ "$actual_version" != "$EXPECTED_GO_VERSION" ]]; then
  echo "OpenBitdo checks require Go ${EXPECTED_GO_VERSION}; running ${actual_version:-<unknown>}" >&2
  exit 1
fi

echo "Go toolchain is pinned to ${EXPECTED_GO_VERSION}"
