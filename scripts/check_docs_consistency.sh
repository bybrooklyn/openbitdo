#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

command -v rg >/dev/null 2>&1 || {
  echo "documentation consistency check requires ripgrep (rg); refusing to skip release scans" >&2
  exit 1
}

EXPECTED_TAG="v0.1.0-rc.1"
EXPECTED_GO="1.26.7"
U2_BLOCK_REASON="button-map framing not hardware-confirmed"

version="$(tr -d '\r\n' <VERSION)"
if [[ "$version" != "$EXPECTED_TAG" ]]; then
  echo "VERSION must be ${EXPECTED_TAG}; found ${version:-<empty>}" >&2
  exit 1
fi

declared_go="$(awk '$1 == "go" { print $2; exit }' go.mod)"
if [[ "$declared_go" != "$EXPECTED_GO" ]]; then
  echo "go.mod must declare Go ${EXPECTED_GO}; found ${declared_go:-<missing>}" >&2
  exit 1
fi

require_text() {
  local file="$1"
  local text="$2"
  if ! grep -Fq -- "$text" "$file"; then
    echo "missing required release wording in ${file}: ${text}" >&2
    exit 1
  fi
}

require_text CHANGELOG.md "## ${EXPECTED_TAG}"
require_text docs/RC_CHECKLIST.md "# OpenBitdo RC Checklist (\`${EXPECTED_TAG}\`)"
require_text docs/RC_CHECKLIST.md "Current RC tag: \`${EXPECTED_TAG}\`"
require_text .github/ISSUE_TEMPLATE/release-blocker.yml "placeholder: ${EXPECTED_TAG}"
if [[ "$(grep -Fxc "## ${EXPECTED_TAG}" CHANGELOG.md || true)" != "1" ]]; then
  echo "CHANGELOG.md must contain exactly one '${EXPECTED_TAG}' release section" >&2
  exit 1
fi

for file in README.md CHANGELOG.md docs/RC_CHECKLIST.md docs/MIGRATION.md; do
  require_text "$file" "${EXPECTED_TAG}"
done
for file in README.md CHANGELOG.md docs/RC_CHECKLIST.md docs/process/release_scope_gate.toml; do
  require_text "$file" "$U2_BLOCK_REASON"
done
require_text docs/MIGRATION.md "button-map framing"
require_text docs/MIGRATION.md "is not hardware-confirmed"
require_text CHANGELOG.md "Deferred in 0.1.0"
require_text docs/process/release_scope_gate.toml 'firmware_ui_label = "Deferred in 0.1.0"'
require_text README.md "firmware updates are unavailable"
require_text docs/MIGRATION.md "Firmware update is unavailable"
require_text docs/RC_CHECKLIST.md "Firmware | unavailable in production"

# Current user-facing release documents must not claim that the two deferred
# real-hardware capabilities are fully available. Historical changelog entries
# and general promotion policy remain valid and are deliberately excluded.
if rg -in \
  'ultimate ?2.{0,100}(full support|fully supported|real.hardware mapping (is )?available)|firmware (update|updates)? (is|are) available' \
  README.md docs/RC_CHECKLIST.md docs/MIGRATION.md SECURITY.md; then
  echo "stale full-support wording contradicts the v0.1.0-rc.1 capability scope" >&2
  exit 1
fi

require_text README.md "GNU General Public License v3.0 or later"
require_text README.md "| From source | \`just build\` with Go 1.26.7"
require_text docs/MIGRATION.md "GPL-3.0-or-later"
require_text packaging/aur/openbitdo-bin/PKGBUILD.tmpl "license=('GPL-3.0-or-later')"
require_text packaging/homebrew/Formula/openbitdo.rb.tmpl 'license "GPL-3.0-or-later"'
require_text LICENSE "GNU GENERAL PUBLIC LICENSE"

for flag in --version --diagnostics-dump; do
  require_text cmd/openbitdo/main.go "$flag"
  require_text completions/openbitdo.bash "$flag"
  require_text completions/openbitdo.zsh "$flag"
done
require_text completions/openbitdo.fish "-l version"
require_text completions/openbitdo.fish "-l diagnostics-dump"

if [[ -e packaging/aur/openbitdo-bin/PKGBUILD || -e packaging/aur/openbitdo-bin/.SRCINFO ]]; then
  echo "rendered AUR metadata must not be checked in; PKGBUILD.tmpl is the source of truth" >&2
  exit 1
fi
if [[ -e packaging/homebrew/Formula/openbitdo.rb ]]; then
  echo "rendered Homebrew formula must not be checked in; openbitdo.rb.tmpl is the source of truth" >&2
  exit 1
fi

if rg -n \
  --glob '*.md' \
  --glob '*.yml' \
  --glob '*.yaml' \
  --glob '*.sh' \
  --glob '*.rb' \
  --glob 'PKGBUILD*' \
  --glob '.SRCINFO' \
  -g '!CHANGELOG.md' \
  'v0\.0\.1-rc\.[0-9]+|0\.0\.1-rc\.[0-9]+|0\.0\.1rc[0-9]+' \
  .github README.md docs packaging scripts; then
  echo "stale pre-0.1.0 release-candidate references remain outside CHANGELOG.md" >&2
  exit 1
fi

action_refs="$(rg -n 'uses:[[:space:]]+' .github/workflows || true)"
unpinned_actions="$(printf '%s\n' "$action_refs" \
  | grep -Ev 'uses:[[:space:]]+\./|@[0-9a-f]{40}([[:space:]]|$)' || true)"
if [[ -n "$unpinned_actions" ]]; then
  printf '%s\n' "$unpinned_actions" >&2
  echo "GitHub Actions must be pinned to immutable commit SHAs" >&2
  exit 1
fi

require_text .github/workflows/ci.yml "version: v2.13.1"
require_text .github/workflows/ci.yml "golang.org/x/vuln/cmd/govulncheck@v1.7.0"
require_text .github/workflows/release.yml "fail_on_unmatched_files: true"
require_text .github/workflows/release.yml "./scripts/extract_changelog_section.sh \"\$GITHUB_REF_NAME\""
require_text .github/workflows/release.yml "ULTIMATE2_RC_GATE_SHA"
require_text .github/workflows/aur-publish.yml "ULTIMATE2_RC_GATE_SHA"
require_text .github/workflows/homebrew-publish.yml "ULTIMATE2_RC_GATE_SHA"
require_text docs/RC_CHECKLIST.md "ULTIMATE2_RC_GATE_SHA"

if rg -n 'if:[[:space:]].*(AUR|HOMEBREW)_PUBLISH_ENABLED' .github/workflows; then
  echo "AUR and Homebrew publication must be required, not conditionally skipped" >&2
  exit 1
fi
if rg -n 'workflow_dispatch:' \
  .github/workflows/aur-publish.yml \
  .github/workflows/homebrew-publish.yml; then
  echo "package-manager publish workflows must only be callable through release preflight" >&2
  exit 1
fi

echo "release docs, CLI help, packaging, and automation consistently target ${EXPECTED_TAG}"
