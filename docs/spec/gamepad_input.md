# Gamepad input decoding (menu navigation)

This document is engineering documentation for `internal/input`, not
clean-room evidence — it describes how the Go TUI decodes controller button
presses for *menu navigation*, which is a completely separate concern from
the 8BitDo command/diagnostic protocol documented in `docs/spec/protocol_spec.md`.

## Scope

The Rust implementation had no gamepad input handling at all (verified: no
`gilrs`/`sdl2`/joystick dependency anywhere in `sdk/`). The Go rewrite adds
it so the TUI can be navigated with a controller's own d-pad/buttons, not
just a keyboard.

This is decoded entirely from the **standard USB-HID gamepad usage page**
(USB HID Usage Tables, Generic Desktop Page `0x01` and Button Page `0x09`) —
a public USB-IF standard that any compliant HID gamepad exposes, including
8BitDo controllers in DInput mode. It is not vendor-proprietary information
and does not touch the clean-room evidence boundary.

## How it works

1. **Report descriptor parsing** (`internal/input/descriptor.go`): a
   general-purpose parser for the USB HID 1.11 report descriptor format
   (Main/Global/Local items). It walks a device's descriptor and produces a
   flat list of `Field`s — usage page, usage (or usage range for arrays),
   report size/count, and bit offset within an input report. This is
   standard-compliant and works for any HID device's descriptor, not just
   8BitDo's.

2. **Report decoding** (`internal/input/gamepad.go`): given the parsed
   fields and one raw input report, extracts:
   - **Hat Switch** (Generic Desktop usage `0x39`): the standard 8-way d-pad
     encoding (0-7 = N/NE/E/SE/S/SW/W/NW, 8/0xF = neutral).
   - **X/Y axes** (usages `0x30`/`0x31`) as an analog-stick d-pad fallback
     when no hat switch field is present, thresholded around the field's
     logical-range midpoint.
   - **Button page** (`0x09`) fields, whether declared as a bitmask array
     (one button index per bit-width chunk) or as individual variable bits,
     as a set of pressed button-usage IDs.

3. **Descriptor acquisition** — `github.com/karalabe/hid` does not expose a
   device's report descriptor through its Go API, so each platform has its
   own acquisition file behind a `fetchReportDescriptor(info hid.DeviceInfo)
   ([]byte, error)` build-tagged function (`internal/input/descriptor_*.go`),
   feeding the same platform-agnostic parser above:
   - **Linux** (`descriptor_linux.go`): the kernel exposes it directly as a
     file, `/sys/class/hidraw/<N>/device/report_descriptor`, derived from
     `info.Path` (karalabe/hid gives `/dev/hidrawN` paths on Linux).
   - **macOS** (`descriptor_darwin.go`): reads it via IOKit's `IOHIDManager`
     — re-enumerates devices independently (`IOHIDManagerCreate` /
     `SetDeviceMatching` / `Open` / `CopyDevices`) and matches by
     vendor/product/usage-page/usage (all reliably populated by
     `hid.DeviceInfo` on this platform), then reads the device's
     `ReportDescriptor` property (`kIOHIDReportDescriptorKey`) directly via
     `IOHIDDeviceGetProperty`. **Deliberately does not use `info.Path`**:
     karalabe/hid's vendored hidapi C backend resolves a device's
     `io_service_t` via `dlopen("/System/Library/IOKit.framework/IOKit",
     ...)` + `dlsym("IOHIDDeviceGetService")` — an OS X 10.5-era shim for
     detecting whether the modern API exists at runtime. That hardcoded
     framework path no longer resolves on modern macOS (confirmed directly:
     the `dlopen` call fails, reporting the library isn't in the dyld
     shared cache), so it silently falls through to a stale struct-offset
     hack that hasn't matched the real `IOHIDDevice` layout since OS X 10.5
     and produces a garbage `io_service_t` — meaning `Path` is empty for
     *every* device on a modern macOS system, not just non-8BitDo ones.
     Verified end-to-end against real hardware (no 8BitDo controller is
     available in this project's environment, but real IOKit HID devices
     are): `internal/input/descriptor_darwin_test.go` genuinely acquires
     and parses report descriptors from whatever real HID devices exist on
     the machine running it. **If a future change ever "simplifies" this
     back to using `info.Path` on darwin, it will silently break** — this
     isn't a style preference, `Path` is broken at the dependency level.
   - **Other platforms** (`descriptor_other.go`, e.g. Windows): still
     unimplemented — there is no release artifact for these platforms
     currently (see `RC_CHECKLIST.md`), so this degrades to keyboard-only
     navigation there by design, not by oversight. See `MIGRATION.md` for
     the still-outstanding real-8BitDo-hardware validation gap that applies
     across all platforms regardless of acquisition method.

4. **Nav-only, separate from the command session**: the nav stream opens a
   read-only input-report loop on every enumerated `vid==0x2dc8` device at
   TUI startup, independent of `internal/protocol`'s command/diagnostic
   session. It never writes to the device and never competes with a
   diagnostic/mapping session for the same handle.
