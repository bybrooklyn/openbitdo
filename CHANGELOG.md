# Changelog

All notable changes to OpenBitdo are tracked here.

## Unreleased

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
