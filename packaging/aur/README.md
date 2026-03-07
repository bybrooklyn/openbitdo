# AUR Packaging

This directory holds the tracked AUR source for `openbitdo-bin`.

## Source Of Truth

- tracked metadata: `packaging/aur/openbitdo-bin/PKGBUILD` and `.SRCINFO`
- template: `packaging/aur/openbitdo-bin/PKGBUILD.tmpl`
- renderer: `packaging/scripts/render_release_metadata.sh`

## Publish Flow

1. Publish GitHub release assets for a `v*` tag.
2. Render `PKGBUILD` and `.SRCINFO` from those assets.
3. Upload rendered metadata as a workflow artifact for audit.
4. Push the updated metadata to the AUR repo for `openbitdo-bin`.

## Required Controls

- repo variable `AUR_PUBLISH_ENABLED=1`
- secrets `AUR_USERNAME` and `AUR_SSH_PRIVATE_KEY`
- no placeholder checksums in published metadata
