# OpenBitdo

OpenBitdo is a clean-room, beginner-first 8BitDo utility built around a modern terminal UI.
It focuses on safe diagnostics first, guided firmware/update flows for confirmed devices, and clear blocked-action messaging for devices that are still under investigation.

OpenBitdo is unofficial and not affiliated with 8BitDo. Firmware updates and device changes always carry risk. Read the prompts carefully and keep backups or recovery plans where available.

## What OpenBitdo Does Today

- Detect connected 8BitDo devices and explain their current support level.
- Run diagnostics and save support reports.
- Show a support scorecard for every selected device.
- Stage verified firmware updates on full-support devices.
- Edit supported mappings for the currently confirmed JP108 and Ultimate 2 flows.
- Keep unconfirmed devices in safe read-only or detect-only paths.

## Install

| Path | Command | Best for |
| --- | --- | --- |
| Homebrew | `brew tap bybrooklyn/openbitdo && brew install openbitdo` | macOS or Linux users who want the standard tap flow |
| AUR | `paru -S openbitdo-bin` | Arch Linux users who want a prebuilt package |
| GitHub Releases | Download a release tarball and run `bin/openbitdo` | Users who want a standalone binary without a package manager |
| From source | `cargo run -p openbitdo --` from `sdk/` with Rust 1.85+ | Contributors and local development |

## First Run

1. Launch `openbitdo`.
2. If you do not have hardware attached yet, launch `openbitdo --mock` to preview the interface.
3. Pick a controller from the grouped dashboard: supported, read-only candidate, or detect-only.
4. Run `Diagnose` first. Diagnostics are the safe path for every detected 8BitDo device.
5. Save the TOML support report when a device is blocked, experimental, or behaving unexpectedly.
6. Follow the `Works Now`, `Blocked`, `Support Scorecard`, and `Missing Evidence` guidance before attempting update or mapping work.

## Support Tiers

| Tier | What it means |
| --- | --- |
| `supported` | Diagnostics, update, and any confirmed mapping flows are available when safety gates are satisfied. |
| `read-only candidate` | Diagnostics are available, but update and mapping stay blocked until runtime and hardware confirmation are complete. |
| `detect-only` | OpenBitdo can identify the device, but deeper workflows are intentionally unavailable. |

Candidate-readonly devices may expose a guarded non-firmware write/readback probe for maintainers.
It requires advanced mode, local write-risk acknowledgement, and a per-PID unlock file; firmware and bootloader writes remain blocked.

## Hardware Verification Without A Lab

If you do not have a controller connected, use `openbitdo --mock` plus the automated test suite to verify the app flow.
Real-device promotion still requires hardware smoke evidence; mock mode does not prove firmware or mapping safety.
When a device is not fully supported, run diagnostics and share the generated TOML report instead of attempting writes.

## macOS Packaging Caveat

Current macOS release artifacts are unsigned and non-notarized.
That means Gatekeeper friction is expected for the `.pkg`, tarball, and standalone binary.
If the installer path is inconvenient, use the extracted tarball or Homebrew tap as the fallback path.
Apple Developer signing and notarization are deferred until credentials are available.

## Release And Package Map

- GitHub prereleases publish the canonical release assets.
- AUR publishes `openbitdo-bin` from the Linux tarballs and release-derived checksums.
- Homebrew publishes through the separate tap repo `bybrooklyn/homebrew-openbitdo`.
- Package-manager metadata is rendered from published assets so release checksums stay authoritative.

## Where To Go Next

- Developer and release-engineering details: [sdk/README.md](sdk/README.md)
- Current RC release gate: [RC_CHECKLIST.md](RC_CHECKLIST.md)
- Changelog and release notes: [CHANGELOG.md](CHANGELOG.md)
- CLI and packaging migration notes: [MIGRATION.md](MIGRATION.md)
- Device catalog: [spec/device_name_catalog.md](spec/device_name_catalog.md)
- Protocol overview: [spec/protocol_spec.md](spec/protocol_spec.md)
