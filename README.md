# OpenBitdo

OpenBitdo is a clean-room, beginner-first 8BitDo utility built around a modern terminal UI.
It focuses on safe diagnostics first, clear support status, and blocked-action messaging for device flows that are still under investigation.

OpenBitdo is unofficial and not affiliated with 8BitDo. Device writes always carry risk. For `v0.0.3`, firmware updates are unavailable and Ultimate 2 mapping on real hardware is intentionally blocked until the button-map framing is hardware-confirmed.

## What OpenBitdo Does Today

- Detect connected 8BitDo devices and give each a plain verdict: working, limited (it can be read but not changed), can't connect, or no access.
- Check the connection, and view, copy or save the report.
- Say for every device what you can do with it now, and what you can't do yet and why.
- Edit supported mappings for the currently confirmed JP108 flow.
- Preview Ultimate 2 mapping in mock mode only; real Ultimate 2 mapping is blocked with the reason `button-map framing not hardware-confirmed`.
- Show Firmware Update as disabled and deferred in `v0.0.3`; activating it does not download firmware, preflight firmware, or open a device session.
- Keep unconfirmed devices in safe read-only or detect-only paths.
- Navigate with a keyboard, mouse, or an 8BitDo controller when the OS exposes that controller as a standard HID gamepad (`usagePage=0x0001`, `usage=0x0005`).

## Install

| Path | Command | Best for |
| --- | --- | --- |
| Homebrew | `brew tap bybrooklyn/openbitdo && brew install openbitdo` | macOS or Linux users who want the standard tap flow |
| AUR | `paru -S openbitdo-bin` | Arch Linux users who want a prebuilt package |
| GitHub Releases | Download a release tarball and run `bin/openbitdo` | Users who want a standalone binary without a package manager |
| From source | `just build` with Go 1.27.0 and `just` installed | Contributors and local development |

Contributors: [`justfile`](justfile) has common dev commands (`just build`, `just test`, `just run-mock`, `just check` before pushing) — install [`just`](https://github.com/casey/just) and run `just` with no arguments to list them.

## First Run

1. Launch `openbitdo`.
2. If you do not have hardware attached yet, launch `openbitdo --mock` to preview the interface.
3. Your devices are listed down the left with a verdict each. The right side shows the selected one in three sections: **Overview** (what you can and can't do), **Checks** (what the device answered) and **Mapping**.
4. Move between sections with `tab` (or `1`, `2`, `3`) and between devices with `d`. `↑↓` and `enter` act inside the section you are in.
5. Checks only read from the device. Open the report there (`v`) and copy or save it when a device is limited or behaving unexpectedly.
6. Press `?` anywhere for the keys that work there.

OpenBitdo enables mouse support for clicking and scrolling, which by default
intercepts click-drag so your terminal can't use it for normal text
selection. To copy text (a PID, an error message, a support-request report)
anyway, hold your terminal's selection-override modifier while you
click-drag — on Ghostty this is **Shift** (`mouse-shift-capture` in
`ghostty.5`); other terminals commonly use Option or Shift instead. Check
your terminal's own mouse/selection settings if holding a modifier doesn't
work, or if you'd rather disable mouse reporting for OpenBitdo entirely (most
terminals, including Ghostty's `mouse-reporting`, let you turn this off
app-wide).

## Support Tiers

| Tier | What it means |
| --- | --- |
| `supported` | Diagnostics and any confirmed mapping flows are available when safety gates are satisfied. Firmware remains unavailable in `v0.0.3`. |
| `read-only candidate` | Diagnostics are available, but mapping and firmware stay blocked until runtime and hardware confirmation are complete. |
| `detect-only` | OpenBitdo can identify the device, but deeper workflows are intentionally unavailable. |

Candidate-readonly devices may expose a guarded non-firmware write/readback probe for maintainers.
It requires advanced mode, local write-risk acknowledgement, and a per-PID unlock file; firmware and bootloader writes remain blocked.

## Hardware Verification Without A Lab

If you do not have a controller connected, use `openbitdo --mock` plus the automated test suite to verify the app flow.
Real-device promotion still requires hardware smoke evidence; mock mode does not prove firmware or mapping safety.
When a device is not fully supported, run diagnostics and share the generated TOML report instead of attempting writes.

A non-destructive Ultimate 2 qualification was run for `v0.0.3` and did not pass. On the unit tested (`0x2dc8:0x6013`) the controller exposes only its vendor configuration interface and no Generic Desktop Gamepad interface, so controller navigation has nothing to read from, and every safe-read diagnostic writes successfully but reads back no data. Both results were traced to device/protocol behavior rather than defects in this project. The measured results, the reasoning, and how to re-run the qualification are recorded in [docs/RC_CHECKLIST.md](docs/RC_CHECKLIST.md). Keyboard and mouse navigation are unaffected.

A later Linux run against the same controller and a Retro 108 keyboard is recorded in [docs/clean-room-evidence/hardware_run_linux_2026-10-08.md](docs/clean-room-evidence/hardware_run_linux_2026-10-08.md). Over hidraw the Ultimate 2 answers 4 of the 12 safe reads, so the empty reads above do not reproduce on Linux; it still exposes no gamepad interface. The Retro 108, connected over USB, exposes no interface that carries the documented protocol, so OpenBitdo identifies it and reports it as unreachable rather than offering diagnostics or mapping.

## Shell Completions

Completion scripts for the CLI's flags live in [completions/](completions/):

| Shell | Install |
| --- | --- |
| bash | `source completions/openbitdo.bash` (e.g. from `~/.bashrc`), or copy it into your `bash-completion` directory |
| zsh | Copy `completions/openbitdo.zsh` as `_openbitdo` onto a directory in your `$fpath`, added before `compinit` runs |
| fish | Copy `completions/openbitdo.fish` into `~/.config/fish/completions/` |

Linux archives and the AUR package include the udev rule and completions. OpenBitdo opens the kernel's hidraw node for a device (`/dev/hidrawN`), so the rule, `70-openbitdo.rules`, grants the logged-in user access to hidraw nodes of 8BitDo devices. After installing it, reload rules and replug the device before expecting non-root access:

```sh
sudo udevadm control --reload-rules
sudo udevadm trigger
```

Building from source? Install the rule yourself with `sudo cp packaging/linux/70-openbitdo.rules /etc/udev/rules.d/`. If a device shows "no permission" in the app, this is the fix.

## macOS Packaging Caveat

Current macOS release artifacts target macOS 13 on Apple Silicon and are unsigned and non-notarized.
That means Gatekeeper friction is expected for the `.pkg`, tarball, and standalone binary.
If the installer path is inconvenient, use the extracted tarball or Homebrew tap as the fallback path.
Apple Developer signing and notarization are deferred until credentials are available.
Intel macOS is unsupported for this release.

## Release And Package Map

- `v0.0.3` is published as a GitHub release with exactly 14 nonempty assets: Linux `x86_64`, Linux `aarch64`, and macOS arm64 binaries, archives, the macOS `.pkg`, and basename-only `.sha256` sidecars for each.
- AUR publishes `openbitdo-bin` as `0.0.3` from the Linux tarballs and release-derived checksums.
- Homebrew publishes version `0.0.3` through the separate tap repo `bybrooklyn/homebrew-openbitdo`.
- Package-manager metadata is rendered from published assets so release checksums stay authoritative.

Release support contract:

- Linux `x86_64` and `aarch64`: Ubuntu 22.04-era glibc or newer.
- macOS arm64: deployment target macOS 13; unsigned and non-notarized.
- Firmware: unavailable.
- Ultimate 2 mapping: mock preview only.
- Controller navigation: available only when the OS exposes a standard HID gamepad interface.

## License

OpenBitdo is licensed under the [GNU General Public License v3.0 or later](LICENSE).

## Where To Go Next

- Current RC release gate: [docs/RC_CHECKLIST.md](docs/RC_CHECKLIST.md)
- Changelog and release notes: [CHANGELOG.md](CHANGELOG.md)
- CLI and packaging migration notes: [docs/MIGRATION.md](docs/MIGRATION.md)
- Device catalog: [docs/spec/device_name_catalog.md](docs/spec/device_name_catalog.md)
- Protocol overview: [docs/spec/protocol_spec.md](docs/spec/protocol_spec.md)
- Contributing (build/test, the clean-room rule, code style, PR process): [CONTRIBUTING.md](CONTRIBUTING.md)
- Reporting a security issue: [SECURITY.md](SECURITY.md)
- The prior Rust/ratatui implementation (superseded by this Go/Bubbletea rewrite as of `v0.0.3`) is preserved, unmaintained, on the [`legacy/rust-tui`](https://github.com/bybrooklyn/openbitdo/tree/legacy/rust-tui) branch.
