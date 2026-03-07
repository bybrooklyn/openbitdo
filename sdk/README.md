# OpenBitdo SDK

This workspace contains the OpenBitdo runtime, protocol layer, and release packaging scripts.

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
./scripts/cleanroom_guard.sh
```

## Local Run

```bash
cargo run -p openbitdo --
cargo run -p openbitdo -- --mock
```

`openbitdo` intentionally exposes a single interactive CLI surface.
Headless automation remains available through the Rust API in `bitdo_tui`.

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
