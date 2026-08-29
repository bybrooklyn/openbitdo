# OpenBitdo Migration Notes

This file explains the current user and contributor surface. It covers two migrations: the
CLI/packaging cleanup that predates the rewrite below, and the Rust-to-Go rewrite itself.

## Rust to Go Rewrite

OpenBitdo's implementation moved from a Rust workspace (historical) — `sdk/crates/{bitdo_proto,
bitdo_app_core,bitdo_tui,openbitdo}`, using ratatui — to Go (`cmd/openbitdo`,
`internal/{protocol,core,tui,input}`, using Bubbletea). The user-facing CLI contract below is
unchanged. What changed:

- The TUI was redesigned, not ported line-for-line - responsive compact and wide layouts,
  scrollable bounded content, real overlay-modal confirmations, a settings screen reachable without
  hardware, and beginner-facing dashboard language around status, works-now, blocked state, and
  next step.
- The app now navigates with a keyboard or an 8BitDo controller's own buttons, decoded from the
  standard USB-HID gamepad usage page when the OS exposes that interface (see
  `docs/spec/gamepad_input.md`). Mouse clicks and scrolling are also supported by the TUI.
- The license changed from BSD-3-Clause to GPL-3.0-or-later.
- The PID/command registries are generated directly from `docs/spec/pid_matrix.csv` and
  `docs/spec/command_matrix.csv` at build time (`go generate`), rather than hand-maintained Rust tables
  cross-checked against the CSVs by separate tests.
- Settings and support-report schemas are fresh (see below) — an existing Rust `config.toml` is
  not migrated; if present and unparseable against the new schema, OpenBitdo warns and falls back
  to defaults.
- Firmware update is unavailable in `v0.1.0-rc.1`. The production manifest/feed/key path is
  disabled; firmware code remains only for isolated tests with injected ephemeral keys and a local
  HTTP server.
- Ultimate 2 mapping on real hardware is unavailable in `v0.1.0-rc.1` because button-map framing
  is not hardware-confirmed. The Ultimate 2 mapping screen remains a mock-only preview for UI
  testing.

The Rust implementation itself is preserved, unmaintained, on the
[`legacy/rust-tui`](https://github.com/bybrooklyn/openbitdo/tree/legacy/rust-tui) branch — it's
buildable at that branch's tip if you need to compare behavior against the pre-rewrite version.

## Current CLI Contract

- `openbitdo` launches the interactive dashboard.
- `openbitdo --mock` launches the dashboard without real hardware.
- Historical subcommand-style entry points are no longer part of the supported CLI.
- Diagnostics, support reports, and mapping entry points are reached from the TUI, not from public
  subcommands.
- Firmware actions are rendered as disabled and deferred in `v0.1.0-rc.1`; keyboard or mouse
  activation must not start a download, firmware preflight, or device session.

## Current Packaging Contract

- GitHub prereleases are the canonical release source. The `v0.1.0-rc.1` prerelease must contain
  exactly 14 nonempty assets, including basename-only checksum sidecars.
- AUR publishes `openbitdo-bin` as `0.1.0rc1`.
- Homebrew publishes through the separate tap repo `bybrooklyn/homebrew-openbitdo` as
  `0.1.0-rc.1`.
- Linux artifacts support `x86_64` and `aarch64` on Ubuntu 22.04-era glibc or newer, and include
  shell completions plus the udev rule.
- macOS artifacts target Apple Silicon with `MACOSX_DEPLOYMENT_TARGET=13.0` and remain unsigned and
  non-notarized until Apple credentials exist. Intel macOS is unsupported for this release.

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
- If you document install paths, prefer Homebrew tap, AUR, tarball, or a Go 1.27.0 source build
  rather than old ad hoc command forms.
- If you have an old Rust-era `config.toml`, it will be replaced with fresh Go-schema defaults on
  first run rather than migrated.
- If you need Ultimate 2 mapping on real hardware or firmware updates, stay on a later development
  branch once that work is hardware-confirmed; those flows are intentionally deferred from
  `v0.1.0-rc.1`.
