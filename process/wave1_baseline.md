# Wave 1 Baseline Snapshot

Generated: 2026-02-28

## Hardware Access Baseline
- Available hardware lines: JP108 + Ultimate2.
- Exact attached PID variants: pending local identify run on connected hardware.
- Temporary lab fixture defaults remain:
  - JP108: `0x5209`
  - Ultimate2: `0x6012`

## Required Next Verification Step
Run identify flow with connected hardware and confirm fixture PIDs:
1. `cargo test --workspace --test hardware_smoke -- --ignored --exact hardware_smoke_detect_devices`
2. `./scripts/run_hardware_smoke.sh` with `BITDO_REQUIRED_SUITE=108jp`
3. `./scripts/run_hardware_smoke.sh` with `BITDO_REQUIRED_SUITE=ultimate2`

If detected variants differ, update `harness/lab/device_lab.yaml` fixture PIDs immediately.

## Support Baseline
- Existing `full` paths: JP108/Ultimate2 and previously confirmed families.
- New expansion wave devices remain `candidate-readonly` (detect/diag only).
- No new firmware/write enablement for no-hardware targets.
