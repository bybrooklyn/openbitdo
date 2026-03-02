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

## Beginner-first behavior
- launch with no subcommands
- if no device is connected, OpenBitdo starts in a waiting screen with `Refresh`, `Help`, and `Quit`
- if one device is connected, it is auto-selected and ready for action
- choose `Recommended Update` or `Diagnose` from large clickable actions
- for JP108 devices (`0x5209`/`0x520a`), `Recommended Update` enters a dedicated-button wizard:
  - edit `A/B/K1-K8`
  - backup + apply
  - guided button test
- for Ultimate2 devices (`0x6012`/`0x6013`), `Recommended Update` enters a core-profile wizard:
  - choose slot (`Slot1/2/3`)
  - set mode
  - edit RC mapping slots (`A/B/K1-K8`) with known controller-button targets
  - view/edit L2/R2 analog values when capability supports writes
  - backup + apply
  - guided button test
- firmware path defaults to verified recommended download; local file fallback is prompted if unavailable
- update transfer requires one plain-language `I Understand` confirmation
- detect-only PIDs stay read/diagnostic-only with a clear block reason
- mouse support:
  - left click for primary actions
  - right click on device rows for context menu actions
  - scroll wheel to navigate device list/detail panes
- support reports are TOML only
  - beginner mode: `failure_only` (default) or `always`
  - advanced mode: `failure_only`, `always`, or `off` (with warning)
- advanced mode is toggled from About (`t` or click) and persisted to OS config TOML
- advanced report hotkeys after a failure report exists:
  - `c` copy report path
  - `o` open report file
  - `f` open report folder
- open About from home (`a` key or click `About`) to view:
  - app version
  - git commit short and full hash
  - build date (UTC)
  - compile target triple
  - runtime OS/arch
  - firmware signing-key fingerprint (short with full-view toggle, plus next-key short)

## Packaging
```bash
./scripts/package-linux.sh v0.0.1-rc.1 x86_64
./scripts/package-linux.sh v0.0.1-rc.1 aarch64
./scripts/package-macos.sh v0.0.1-rc.1 arm64 aarch64-apple-darwin
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
- `v0.0.1-rc.1` style tags publish GitHub pre-releases.
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
- Homebrew Core inclusion is not required for `v0.0.1-rc.1`.
- Homebrew formula scaffold: `/Users/brooklyn/data/8bitdo/cleanroom/packaging/homebrew/Formula/openbitdo.rb`
- Homebrew tap sync script (disabled by default): `/Users/brooklyn/data/8bitdo/cleanroom/packaging/homebrew/sync_tap.sh`
- Tap repository: `bybrooklyn/homebrew-openbitdo`
- AUR package sources:
  - `/Users/brooklyn/data/8bitdo/cleanroom/packaging/aur/openbitdo`
  - `/Users/brooklyn/data/8bitdo/cleanroom/packaging/aur/openbitdo-bin`
- AUR package names:
  - `openbitdo`
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
  - unsigned/ad-hoc is accepted for RC
  - notarization required for `v0.1.0`

## CI Gates
- required:
  - `guard`
  - `test`
  - `tui-smoke-test`
  - `aur-validate`
  - `build-macos-arm64`
