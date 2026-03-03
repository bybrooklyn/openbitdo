#!/usr/bin/env bash
set -euo pipefail

if [[ "${HOMEBREW_PUBLISH_ENABLED:-0}" != "1" ]]; then
  echo "homebrew tap sync disabled (set HOMEBREW_PUBLISH_ENABLED=1 to enable)"
  exit 0
fi

if [[ -z "${HOMEBREW_TAP_TOKEN:-}" ]]; then
  echo "missing HOMEBREW_TAP_TOKEN" >&2
  exit 1
fi

# Trim accidental newline/CR characters from copied secrets.
HOMEBREW_TAP_TOKEN="$(printf '%s' "${HOMEBREW_TAP_TOKEN}" | tr -d '\r\n')"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TAP_REPO="${HOMEBREW_TAP_REPO:-bybrooklyn/homebrew-openbitdo}"
FORMULA_SOURCE="${FORMULA_SOURCE:-$ROOT/packaging/homebrew/Formula/openbitdo.rb}"
TMP="$(mktemp -d)"

if [[ ! -f "$FORMULA_SOURCE" ]]; then
  echo "formula source not found: $FORMULA_SOURCE" >&2
  exit 1
fi

api() {
  GH_TOKEN="${HOMEBREW_TAP_TOKEN}" gh api "$@"
}

formula_path="Formula/openbitdo.rb"
encoded_formula="$(base64 < "$FORMULA_SOURCE" | tr -d '\n')"
remote_sha=""
remote_content_file="$TMP/remote_formula.rb"

if api "repos/${TAP_REPO}/contents/${formula_path}?ref=main" >"$TMP/remote.json" 2>/dev/null; then
  remote_sha="$(jq -r '.sha // ""' "$TMP/remote.json")"
  jq -r '.content // ""' "$TMP/remote.json" | tr -d '\n' | base64 --decode >"$remote_content_file"
  if cmp -s "$FORMULA_SOURCE" "$remote_content_file"; then
    echo "no formula changes to push"
    exit 0
  fi
fi

api_args=(
  --method PUT
  "repos/${TAP_REPO}/contents/${formula_path}"
  -f message="Update openbitdo formula"
  -f content="${encoded_formula}"
  -f branch="main"
)
if [[ -n "${remote_sha}" ]]; then
  api_args+=(-f sha="${remote_sha}")
fi
api "${api_args[@]}" >/dev/null
echo "updated ${TAP_REPO}:${formula_path}"
