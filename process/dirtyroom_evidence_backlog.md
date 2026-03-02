# Dirty-Room Evidence Backlog

## Purpose

Track future dirty-room evidence work for protocol expansion in a structured way, so new functionality can be translated into sanitized clean-room specs without contaminating implementation code.

## Clean-Room Boundaries

- Dirty-room analysis may use approved evidence sources.
- Clean implementation must consume only sanitized artifacts in `spec/` and approved harness data.
- No raw dirty-room snippets, copied code, or direct decompiled fragments may be carried into clean implementation files.

## Prioritized Backlog

1. Wave-2 candidate-readonly expansion (decompiler-first):
   - Popularity +12 PIDs:
     - `0x3100`, `0x3105`, `0x2100`, `0x2101`, `0x901a`, `0x6006`
     - `0x5203`, `0x5204`, `0x301a`, `0x9028`, `0x3026`, `0x3027`
   - Deliverable posture: detect/diag-only (`candidate-readonly`), no firmware transfer/write promotion.
2. Wave-1 candidate-readonly follow-through:
   - Primary 14 PIDs:
     - `0x6002`, `0x6003`, `0x3010`, `0x3011`, `0x3012`, `0x3013`
     - `0x5200`, `0x5201`, `0x203a`, `0x2049`, `0x2028`, `0x202e`
     - `0x3004`, `0x3019`
   - Stretch PIDs:
     - `0x3021`, `0x2039`, `0x2056`, `0x5205`, `0x5206`
   - Deliverable posture: stay candidate-readonly until runtime and hardware evidence is accepted.
3. JP108 deeper mapping coverage:
   - Expand dedicated key mapping confirmation beyond the current A/B/K1-K8 baseline.
   - Confirm feature and voice command behavior with stronger request/response confidence.
4. Ultimate2 advanced paths:
   - Expand confidence for advanced slot/config interactions and additional profile behaviors.
   - Confirm edge cases for mode transitions and per-slot persistence.
5. Firmware trace confidence:
   - Increase confidence for bootloader enter/chunk/commit/exit behavior across supported target variants.
   - Capture and sanitize additional failure and recovery traces.

## Required Sanitized Outputs

- Update `spec/protocol_spec.md` for any newly confirmed operation groups or behavior rules.
- Update `spec/requirements.yaml` with new stable requirement IDs.
- Update `spec/command_matrix.csv` and `spec/pid_matrix.csv` as evidence confidence changes.
- Add or refresh sanitized harness fixtures under `harness/golden/` for replay and regression tests.

## Review Checklist Before Clean Implementation

- Sanitized evidence is traceable to requirement IDs.
- Command confidence levels are explicit (`confirmed` vs `inferred`).
- PID capability changes are reflected in matrices.
- No raw-source text is present in clean implementation artifacts.
