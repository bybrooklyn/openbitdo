# OpenBitdo RC Checklist (`v0.0.1-rc.1`)

This checklist defines release-candidate readiness for the first public RC tag.

## Candidate Policy
- Tag format: `v*` (for this RC: `v0.0.1-rc.1`)
- Tag source: `main` only
- Release trigger: tag push
- RC gate: all required CI checks + manual smoke validation

## Release-Blocker Policy
Use GitHub issue labels:
- `release-blocker`
- `severity:p0`
- `severity:p1`
- `severity:p2`

Public RC gate rule:
- zero open issues labeled `release-blocker`

Daily review cadence:
- run once per day until RC tag:
  - `gh issue list -R bybrooklyn/openbitdo --label release-blocker --state open --limit 200`
- release remains blocked while this list is non-empty.

## Scope-Completeness Gate ("Good Point")
Before tagging `v0.0.1-rc.1`, RC scope must match the locked contract:
- JP108 mapping supports dedicated keys only (`A/B/K1-K8`) for RC.
- Ultimate2 expanded mapping supports RC-required fields only:
  - remappable slots `A/B/K1-K8`
  - known controller-button targets
  - profile slot read/write/readback
  - firmware version in diagnostics/reports
  - L2/R2 analog read (and write where capability allows)

Release gate is checklist-driven for RC (no separate scorecard artifact).

## Clean-Tree Requirement (Before Tagging)
Run from `/Users/brooklyn/data/8bitdo/cleanroom`:

```bash
git status --porcelain
git clean -ndX
```

Expected:
- `git status --porcelain` prints nothing (no modified, staged, or untracked files)
- `git clean -ndX` output reviewed for ignored-build artifacts only

Tracked-path audit:

```bash
git ls-files | rg '(^sdk/dist/|^sdk/target/|^harness/reports/)'
```

Expected:
- no tracked artifact/build-output paths matched

## Required CI Checks
- `guard`
- `aur-validate`
- `tui-smoke-test`
- `build-macos-arm64`
- `test`
- `hardware-ultimate2`
- `hardware-108jp`

Gated/non-required:
- `hardware-jphandshake` (enabled only when `BITDO_ENABLE_JP_HARDWARE=1`)

Hardware execution policy:
- Pull requests run required hardware jobs when surgical runtime/spec paths are touched.
- `main`, nightly, and tag workflows run full required hardware coverage.
- Nightly full hardware run is scheduled for `02:30 America/New_York` policy time (implemented as GitHub cron UTC).

## Release Secret Preflight (Tag Workflow)
Tag preflight must fail early if any required secret is missing:
- `AUR_USERNAME`
- `AUR_SSH_PRIVATE_KEY`
- `HOMEBREW_TAP_TOKEN`

## Artifact Expectations
Release assets must include:
- `openbitdo-v0.0.1-rc.1-linux-x86_64.tar.gz`
- `openbitdo-v0.0.1-rc.1-linux-x86_64`
- `openbitdo-v0.0.1-rc.1-linux-aarch64.tar.gz`
- `openbitdo-v0.0.1-rc.1-linux-aarch64`
- `openbitdo-v0.0.1-rc.1-macos-arm64.tar.gz`
- `openbitdo-v0.0.1-rc.1-macos-arm64`
- `openbitdo-v0.0.1-rc.1-macos-arm64.pkg`
- corresponding `.sha256` files for every artifact above

## Verify Checksums
Run from release asset directory:

```bash
shasum -a 256 -c openbitdo-v0.0.1-rc.1-linux-x86_64.tar.gz.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.1-linux-x86_64.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.1-linux-aarch64.tar.gz.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.1-linux-aarch64.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.1-macos-arm64.tar.gz.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.1-macos-arm64.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.1-macos-arm64.pkg.sha256
```

## Manual Smoke Matrix
1. Linux `x86_64`
- Extract tarball, run `./bin/openbitdo --mock`
- Confirm waiting/home flow renders
- Confirm About page opens (`a` and mouse click)

2. Linux `aarch64`
- Extract tarball, run `./bin/openbitdo --mock`
- Confirm main navigation and update preflight render

3. macOS arm64
- Run standalone binary `openbitdo --mock`
- Install `.pkg`, then run `/opt/homebrew/bin/openbitdo --mock`
- Confirm launch and About page behavior

## Distribution Readiness Notes
- Homebrew publication runs after release asset publish when `HOMEBREW_PUBLISH_ENABLED=1`.
- AUR publication runs after release asset publish when `AUR_PUBLISH_ENABLED=1`.
- Both package paths use release-derived SHA256 values (no `SKIP`, no `:no_check` in published metadata).

## RC Gate Snapshot (Local)
| Gate | Status | Notes |
| --- | --- | --- |
| Clean tree | Pending | Will pass after local baseline commit if `git status --porcelain` is empty. |
| Secrets present | Pass | `AUR_USERNAME`, `AUR_SSH_PRIVATE_KEY`, `HOMEBREW_TAP_TOKEN` exist in repo secrets. |
| Required checks configured | Pass | `guard`, `test`, `tui-smoke-test`, `aur-validate`, `build-macos-arm64`, `hardware-108jp`, `hardware-ultimate2`. |
| Open release-blocker issues | Fail | `1` open (`Wave 2 Dirty-Room Expansion (+12 Popularity Set)`). |
| RC release allowed | Fail | `No` while release-blocker count is non-zero. |

## RC Execution Log
- 2026-03-02T20:54:31Z: governance preflight complete; release blocker remains open by policy.
- Local baseline commit snapshot:
  - commit hash: `PENDING_AFTER_COMMIT` (retrieve with `git rev-parse --short HEAD`)
  - clean-tree check: run `git status --porcelain` and expect empty output.
