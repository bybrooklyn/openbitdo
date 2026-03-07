# Homebrew Packaging

Homebrew publishing uses the separate tap repo `bybrooklyn/homebrew-openbitdo`.

## Source Of Truth

- template: `packaging/homebrew/Formula/openbitdo.rb.tmpl`
- renderer: `packaging/scripts/render_release_metadata.sh`
- sync helper: `packaging/homebrew/sync_tap.sh`

The main repo does not keep a checked-in rendered formula. Release rendering produces the formula from published assets, and the tap repo is the canonical published destination.

## Publish Flow

1. Publish GitHub release assets for a `v*` tag.
2. Render a checksum-pinned formula from those assets.
3. Upload the rendered formula as a workflow artifact for audit.
4. Sync the rendered formula to `bybrooklyn/homebrew-openbitdo`.

## Required Controls

- repo variable `HOMEBREW_PUBLISH_ENABLED=1`
- repo variable `HOMEBREW_TAP_REPO=bybrooklyn/homebrew-openbitdo`
- secret `HOMEBREW_TAP_TOKEN`
