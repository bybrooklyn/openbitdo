# OpenBitdo Release Checklist (`v0.0.3`)

This checklist defines the release gate for `v0.0.3`.

`v0.0.3` is the first Go/Bubbletea release: a from-scratch, clean-room rewrite of the prior Rust
CLI (`v0.0.2`) with a redesigned TUI, keyboard/mouse navigation, and controller navigation on
systems where the OS exposes a standard HID gamepad interface. See `CHANGELOG.md` and
`docs/MIGRATION.md` for the user-facing contract.

## Release Policy

- Preparation branch: `release/v0.0.3`
- Stale worktree branch policy: leave any `worktree-agent-*` branch untouched; it is not a release
  source for `v0.0.3`.
- Current release tag: `v0.0.3`
- Tag source: `main` only
- Release trigger: exact tag push for `v0.0.3`
- Public release rule: zero open issues labeled `release-blocker`, `P0`, or `P1`
- `v0.0.3` publishes as a normal (non-prerelease) GitHub release.

Safety note: no macOS-wide live hardware test may run unless the Darwin probe has the `manual`
build tag and the command is invoked with `-tags manual`. Manual hardware runs are restricted to
safe reads; they perform no mapping writes, candidate probes, bootloader entry, or firmware writes.

## Required Source Gates

Run these before opening or merging the release PR:

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

The release toolchain is Go `1.27.0` for development checks, CI, and artifacts.

## Scope Contract

| Area | `v0.0.3` contract |
| --- | --- |
| Linux support | `x86_64` and `aarch64`, Ubuntu 22.04-era glibc or newer |
| macOS support | arm64, deployment target macOS 13, unsigned and non-notarized |
| Intel macOS | unsupported |
| Firmware | unavailable in production; implementation kept only for isolated tests with injected ephemeral keys and a local server |
| Ultimate 2 mapping | mock preview only; real hardware blocked because `button-map framing not hardware-confirmed` |
| JP108 mapping | in scope |
| Controller navigation | available only when the OS exposes a standard HID gamepad interface; unverified on real hardware, see below |
| Hardware CI fixtures | deferred |

## Artifact Manifest

The GitHub release must contain exactly these 14 nonempty assets:

- `openbitdo-v0.0.3-linux-x86_64`
- `openbitdo-v0.0.3-linux-x86_64.sha256`
- `openbitdo-v0.0.3-linux-x86_64.tar.gz`
- `openbitdo-v0.0.3-linux-x86_64.tar.gz.sha256`
- `openbitdo-v0.0.3-linux-aarch64`
- `openbitdo-v0.0.3-linux-aarch64.sha256`
- `openbitdo-v0.0.3-linux-aarch64.tar.gz`
- `openbitdo-v0.0.3-linux-aarch64.tar.gz.sha256`
- `openbitdo-v0.0.3-macos-arm64`
- `openbitdo-v0.0.3-macos-arm64.sha256`
- `openbitdo-v0.0.3-macos-arm64.tar.gz`
- `openbitdo-v0.0.3-macos-arm64.tar.gz.sha256`
- `openbitdo-v0.0.3-macos-arm64.pkg`
- `openbitdo-v0.0.3-macos-arm64.pkg.sha256`

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
- Homebrew metadata includes completions and renders `version "0.0.3"`.
- AUR metadata renders `pkgver=0.0.3`.
- `v0.0.3` metadata must upgrade cleanly from the published `v0.0.2` package-manager versions.

## Hardware Qualification

`v0.0.3` ships with the Ultimate 2 hardware qualification **recorded but not passed**. This is a
deliberate, accepted limitation for this release, not an oversight. The blocking form of this gate
is deferred to a later release.

Qualification attempted against a wired Ultimate 2, `0x2dc8:0x6013`, serial `22EC9EA4DF`:

| Requirement | Result |
| --- | --- |
| Vendor configuration channel present | Pass — usage page `0xffa0`, usage `0x0001` |
| Generic Desktop Gamepad usage `0x0001:0x0005` present | **Fail** — no such interface in the tested mode |
| Safe-read diagnostics pass with real bytes and `transport_ready=true` | **Fail** — 12/12 commands wrote 64 bytes, read 0 |
| Physical d-pad/Confirm/Cancel drive the TUI | Not runnable — requires the absent gamepad interface |
| Unplug/reconnect updates the dashboard within three seconds | Not runnable — same |
| No mapping writes, candidate probes, bootloader entry, or firmware writes | Pass — all checks read-only |

Both failures were verified as device/protocol behavior, not defects in this codebase:

- The interface count was confirmed at three independent layers — `hid.Enumerate`, the IOKit HID
  registry, and the raw USB device descriptors. The USB layer is conclusive: `bNumConfigurations=1`
  with a single interface, `bInterfaceNumber=0`, `bInterfaceClass=3`. There is no second interface
  present for the OS to hide or seize.
- The empty reads sit on a transport confirmed working: open and `IOHIDDeviceSetReport` both
  return `kIOReturnSuccess`, and a synchronous `GetReport` returns a real USB STALL, proving the
  device is live and actively refusing that specific request. See the package documentation in
  `internal/machid/machid_darwin.go` for the full analysis.

Re-running the qualification: connect the controller, then

```
OPENBITDO_MANUAL_PID=0x6013 go test ./internal/input/... -tags manual -run TestManualUltimate2ReleaseGate -v -count=1
OPENBITDO_MANUAL_PID=0x6013 go test ./internal/tui/...   -tags manual -run TestManualUltimate2ReleaseGate -v -count=1
```

If a future controller mode exposes a Generic Desktop Gamepad interface, record the mode and PID
here and re-run both suites before restoring this gate to blocking.

## Distribution Gate

- GitHub release assets are published successfully and match the exact 14-file manifest.
- The release body includes only the matching `CHANGELOG.md` section for `v0.0.3`.
- AUR publish has required variables and credential write access before publication.
- Homebrew publish has required variables and credential write access before publication.
- Clone/install canaries verify AUR `0.0.3` and Homebrew `0.0.3`.

## Current Status Snapshot

| Gate | Status | Notes |
| --- | --- | --- |
| Source branch | In progress | Work is on `release/v0.0.3`; merge to `main` is still required before tagging. |
| Firmware production availability | Deferred | Public UI must keep Firmware Update disabled as `Deferred in 0.0.3`. |
| Ultimate 2 real mapping | Deferred | Mock preview only until button-map framing is hardware-confirmed. |
| GitHub release assets | Pending | Verify the exact `v0.0.3` 14-asset manifest after tag workflow completion. |
| AUR publication | Pending | Verify `openbitdo-bin` updates to `0.0.3`. |
| Homebrew publication | Pending | Verify `bybrooklyn/homebrew-openbitdo` updates to `0.0.3`. |
| Real Ultimate 2 hardware gate | Accepted limitation | Recorded above as not passed; deliberately non-blocking for `v0.0.3`. |
| macOS 13 launch | Not verified | No macOS 13 launch was performed for this release. |

## Historical Notes

- Historical RC activity for earlier Rust-era candidates is preserved in commit history and the
  changelog.
- Troubleshooting for AUR SSH publication lives in `docs/process/aur_publish_troubleshooting.md`.
