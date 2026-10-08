# Changelog

All notable changes to OpenBitdo are tracked here.

## Unreleased

### Added

- Retro 108: the Mapping tab edits the whole profile. Any of the 104 assignable keys can be set to
  a key (with an optional modifier), a media key or a mouse action through a searchable list; the
  Win, Alt+Tab and Alt+F4 locks and the volume level are settings; the profile can be renamed or
  erased. Each write is read back, and what was written is put back if the keyboard did not keep
  it.
- Retro 108 macros: press `m` on a key to build a macro for it from key taps, holds, releases and
  pauses, with a repeat count and a pause between repeats. A macro that would leave a key held is
  refused. On a real keyboard this stays behind advanced mode until a macro write has been
  confirmed on hardware.
- Profile and macro names are written the way the vendor's application writes them, so each
  program can read names the other wrote.
- Ultimate 2 macros: each slot's four macros can be built in the editor (a trigger button, then
  steps that hold buttons and stick directions for a time), saved, applied and removed.
- Ultimate 2 motion and lights: the editor can steer a stick with the motion sensor (which stick,
  the button that enables it, hold or toggle, sensitivity, dead zone) and set the stick-ring
  lights (off, tracing, fire ring or a colour per light). Simulator-tested, like the rest of the
  controller editor.
- Profile files: `E` in either editor saves the draft (a keyboard profile, or one controller slot)
  as a readable TOML file under the config directory's `profiles/`; `I` loads one back into the
  draft. A file a device could not hold is refused with the reason.
- Ultimate 2: the controller's real configuration protocol (a checksummed header over one
  1592-byte record per platform, written by byte range and committed) replaces frames that no
  controller ever answered. A profile editor covers three slots, the 22-input button map (back
  paddles and extra buttons included), stick and trigger ranges, vibration strength and option
  switches. It runs against a simulated controller in `--mock`; on real hardware it stays behind
  advanced mode until the exchange has been confirmed on a controller.
- A 2.4G receiver is asked whether its controller is connected. A receiver whose controller is
  off is shown as "Controller off" rather than as a working device.

### Fixed

- Linux: devices could not be opened. OpenBitdo now talks to devices through the kernel's hidraw
  nodes instead of a libusb-backed library. The old backend needed write access to the raw USB
  device, which the shipped udev rule never granted (it was named `99-`, so its `uaccess` tag was
  set after the step that applies it), and it could not tell a multi-interface device's
  interfaces apart, so a Retro 108 was rejected as "ambiguous" before any open.
- Linux: controller navigation looked up report descriptors under a path the old backend never
  returned, so it could not start for any device.
- The udev rule is now `70-openbitdo.rules` and matches hidraw nodes. Opening a device no longer
  detaches the kernel's HID driver, so a keyboard keeps working while it is open.
- A retried read never resent its request, so every retry waited on a reply that was not coming.
- A stale reply to an earlier command could be validated as the reply to the next one.
- JP108 mapping: a short table reply was padded with zeros and used as the pre-write backup, so
  a rollback could write zeros back. A short reply is now an error, and a write is confirmed by
  reading the table back.
- TUI: `q`, `?` and `x` were handled before the device filter, so typing a name could quit the
  app. The filter title printed a raw escape code. At 80x24 the device list was not shown.
- TUI: a brick-risk dialog confirmed on enter as soon as it appeared; it now starts on Cancel.
  The write probe's dialog no longer describes a firmware write.
- TUI: after a failed rollback, a mouse click could still reach the mapping screen.
- `--mock` no longer opens attached hardware for controller navigation.

### Changed

- The TUI is laid out differently. Devices are always listed down the left, each with a plain
  verdict (working, limited, can't connect, no access). The right side shows the selected device
  in three tabs: Overview, Checks and Mapping. This replaces the two-panel dashboard and its
  `Status` / `Works now` / `Blocked` / `Next step` card.
- There is one cursor, always in the right-hand pane. `tab` (or `1`-`3`, or `←` `→`) changes
  section and `d` changes device from anywhere; there is no focus to move between panels.
- Overview splits a device's actions into "You can" and "Not yet", with the reason beside each
  thing that is not available. Only what can be done is selectable.
- A device that answers checks but whose settings cannot be changed is "Limited", not
  "Supported". A device with no configuration interface is "Can't connect", and nothing is
  offered for it.
- Device names replace registry IDs. Text wraps instead of being cut off at the edge.
- The help overlay lists the keys for the current view; the footer only shows keys that work
  where you are.
- Checks names each check in plain words and explains unanswered ones. `enter` shows a check's
  raw details, `v` opens the report (copy with `c`, save with `w`), `f` filters to unanswered
  checks.
- Firmware is listed under "Not yet" as `Deferred in 0.0.3` instead of as a disabled action.
  Settings is a page opened with `s`, not a row in each device's action list.
- A controller can reach everything: the d-pad changes section and its third button changes
  device.
- A device that works as a controller or keyboard but has no configuration interface is shown
  as "Playing" or "Typing", with the settings limitation stated separately, instead of "Can't
  connect".

### Added

- Retro 108 keyboards (`0x5209`) can be read and remapped. The keyboard's configuration interface
  takes 33-byte commands on report `0x52`, not the 64-byte frames the registry had for it, so
  until now nothing sent to it was answered. OpenBitdo now reads its profile name, feature flags
  and the assignment of each of the ten dedicated buttons (A, B, K1-K8), and can assign a button
  to a key or clear it. A write is checked by reading it back, and a profile name is written
  first when the keyboard has none. The framing is recorded in
  `docs/clean-room-evidence/dossiers/5209/jp108_hid.toml`. On a real keyboard the reads, the
  name write and two assignments were acknowledged and read back, and the A and B buttons were
  seen sending the assigned keys. A mapping is only used while the keyboard's Profile button is
  on. F13-F24 are offered as targets by their HID usages (`0x68`-`0x73`).
- A Buttons tab: press anything on a controller and its button number lights up. It only reads
  what the controller already sends, and is how to find the number of a back button or extra
  shoulder button, which have no standard one. Decoding is tested against the report
  descriptor and reports of a real Ultimate 2 in gamepad mode (`0x6012`).

### Hardware evidence

- First Linux hardware run, recorded in
  `docs/clean-room-evidence/hardware_run_linux_2026-10-08.md`. An Ultimate 2 (`0x6013`) answers 4
  of the 12 safe reads over hidraw; the "wrote 64 bytes, read 0" result below does not reproduce
  on Linux. A Retro 108 (`0x5209`) over USB did not answer the protocol documented at the time;
  it does answer its own (see Added).

## v0.0.3

### Changed

- Rewrote the implementation from Rust (ratatui) to Go (Bubbletea). Full functional parity with
  v0.0.2, verified by porting the Rust behavioral test suites and adding end-to-end interactive
  tests against the running program (`charmbracelet/x/exp/teatest`).
- Redesigned the TUI from scratch rather than porting the screen layout 1:1: responsive compact
  and wide layouts, bounded scrollable views, real overlay-modal confirmations, adaptive color
  roles, and a dashboard organized around `Status`, `Works now`, `Blocked`, and `Next step`.
- Relicensed from BSD-3-Clause to GPL-3.0-or-later.
- PID and command registry tables are now generated directly from `spec/pid_matrix.csv` and
  `spec/command_matrix.csv` at build time, making the spec files the literal single source of
  truth instead of hand-maintained tables checked against the CSVs by separate tests.
- Pinned release-facing metadata to `v0.0.3` across AUR and Homebrew.

### Added

- Controller/gamepad navigation: the app can be driven with an 8BitDo controller's own d-pad and
  buttons, alongside keyboard and mouse navigation, live from the device dashboard at startup.
  This requires the OS to expose the controller as a standard USB-HID Generic Desktop Gamepad
  (usage `0x0001:0x0005`). The feature is implemented and covered by tests, but it is **not
  verified on real hardware** — see "Known limitations" below.
- A real one-time "this may brick your device" confirmation before any unsafe/firmware action.
  Previously this flag was hardcoded true with a comment claiming a UI surface that didn't
  actually exist.
- Clearer diagnostics messaging for candidate-readonly / inferred-evidence devices: instead of a
  bare wall of "response signature mismatch" failures, the app now explains plainly that the
  device isn't hardware-confirmed yet, what that means for the checks shown, and how to help
  (improves the issue #15 user experience; the issue remains open until real hardware evidence
  closes it).

### Deferred in 0.0.3

- Firmware update is unavailable in production. The manifest feed and signing-key path remain
  test-only, and the TUI renders firmware as a disabled action labeled `Deferred in 0.0.3`.
- Ultimate 2 mapping on real hardware is blocked with the explicit reason
  `button-map framing not hardware-confirmed`. The Ultimate 2 editor remains available as a
  mock-only preview for UI testing.
- Fixture-backed hardware CI and firmware writes are not part of this release.

### Known limitations

These were measured against a wired 8BitDo Ultimate 2 (`0x2dc8:0x6013`) and are shipped known:

- Controller navigation is unverified on real hardware. In its tested mode this Ultimate 2
  publishes exactly one USB HID interface (`bNumConfigurations=1`, one interface, class 3) on the
  vendor page `0xffa0`, and no Generic Desktop Gamepad interface. Navigation has nothing to read
  from on that unit, so d-pad/button input does not drive the TUI there. Keyboard and mouse
  navigation are unaffected.
- Safe-read diagnostics return no data on this PID. All 12 applicable commands write 64 bytes
  successfully and read back 0. The transport itself is confirmed working — open and
  `IOHIDDeviceSetReport` both return `kIOReturnSuccess`, and a synchronous `GetReport` draws a
  real USB STALL, which shows the device is live and actively refusing that request. The command
  registry marks these commands `Confidence: "confirmed"`, meaning confirmed present in the
  vendor binary by static analysis, not confirmed to answer on real hardware; no evidence dossier
  exists for `0x6013`. Resolving this needs protocol reverse-engineering in the separate
  dirty-room evidence process, not transport changes.
- As a consequence, the dashboard can present a device as `Supported` with `works now: safe
  diagnostics` while every diagnostic on it fails. The support tier is derived from static
  evidence, not from a live probe result.

## v0.0.2

### Added

- Expanded `U2ButtonId` mapping to support all 17 keys for Ultimate 2 controllers.
- Added mapping and slot configuration support for Ultimate 2 and JP108 candidate-readonly PIDs.
- Promoted Ultimate 2 Bluetooth variant and receiver PIDs `0x600f` and `0x6011` to the full support tier.
- Added candidate write/readback validation tests.

### Fixed

- Resolved Homebrew tap publishing CI failures by updating access token scopes and handling.


## v0.0.1-rc.4

### Changed

- Release docs are being rewritten around the `v0.0.1-rc.4` flow.
- Homebrew publishing is being moved to a reusable workflow with the separate tap repo kept as the canonical Homebrew destination.
- TUI copy is being expanded so first-run guidance is clearer and blocked-action reasons are easier to understand.
- The checked-in Homebrew formula output is being removed; the template and rendered release metadata remain the source of truth.

## v0.0.1-rc.3

### Added

- Tag-driven GitHub prerelease assets for Linux `x86_64`, Linux `aarch64`, and macOS arm64.
- AUR publication for `openbitdo-bin` with release-derived checksums.
- Diagnostics screen with richer per-check detail and saved-report flow.

### Changed

- Firmware update defaults remain safe until the user explicitly acknowledges risk.
- Temporary recommended-firmware downloads are cleaned up after success, failure, or cancellation.
- Invalid persisted settings are surfaced as warnings instead of being silently discarded.

## v0.0.1-rc.1

### Added

- Beginner-first `openbitdo` launcher and terminal dashboard.
- Release packaging scripts for Linux and macOS artifacts.
- Unsigned, non-notarized macOS `.pkg` output for RC distribution.
- AUR and Homebrew release metadata rendering.

### Notes

- Historical RC notes are preserved here for audit continuity.
