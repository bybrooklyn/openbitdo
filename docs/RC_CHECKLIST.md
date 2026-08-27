# OpenBitdo RC Checklist (`v0.1.0-rc.1`)

This checklist defines the release-candidate gate for `v0.1.0-rc.1` and the later stable
`v0.1.0` promotion.

`v0.1.0-rc.1` is the first Go/Bubbletea release candidate: a from-scratch, clean-room rewrite of
the prior Rust CLI (`v0.0.2`) with a redesigned TUI, keyboard/mouse navigation, and controller
navigation when the OS exposes a standard HID gamepad interface. See `CHANGELOG.md` and
`docs/MIGRATION.md` for the user-facing contract.

## Release Policy

- Preparation branch: `rewrite/go-tui`
- Stale worktree branch policy: leave any `worktree-agent-*` branch untouched; it is not a release
  source for `v0.1.0-rc.1`.
- Current RC tag: `v0.1.0-rc.1`
- Tag source: `main` only
- Release trigger: exact tag push for `v0.1.0-rc.1`
- Public release rule: zero open issues labeled `release-blocker`, `P0`, or `P1`
- Manual Ultimate 2 release control: repo variable `ULTIMATE2_RC_GATE_SHA` must equal the exact
  tagged `main` commit after strict manual hardware qualification
- Stable promotion rule: seven clean days after `v0.1.0-rc.1`, with any release-impacting fix
  producing `rc.2` and restarting the soak

Current source reference: commit `22f0c6f` on `rewrite/go-tui` has successful GitHub Actions run
<https://github.com/bybrooklyn/openbitdo/actions/runs/33111934016>. Re-run required checks on the
final PR tree, the merged `main` tree, and the exact tag commit.

Safety note: no macOS-wide live hardware test may run unless the Darwin probe has the `manual`
build tag and the command is invoked with `-tags manual`. One prior planning run may have sent up
to three non-firmware `GetMode` requests if PID `0x6013` was connected; it could not perform
mapping or firmware writes.

## Required Source Gates

Run these before opening or merging the RC PR:

- generated registry diff check
- `gofmt`
- `go vet`
- pinned `golangci-lint` (`2.13.1`)
- pinned `govulncheck` (`v1.7.0`) with zero reachable findings; archive the JSON output
- `go test -race ./...`
- `go mod verify`
- clean-room/evidence guards
- docs consistency checks
- TUI golden and teatest matrix for `60x18`, `80x24`, `96x24`, `100x30`, and `120x40`

The release toolchain is Go `1.26.7` for development checks, CI, and artifacts.

## Scope Contract

| Area | `v0.1.0-rc.1` contract |
| --- | --- |
| Linux support | `x86_64` and `aarch64`, Ubuntu 22.04-era glibc or newer |
| macOS support | arm64, deployment target macOS 13, unsigned and non-notarized |
| Intel macOS | unsupported |
| Firmware | unavailable in production; implementation kept only for isolated tests with injected ephemeral keys and a local server |
| Ultimate 2 mapping | mock preview only; real hardware blocked because `button-map framing not hardware-confirmed` |
| JP108 mapping | in scope |
| Controller navigation | available only when the OS exposes a standard HID gamepad interface |
| Hardware CI fixtures | deferred |

## Artifact Manifest

The GitHub prerelease must contain exactly these 14 nonempty assets:

- `openbitdo-v0.1.0-rc.1-linux-x86_64`
- `openbitdo-v0.1.0-rc.1-linux-x86_64.sha256`
- `openbitdo-v0.1.0-rc.1-linux-x86_64.tar.gz`
- `openbitdo-v0.1.0-rc.1-linux-x86_64.tar.gz.sha256`
- `openbitdo-v0.1.0-rc.1-linux-aarch64`
- `openbitdo-v0.1.0-rc.1-linux-aarch64.sha256`
- `openbitdo-v0.1.0-rc.1-linux-aarch64.tar.gz`
- `openbitdo-v0.1.0-rc.1-linux-aarch64.tar.gz.sha256`
- `openbitdo-v0.1.0-rc.1-macos-arm64`
- `openbitdo-v0.1.0-rc.1-macos-arm64.sha256`
- `openbitdo-v0.1.0-rc.1-macos-arm64.tar.gz`
- `openbitdo-v0.1.0-rc.1-macos-arm64.tar.gz.sha256`
- `openbitdo-v0.1.0-rc.1-macos-arm64.pkg`
- `openbitdo-v0.1.0-rc.1-macos-arm64.pkg.sha256`

Checksum sidecars must contain basenames only. Missing checksum tools are fatal.

## Artifact Gates

- Linux `x86_64` and `aarch64` builds run on Ubuntu 22.04 runners.
- No imported `GLIBC_*` symbol may exceed `2.35`.
- Extracted Linux artifacts launch on Ubuntu 22.04 and Debian 12.
- Linux archives and AUR metadata include shell completions and the udev rule in conventional
  system paths, with rule reload/replug instructions documented.
- macOS arm64 builds use `MACOSX_DEPLOYMENT_TARGET=13.0`.
- The Mach-O minimum OS is asserted before publication.
- The `.pkg` installs `openbitdo` to `/usr/local/bin`.
- Homebrew metadata includes completions and renders `version "0.1.0-rc.1"`.
- AUR metadata renders `pkgver=0.1.0rc1`.
- Final `v0.1.0` metadata must upgrade cleanly from the RC package-manager versions.

## Hardware Gate

`v0.1.0-rc.1` remains blocked until the current Ultimate 2 passes one non-destructive hardware
qualification on the final PR tree:

- Try each physically available USB/controller mode.
- Record the resulting `0x2dc8` PID and HID interfaces.
- Confirm one mode exposes both the vendor configuration channel and Generic Desktop Gamepad usage
  `0x0001:0x0005`.
- Confirm all applicable, non-experimental safe-read diagnostics pass with real response bytes and
  `transport_ready=true`.
- Confirm physical up/down/left/right plus Confirm/Cancel drive the real TUI.
- Confirm unplug/reconnect updates the dashboard within three seconds, without duplicate devices or
  restart.
- Perform no mapping writes, candidate probes, bootloader entry, or firmware writes.

If no mode meets every requirement, do not weaken the gate; produce a later RC after the blocker is
closed.

## Distribution Gate

- Repo variable `ULTIMATE2_RC_GATE_SHA` equals the exact tagged `main` commit that passed the
  strict manual Ultimate 2 hardware gate.
- GitHub prerelease assets are published successfully and match the exact 14-file manifest.
- The release body includes only the matching `CHANGELOG.md` section for `v0.1.0-rc.1`.
- AUR publish has required variables and credential write access before publication.
- Homebrew publish has required variables and credential write access before publication.
- Clone/install canaries verify AUR `0.1.0rc1` and Homebrew `0.1.0-rc.1`.

## Stable Promotion

After `v0.1.0-rc.1`:

1. Soak for at least seven days with zero open release-blocker, P0, or P1 regressions.
2. If a release-impacting fix lands, cut `rc.2` and restart the seven-day soak.
3. Before stable promotion, perform a real macOS 13 launch.
4. Repeat package-manager canaries and the Ultimate 2 canary.
5. Update version and release notes to `v0.1.0`.
6. Re-run all gates.
7. Merge to `main` and tag the exact stable commit.

## Current Status Snapshot

| Gate | Status | Notes |
| --- | --- | --- |
| Source branch | In progress | Work is on `rewrite/go-tui`; merge to `main` is still required before tagging. |
| Current CI reference | Done | Run `33111934016` succeeded for `rewrite/go-tui` at `22f0c6f`; re-run required checks on the final tree. |
| Firmware production availability | Deferred | Public UI must keep Firmware Update disabled as `Deferred in 0.1.0`. |
| Ultimate 2 real mapping | Deferred | Mock preview only until button-map framing is hardware-confirmed. |
| GitHub prerelease assets | Pending | Verify the exact `v0.1.0-rc.1` 14-asset manifest after tag workflow completion. |
| AUR publication | Pending | Verify `openbitdo-bin` updates to `0.1.0rc1`. |
| Homebrew publication | Pending | Verify `bybrooklyn/homebrew-openbitdo` updates to `0.1.0-rc.1`. |
| Real Ultimate 2 hardware gate | Blocking | Must pass the non-destructive gate above on the final PR tree before RC publication. |
| macOS 13 launch | Promotion blocker | Required before stable `v0.1.0`, not before `rc.1`. |

## Historical Notes

- Historical RC activity for earlier Rust-era candidates is preserved in commit history and the
  changelog.
- Troubleshooting for AUR SSH publication lives in `docs/process/aur_publish_troubleshooting.md`.
