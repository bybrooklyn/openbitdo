#!/usr/bin/env bash
# Print the canonical Go linker flags for an OpenBitdo build.
#
# Usage:
#   GOOS=linux GOARCH=amd64 scripts/build_metadata.sh [version]
#
# The optional version defaults to OPENBITDO_VERSION, then the repository's
# VERSION file. CI/release callers can make the result reproducible with:
#   OPENBITDO_GIT_COMMIT
#   OPENBITDO_BUILD_DATE
#   OPENBITDO_BUILD_PLATFORM
#   OPENBITDO_GIT_DIRTY
set -euo pipefail

if (( $# > 1 )); then
  echo "usage: $0 [version]" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if (( $# == 1 )); then
  VERSION_VALUE="$1"
elif [[ -n "${OPENBITDO_VERSION:-}" ]]; then
  VERSION_VALUE="$OPENBITDO_VERSION"
elif [[ -r "$ROOT/VERSION" ]]; then
  VERSION_VALUE="$(tr -d '\r\n' < "$ROOT/VERSION")"
else
  echo "cannot determine version: pass one or set OPENBITDO_VERSION" >&2
  exit 1
fi

if [[ -n "${OPENBITDO_GIT_COMMIT:-}" ]]; then
  COMMIT_VALUE="$OPENBITDO_GIT_COMMIT"
else
  COMMIT_VALUE="$(git -C "$ROOT" rev-parse --short=12 HEAD 2>/dev/null || true)"
  COMMIT_VALUE="${COMMIT_VALUE:-unknown}"
fi

if [[ -n "${OPENBITDO_BUILD_DATE:-}" ]]; then
  DATE_VALUE="$OPENBITDO_BUILD_DATE"
else
  DATE_VALUE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi

if [[ -n "${OPENBITDO_BUILD_PLATFORM:-}" ]]; then
  PLATFORM_VALUE="$OPENBITDO_BUILD_PLATFORM"
else
  GOOS_VALUE="${GOOS:-$(go env GOOS)}"
  GOARCH_VALUE="${GOARCH:-$(go env GOARCH)}"
  PLATFORM_VALUE="${GOOS_VALUE}/${GOARCH_VALUE}"
fi

if [[ -n "${OPENBITDO_GIT_DIRTY:-}" ]]; then
  DIRTY_VALUE="$(printf '%s' "$OPENBITDO_GIT_DIRTY" | tr '[:upper:]' '[:lower:]')"
elif git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  if [[ -n "$(git -C "$ROOT" status --porcelain --untracked-files=normal)" ]]; then
    DIRTY_VALUE="true"
  else
    DIRTY_VALUE="false"
  fi
else
  DIRTY_VALUE="unknown"
fi

case "$DIRTY_VALUE" in
  true|false|unknown) ;;
  *)
    echo "OPENBITDO_GIT_DIRTY must be true, false, or unknown" >&2
    exit 1
    ;;
esac

for metadata_value in "$VERSION_VALUE" "$COMMIT_VALUE" "$DATE_VALUE" "$PLATFORM_VALUE" "$DIRTY_VALUE"; do
  if [[ -z "$metadata_value" || "$metadata_value" == *[[:space:]]* ]]; then
    echo "build metadata values must be nonempty and contain no whitespace" >&2
    exit 1
  fi
  if [[ "$metadata_value" == *[!A-Za-z0-9._:+/-]* ]]; then
    echo "build metadata values contain an unsupported character" >&2
    exit 1
  fi
done

printf '%s\n' \
  "-X main.appVersion=${VERSION_VALUE} -X main.gitCommit=${COMMIT_VALUE} -X main.buildDate=${DATE_VALUE} -X main.buildPlatform=${PLATFORM_VALUE} -X main.gitDirty=${DIRTY_VALUE}"
