# OpenBitdo RC Checklist (`v0.1.0`)

This checklist defines the release-candidate gate for the current public release tag.

`v0.1.0` is the first Go/Bubbletea release: a from-scratch, clean-room rewrite of the prior Rust
CLI (`v0.0.2`), not an iterative RC of it — same functional capability, a redesigned TUI, and
controller/keyboard navigation. See `CHANGELOG.md` and `docs/MIGRATION.md` for what changed.

## Release Policy

- Tag format: `v*`
- Current release tag: `v0.1.0`
- Tag source: `main` only
- Release trigger: tag push
- Public release rule: zero open issues labeled `release-blocker`

## Required CI Checks

From `.github/workflows/ci.yml`:

- `guard`
- `aur-validate`
- `tui-smoke-test`
- `build-macos-arm64`
- `build-linux-x86_64`
- `build-linux-aarch64`
- `test` (aggregates the above via `needs:`, plus `go vet`/`golangci-lint`/`go test -race`)

## Clean Tree Gate

From the repository root:

```bash
git status --porcelain
git clean -ndX
```

Expected:

- no tracked modifications or staged changes
- ignored-output review only from `git clean -ndX`

## Artifact Expectations

Release assets must include:

- `openbitdo-v0.1.0-linux-x86_64.tar.gz`
- `openbitdo-v0.1.0-linux-x86_64`
- `openbitdo-v0.1.0-linux-aarch64.tar.gz`
- `openbitdo-v0.1.0-linux-aarch64`
- `openbitdo-v0.1.0-macos-arm64.tar.gz`
- `openbitdo-v0.1.0-macos-arm64`
- `openbitdo-v0.1.0-macos-arm64.pkg`
- `.sha256` files for every artifact above

## Distribution Gate

- GitHub prerelease assets must be published successfully.
- AUR publish must render checksum-pinned metadata and update `openbitdo-bin`.
- Homebrew publish must render a checksum-pinned formula and update `bybrooklyn/homebrew-openbitdo`.
- Both `AUR_PUBLISH_ENABLED` and `HOMEBREW_PUBLISH_ENABLED` repo variables are live with real
  credentials configured — a `v0.1.0` tag push triggers genuine external publishes, not a dry run.

## macOS Packaging Gate

- `.pkg` remains unsigned and non-notarized for this release.
- Gatekeeper friction is expected and must be documented.
- Tarball and standalone binary remain the fallback paths.
- No Apple Developer credentials exist yet; unrelated to and unchanged by the Go rewrite.

## Manual Smoke Expectations

1. Linux `x86_64`: launch `openbitdo --mock`
2. Linux `aarch64`: launch `openbitdo --mock`
3. macOS arm64 standalone binary: launch `openbitdo --mock`
4. macOS arm64 `.pkg`: confirm payload installation path and launch behavior where possible
5. Controller navigation: on Linux and macOS, confirm the device dashboard responds to an
   attached 8BitDo controller's own buttons, not just keyboard input — this is genuinely
   untested against real hardware as of `v0.1.0` (no 8BitDo controller was available during
   development; see `docs/spec/gamepad_input.md`) and is the first thing to verify manually.

## Current Status Snapshot

| Gate | Status | Notes |
| --- | --- | --- |
| Rewrite functional parity | Done | Ported from the Rust `v0.0.2` implementation; behavioral test suites ported alongside it; TUI redesigned rather than 1:1 ported. |
| Local build/test verification | Done | `go build`, `go vet`, `go test`, `go test -race`, `golangci-lint`, `gofmt`, `cleanroom_guard.sh`, `check_docs_consistency.sh` all clean. |
| Real CI run on GitHub Actions | Done | Confirmed green on real GitHub Actions runners as of run #202 (`rewrite/go-tui`, commit `2888ae5`) and every push since; re-verify once more on the exact commit tagged for release. |
| GitHub prerelease assets | Pending | Verify `v0.1.0` assets after the tag workflow completes. |
| AUR publication | Pending | Verify `openbitdo-bin` updates to `v0.1.0` after release publication. |
| Homebrew publication | Pending | Verify `bybrooklyn/homebrew-openbitdo` updates to `v0.1.0` after release publication. |
| macOS notarization | Deferred | Explicitly out of scope until Apple credentials exist. |
| Real-hardware validation | Partial — real protocol-response mystery found | 2026-08-27, real Ultimate2 (PID `0x6013`, serial `22EC9EA4DF`) connected and tested via `--diagnostics-dump` and a new manual `teatest`-driven integration run (`internal/tui/manual_hardware_test.go`, `-tags manual`). **Confirmed working**: device enumeration, `Open`/`Write` all succeed end-to-end through the whole app (Phase 1's `internal/machid` IOKit fix genuinely works, not just in isolated manual tests); the Devices, Diagnostics, and Mapping Editor screens all handle a real non-mock session honestly — no crash, no hang, accurate "0/12 passed" with correct per-command detail, real report saved under the device's real serial. **Confirmed broken, pre-existing, NOT introduced by the rewrite**: this specific controller never sends a response to any protocol command (all 12 `DiagProbe` checks — including `Confidence:"confirmed"` ones like `GetPid`/`GetMode`/`Version` — get `bytes_written=64, bytes_read=0`); this was already documented as an open mystery in `internal/machid/machid_darwin.go`'s package doc before tonight and is now reconfirmed via the full app, not just an isolated transport test. Root cause needs hardware-informed protocol reverse-engineering (a real USB traffic capture against the vendor app, most likely) — a distinct, larger effort from anything in this rewrite, tracked separately, not a `v0.1.0` blocker in itself since the app already degrades honestly. **Real controller navigation tested and root-caused (not a code bug)**: a live capture (`internal/input/manual_nav_capture_test.go`, two runs with the user actively pressing buttons/d-pad/sticks, second one with `-count=1` to rule out a stale Go test-cache hit on the first) received zero `EventDPadChanged`/`EventButtonDown` events both times. Direct enumeration (`hid.Enumerate(0x2dc8, 0)`) shows this controller currently exposes exactly **one** HID interface — `usagePage=0xffa0, usage=0x0001`, the same vendor config channel diagnostics already couldn't get a response from — not a standard Generic-Desktop Gamepad interface (`usagePage=0x0001, usage=0x0005`). The app opens and reads everything the OS enumerates for this vid; there's nothing else there to open. This looks like a controller-mode question (8BitDo controllers commonly expose different USB interfaces per mode — X-input/D-input/Switch/etc., usually switched via a button-combo or physical switch) rather than anything fixable in this codebase; needs the user to try a different connection mode and re-test, not further code changes. Hotplug connect/disconnect (`internal/input`'s poller) has synthetic/unit coverage but not yet a live unplug/replug confirmation — needs the user's physical action to close out. |

## Historical Notes

- Historical RC activity for earlier (Rust-era) candidates is preserved in commit history and the
  changelog.
- Troubleshooting for AUR SSH publication lives in `docs/process/aur_publish_troubleshooting.md`.
