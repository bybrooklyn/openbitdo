# OpenBitdo Migration Notes

This file explains the current user and contributor surface after the CLI and packaging cleanup.

## Current CLI Contract

- `openbitdo` launches the interactive dashboard.
- `openbitdo --mock` launches the dashboard without real hardware.
- Historical subcommand-style entry points are no longer part of the supported CLI.

## Current Packaging Contract

- GitHub prereleases are the canonical release source.
- AUR publishes `openbitdo-bin`.
- Homebrew publishes through the separate tap repo `bybrooklyn/homebrew-openbitdo`.
- macOS artifacts remain unsigned and non-notarized until Apple credentials exist.

## Current Settings Contract

Persisted UI state uses `schema_version = 2` with these fields:

- `advanced_mode`
- `report_save_mode`
- `device_filter_text`
- `dashboard_layout_mode`
- `last_panel_focus`

Compatibility behavior:

- v1 settings still load with defaults for new fields.
- invalid settings files now raise a warning and fall back to defaults instead of being silently accepted.

## Current Library Contract

OpenBitdo keeps headless automation as a Rust API, not a public CLI surface.
The supported entry points remain:

- `bitdo_tui::run_headless`
- `bitdo_tui::RunLaunchOptions`
- `bitdo_tui::HeadlessOutputMode`

## Practical Migration Guidance

- If you used the historical CLI subcommands, switch to `openbitdo` or `openbitdo --mock`.
- If you need automation, call the Rust API instead of expecting a supported headless CLI.
- If you document install paths, prefer Homebrew tap, AUR, tarball, or source build rather than old ad hoc command forms.
