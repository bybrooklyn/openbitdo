# Community Evidence Intake

This process collects safe, sanitized device evidence from users and testers.

## Required Submission Data

- device name
- VID/PID in `0xVVVV:0xPPPP` form
- firmware version
- operation attempted
- sanitized request or response description
- OS, transport mode, and reproducibility notes

## Prohibited Content

- raw decompiled code
- copied vendor source snippets
- proprietary dumps that are not sanitized into structure-level notes

## Maintainer Flow

1. validate the report format
2. map it to a known PID or create a new sanitized record
3. update the relevant spec or dossier artifacts
4. keep the device read-only until runtime and hardware confirmation justify promotion
5. run `sdk/scripts/check_evidence_readiness.py` before treating a candidate as promotion-ready

## Candidate Write Probe

The TUI exposes a guarded write probe for candidate-readonly devices only.
It is report-only support evidence unless a maintainer also updates the sanitized spec/dossier artifacts.

The probe never enables firmware or bootloader writes.
It can attempt only non-firmware safe-write/readback checks after all of these are true:

- advanced mode is enabled
- local write risk is acknowledged
- `candidate-unlocks/<vid>_<pid>.toml` exists beside the UI settings file
- the unlock file contains both `pid = "vvvv:pppp"` and `candidate_write_unlock = true`
