# OpenBitdo

OpenBitdo is a clean-room, beginner-first 8BitDo utility built around a modern terminal UI.
It focuses on safe diagnostics first, guided firmware/update flows for confirmed devices, and clear blocked-action messaging for devices that are still under investigation.

OpenBitdo is unofficial and not affiliated with 8BitDo. Firmware updates and device changes always carry risk. Read the prompts carefully and keep backups or recovery plans where available.

## What OpenBitdo Does Today

- Detect connected 8BitDo devices and explain their current support level.
- Run diagnostics and save support reports.
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
3. Pick a controller from the dashboard.
4. Use `Diagnose` first if you are unsure what is safe for that device.
5. Follow the support-tier guidance shown in the UI before attempting update or mapping work.

## Support Tiers

| Tier | What it means |
| --- | --- |
| `supported` | Diagnostics, update, and any confirmed mapping flows are available when safety gates are satisfied. |
| `read-only candidate` | Diagnostics are available, but update and mapping stay blocked until runtime and hardware confirmation are complete. |
| `detect-only` | OpenBitdo can identify the device, but deeper workflows are intentionally unavailable. |

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
