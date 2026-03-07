# OpenBitdo RC Checklist (`v0.0.1-rc.4`)

This checklist defines the release-candidate gate for the current public RC tag.

## Release Policy

- Tag format: `v*`
- Current RC tag: `v0.0.1-rc.4`
- Tag source: `main` only
- Release trigger: tag push
- Public RC rule: zero open issues labeled `release-blocker`

## Required CI Checks

- `guard`
- `aur-validate`
- `tui-smoke-test`
- `build-macos-arm64`
- `test`

## Clean Tree Gate

From `cleanroom/`:

```bash
git status --porcelain
git clean -ndX
```

Expected:

- no tracked modifications or staged changes
- ignored-output review only from `git clean -ndX`

## Artifact Expectations

Release assets must include:

- `openbitdo-v0.0.1-rc.4-linux-x86_64.tar.gz`
- `openbitdo-v0.0.1-rc.4-linux-x86_64`
- `openbitdo-v0.0.1-rc.4-linux-aarch64.tar.gz`
- `openbitdo-v0.0.1-rc.4-linux-aarch64`
- `openbitdo-v0.0.1-rc.4-macos-arm64.tar.gz`
- `openbitdo-v0.0.1-rc.4-macos-arm64`
- `openbitdo-v0.0.1-rc.4-macos-arm64.pkg`
- `.sha256` files for every artifact above

## Distribution Gate

- GitHub prerelease assets must be published successfully.
- AUR publish must render checksum-pinned metadata and update `openbitdo-bin`.
- Homebrew publish must render a checksum-pinned formula and update `bybrooklyn/homebrew-openbitdo`.

## macOS Packaging Gate

- `.pkg` remains unsigned and non-notarized for this RC.
- Gatekeeper friction is expected and must be documented.
- Tarball and standalone binary remain the fallback paths.

## Manual Smoke Expectations

1. Linux `x86_64`: launch `openbitdo --mock`
2. Linux `aarch64`: launch `openbitdo --mock`
3. macOS arm64 standalone binary: launch `openbitdo --mock`
4. macOS arm64 `.pkg`: confirm payload installation path and launch behavior where possible

## Current Status Snapshot

| Gate | Status | Notes |
| --- | --- | --- |
| Required CI checks | Pass | Current required checks are configured in GitHub branch protection. |
| GitHub prerelease assets | Pending | Verify `v0.0.1-rc.4` assets after the tag workflow completes. |
| AUR publication | Pending | Verify `openbitdo-bin` updates to `v0.0.1-rc.4` after release publication. |
| Homebrew publication | Pending | Verify `bybrooklyn/homebrew-openbitdo` updates to `v0.0.1-rc.4` after release publication. |
| macOS notarization | Deferred | Explicitly out of scope until Apple credentials exist. |

## Historical Notes

- Historical RC activity for earlier candidates is preserved in commit history and the changelog.
- Troubleshooting for AUR SSH publication lives in `process/aur_publish_troubleshooting.md`.
