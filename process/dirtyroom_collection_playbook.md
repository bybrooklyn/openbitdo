# Dirty-Room Collection Playbook (Decompiler-First Expansion)

## Goal
Create sanitized, requirement-linked evidence that expands device detect/diagnostics support without contaminating clean-room implementation.

## Scope of This Wave
- Evidence source: decompiler/static-only.
- Target: Wave 2 +12 popularity cohort (plus previously tracked candidate-readonly set).
- Promotion policy: no new `full` promotions in this wave.
- Output artifacts: `spec/dossiers/**`, `spec/evidence_index.csv`, updated `spec/*.csv`, updated `requirements.yaml`.

## Allowed Dirty-Room Inputs
- `/Users/brooklyn/data/8bitdo/decompiled_dll/8BitDo_Ultimate_Software_V2.decompiled.cs`
- `/Users/brooklyn/data/8bitdo/decompiled/*.cs`
- `/Users/brooklyn/data/8bitdo/decompiled_autoupdate/*.cs`
- Existing dirty-room transcript files under `/Users/brooklyn/data/8bitdo/`

## Required Sanitization Rules
- Do not copy raw vendor/decompiled code snippets into clean artifacts.
- Record only sanitized structure-level findings:
  - command intent
  - request/response byte-shape
  - validator expectations
  - gating/policy notes
- Use requirement IDs only (`REQ-DR-*`, `REQ-PROM-*`, `REQ-COMM-*`, `REQ-GH-*`).

## Dossier Workflow
1. Pick PID and operation group.
2. Collect static evidence anchors (class/function names and behavior summaries).
3. Derive sanitized command mapping and validation expectations.
4. Write TOML dossier in `spec/dossiers/<pid_hex>/<operation_group>.toml`.
5. Link dossier ID into `spec/command_matrix.csv` (`dossier_id` column).
6. Update `spec/evidence_index.csv`.
7. Ensure each Wave 2 PID has all three required dossier files:
   - `core_diag.toml`
   - `mode_or_profile_read.toml`
   - `firmware_preflight.toml`

## Authoring Approach (No Helper Scripts)
- Dossiers and matrix updates are maintained directly in repository source files.
- `spec/evidence_index.csv` is updated manually with deterministic ordering.
- Validation is performed through normal repository review plus workspace tests.

## Confidence Rules
- `confirmed`: requires static + runtime + hardware confirmation (not achievable in this wave).
- `inferred`: static-only or partial confidence.
- For this wave, new entries should remain `inferred` unless already confirmed historically.

## Promotion Gate
A device can move from `candidate-readonly` to `full` only when all three are true:
1. static evidence complete
2. runtime trace evidence complete
3. hardware read/write/readback complete

## Review Checklist
- Dossier contains required fields from schema.
- Requirement linkage is explicit.
- No raw decompiled text/snippets are present.
- `support_tier` remains `candidate-readonly` for new no-hardware devices.
- Runtime and hardware placeholders are populated with concrete promotion evidence tasks.
