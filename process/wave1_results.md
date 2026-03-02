# Wave 1 Results (Template)

Generated: 2026-02-28

## Summary
- Primary target PIDs processed: 14
- Stretch target PIDs processed: TBD
- New `full` promotions: 0 (expected in decompiler-only wave)

## Deliverables
- Dossiers created: `spec/dossiers/**`
- Evidence index updated: `spec/evidence_index.csv`
- Matrices updated: `spec/pid_matrix.csv`, `spec/command_matrix.csv`
- Requirements updated: `spec/requirements.yaml`

## Validation
- `cargo test --workspace --all-targets`: pending
- `./scripts/cleanroom_guard.sh`: pending
- Detect/diag targeted tests: pending

## Follow-Up
- Collect runtime traces for candidate-readonly devices.
- Run hardware confirmation on each candidate before promotion to `full`.
