# 8BitDo Clean-Room Protocol Specification (Sanitized)

## Scope
This document defines a sanitized command and transport contract for a clean-room Rust implementation.
It is intentionally independent from reverse-engineered source code details and uses stable requirement IDs.

## Wire Model
- Transport: HID-like reports
- Primary report width: 64 bytes (`Standard64`, `DInput`, `JpHandshake` families)
- Variable-length reports: allowed for boot/firmware phases
- Byte order: little-endian for multi-byte numeric fields

## Protocol Families
- `Standard64`: standard 64-byte command and response flow
- `JpHandshake`: alternate handshake and version probing workflow
- `DInput`: command family used for mode and runtime profile operations
- `DS4Boot`: reserved boot mode for DS4-style update path
- `Unknown`: fallback for unknown devices

## Safety Classes
- `SafeRead`: read-only operations
- `SafeWrite`: runtime settings/profile writes
- `UnsafeBoot`: bootloader transitions with brick risk
- `UnsafeFirmware`: firmware transfer/commit operations with brick risk

## Response Validation Contract
- Responses are validated per command against byte-pattern expectations from `command_matrix.csv`
- Validation outcomes: `Ok`, `Invalid`, `Malformed`
- Retry policy applies on `Malformed` or timeout responses

## Operation Groups
- `Core`: generic identify/mode/profile/boot/fallback commands
- `JP108Dedicated`: 108-key dedicated-button mapping + feature/voice operations
- `Ultimate2Core`: Ultimate2 mode/slot/core-map operations
- `Firmware`: device-scoped firmware enter/chunk/commit/exit operations
- `CoreDiag`: decompiler-first detect/diagnostic command subset for candidate-readonly PIDs
- `ModeProfileRead`: decompiler-first read-only mode/profile snapshot group for candidate-readonly PIDs
- `FirmwarePreflight`: decompiler-first firmware readiness metadata reads (no transfer enablement)

## JP108 Dedicated Support
- Supported targets: `0x5209` (`PID_108JP`), `0x520a` (`PID_108JPUSB`)
- First milestone mapping scope: `A`, `B`, `K1`-`K8`
- Additional controls in this group:
  - feature flags read/write
  - voice setting read/write
- Full 111-key matrix remap is explicitly out of scope for this milestone.

## Ultimate2 Core Support
- Supported targets: `0x6012` (`PID_Ultimate2`), `0x6013` (`PID_Ultimate2RR`)
- First milestone editable scope:
  - current mode read/set
  - current slot read
  - slot config read/write
  - core button map read/write
- Advanced subsystems (theme/sixaxis/deep macro editing) are intentionally hidden in this milestone.

## PID-Aware Command Gating
- Command availability is gated by:
  1. safety class and runtime unsafe acknowledgements
  2. capability flags
  3. explicit PID allowlist from `command_matrix.csv:applies_to`
- `applies_to="*"` means globally available within existing safety/capability constraints.

## Device Support Levels
- `full`: command execution permitted for safe and unsafe operations (with user gates)
- `detect-only`: identification allowed; unsupported operations return `UnsupportedForPid`

## Support Tiers
- `full`: read/write/unsafe operations available according to existing safety gates.
- `candidate-readonly`: detect/diag safe reads are allowed per PID allowlist; safe writes and unsafe flows are blocked.
- `detect-only`: identify-only posture for unsupported or unknown PIDs.

## Candidate Read-Only Wave Policy
- Wave-1 and Wave-2 expansion PIDs are classified as `candidate-readonly`.
- Command policy for this tier:
  - allow: detect/diag safe-read subset.
  - allow: read-only mode/profile snapshot reads when family-appropriate.
  - allow: firmware metadata/preflight reads only.
  - deny: all safe-write operations.
  - deny: all unsafe boot/firmware operations.
- Promotion from `candidate-readonly` to `full` requires 3-signal evidence:
  1. static dossier coverage
  2. runtime trace evidence
  3. hardware read/write/readback confirmation

## Dossier Linkage
- Per-PID operation evidence is tracked in `spec/dossiers/**`.
- `command_matrix.csv:dossier_id` links command rows to sanitized dossier artifacts.
- `evidence_index.csv` maps PID to class-family anchors and operation groups.

## Required Runtime Gating
Unsafe commands execute only when both conditions are true:
1. `--unsafe`
2. `--i-understand-brick-risk`

## Clean-Room Requirements Linkage
Implementation and tests must trace to IDs in `requirements.yaml`.
All public APIs and behavior are governed by `REQ-PROT-*`, `REQ-PID-*`, `REQ-SAFE-*`, and `REQ-TEST-*` IDs.
