# Wave 2 Runtime Intake

This note describes the evidence still needed to move Wave 2 devices beyond static-only confidence.

## Required Submission Data

1. VID/PID
2. firmware version
3. operation attempted
4. sanitized request structure
5. sanitized response structure
6. reproducibility notes

## Acceptance Rules

- no copied vendor code or decompiled snippets
- failure signatures must map to stable categories
- the report must be repeatable enough for an independent rerun

## Promotion Readiness

A PID is promotion-ready only when static, runtime, and hardware evidence all exist together.
