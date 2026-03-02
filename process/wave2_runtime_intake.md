# Wave 2 Runtime/Hardware Intake (Prepared, Deferred)

## Purpose
Define exactly what sanitized runtime/hardware evidence is needed to move Wave 2 devices beyond static-only dossiers.

## Required Submission Data
Every submission must include:
1. VID/PID in hex.
2. Firmware version.
3. Operation attempted.
4. Sanitized request structure.
5. Sanitized response structure.
6. Reproducibility notes (OS, transport, retries, success/failure rate).

## Sanitization Rules
Allowed content:
- byte-layout summaries,
- command/response shape descriptions,
- validation predicates,
- timing/retry observations.

Forbidden content:
- raw decompiled code snippets,
- copied vendor constants blocks,
- copied source fragments from official binaries/tools.

## Evidence Acceptance Checklist
1. VID/PID and firmware fields are present.
2. Request/response structure is sanitized and technically complete.
3. Failure signatures are mapped to stable categories (`timeout`, `malformed`, `unsupported`, `invalid_signature`).
4. Repro steps are clear enough for independent rerun.
5. No forbidden raw-source content appears.

## Promotion Readiness Mapping
A PID is promotion-eligible only when all are true:
1. Static dossiers complete.
2. Runtime traces accepted from at least 2 independent runs.
3. Hardware read/write/readback validation passes on owned fixture(s).
