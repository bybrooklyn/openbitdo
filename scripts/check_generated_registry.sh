#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GENERATED="$ROOT/internal/protocol/registry_generated.go"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

candidate="$TMP_ROOT/registry_generated.go"
(
  cd "$ROOT"
  go run ./internal/protocol/gen \
    -spec-dir docs/spec \
    -out "$candidate"
)

if ! cmp -s "$GENERATED" "$candidate"; then
  echo "generated protocol registry is stale; run: go generate ./..." >&2
  diff -u "$GENERATED" "$candidate" || true
  exit 1
fi

echo "generated protocol registry matches docs/spec"
