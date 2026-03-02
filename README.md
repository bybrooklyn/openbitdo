# OpenBitdo

OpenBitdo is a clean-room implementation workspace for beginner-friendly 8BitDo tooling.
OpenBitDo is an unofficial tool and should be used at your own risk. The authors are not responsible for device damage, firmware corruption, or data loss. This project is not affiliated with or endorsed by 8BitDo. See the [LICENSE](/Users/brooklyn/data/8bitdo/cleanroom/LICENSE) file for details.

## Beginner Quickstart
From `/Users/brooklyn/data/8bitdo/cleanroom/sdk`:

```bash
cargo run -p openbitdo --
```

Optional mock mode:

```bash
cargo run -p openbitdo -- --mock
```

Beginner flow is always `openbitdo` only. No extra command surface is required.

## Install (Public RC)

Homebrew tap install path for RC:

```bash
brew tap bybrooklyn/openbitdo
brew install openbitdo
```

Homebrew Core inclusion (`brew install openbitdo` without tapping) is not an RC blocker and may land later.

AUR package names:
- `openbitdo` (source build)
- `openbitdo-bin` (prebuilt binary)

macOS RC caveat:
- `.pkg` installers are unsigned/ad-hoc for `v0.0.1-rc.1`.
- notarization is required starting with `v0.1.0`.

## UI Language Support

| Language | Status | Notes |
| --- | --- | --- |
| English | Supported | Default and primary beginner flow language. |
| Spanish | In Progress | Roadmap item for core wizard screens. |
| Japanese | Planned | Planned after Spanish stabilization. |
| German | Planned | Planned after Spanish stabilization. |
| French | Planned | Planned after Spanish stabilization. |

## Support Status Model

| Runtime Tier | README Status | Meaning |
| --- | --- | --- |
| `full` | Supported | Mapping/update paths allowed when safety gates are satisfied. |
| `candidate-readonly` | In Progress | Detect/identify/diagnostics enabled, writes and firmware transfer blocked. |
| `detect-only` | Planned | Detect/identify baseline now, broader functionality planned. |

Beginner UI note: candidate devices are shown with blocked-action messaging in the TUI so new users always see the safe path first.

## Protocol Family Overview

| Family | PID Rows | Notes |
| --- | --- | --- |
| Standard64 | 36 | Largest family, mixed status from Planned to Supported. |
| DInput | 11 | Includes current full-support Ultimate2 and Pro3 lines. |
| JpHandshake | 9 | Includes JP108 full support and keyboard-related candidate lines. |
| DS4Boot | 0 | No standalone canonical PID rows in current catalog. |
| Unknown | 1 | Sentinel/internal row only. |

DS4Boot explicit note: there are currently no active canonical PID rows in this catalog.

## Compatibility Verification Dates (Manual, ISO)
| Family | Last verified date | Notes |
| --- | --- | --- |
| Standard64 | 2026-03-02 | Manual RC verification date. |
| DInput | 2026-03-02 | Manual RC verification date. |
| JpHandshake | 2026-03-02 | Manual RC verification date. |
| DS4Boot | 2026-03-02 | No active canonical rows. |
| Unknown | 2026-03-02 | Sentinel/internal row only. |

## Device Compatibility (Canonical, No Duplicate PIDs)
Device naming references are documented in [device_name_catalog.md](/Users/brooklyn/data/8bitdo/cleanroom/spec/device_name_catalog.md) and [device_name_sources.md](/Users/brooklyn/data/8bitdo/cleanroom/process/device_name_sources.md).

### Unknown

| Canonical ID | Display Name | PID (hex) | Family | Status | Current User Actions | Firmware Path | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `PID_None` | No Device (Sentinel) | `0x0000` | Unknown | Planned | None | N/A | Internal sentinel row only. |

### Standard64

| Canonical ID | Display Name | PID (hex) | Family | Status | Current User Actions | Firmware Path | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `PID_IDLE` | Unconfirmed Internal Device (PID_IDLE) | `0x3109` | Standard64 | Planned | Detect, identify | Blocked | Internal interface row. |
| `PID_SN30Plus` | SN30 Pro+ | `0x6002` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_USB_Ultimate` | Unconfirmed Internal Device (PID_USB_Ultimate) | `0x3100` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_USB_Ultimate2` | Unconfirmed Internal Device (PID_USB_Ultimate2) | `0x3105` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_USB_UltimateClasses` | Unconfirmed Internal Device (PID_USB_UltimateClasses) | `0x3104` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_Xcloud` | Unconfirmed Internal Device (PID_Xcloud) | `0x2100` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_Xcloud2` | Unconfirmed Internal Device (PID_Xcloud2) | `0x2101` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_ArcadeStick` | Arcade Stick | `0x901a` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_Pro2` | Pro 2 Bluetooth Controller | `0x6003` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_Pro2_CY` | Unconfirmed Variant Name (PID_Pro2_CY) | `0x6006` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_Pro2_Wired` | Pro 2 Wired Controller | `0x3010` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_Ultimate_PC` | Ultimate PC Controller | `0x3011` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_Ultimate2_4` | Ultimate 2.4G Controller | `0x3012` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_Ultimate2_4RR` | Ultimate 2.4G Adapter | `0x3013` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_UltimateBT` | Ultimate Wireless Controller | `0x6007` | Standard64 | Supported | Detect, diagnose, recommended update | Stable download + local fallback | Full-support runtime. |
| `PID_UltimateBTRR` | Ultimate Wireless Adapter | `0x3106` | Standard64 | Supported | Detect, diagnose, recommended update | Stable download + local fallback | Full-support runtime. |
| `PID_NUMPAD` | Retro 18 Mechanical Numpad | `0x5203` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_NUMPADRR` | Retro 18 Adapter | `0x5204` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_Pro3DOCK` | Charging Dock for Pro 3S Gamepad | `0x600d` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_NGCDIY` | Mod Kit for NGC Controller | `0x5750` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_NGCRR` | Retro Receiver for NGC | `0x902a` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_Mouse` | Retro R8 Mouse | `0x5205` | Standard64 | Planned | Detect, identify | Blocked | Canonical row for `0x5205`. |
| `PID_MouseRR` | Retro R8 Adapter | `0x5206` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_SaturnRR` | Retro Receiver for Saturn | `0x902b` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_UltimateBT2C` | Ultimate 2C Bluetooth Controller | `0x301a` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_Lashen` | Ultimate Mobile Gaming Controller | `0x301e` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_N64BT` | 64 Bluetooth Controller | `0x3019` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_N64` | 64 2.4G Wireless Controller | `0x3004` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_N64RR` | Retro Receiver for N64 | `0x9028` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_XBOXUK` | Retro 87 Mechanical Keyboard - Xbox (UK) | `0x3026` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_XBOXUKUSB` | Retro 87 Mechanical Keyboard Adapter - Xbox (UK) | `0x3027` | Standard64 | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_LashenX` | Ultimate Mobile Gaming Controller For Xbox | `0x200b` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_N64JoySticks` | Joystick v2 for N64 Controller | `0x3021` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_DoubleSuper` | Wireless Dual Super Button | `0x203e` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_Cube2RR` | Retro Cube 2 Adapter - N Edition | `0x2056` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_Cube2` | Retro Cube 2 Speaker - N Edition | `0x2039` | Standard64 | Planned | Detect, identify | Blocked | Planned support only. |

### JpHandshake

| Canonical ID | Display Name | PID (hex) | Family | Status | Current User Actions | Firmware Path | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `PID_JP` | Retro Mechanical Keyboard | `0x5200` | JpHandshake | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_JPUSB` | Retro Mechanical Keyboard Receiver | `0x5201` | JpHandshake | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_108JP` | Retro 108 Mechanical Keyboard | `0x5209` | JpHandshake | Supported | Detect, diagnose, dedicated mapping (`A/B/K1-K8`), recommended update | Stable download + local fallback | Full-support JP108 flow. |
| `PID_108JPUSB` | Retro 108 Mechanical Adapter | `0x520a` | JpHandshake | Supported | Detect, diagnose, dedicated mapping (`A/B/K1-K8`), recommended update | Stable download + local fallback | Full-support JP108 receiver flow. |
| `PID_XBOXJP` | Retro 87 Mechanical Keyboard - Xbox Edition | `0x2028` | JpHandshake | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_XBOXJPUSB` | Retro 87 Mechanical Keyboard Adapter - Xbox Edition | `0x202e` | JpHandshake | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_68JP` | Retro 68 Keyboard - N40 | `0x203a` | JpHandshake | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_68JPUSB` | Retro 68 Keyboard Adapter - N40 | `0x2049` | JpHandshake | In Progress | Detect, identify, diagnose | Blocked (`NotHardwareConfirmed`) | Candidate read-only. |
| `PID_ASLGJP` | Riviera Keyboard | `0x205a` | JpHandshake | Planned | Detect, identify | Blocked | Planned support only. |

### DInput

| Canonical ID | Display Name | PID (hex) | Family | Status | Current User Actions | Firmware Path | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `PID_QINGCHUN2` | Ultimate 2C Controller | `0x310a` | DInput | Supported | Detect, diagnose, recommended update | Stable download + local fallback | Full-support runtime. |
| `PID_QINGCHUN2RR` | Ultimate 2C Wireless Adapter | `0x301c` | DInput | Supported | Detect, diagnose, recommended update | Stable download + local fallback | Full-support runtime. |
| `PID_Xinput` | Unconfirmed Interface Name (PID_Xinput) | `0x310b` | DInput | Planned | Detect, identify | Blocked | Planned support only. |
| `PID_Pro3` | Pro 3 Bluetooth Gamepad | `0x6009` | DInput | Supported | Detect, diagnose, recommended update | Stable download + local fallback | Full-support runtime. |
| `PID_Pro3USB` | Pro 3 Bluetooth Adapter | `0x600a` | DInput | Supported | Detect, diagnose, recommended update | Stable download + local fallback | Full-support runtime. |
| `PID_Ultimate2` | Ultimate 2 Wireless Controller | `0x6012` | DInput | Supported | Detect, diagnose, slot mapping (`A/B/K1-K8`), analog L2/R2, recommended update | Stable download + local fallback | Full-support Ultimate2 RC scope (known controller-button targets). |
| `PID_Ultimate2RR` | Ultimate 2 Wireless Adapter | `0x6013` | DInput | Supported | Detect, diagnose, slot mapping (`A/B/K1-K8`), analog L2/R2, recommended update | Stable download + local fallback | Full-support Ultimate2 receiver RC scope (known controller-button targets). |
| `PID_UltimateBT2` | Ultimate 2 Bluetooth Controller | `0x600f` | DInput | Supported | Detect, diagnose, recommended update | Stable download + local fallback | Full-support runtime. |
| `PID_UltimateBT2RR` | Ultimate 2 Bluetooth Adapter | `0x6011` | DInput | Supported | Detect, diagnose, recommended update | Stable download + local fallback | Full-support runtime. |
| `PID_HitBox` | Arcade Controller | `0x600b` | DInput | Supported | Detect, diagnose, recommended update | Stable download + local fallback | Full-support runtime. |
| `PID_HitBoxRR` | Arcade Controller Adapter | `0x600c` | DInput | Supported | Detect, diagnose, recommended update | Stable download + local fallback | Full-support runtime. |

## Alias Appendix (Non-Canonical Names)
These aliases are intentionally excluded from canonical PID rows to guarantee uniqueness.

| Alias PID Name | Canonical PID Name | PID (hex) |
| --- | --- | --- |
| `PID_Pro2_OLD` | `PID_Pro2` | `0x6003` |
| `PID_ASLGMouse` | `PID_Mouse` | `0x5205` |

See [alias_index.md](/Users/brooklyn/data/8bitdo/cleanroom/spec/alias_index.md) for details.

## Hardware Support Confidence
Support is implemented to our best current knowledge. Coverage and confidence are expanded and confirmed over time through community testing and hardware reports.

## Dirty-Room Evidence Backlog
- [dirtyroom_evidence_backlog.md](/Users/brooklyn/data/8bitdo/cleanroom/process/dirtyroom_evidence_backlog.md)
- [dirtyroom_collection_playbook.md](/Users/brooklyn/data/8bitdo/cleanroom/process/dirtyroom_collection_playbook.md)
- [dirtyroom_dossier_schema.md](/Users/brooklyn/data/8bitdo/cleanroom/process/dirtyroom_dossier_schema.md)
- [community_evidence_intake.md](/Users/brooklyn/data/8bitdo/cleanroom/process/community_evidence_intake.md)

## Source Notes
- Canonical clean-room naming catalog: [device_name_catalog.md](/Users/brooklyn/data/8bitdo/cleanroom/spec/device_name_catalog.md)
- Dirty-room source index + official web cross-check URLs: [device_name_sources.md](/Users/brooklyn/data/8bitdo/cleanroom/process/device_name_sources.md)

## Public RC Gate
`v0.0.1-rc.1` remains blocked until release-blocker issues are zero and all required checks are green.
RC gating is checklist-based (see [RC_CHECKLIST.md](/Users/brooklyn/data/8bitdo/cleanroom/RC_CHECKLIST.md)).
