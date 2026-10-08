# Parity with the vendor's Windows application

What 8BitDo's own configuration application (Ultimate Software V2, Windows, v1.37) offers, per
product, and where OpenBitdo stands on each. Wire facts behind the "done" rows are in
`docs/clean-room-evidence/dossiers/`; nothing here reproduces vendor code.

Status words:

- **hardware**: implemented and exchanged with a real device.
- **simulated**: implemented and tested against a simulator built from the dossier; not yet
  exchanged with a real device, so real writes stay behind advanced mode.
- **no**: not implemented.
- **n/a**: the vendor application does not offer it for that product either.

## What the vendor application actually configures

Of the 75 product IDs it knows, the application has settings pages for these families only.
Everything else it lists is firmware update only.

| Family | Products | Vendor pages |
|---|---|---|
| Retro keyboards, per-key protocol | Retro Mechanical Keyboard (0x5200), Retro 108 (0x5209) | key mapping, macros, key locks, volume, one on-device profile |
| Retro keyboards, record protocol | Retro 87 Xbox (0x2028, 0x3026), Retro 68 (0x203a), Riviera (0x205a) | key mapping, macros, lighting, volume, sleep, Super Button pairing |
| Controllers | Pro 3 (0x6009), Ultimate 2 (0x6012), Ultimate 2 Bluetooth (0x600f), Ultimate Bluetooth 1st gen (0x6007) | 3 profile slots, button map, sticks, triggers, vibration, macros, motion, stick-ring lighting |
| Arcade controllers | Arcade Controller (0x600b), Arcade Controller Pro (0x2062) | button map, SOCD mode, macros, lighting and combos (Pro) |
| Mice | Retro R8 (0x5205), Riviera mouse (0x205d), SN30 Pro mouse (0x2076) | buttons, DPI stages, polling rate, lift-off, macros |
| Other | N64 receiver (0x9028) controller-pak manager; N64 Bluetooth and stick module calibration | data transfer, calibration |

The application reaches Pro 3, Ultimate 2, Ultimate 2 Bluetooth and the Arcade Controller only
when they enumerate under the shared ID 0x310b (wired, or the 2.4G receiver in XInput mode) and
asks the device which product it is. OpenBitdo does the same for the three controllers it has an editor for; it is looked for on the interface the vendor library uses for that ID (usage page 0xff7a). Untested on hardware.

## Retro 108 Mechanical Keyboard (0x5209)

| Feature | Vendor | OpenBitdo |
|---|---|---|
| Read the profile (name, every remapped key, locks, volume) | yes | hardware |
| Remap any key to a key, with an optional modifier | yes | hardware (A and B buttons); simulated for the other 109 keys, same command |
| Remap to a media key | yes (11 targets) | simulated |
| Remap to a mouse button or wheel step | yes | simulated |
| Disable a key | yes | simulated |
| Lock Win key / Alt+Tab / Alt+F4 | yes | read: hardware; write: simulated |
| Volume level 1-5 | yes | read: hardware; write: simulated |
| Rename the profile | yes | hardware (first write of a name); rename of an existing profile simulated |
| Erase the profile | yes | simulated |
| Macros: 8 slots, any key, up to 200 steps, repeat count and interval | yes | simulated (list read: hardware). Built step by step, or recorded by typing the text the macro should type |
| Profile library on the computer: save, copy, export, import | yes | save and load readable profile files (`E` / `I` in the editor) |
| Live view of the Super Button ports | yes | no |
| Firmware update | yes | no (deferred) |

## Ultimate 2 Wireless Controller (0x6012, receiver 0x6013)

| Feature | Vendor | OpenBitdo |
|---|---|---|
| Tell whether the controller is connected to its receiver | yes | hardware |
| Read the configuration record for the mode switch's platform | yes | simulated |
| Three profile slots: view, name, edit | yes | simulated |
| Button map, 22 inputs including back paddles and extra buttons | yes | simulated |
| Stick ranges (dead zone and outer limit) | yes | simulated |
| Trigger ranges | yes | simulated |
| Vibration strength per motor | yes | simulated |
| Invert stick axes, swap sticks, swap triggers, swap d-pad and left stick | yes | simulated |
| XInput vibration range | yes | no |
| Macros: 4 per slot, a trigger button, up to 200 steps, repeat and interval | yes | simulated. Built step by step; recording live input: no |
| Motion (gyro) mapping: target stick, enabling button, hold or toggle, sensitivity, dead zone | yes | simulated |
| Stick-ring lighting: off, tracing, fire ring, per-LED colours, speed | yes | simulated (colours by hex or swatch; no colour wheel) |
| Stick and trigger calibration | no (the vendor application calibrates only the N64 controller and stick module) | n/a |
| Profile library on the computer | yes | save and load one slot as a readable profile file |
| Reached under the shared ID 0x310b | yes (only this way) | simulated: the device is asked which product it is and gets that product's editor |
| Firmware update | yes | no (deferred) |

Everything marked simulated for this controller waits on one thing: a real controller, switched
on, answering the read. The receiver alone answers only the connection query.

## Other products

| Family | OpenBitdo |
|---|---|
| Pro 3 (0x6009), Ultimate 2 Bluetooth (0x600f) | simulated: the same editor as the Ultimate 2, over each model's own record layout (a Pro 3 has no motion or lights). Their older "hot-key" macro section is preserved but not editable. No hardware has been tried. |
| Ultimate Bluetooth 1st gen (0x6007) | no |
| Arcade Controller (0x600b) | simulated: button map, the opposite-directions (SOCD) choice, macros and profile name, reached under the shared ID. No hardware has been tried. |
| Arcade Controller Pro (0x2062) | no |
| Retro Mechanical Keyboard (0x5200) | simulated, read-only: its profile reads with the Retro 108's commands, its own key ids (modifiers 100-106, A/B 109/108, K1-K8 116-110, F13-F24 targets 118-129) and no numpad. Writes and the editor stay off while it is a read-only-tier device in `pid_matrix.csv`. |
| Retro 87 Xbox, Retro 68, Riviera keyboard | no |
| Retro R8, Riviera and SN30 Pro mice | no |
| N64 receiver controller-pak manager, calibration pages | no |
| Firmware update, any product | no (deferred; see `docs/RC_CHECKLIST.md`) |

## Things OpenBitdo does that the vendor application does not

- Runs on Linux and macOS.
- Reads back every write and restores the previous value when the device did not keep it.
- Keeps a backup of what a device held before each write, for the session.
- Shows a receiver whose controller is off as exactly that.
- Shows live controller input (the Buttons tab) for any controller the OS exposes as a gamepad.
