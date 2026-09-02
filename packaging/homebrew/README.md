# Homebrew Packaging

Homebrew publishing uses the separate tap repo `bybrooklyn/homebrew-openbitdo`.

## Source Of Truth

- template: `packaging/homebrew/Formula/openbitdo.rb.tmpl`
- renderer: `scripts/render_release_metadata.sh`
- sync helper: `packaging/homebrew/sync_tap.sh`

The main repo does not keep a checked-in rendered formula. Release rendering produces the formula from published assets, and the tap repo is the canonical published destination.

The formula supports Linux x86_64/arm64 and Apple Silicon macOS 13 or newer.
Intel macOS is intentionally unsupported because no Intel macOS release artifact
is published. Bash, fish, and zsh completions are installed with the binary.

## Publish Flow

1. Publish GitHub release assets for a `v*` tag.
2. Render a checksum-pinned formula from those assets.
3. Upload the rendered formula as a workflow artifact for audit.
4. Sync the rendered formula to `bybrooklyn/homebrew-openbitdo`.

## Required Controls

- repo variable `HOMEBREW_PUBLISH_ENABLED=1`
- repo variable `HOMEBREW_TAP_REPO=bybrooklyn/homebrew-openbitdo`
- secret `HOMEBREW_TAP_TOKEN`
