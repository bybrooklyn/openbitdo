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

## Device Support Levels
- `full`: command execution permitted for safe and unsafe operations (with user gates)
- `detect-only`: identification allowed; unsupported operations return `UnsupportedForPid`

## Required Runtime Gating
Unsafe commands execute only when both conditions are true:
1. `--unsafe`
2. `--i-understand-brick-risk`

## Clean-Room Requirements Linkage
Implementation and tests must trace to IDs in `requirements.yaml`.
All public APIs and behavior are governed by `REQ-PROT-*`, `REQ-PID-*`, `REQ-SAFE-*`, and `REQ-TEST-*` IDs.
