#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

command -v rg >/dev/null 2>&1 || {
  echo "cleanroom guard requires ripgrep (rg); refusing to skip safety scans" >&2
  exit 1
}

forbidden_pattern='decompiled(_dll|_autoupdate)?/|bundle_extract/|extracted(_net)?/|session-ses_35e4|8BitDo_Ultimate_Software_V2\.decompiled\.cs'
scan_paths=(cmd internal .github scripts docs)

if rg -n --hidden -g '!scripts/cleanroom_guard.sh' "$forbidden_pattern" "${scan_paths[@]}"; then
  echo "cleanroom guard failed: forbidden dirty-room reference detected"
  exit 1
fi

# Prevent stale subcommand-era examples in active user docs.
active_docs=(README.md docs/MIGRATION.md)
stale_command_pattern='cargo run -p openbitdo -- ui([[:space:]]|$)|cargo run -p openbitdo -- run([[:space:]]|$)|(^|[^[:alnum:]_])openbitdo ui([[:space:]]|$)|(^|[^[:alnum:]_])openbitdo run([[:space:]]|$)'
if rg -n "$stale_command_pattern" "${active_docs[@]}" | rg -v '\(legacy\)' | rg -v '\(historical\)'; then
  echo "cleanroom guard failed: stale openbitdo subcommand surface found in active docs"
  echo "expected current usage: openbitdo [--mock]"
  exit 1
fi

# shellcheck disable=SC2016 # literal backticks are part of the Markdown regex
stale_aur_pattern='packaging/aur/openbitdo(/|$)|`openbitdo` \(source build\)'
if rg -n "$stale_aur_pattern" "${active_docs[@]}" | rg -v '\(legacy\)' | rg -v '\(historical\)'; then
  echo "cleanroom guard failed: stale source AUR package reference found in active docs"
  echo "expected current AUR package surface: openbitdo-bin only"
  exit 1
fi

# Prevent stale Rust/sdk-era references from creeping back into active docs
# now that the implementation is Go (cmd/, internal/).
stale_rust_pattern='\bsdk/crates\b|\bcargo run -p openbitdo\b|\bsdk/README\.md\b'
if rg -n "$stale_rust_pattern" "${active_docs[@]}" | rg -v '\(legacy\)' | rg -v '\(historical\)'; then
  echo "cleanroom guard failed: stale Rust/sdk-era reference found in active docs"
  echo "expected current implementation surface: cmd/openbitdo, internal/*"
  exit 1
fi

echo "cleanroom guard passed"
