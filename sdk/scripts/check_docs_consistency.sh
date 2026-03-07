#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

if rg -n \
  --glob '*.md' \
  --glob '*.yml' \
  --glob '*.sh' \
  --glob '*.rb' \
  --glob 'PKGBUILD' \
  --glob '.SRCINFO' \
  -g '!CHANGELOG.md' \
  'v0\.0\.1-rc\.1|v0\.0\.1-rc\.2|0\.0\.1-rc\.1|0\.0\.1-rc\.2|0\.0\.1rc1|0\.0\.1rc2' \
  .github \
  README.md \
  MIGRATION.md \
  RC_CHECKLIST.md \
  packaging \
  process \
  sdk \
  spec; then
  echo "stale rc.1/rc.2 references remain outside CHANGELOG.md" >&2
  exit 1
fi
