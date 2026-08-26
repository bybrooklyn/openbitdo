# Changelog

All notable changes to OpenBitdo are tracked here.

## Unreleased

## v0.1.0

### Changed

- Rewrote the implementation from Rust (ratatui) to Go (Bubbletea). Full functional parity with
  v0.0.2, verified by porting the Rust behavioral test suites and adding end-to-end interactive
  tests against the running program (`charmbracelet/x/exp/teatest`).
- Redesigned the TUI from scratch rather than porting the screen layout 1:1: real overlay-modal
  confirmations, a new visual theme, and a device dashboard/diagnostics/mapping/firmware/settings
  flow reorganized for keyboard-and-controller navigation.
- Relicensed from BSD-3-Clause to GPL-3.0-or-later.
- PID and command registry tables are now generated directly from `spec/pid_matrix.csv` and
  `spec/command_matrix.csv` at build time, making the spec files the literal single source of
  truth instead of hand-maintained tables checked against the CSVs by separate tests.

### Added

- Controller/gamepad navigation: the app can be driven end-to-end with an 8BitDo controller's own
  d-pad and buttons (decoded from the standard USB-HID gamepad usage page), live from the device
  dashboard at startup, alongside full keyboard navigation.
- A real one-time "this may brick your device" confirmation before any unsafe/firmware action.
  Previously this flag was hardcoded true with a comment claiming a UI surface that didn't
  actually exist.
- Clearer diagnostics messaging for candidate-readonly / inferred-evidence devices: instead of a
  bare wall of "response signature mismatch" failures, the app now explains plainly that the
  device isn't hardware-confirmed yet, what that means for the checks shown, and how to help
  (fixes #15).

### Known limitations

- Verified in mock mode and against the ported test suites; real-hardware validation is still
  outstanding (no 8BitDo device was available during this rewrite) and is expected as a follow-up.

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
