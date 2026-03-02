#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

forbidden_pattern='decompiled(_dll|_autoupdate)?/|bundle_extract/|extracted(_net)?/|session-ses_35e4|8BitDo_Ultimate_Software_V2\.decompiled\.cs'
scan_paths=(crates tests scripts ../.github)

if rg -n --hidden -g '!target/**' -g '!scripts/cleanroom_guard.sh' "$forbidden_pattern" "${scan_paths[@]}"; then
  echo "cleanroom guard failed: forbidden dirty-room reference detected"
  exit 1
fi

echo "cleanroom guard passed"
