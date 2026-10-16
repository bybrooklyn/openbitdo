# Linux hardware run, 2026-10-08

Runtime evidence from two attached devices, gathered with OpenBitdo itself over the kernel's
hidraw interface. Nothing here comes from vendor software: every frame sent is a `SafeRead` row
already in `docs/spec/command_matrix.csv`, and every frame received is quoted as the device
returned it. No write, boot or firmware command was sent to either device.

| | Ultimate 2 | Retro 108 |
| --- | --- | --- |
| VID:PID | `2dc8:6013` | `2dc8:5209` |
| Connection | USB, wired | USB, wired |
| Kernel name | `8BitDo Ultimate 2` | `8BitDo 8BitDo Retro 108 Keyboard` |
| Host | Linux 7.2, x86_64 | same |

## Ultimate 2 (`0x6013`)

One HID interface, usage page `0xffa0` usage `0x01`: input report ID `0x02` and output report ID
`0x81`, 63 data bytes each. This matches the 64-byte framing in `protocol_spec.md`. There is no
Generic Desktop gamepad interface in this mode, as the macOS run in `RC_CHECKLIST.md` also found.

Result: **4 of the 12 applicable safe reads are answered.** `Idle` sends the same bytes as
`GetReportRevision`, so the device answers four distinct requests, five checks.

| Command | Request (leading bytes) | Reply |
| --- | --- | --- |
| `GetPid` | `81 05 c1` | answered |
| `GetReportRevision` / `Idle` | `81 04 00 01` | answered |
| `GetModeAlt` | `81 05 08` | answered |
| `GetMode` | `81 04 05 01` | no reply within 200 ms, 3 attempts |
| `GetControllerVersion` | `81 04 21 01 00 00 06` | no reply |
| `Version` | `81 04 21 01` | no reply |
| `GetSuperButton` | `81 05 21` | no reply |
| `ReadProfile` | `81 06 00 01` | no reply |
| `U2GetCurrentSlot` | `81 05 40 12 01` | no reply |
| `U2ReadConfigSlot` | `81 05 41 12 01` | no reply |
| `U2ReadButtonMap` | `81 05 43 12 01` | no reply |

A passive listen of one second before any request received nothing: the device does not send
unsolicited reports on this interface.

Replies as received (64 bytes each, trailing zero bytes omitted):

```
GetPid             02 05 00 00 c1 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 a0 32 84 00 00 00 00 00 00 10
GetPid (other run) 02 05 00 00 c1 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 ff ff ff ff ff ff ff ff 00 10
GetReportRevision  02 04 04 00 00 01 00 00 00 00 00 00 00 00 00 00 00 00 67 00 01 00 13 60
GetModeAlt         02 05 00 00 08 00 02 00 00 00 02 00 00 00 00 00 00 00 13 60
```

What these establish:

- **The transport works.** The earlier macOS result of "12/12 wrote 64 bytes, read 0" does not
  reproduce over hidraw. The device answers.
- **`GetPid`'s reply does not carry the PID at bytes 22-23 on this device.** Those bytes read
  `a0 32` in one run and `ff ff` in another; the device's PID is `0x6013`. The spec's
  `detected_pid` field is therefore not valid for `0x6013`.
- **The PID does appear in two other replies:** bytes 22-23 of the `GetReportRevision` reply and
  bytes 18-19 of the `GetModeAlt` reply both read `13 60` (`0x6013`, little-endian).
- **The `05`-class replies echo the request's sub-command at byte 4** (`c1` for `GetPid`, `08`
  for `GetModeAlt`).

Open questions, not answered by this run:

- What bytes 22-29 of the `GetPid` reply mean. They change between runs.
- What bytes 18-21 of the `GetReportRevision` reply (`67 00 01 00`) mean.
- Which byte of the `GetModeAlt` reply is the mode. The runtime reads byte 5 (`00`); bytes 6 and
  10 both read `02`.
- Why the eight unanswered reads are unanswered: wrong command bytes for this model, a required
  preceding request, or a mode the controller was not in.

## Retro 108 (`0x5209`)

Three HID interfaces:

| Interface | Usage page : usage | Reports |
| --- | --- | --- |
| 0 | `0x0001:0x0002` (mouse) | input |
| 1 | `0x0001:0x0006` (keyboard), plus consumer and system control | input |
| 2 | `0x008c:0x0001` | input IDs `0xb1`, `0x54`; output IDs `0xb2`, `0x51`, `0x52`; 32 data bytes each |

Result: **no interface carries the documented protocol.** `protocol_spec.md` describes 64-byte
frames with output report ID `0x81` and input report ID `0x02` on usage page `0xffa0`. This
device, connected over USB, exposes no `0xffa0` interface, no `0x81` output report and no report
longer than 32 data bytes. The JP108 command rows cannot be sent to it as specified.

Interface 2 was tried with the documented read payloads re-framed to its 32-byte `0xb2` output
report, each followed by a 400 ms listen, after a 1.5 s passive listen:

```
b2 05 c1            b2 81 05 c1         b2 81 04 00 01      b2 04 00 01 00
b2 05 30 20 01      b2 81 05 30 20 01   b2 05 32 20 01      b2 05 34 20 01
```

All eight were accepted by the kernel and none drew a reply. Nothing unsolicited arrived either.
Output reports `0x51` and `0x52` were not tried: what they do is unknown, so a payload sent on
them cannot be called a read.

Open questions:

- Whether another connection mode (the 2.4G adapter `0x520a`, Bluetooth) exposes a `0xffa0`
  interface.
- What framing interface 2 expects.

### Follow-up the same day: the keyboard does answer

The paragraphs above were written before the keyboard's own framing was known. It is recorded in
`dossiers/5209/jp108_hid.toml`: commands travel in output report `0x52`, not `0xb2`, and are
answered on input report `0x54`. With that framing the same keyboard answered every read, and
accepted and read back a profile name and an assignment for each of its A and B buttons.

So "no interface carries the documented protocol" stands only for the 64-byte protocol that was
documented at the time. Interface 2 is the configuration interface.

## Consequences in the runtime

- A device with no vendor configuration interface is reported as unreachable up front, and no
  diagnostics or mapping are offered for it.
- A device's displayed state comes from what it answered, not from its tier in `pid_matrix.csv`.
- `0x5209` and `0x6013` are still listed as tier `full` in `pid_matrix.csv`. That tier was
  assigned from static evidence. For `0x5209` the follow-up above now supplies hardware evidence
  for reads and mapping writes. For `0x6013` only four reads are confirmed. Changing a tier is a
  registry decision left to a maintainer; the runtime no longer depends on it to be honest.

## Reproducing

```
openbitdo --diagnostics-dump --debug-log run.log
```

`run.log` holds every frame written and read, with timings.
