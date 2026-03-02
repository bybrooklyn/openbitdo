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

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TAP_REPO="${HOMEBREW_TAP_REPO:-bybrooklyn/homebrew-openbitdo}"
FORMULA_SOURCE="${FORMULA_SOURCE:-$ROOT/packaging/homebrew/Formula/openbitdo.rb}"
TMP="$(mktemp -d)"

if [[ ! -f "$FORMULA_SOURCE" ]]; then
  echo "formula source not found: $FORMULA_SOURCE" >&2
  exit 1
fi

git clone "https://x-access-token:${HOMEBREW_TAP_TOKEN}@github.com/${TAP_REPO}.git" "$TMP/tap"
mkdir -p "$TMP/tap/Formula"
cp "$FORMULA_SOURCE" "$TMP/tap/Formula/openbitdo.rb"

cd "$TMP/tap"
git config user.name "${GIT_AUTHOR_NAME:-openbitdo-ci}"
git config user.email "${GIT_AUTHOR_EMAIL:-actions@users.noreply.github.com}"
git add Formula/openbitdo.rb
git commit -m "Update openbitdo formula" || {
  echo "no formula changes to push"
  exit 0
}
git push
