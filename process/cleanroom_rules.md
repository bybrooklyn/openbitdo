# Clean-Room Rules

## Allowed Inputs

- `cleanroom/spec/**`
- `cleanroom/process/**`
- approved harness fixtures and generated release artifacts

## Forbidden Inputs

- decompiled vendor code
- copied proprietary snippets
- direct references to dirty-room paths from clean implementation or user-facing docs

## Enforcement

- `cleanroom/sdk/scripts/cleanroom_guard.sh` scans for forbidden references
- CI runs the guard before packaging and test jobs

## Commit Hygiene

- no copied decompiled code
- no raw vendor-source excerpts
- new protocol facts must arrive through sanitized spec or evidence updates first
