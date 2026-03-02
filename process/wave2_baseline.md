# Wave 2 Baseline (Frozen)

## Snapshot Date
- 2026-03-01

## Pre-Wave Counts (Frozen)
- `spec/pid_matrix.csv` rows: 59
- Support tier counts (pre-wave):
  - `full`: 14
  - `candidate-readonly`: 15
  - `detect-only`: 30
- `spec/command_matrix.csv` rows (pre-wave): 37

## Hardware Reality (Current)
- Available fixtures: JP108 line and Ultimate2 line only.
- Non-owned devices must remain `candidate-readonly` until strict promotion signals are complete.

## Required Checks Baseline
Branch protection for `main` must require:
- `guard`
- `aur-validate`
- `tui-smoke-test`
- `build-macos-arm64`
- `test`
- `hardware-108jp`
- `hardware-ultimate2`

## Promotion Policy
Promotion from `candidate-readonly` to `full` requires all 3 signals:
1. static dossier evidence,
2. runtime sanitized traces,
3. hardware read/write/readback confirmation.
