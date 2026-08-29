# Contributing to OpenBitdo

Thanks for wanting to help. This project is small, community-maintained, and
touches real hardware — a couple of rules here matter more than usual because
of that, please read this whole file before opening a PR.

## Building and Testing

Use Go `1.27.0` for local development, CI parity, and release artifacts.

This project uses [`just`](https://github.com/casey/just) as its command
runner — see the `justfile` at the repo root, or run `just` with no arguments
to list every recipe. The two you'll use most:

```sh
just check   # everything CI runs: generate, build, fmt-check, lint, test-race, both guard scripts
just run-mock  # launch the TUI against mock devices, no hardware needed
```

Run `just check` before opening a PR — it's exactly what CI gates on, so a
clean local run means CI should pass too.

Release-blocking checks also include pinned `golangci-lint` `2.13.1`,
`govulncheck` `v1.7.0` with zero reachable findings, and archived
`govulncheck` JSON output.

Live HID tests are never part of an ordinary local or CI run. Darwin hardware
probes must carry the `manual` build tag, and every live hardware test command
must be invoked with `-tags manual`.

## The Clean-Room Rule (read this)

OpenBitdo is a **clean-room** reimplementation of 8BitDo's device protocol.
That means the implementation (`cmd/`, `internal/`) is built only from
`docs/spec/**`, `docs/process/**`, and approved fixtures — never from
decompiled vendor code, copied proprietary snippets, or any direct exposure
to 8BitDo's own software. See
[`docs/process/cleanroom_rules.md`](docs/process/cleanroom_rules.md) for the
full rule set, and
[`docs/clean-room-evidence/dirtyroom_collection_playbook.md`](docs/clean-room-evidence/dirtyroom_collection_playbook.md)
if you're contributing evidence for a new device — that evidence-gathering
process is intentionally separate from writing clean-room code, and the two
should never be done by the same person/session in the same sitting.

`scripts/cleanroom_guard.sh` (run as part of `just check` and in CI) scans
for forbidden references and will reject a PR that violates this — not
because we don't trust you, but because the whole point of the clean-room
boundary is that it has to hold even when someone genuinely didn't mean to
cross it.

**If you're adding support for a new device**: don't paste in anything
derived from decompiling or observing 8BitDo's official software directly
into a PR against this repo. Follow the dirty-room evidence process instead
— sanitized findings go into `docs/spec/pid_matrix.csv` /
`docs/spec/command_matrix.csv` and a dossier, and the Go registry is
generated from those (`go generate ./...` — see
`internal/protocol/gen/main.go`), not hand-edited.

For the `v0.1.0-rc.1` line, firmware updates and real-hardware Ultimate 2
mapping are explicitly deferred. Do not describe either as working in a PR,
README change, issue response, package note, or release note unless a later
release gate has been updated with hardware-confirmed evidence.

## Code Style

Sparse, high-value comments only — see
[`docs/process/commenting_standard.md`](docs/process/commenting_standard.md).
Standard `gofmt`; `golangci-lint run ./...` (part of `just check`) is the
source of truth for everything else.

## Submitting a Pull Request

1. Fork and branch off `main`.
2. Make your change, with tests — this codebase treats "verified" and
   "compiles" as different things; a change without a test that would have
   caught the bug you're fixing (or proves the feature you're adding
   actually works) is likely to get asked for one.
3. `just check` clean, locally, before opening the PR.
4. Describe *why*, not just *what* — the commit message and PR description
   should explain the reasoning, not restate the diff.
5. By opening a PR, you're agreeing to the Contributor License Agreement
   below — there's no separate form to sign.

## Contributor License Agreement

This section is a real (if informal) legal agreement. It's written to be
clear rather than dense, but it's still a starting draft for a small
project, not a substitute for actual legal review if OpenBitdo ever takes
contributions at meaningful scale — treat it as good-faith terms, not a
polished corporate CLA.

By submitting a contribution (a pull request, patch, or any other
material you intend to be merged into this project — "**Your
Contribution**"), you agree to the following, effective as of when you
submit it:

**1. You keep your copyright.** You are not assigning ownership of Your
Contribution to the maintainer or to the project. You remain the copyright
holder of your own work.

**2. You grant a broad license.** You grant the maintainer of this project
(currently the repository owner) a perpetual, worldwide, non-exclusive,
royalty-free, irrevocable license to use, reproduce, prepare derivative
works of, publicly display, publicly perform, sublicense, and distribute
Your Contribution as part of OpenBitdo — **including the right to relicense
Your Contribution, alone or as part of the combined work, under different
license terms in the future**, without needing to ask you again or track
you down for permission. This is the specific thing this CLA exists to
grant, beyond what OpenBitdo's current license (GPL-3.0-or-later) alone
would give a downstream user of your contributed code.

**3. Patent grant.** You grant the same kind of license (perpetual,
worldwide, non-exclusive, royalty-free, irrevocable) under any patent
claims you own or control that are necessarily infringed by Your
Contribution alone or combined with the project it was contributed to —
enough to let the maintainer and downstream users actually use the
software without a patent claim from you blocking them.

**4. Your Contribution is your own, or you have the right to submit it.**
You represent that Your Contribution is your original creation, or that
you have sufficient rights to submit it under these terms (for example,
your employer has given permission, if relevant). If Your Contribution
includes work that isn't entirely your own, say so clearly in the PR.

**5. No warranty.** Your Contribution is provided "as is," without warranty
of any kind, express or implied — same disclaimer spirit as the project's
GPL-3.0-or-later license itself.

**6. No obligation.** The maintainer is not obligated to use, merge, or
keep Your Contribution in the project.

**7. You keep your own rights.** Nothing here stops you from using,
relicensing, or distributing Your Contribution yourself, elsewhere, under
whatever terms you want. This CLA only grants rights *to the maintainer* —
it doesn't take anything away from you.

If any of this doesn't work for you, say so in your PR before it's merged
— it's easier to sort out before your contribution is part of the project
than after.
