# Community Evidence Intake

## Purpose
Collect hardware and protocol evidence from the community in a clean-room-safe format.

## Submission Requirements
Every report must include:
- Device name
- VID/PID (`0xVVVV:0xPPPP`)
- Firmware version shown by official software/device
- Operation attempted
- Sanitized request/response shape description
- Reproducibility notes (steps, OS, transport mode)

## Prohibited Content
- Raw copied decompiled code.
- Vendor source snippets.
- Binary dumps with proprietary content not required for protocol structure.

## Acceptance Levels
- `intake`: report received, unverified.
- `triaged`: mapped to a PID/operation group and requirement IDs.
- `accepted`: converted into sanitized dossier/spec updates.

## Maintainer Processing
1. Validate report format.
2. Cross-reference PID with `spec/pid_matrix.csv`.
3. Create/update `spec/dossiers/<pid_hex>/*.toml`.
4. Update `spec/evidence_index.csv` and command/pid matrices.
5. Keep device as `candidate-readonly` until full 3-signal promotion gate is met.
