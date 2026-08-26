# OpenBitdo Migration Notes

This file explains the current user and contributor surface. It covers two migrations: the
CLI/packaging cleanup that predates the rewrite below, and the Rust-to-Go rewrite itself.

## Rust to Go Rewrite

OpenBitdo's implementation moved from a Rust workspace (historical) — `sdk/crates/{bitdo_proto,
bitdo_app_core,bitdo_tui,openbitdo}`, using ratatui — to Go (`cmd/openbitdo`,
`internal/{protocol,core,tui,input}`, using Bubbletea). The user-facing CLI contract below is
unchanged. What changed:

- The TUI was redesigned, not ported line-for-line — new screen layout, real overlay-modal
  confirmations (including a genuine brick-risk acknowledgement before unsafe/firmware actions,
  which the Rust CLI had hardcoded as always-true with no actual confirmation dialog), and a new
  visual theme.
- The app now navigates with a keyboard or an 8BitDo controller's own buttons, decoded from the
  standard USB-HID gamepad usage page (see `spec/gamepad_input.md`).
- The license changed from BSD-3-Clause to GPL-3.0-or-later.
- The PID/command registries are generated directly from `spec/pid_matrix.csv` and
  `spec/command_matrix.csv` at build time (`go generate`), rather than hand-maintained Rust tables
  cross-checked against the CSVs by separate tests.
- Settings and support-report schemas are fresh (see below) — an existing Rust `config.toml` is
  not migrated; if present and unparseable against the new schema, OpenBitdo warns and falls back
  to defaults.

## Current CLI Contract

- `openbitdo` launches the interactive dashboard.
- `openbitdo --mock` launches the dashboard without real hardware.
- Historical subcommand-style entry points are no longer part of the supported CLI.
- Diagnostics, support reports, firmware preflight, and mapping entry points are reached from the TUI, not from public subcommands.

## Current Packaging Contract

- GitHub prereleases are the canonical release source.
- AUR publishes `openbitdo-bin`.
- Homebrew publishes through the separate tap repo `bybrooklyn/homebrew-openbitdo`.
- macOS artifacts remain unsigned and non-notarized until Apple credentials exist.

## Current Settings Contract

Persisted UI state uses `schema_version = 1` (Go's own schema — deliberately not continuous with
the prior Rust TUI's `schema_version = 2`, since the redesigned UI doesn't have equivalents for
some of the old fields) with these fields:

- `advanced_mode`
- `report_save_mode`

Compatibility behavior: an invalid or unparseable settings file (including an old Rust-era one)
raises a warning and falls back to defaults rather than being silently accepted or crashing.

## Current Library Contract

OpenBitdo keeps headless/programmatic access as a Go API, not a public CLI surface. The Bubbletea
model is constructed via `internal/tui.NewModel` and driven directly (or through
`charmbracelet/x/exp/teatest`) — this is how the test suite exercises the app without a real
terminal; it isn't exposed as a public package or a CLI flag.

## Practical Migration Guidance

- If you used the historical CLI subcommands, switch to `openbitdo` or `openbitdo --mock`.
- If you need automation, this isn't a supported public surface — OpenBitdo is an interactive TUI.
- If you document install paths, prefer Homebrew tap, AUR, tarball, or source build rather than old ad hoc command forms.
- If you have an old Rust-era `config.toml`, it will be replaced with fresh Go-schema defaults on
  first run rather than migrated.
