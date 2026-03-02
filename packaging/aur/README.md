# AUR Packaging

This directory contains AUR package sources for:
- `openbitdo` (source build)
- `openbitdo-bin` (prebuilt release assets)

Publishing is automated by `.github/workflows/aur-publish.yml` and remains gated:
- requires repository variable `AUR_PUBLISH_ENABLED=1`
- requires secrets `AUR_SSH_PRIVATE_KEY` and `AUR_USERNAME`

Publish flow:
1. wait for release assets from a `v*` tag
2. compute authoritative SHA256 values from released artifacts
3. render `PKGBUILD`/`.SRCINFO` with pinned hashes
4. push updates to AUR repos

Template files used for release rendering:
- `openbitdo/PKGBUILD.tmpl`
- `openbitdo-bin/PKGBUILD.tmpl`
