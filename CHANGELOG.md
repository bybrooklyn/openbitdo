# Changelog

All notable changes to this project will be documented in this file.

## v0.0.1-rc.1

### Added
- Beginner-first `openbitdo` TUI flow with device-specific JP108 and Ultimate2 guided mapping paths.
- About screen showing app version, git commit, build date, and runtime/target platform.
- Release packaging scripts for Linux (`x86_64`, `aarch64`) and macOS arm64 outputs.
- macOS arm64 unsigned/ad-hoc `.pkg` packaging to `/opt/homebrew/bin/openbitdo`.
- AUR packaging sources for `openbitdo` and `openbitdo-bin`.
- Homebrew formula scaffolding and deferred tap sync script.
- Release workflow for tag-triggered GitHub pre-releases using changelog content.
- Release metadata rendering that computes authoritative checksums from published assets for AUR/Homebrew updates.

### Changed
- Project license transitioned to BSD 3-Clause.
- CI expanded to include macOS arm64 package build validation and AUR package metadata validation.
- Release process documentation updated for clean-tree requirements and RC gating policy.

### Notes
- Homebrew and AUR publication paths are automated and remain controlled by repo variables (`HOMEBREW_PUBLISH_ENABLED`, `AUR_PUBLISH_ENABLED`).
- Hardware CI gates remain required as configured in `ci.yml`.
