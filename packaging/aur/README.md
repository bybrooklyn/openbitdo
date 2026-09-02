# AUR Packaging

This directory holds the tracked AUR source for `openbitdo-bin`.

## Source Of Truth

- template: `packaging/aur/openbitdo-bin/PKGBUILD.tmpl` (the only checked-in package metadata)
- renderer: `scripts/render_release_metadata.sh`

Rendered `PKGBUILD` and `.SRCINFO` files are release outputs. They are generated
in a temporary release workspace and published to the AUR repository; they must
not be checked into this repository.

The release archive and AUR package install bash, fish, and zsh completions plus
`99-openbitdo.rules`. After installing or upgrading the rule, reload udev and
reconnect the controller:

```sh
sudo udevadm control --reload-rules
sudo udevadm trigger
```

## Publish Flow

1. Publish GitHub release assets for a `v*` tag.
2. Render `PKGBUILD` and `.SRCINFO` from those assets.
3. Upload rendered metadata as a workflow artifact for audit.
4. Push the updated metadata to the AUR repo for `openbitdo-bin`.

## Required Controls

- repo variable `AUR_PUBLISH_ENABLED=1`
- secrets `AUR_USERNAME` and `AUR_SSH_PRIVATE_KEY`
- no placeholder checksums in published metadata
