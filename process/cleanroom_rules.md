# Clean-Room Rules

## Allowed Inputs During Clean Implementation
- `cleanroom/spec/**`
- `cleanroom/process/cleanroom_rules.md`
- `cleanroom/harness/golden/**`

## Forbidden Inputs During Clean Implementation
- `decompiled/**`
- `decompiled_*/*`
- `bundle_extract/**`
- `extracted/**`
- `extracted_net/**`
- `session-ses_35e4.md`

## Enforcement
- `cleanroom/sdk/scripts/cleanroom_guard.sh` checks for forbidden path and token references.
- CI runs the guard before test jobs.

## Commit Hygiene
- No copied decompiled code snippets.
- No direct references to dirty-room files in SDK implementation/tests.
- Any new protocol fact must be added to sanitized spec artifacts first.
