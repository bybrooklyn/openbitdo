# Changelog

All notable changes to OpenBitdo are tracked here.

## Unreleased

## v0.0.3

### Changed

- Rewrote the implementation from Rust (ratatui) to Go (Bubbletea). Full functional parity with
  v0.0.2, verified by porting the Rust behavioral test suites and adding end-to-end interactive
  tests against the running program (`charmbracelet/x/exp/teatest`).
- Redesigned the TUI from scratch rather than porting the screen layout 1:1: responsive compact
  and wide layouts, bounded scrollable views, real overlay-modal confirmations, adaptive color
  roles, and a dashboard organized around `Status`, `Works now`, `Blocked`, and `Next step`.
- Relicensed from BSD-3-Clause to GPL-3.0-or-later.
- PID and command registry tables are now generated directly from `spec/pid_matrix.csv` and
  `spec/command_matrix.csv` at build time, making the spec files the literal single source of
  truth instead of hand-maintained tables checked against the CSVs by separate tests.
- Pinned release-facing metadata to `v0.0.3` across AUR and Homebrew.

### Added

- Controller/gamepad navigation: the app can be driven with an 8BitDo controller's own d-pad and
  buttons, alongside keyboard and mouse navigation, live from the device dashboard at startup.
  This requires the OS to expose the controller as a standard USB-HID Generic Desktop Gamepad
  (usage `0x0001:0x0005`). The feature is implemented and covered by tests, but it is **not
  verified on real hardware** — see "Known limitations" below.
- A real one-time "this may brick your device" confirmation before any unsafe/firmware action.
  Previously this flag was hardcoded true with a comment claiming a UI surface that didn't
  actually exist.
- Clearer diagnostics messaging for candidate-readonly / inferred-evidence devices: instead of a
  bare wall of "response signature mismatch" failures, the app now explains plainly that the
  device isn't hardware-confirmed yet, what that means for the checks shown, and how to help
  (improves the issue #15 user experience; the issue remains open until real hardware evidence
  closes it).

### Deferred in 0.0.3

- Firmware update is unavailable in production. The manifest feed and signing-key path remain
  test-only, and the TUI renders firmware as a disabled action labeled `Deferred in 0.0.3`.
- Ultimate 2 mapping on real hardware is blocked with the explicit reason
  `button-map framing not hardware-confirmed`. The Ultimate 2 editor remains available as a
  mock-only preview for UI testing.
- Fixture-backed hardware CI and firmware writes are not part of this release.

### Known limitations

These were measured against a wired 8BitDo Ultimate 2 (`0x2dc8:0x6013`) and are shipped known:

- Controller navigation is unverified on real hardware. In its tested mode this Ultimate 2
  publishes exactly one USB HID interface (`bNumConfigurations=1`, one interface, class 3) on the
  vendor page `0xffa0`, and no Generic Desktop Gamepad interface. Navigation has nothing to read
  from on that unit, so d-pad/button input does not drive the TUI there. Keyboard and mouse
  navigation are unaffected.
- Safe-read diagnostics return no data on this PID. All 12 applicable commands write 64 bytes
  successfully and read back 0. The transport itself is confirmed working — open and
  `IOHIDDeviceSetReport` both return `kIOReturnSuccess`, and a synchronous `GetReport` draws a
  real USB STALL, which shows the device is live and actively refusing that request. The command
  registry marks these commands `Confidence: "confirmed"`, meaning confirmed present in the
  vendor binary by static analysis, not confirmed to answer on real hardware; no evidence dossier
  exists for `0x6013`. Resolving this needs protocol reverse-engineering in the separate
  dirty-room evidence process, not transport changes.
- As a consequence, the dashboard can present a device as `Supported` with `works now: safe
  diagnostics` while every diagnostic on it fails. The support tier is derived from static
  evidence, not from a live probe result.

## v0.0.2

### Added

- Expanded `U2ButtonId` mapping to support all 17 keys for Ultimate 2 controllers.
- Added mapping and slot configuration support for Ultimate 2 and JP108 candidate-readonly PIDs.
- Promoted Ultimate 2 Bluetooth variant and receiver PIDs `0x600f` and `0x6011` to the full support tier.
- Added candidate write/readback validation tests.

### Fixed

- Resolved Homebrew tap publishing CI failures by updating access token scopes and handling.


## v0.0.1-rc.4

### Changed

- Release docs are being rewritten around the `v0.0.1-rc.4` flow.
- Homebrew publishing is being moved to a reusable workflow with the separate tap repo kept as the canonical Homebrew destination.
- TUI copy is being expanded so first-run guidance is clearer and blocked-action reasons are easier to understand.
- The checked-in Homebrew formula output is being removed; the template and rendered release metadata remain the source of truth.

## v0.0.1-rc.3

### Added

- Tag-driven GitHub prerelease assets for Linux `x86_64`, Linux `aarch64`, and macOS arm64.
- AUR publication for `openbitdo-bin` with release-derived checksums.
- Diagnostics screen with richer per-check detail and saved-report flow.

### Changed

- Firmware update defaults remain safe until the user explicitly acknowledges risk.
- Temporary recommended-firmware downloads are cleaned up after success, failure, or cancellation.
- Invalid persisted settings are surfaced as warnings instead of being silently discarded.

## v0.0.1-rc.1

### Added

- Beginner-first `openbitdo` launcher and terminal dashboard.
- Release packaging scripts for Linux and macOS artifacts.
- Unsigned, non-notarized macOS `.pkg` output for RC distribution.
- AUR and Homebrew release metadata rendering.

### Notes

- Historical RC notes are preserved here for audit continuity.
