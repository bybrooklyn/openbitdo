# OpenBitdo SDK

This workspace contains the OpenBitdo runtime, protocol layer, and release packaging scripts.

## Toolchain

- Rust edition: 2024
- Minimum supported Rust version: 1.85
- CI and release builds continue to use the current stable toolchain

## Crates

- `bitdo_proto`: command registry, transport, session, and diagnostics behavior
- `bitdo_app_core`: firmware policy, device workflows, and support-tier gating
- `bitdo_tui`: terminal UI, app state, runtime loop, persistence, and headless API
- `openbitdo`: beginner-first launcher binary

## Build And Test

From `cleanroom/sdk`:

```bash
cargo build --workspace
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace --all-targets
./scripts/check_evidence_readiness.py
./scripts/cleanroom_guard.sh
```

## Local Run

```bash
cargo run -p openbitdo --
cargo run -p openbitdo -- --mock
```

`openbitdo` intentionally exposes a single interactive CLI surface.
Headless automation remains available through the Rust API in `bitdo_tui`.

The dashboard groups every detected 8BitDo device by support tier and keeps diagnostics as the primary safe workflow.
Candidate devices show a scorecard with missing static, runtime, hardware, write, backup/readback, and firmware evidence.
Use mock mode for UI verification when no hardware is attached; use the gated hardware smoke tests only with real lab devices and the required `BITDO_*` expectation variables.
The candidate write probe is a non-firmware, write/readback probe only; it requires advanced mode, local risk acknowledgement, and a per-PID unlock file under `candidate-unlocks/`.

## Packaging

```bash
./scripts/package-linux.sh v0.0.0-local x86_64
./scripts/package-linux.sh v0.0.0-local aarch64
./scripts/package-macos.sh v0.0.0-local arm64 aarch64-apple-darwin
```

Outputs:

- `openbitdo-<version>-linux-x86_64.tar.gz`
- `openbitdo-<version>-linux-aarch64.tar.gz`
- `openbitdo-<version>-macos-arm64.tar.gz`
- standalone binaries for each packaged target
- `.sha256` files for every artifact
- macOS `.pkg` from `pkgbuild`

Current macOS packaging remains unsigned and non-notarized by design.

## Release Flow

1. Tag from `main` using a `v*` tag.
2. `release.yml` verifies CI, secrets, and release blockers.
3. Linux and macOS artifacts are built and uploaded.
4. GitHub prerelease assets are published from those artifacts.
5. AUR and Homebrew metadata are rendered from published release assets.
6. AUR and Homebrew publication run only when their repo-variable gates are enabled.

## Package Manager Publishing

- AUR workflow: `.github/workflows/aur-publish.yml`
- Homebrew workflow: `.github/workflows/homebrew-publish.yml`
- Release metadata renderer: `packaging/scripts/render_release_metadata.sh`
- AUR source of truth:
  - tracked package metadata in `packaging/aur/openbitdo-bin`
  - template in `packaging/aur/openbitdo-bin/PKGBUILD.tmpl`
- Homebrew source of truth:
  - template in `packaging/homebrew/Formula/openbitdo.rb.tmpl`
  - published tap repo `bybrooklyn/homebrew-openbitdo`

Current repo-variable contract:

- `AUR_PUBLISH_ENABLED=1`
- `HOMEBREW_PUBLISH_ENABLED=1` when Homebrew publication is enabled
- `HOMEBREW_TAP_REPO=bybrooklyn/homebrew-openbitdo`

Required secrets:

- `AUR_USERNAME`
- `AUR_SSH_PRIVATE_KEY`
- `HOMEBREW_TAP_TOKEN`

## Docs Map

- Public project overview: [`../README.md`](../README.md)
- RC checklist: [`../RC_CHECKLIST.md`](../RC_CHECKLIST.md)
- Process docs: [`../process`](../process)
- Spec docs: [`../spec`](../spec)
