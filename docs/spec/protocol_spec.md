# OpenBitdo Protocol Overview

This document summarizes the sanitized protocol model used by the clean-room runtime.

## Wire Model

- HID-like command transport
- primary 64-byte reports for `Standard64`, `DInput`, and `JpHandshake`
- variable-length reports only where firmware or boot phases require them
- little-endian multi-byte numbers

## Protocol Families

- `Standard64`
- `JpHandshake`
- `DInput`
- `DS4Boot`
- `Unknown`

## Safety Classes

- `SafeRead`: diagnostics and metadata reads
- `SafeWrite`: profile, setting, or mapping writes
- `UnsafeBoot`: bootloader transitions
- `UnsafeFirmware`: firmware transfer and commit operations

## Response Validation

- every command validates against the registry table
- outcomes are `Ok`, `Invalid`, or `Malformed`
- retry logic applies on timeout or malformed data according to session policy

## Operation Groups

- `Core`
- `JP108Dedicated`
- `Ultimate2Core`
- `Firmware`
- `CoreDiag`
- `ModeProfileRead`
- `FirmwarePreflight`

## Support Model

### Support Levels

- `full`
- `detect-only`

### Support Tiers

- `full`: normal read, write, and gated unsafe paths
- `candidate-readonly`: safe-read diagnostics only
- `detect-only`: identify-only posture

## Candidate Read-Only Policy

Read-only candidates may:

- identify themselves
- run allowed safe-read diagnostics
- perform family-appropriate read-only metadata checks

Read-only candidates may not:

- write mappings or profiles
- enter unsafe boot paths
- transfer firmware

Promotion to full support requires:

1. static evidence
2. runtime evidence
3. hardware evidence

## Feature Scopes

### JP108

- supported targets: `0x5209`, `0x520a`
- current mapping scope: `A`, `B`, `K1`-`K8`

### Ultimate 2

- supported targets: `0x6012`, `0x6013`
- current scope: mode, slot, slot config, core button map, and required analog handling

## Runtime Safety Rule

Unsafe commands are only allowed when the runtime has both:

1. unsafe mode enabled
2. explicit brick-risk acknowledgment
