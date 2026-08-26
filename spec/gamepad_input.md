# Gamepad input decoding (menu navigation)

This document is engineering documentation for `internal/input`, not
clean-room evidence — it describes how the Go TUI decodes controller button
presses for *menu navigation*, which is a completely separate concern from
the 8BitDo command/diagnostic protocol documented in `spec/protocol_spec.md`.

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

3. **Descriptor acquisition** (`internal/input/navstream.go`) — the one
   real platform limitation: `github.com/karalabe/hid` does not expose a
   device's report descriptor through its Go API, and there is no portable
   Go way to fetch it. On Linux, the kernel exposes it directly as a file
   (`/sys/class/hidraw/<N>/device/report_descriptor`), so descriptor-driven
   decoding works there. On other platforms (including macOS, where this
   rewrite was built and where no 8BitDo hardware was available to validate
   against — see `MIGRATION.md`/the RC checklist for the current hardware
   validation gap), the nav manager cannot obtain a descriptor through
   `karalabe/hid` alone; it degrades to keyboard-only navigation rather than
   guessing a byte layout it has no evidence for. Closing that gap on macOS
   needs either an IOKit (`IOHIDDeviceGetReportDescriptor`) cgo binding or a
   HID library that exposes it directly — tracked as follow-up work, not
   something this rewrite fabricates a workaround for.

4. **Nav-only, separate from the command session**: the nav stream opens a
   read-only input-report loop on every enumerated `vid==0x2dc8` device at
   TUI startup, independent of `internal/protocol`'s command/diagnostic
   session. It never writes to the device and never competes with a
   diagnostic/mapping session for the same handle.
