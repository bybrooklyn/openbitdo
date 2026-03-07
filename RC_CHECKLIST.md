# OpenBitdo RC Checklist (`v0.0.1-rc.2`)

This checklist defines release-candidate readiness for the `v0.0.1-rc.2` public RC tag.

## Candidate Policy
- Tag format: `v*` (for this RC: `v0.0.1-rc.2`)
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
- AUR auth troubleshooting runbook:
  - [aur_publish_troubleshooting.md](/Users/brooklyn/data/8bitdo/cleanroom/process/aur_publish_troubleshooting.md)

## Scope-Completeness Gate ("Good Point")
Before tagging `v0.0.1-rc.2`, RC scope must match the locked contract:
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

## Release Secret Preflight (Tag Workflow)
Tag preflight must fail early if any required secret is missing:
- `AUR_USERNAME`
- `AUR_SSH_PRIVATE_KEY`
- `HOMEBREW_TAP_TOKEN`

## Artifact Expectations
Release assets must include:
- `openbitdo-v0.0.1-rc.2-linux-x86_64.tar.gz`
- `openbitdo-v0.0.1-rc.2-linux-x86_64`
- `openbitdo-v0.0.1-rc.2-linux-aarch64.tar.gz`
- `openbitdo-v0.0.1-rc.2-linux-aarch64`
- `openbitdo-v0.0.1-rc.2-macos-arm64.tar.gz`
- `openbitdo-v0.0.1-rc.2-macos-arm64`
- `openbitdo-v0.0.1-rc.2-macos-arm64.pkg`
- corresponding `.sha256` files for every artifact above

## Verify Checksums
Run from release asset directory:

```bash
shasum -a 256 -c openbitdo-v0.0.1-rc.2-linux-x86_64.tar.gz.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.2-linux-x86_64.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.2-linux-aarch64.tar.gz.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.2-linux-aarch64.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.2-macos-arm64.tar.gz.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.2-macos-arm64.sha256
shasum -a 256 -c openbitdo-v0.0.1-rc.2-macos-arm64.pkg.sha256
```

## Manual Smoke Matrix
1. Linux `x86_64`
- Extract tarball, run `./bin/openbitdo --mock`
- Confirm dashboard renders (device list, quick actions, event panel)
- Confirm settings screen opens and returns to dashboard

2. Linux `aarch64`
- Extract tarball, run `./bin/openbitdo --mock`
- Confirm dashboard navigation and preflight/task render

3. macOS arm64
- Run standalone binary `openbitdo --mock`
- Install `.pkg`, then run `/opt/homebrew/bin/openbitdo --mock`
- Confirm launch and settings view behavior

## Help Surface Verification
- `openbitdo --help` shows single-command usage with `--mock`.

## Distribution Readiness Notes
- Homebrew publication runs after release asset publish when `HOMEBREW_PUBLISH_ENABLED=1`.
- AUR publication runs after release asset publish when `AUR_PUBLISH_ENABLED=1`.
- Both package paths use release-derived SHA256 values (no `SKIP`, no `:no_check` in published metadata).

## RC Gate Snapshot (Local)
| Gate | Status | Notes |
| --- | --- | --- |
| Clean tree | Pass | Verified empty on `c3115da` before final checklist update (`git status --porcelain`). |
| Secrets present | Pass | `AUR_USERNAME`, `AUR_SSH_PRIVATE_KEY`, `HOMEBREW_TAP_TOKEN` exist in repo secrets. |
| Required checks configured | Pass | `guard`, `test`, `tui-smoke-test`, `aur-validate`, `build-macos-arm64`. |
| Open release-blocker issues | Pass | `0` open (`gh issue list --label release-blocker --state open`). |
| RC release allowed | Fail | `No` yet: AUR SSH auth still returns `Permission denied (publickey)`. |

## RC Execution Log
Historical note: entries below may mention older command forms from prior milestones and are preserved as-is for audit history.

- 2026-03-02T20:54:31Z: governance preflight complete; release blocker remains open by policy.
- 2026-03-02T21:38:17Z: set `HOMEBREW_TAP_REPO=bybrooklyn/homebrew-openbitdo`; repository and tap visibility switched to public.
- 2026-03-02T21:40:00Z: bootstrapped tap repo `bybrooklyn/homebrew-openbitdo` with initial `Formula/openbitdo.rb`.
- 2026-03-02T21:45:27Z to 2026-03-02T21:48:55Z: CI run `22597105846` on commit `c3115da` passed `guard`, `test`, `tui-smoke-test`, `aur-validate`, `build-macos-arm64`, `build-linux-x86_64`, and `build-linux-aarch64`.
- 2026-03-02T21:49:00Z to 2026-03-02T21:55:00Z: downloaded CI artifacts and manually verified each artifact hash against `.sha256` content (all matched) for:
  - `openbitdo-v0.0.0-ci-linux-x86_64.tar.gz`
  - `openbitdo-v0.0.0-ci-linux-x86_64`
  - `openbitdo-v0.0.0-ci-linux-aarch64.tar.gz`
  - `openbitdo-v0.0.0-ci-linux-aarch64`
  - `openbitdo-v0.0.0-ci-macos-arm64.tar.gz`
  - `openbitdo-v0.0.0-ci-macos-arm64`
  - `openbitdo-v0.0.0-ci-macos-arm64.pkg`
- 2026-03-02T21:56:00Z to 2026-03-02T22:00:00Z: Linux artifact smoke completed in containers for `linux/amd64` and `linux/arm64` by launching `openbitdo --mock` and observing successful TUI startup.
- 2026-03-02T21:57:00Z: local macOS packaging smoke completed via `./sdk/scripts/package-macos.sh v0.0.0-local arm64` (tarball, standalone binary, pkg generated).
- 2026-03-02T21:58:00Z: local standalone macOS smoke completed (`openbitdo-v0.0.0-local-macos-arm64 --mock`) with TUI startup and clean exit via scripted key input.
- 2026-03-02T21:59:00Z: pkg payload path validated by expansion (`Payload/opt/homebrew/bin/openbitdo`); direct installer invocation requires root (`installer: Must be run as root to install this package`).
- 2026-03-02T21:59:30Z: About behavior validated by test run `cargo test -p bitdo_tui about_state_roundtrip_returns_home`.
- 2026-03-02T22:02:00Z: Wave 2 issues `#2` through `#13` closed with per-issue evidence comments.
- 2026-03-02T22:03:00Z: epic issue `#1` closed and `release-blocker` label removed after child closure summary.
- 2026-03-02T22:04:00Z: clean-tree gate confirmed on baseline commit `c3115da` (`git status --porcelain` empty).
- 2026-03-02T22:10:00Z: removed hardware self-hosted checks from branch protection and CI/release gate policy.
