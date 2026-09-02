# Security Policy

## Supported Versions

Only the latest released version is supported with security fixes. OpenBitdo
does not maintain long-term-support branches at this stage of the project.

## Scope

OpenBitdo talks directly to USB/HID hardware. For `v0.1.0-rc.1`, firmware
updates are unavailable in production and Ultimate 2 mapping on real hardware
is intentionally blocked. The realistic risk surface is narrow but real:

- HID device access (`internal/protocol`, `internal/input`) — malformed or
  unexpected device responses being handled unsafely.
- Firmware runtime gating (`internal/core`) — production builds must not expose
  a default manifest feed, production signing key, CLI override, firmware
  download, firmware preflight, device session, or transfer path.
- Firmware test isolation (`internal/core/transfer_task.go`) — firmware code
  may be exercised only in isolated tests with injected ephemeral keys and a
  local HTTP server.
- Mapping runtime gating — disabled real-hardware Ultimate 2 mapping must not
  reach apply/reset/write paths while the button-map framing remains
  unconfirmed.
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
  disabled action," not just "this looks wrong").

## What to Expect

This is a small, community-maintained open-source project, not a company with
an SLA. In good faith:

- You should get an acknowledgement within a reasonable time.
- A genuine security issue will be prioritized over other work once
  confirmed.
- Credit is happily given in the release notes for the fix, unless you'd
  rather stay anonymous — say so in your report.

## Firmware Safety Note

Firmware writes are unavailable in production for `v0.1.0-rc.1`. If you find a
way to reach a firmware download, preflight, device session, bootloader entry,
or transfer path from a production build, that is exactly the kind of report
this policy exists for.
