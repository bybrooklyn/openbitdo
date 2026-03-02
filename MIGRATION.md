# OpenBitdo Migration Notes

## What changed
- `bitdoctl` was removed.
- `openbitdo cmd ...` was removed.
- JSON report/output flags were removed from user-facing flows.
- OpenBitdo now focuses on a single beginner entrypoint: `openbitdo`.

## New usage
From `/Users/brooklyn/data/8bitdo/cleanroom/sdk`:

```bash
cargo run -p openbitdo --
```

Optional mock mode:

```bash
cargo run -p openbitdo -- --mock
```

## Beginner flow
1. Launch `openbitdo`.
2. Select a detected device.
3. Click or choose an action:
- `Update` (guided firmware flow)
- `Diagnose` (quick readiness checks)
- `Refresh`
- `Quit`
4. Confirm with a simple `y`/`yes` prompt before firmware transfer.

## Firmware behavior
- OpenBitdo first attempts a recommended firmware download.
- If download or verification fails, it immediately asks for a local firmware file (`.bin`/`.fw`).
- Detect-only devices remain blocked from firmware write operations with a clear reason.

## New device-specific wizards
- JP108 (`0x5209`/`0x520a`):
  - Dedicated button mapping for `A`, `B`, and `K1-K8`
  - Auto-backup before write
  - One-click restore if needed
  - Guided button test text after apply
- Ultimate2 (`0x6012`/`0x6013`):
  - Slot + mode + core map editing
  - Auto-backup and rollback path
  - Guided button test text after apply

## CI changes
- Hardware CI split into per-family jobs:
- `hardware-dinput` (required)
- `hardware-standard64` (required)
- `hardware-ultimate2` (required)
- `hardware-108jp` (required)
- `hardware-jphandshake` (gated until fixture availability)
