# OpenBitdo Migration Notes

## Scope
This migration restores the single-command `openbitdo` CLI contract and removes the `ui`/`run` subcommand surface from user-facing usage.

## What changed
- `bitdoctl` was removed.
- `openbitdo cmd ...` was removed.
- `openbitdo` now launches interactive TUI directly (with optional `--mock`).
- subcommand forms `openbitdo ui ...` and `openbitdo run ...` are rejected (historical).
- headless output modes remain available through the Rust API, not the CLI.
- Settings schema moved to v2 fields while keeping compatibility defaults for v1 files.

## Command mapping
| Prior command form | Current command |
| --- | --- |
| `cargo run -p openbitdo --` | `cargo run -p openbitdo --` |
| `cargo run -p openbitdo -- --mock` | `cargo run -p openbitdo -- --mock` |
| `openbitdo ui --mock` (historical) | `openbitdo --mock` |
| `openbitdo run ...` (historical) | Not supported in CLI |

## New usage
From `/Users/brooklyn/data/8bitdo/cleanroom/sdk`:

Interactive dashboard:

```bash
cargo run -p openbitdo --
cargo run -p openbitdo -- --mock
```

## Historical note
The temporary subcommand surface (`openbitdo ui` / `openbitdo run`) is historical (historical) and should not be used for current workflows.

## Headless library API
Headless automation remains available to Rust callers through `bitdo_tui`:

```bash
run_headless(core, RunLaunchOptions { output_mode: HeadlessOutputMode::Json, ..Default::default() })
```

## Settings schema migration
Current schema is `schema_version = 2` with fields:
- `advanced_mode`
- `report_save_mode`
- `device_filter_text`
- `dashboard_layout_mode`
- `last_panel_focus`

Compatibility behavior:
- v1 settings files load with defaults for missing v2 fields.
- if `advanced_mode = false`, `report_save_mode = off` is normalized to `failure_only`.

## CI note
The CLI smoke coverage now validates:
- `openbitdo --help` exposes single-command option usage.
- `openbitdo ui ...` and `openbitdo run ...` fail as unsupported forms (historical).
