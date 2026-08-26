# Security Policy

## Supported Versions

Only the latest released version is supported with security fixes. OpenBitdo
does not maintain long-term-support branches at this stage of the project.

## Scope

OpenBitdo talks directly to USB/HID hardware and, for confirmed devices, writes
firmware to that hardware. The realistic risk surface is narrow but real:

- HID device access (`internal/protocol`, `internal/input`) — malformed or
  unexpected device responses being handled unsafely.
- Firmware manifest download and verification (`internal/core`) — SHA-256 hash
  and Ed25519 signature checks against a pinned public key, over HTTPS.
- Firmware transfer itself (`internal/core/transfer_task.go`) — anything that
  could cause a write to the wrong device, the wrong offset, or without the
  safety/confirmation gates (support tier, brick-risk acknowledgement,
  candidate-write-probe unlock file) actually being honored.
- Settings and report files written to the user's local config/reports
  directory — path handling, permissions.

Denial-of-service or "the TUI is slow/ugly" reports are not security issues —
open those as a normal GitHub issue instead.

## Reporting a Vulnerability

**Please do not open a public GitHub issue for a security report.**

Instead, report it privately via GitHub's
["Report a vulnerability"](https://github.com/bybrooklyn/openbitdo/security/advisories/new)
flow (Security tab → Advisories → Report a vulnerability) on this repository.
That creates a private draft advisory only the maintainer can see until a fix
is ready.

Please include:

- What you found and why it's a security issue, not just a bug.
- Steps to reproduce, including the OS, device (if hardware-specific), and
  whether it requires `--mock` or real hardware to trigger.
- The impact you believe it has (e.g. "this could write to an unintended
  offset during firmware transfer," not just "this looks wrong").

## What to Expect

This is a small, community-maintained open-source project, not a company with
an SLA. In good faith:

- You should get an acknowledgement within a reasonable time.
- A genuine security issue will be prioritized over other work once
  confirmed.
- Credit is happily given in the release notes for the fix, unless you'd
  rather stay anonymous — say so in your report.

## Firmware Safety Note

Firmware writes are gated behind multiple layers on purpose (support-tier
confirmation, an explicit brick-risk acknowledgement, and — for
candidate-readonly devices — a separate per-PID unlock file). If you find a
way to reach a firmware write path while bypassing any of those gates,
that is exactly the kind of report this policy exists for.
