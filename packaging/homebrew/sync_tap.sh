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
TAP_OWNER="${TAP_REPO%%/*}"
TAP_USER="${HOMEBREW_TAP_USERNAME:-$TAP_OWNER}"
FORMULA_SOURCE="${FORMULA_SOURCE:-$ROOT/packaging/homebrew/Formula/openbitdo.rb}"
TMP="$(mktemp -d)"

if [[ ! -f "$FORMULA_SOURCE" ]]; then
  echo "formula source not found: $FORMULA_SOURCE" >&2
  exit 1
fi

clone_url() {
  local user="$1"
  echo "attempting tap clone using token auth as '${user}'"
  git clone "https://${user}:${HOMEBREW_TAP_TOKEN}@github.com/${TAP_REPO}.git" "$TMP/tap"
}

if ! clone_url "$TAP_USER"; then
  # Some token types (for example GitHub App tokens) require x-access-token.
  if [[ "$TAP_USER" != "x-access-token" ]]; then
    rm -rf "$TMP/tap"
    clone_url "x-access-token"
    TAP_USER="x-access-token"
  else
    echo "failed to clone tap repo with HOMEBREW_TAP_TOKEN" >&2
    exit 1
  fi
fi

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
git remote set-url origin "https://${TAP_USER}:${HOMEBREW_TAP_TOKEN}@github.com/${TAP_REPO}.git"
git push
