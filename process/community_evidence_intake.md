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
