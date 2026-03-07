# OpenBitdo SDK

OpenBitdo SDK includes:
- `bitdo_proto`: protocol/transport/session library
- `bitdo_app_core`: shared firmware-first workflow and policy layer
- `bitdo_tui`: Ratatui/Crossterm terminal app
- `openbitdo`: beginner-first launcher (`openbitdo` starts guided TUI)

## Build
```bash
cargo build --workspace
```

## Test
```bash
cargo test --workspace --all-targets
```

## Guard
```bash
./scripts/cleanroom_guard.sh
```

## Hardware smoke report
```bash
./scripts/run_hardware_smoke.sh
```

## TUI app examples (`openbitdo`)
```bash
cargo run -p openbitdo -- --mock
```

## CLI surface
- `openbitdo [--mock]`: interactive dashboard flow (mouse-primary, minimal keyboard).

## Interactive behavior (`openbitdo`)
- dashboard starts with:
  - searchable device list (left)
  - quick actions (center)
  - persistent event panel (right)
- primary quick actions:
  - `Refresh`
  - `Diagnose`
  - `Recommended Update`
  - `Edit Mapping` (capability-gated)
  - `Settings`
  - `Quit`
- firmware transfer path:
  - preflight generation
  - explicit confirm/cancel action
  - updating progress and final result screen
- mapping editors are draft-first with:
  - apply
  - undo
  - reset
  - restore backup
- recovery lock behavior is preserved when rollback fails.

## Headless library API
- headless automation remains available as a Rust API in `bitdo_tui`:
  - `run_headless`
  - `RunLaunchOptions`
  - `HeadlessOutputMode`
- `openbitdo` CLI does not expose a headless command surface.

## Config schema (v2)
- persisted fields:
  - `schema_version`
  - `advanced_mode`
  - `report_save_mode`
  - `device_filter_text`
  - `dashboard_layout_mode`
  - `last_panel_focus`
- v1 files are read with compatibility defaults and normalized to v2 fields at load time.

## Packaging
```bash
./scripts/package-linux.sh v0.0.1-rc.2 x86_64
./scripts/package-linux.sh v0.0.1-rc.2 aarch64
./scripts/package-macos.sh v0.0.1-rc.2 arm64 aarch64-apple-darwin
```

Packaging outputs use:
- `openbitdo-<version>-<os>-<arch>.tar.gz`
- `openbitdo-<version>-<os>-<arch>` standalone binary
- `.sha256` checksum file for each artifact
- macOS arm64 additionally emits `.pkg` (unsigned/ad-hoc for RC)

## Release Workflow
- CI checks remain in `.github/workflows/ci.yml`.
- Tag-based release workflow is in `.github/workflows/release.yml`.
- Release tags must originate from `main`.
- `v0.0.1-rc.2` style tags publish GitHub pre-releases.
- Release notes are sourced from `/Users/brooklyn/data/8bitdo/cleanroom/CHANGELOG.md`.
- Package-manager publish runs only after release assets are published.

## Public RC Gate
- No open GitHub issues with label `release-blocker`.
- Scope-completeness gate:
  - JP108 RC scope is dedicated mapping only (`A/B/K1-K8`).
  - Ultimate2 RC scope is expanded mapping for required fields only.
- Clean-tree requirement from `/Users/brooklyn/data/8bitdo/cleanroom/RC_CHECKLIST.md` must be satisfied before tagging.

## Distribution Prep
- Homebrew install path for public RC:
  - `brew tap bybrooklyn/openbitdo`
  - `brew install openbitdo`
- Homebrew Core inclusion is not required for `v0.0.1-rc.2`.
- Homebrew formula scaffold: `/Users/brooklyn/data/8bitdo/cleanroom/packaging/homebrew/Formula/openbitdo.rb`
- Homebrew tap sync script (disabled by default): `/Users/brooklyn/data/8bitdo/cleanroom/packaging/homebrew/sync_tap.sh`
- Tap repository: `bybrooklyn/homebrew-openbitdo`
- AUR package sources:
  - `/Users/brooklyn/data/8bitdo/cleanroom/packaging/aur/openbitdo-bin`
- AUR package names:
  - `openbitdo-bin`
- Release metadata renderer:
  - `/Users/brooklyn/data/8bitdo/cleanroom/packaging/scripts/render_release_metadata.sh`
- AUR publish workflow:
  - `/Users/brooklyn/data/8bitdo/cleanroom/.github/workflows/aur-publish.yml`
  - gated by `AUR_PUBLISH_ENABLED=1`
- Homebrew publish path:
  - `release.yml` renders checksum-pinned formula and runs `sync_tap.sh`
  - gated by `HOMEBREW_PUBLISH_ENABLED=1`
- macOS `.pkg` caveat:
  - unsigned/ad-hoc is accepted for `v0.0.1-rc.2`
  - notarization required for `v0.1.0`

## CI Gates
- required:
  - `guard`
  - `test`
  - `tui-smoke-test`
  - `aur-validate`
  - `build-macos-arm64`
