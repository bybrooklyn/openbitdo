# Device Name Catalog

This catalog is the canonical PID-to-name reference for OpenBitdo.
Use it when you need a stable display name for runtime UI, docs, packaging copy, or evidence review.

## How To Use This Catalog

- Match devices by `pid_hex` first.
- Treat `display_name_en` as the preferred user-facing label.
- Keep aliases out of new canonical rows; add them to the `aliases` column and the alias index instead.
- When confidence is low or the source is `internal-fallback`, treat the name as provisional until better evidence is collected.

| canonical_pid_name | pid_hex | display_name_en | protocol_family | name_source | source_confidence | aliases |
| --- | --- | --- | --- | --- | --- | --- |
| PID_None | 0x0000 | No Device (Sentinel) | Unknown | internal-fallback | low | |
| PID_IDLE | 0x3109 | Unconfirmed Internal Device (PID_IDLE) | Standard64 | internal-fallback | low | |
| PID_SN30Plus | 0x6002 | SN30 Pro+ | Standard64 | official-web | medium | |
| PID_USB_Ultimate | 0x3100 | Unconfirmed Internal Device (PID_USB_Ultimate) | Standard64 | internal-fallback | low | |
| PID_USB_Ultimate2 | 0x3105 | Unconfirmed Internal Device (PID_USB_Ultimate2) | Standard64 | internal-fallback | low | |
| PID_USB_UltimateClasses | 0x3104 | Unconfirmed Internal Device (PID_USB_UltimateClasses) | Standard64 | internal-fallback | low | |
| PID_Xcloud | 0x2100 | Unconfirmed Internal Device (PID_Xcloud) | Standard64 | internal-fallback | low | |
| PID_Xcloud2 | 0x2101 | Unconfirmed Internal Device (PID_Xcloud2) | Standard64 | internal-fallback | low | |
| PID_ArcadeStick | 0x901a | Arcade Stick | Standard64 | internal-fallback | medium | |
| PID_Pro2 | 0x6003 | Pro 2 Bluetooth Controller | Standard64 | official-web | high | PID_Pro2_OLD |
| PID_Pro2_CY | 0x6006 | Unconfirmed Variant Name (PID_Pro2_CY) | Standard64 | internal-fallback | low | |
| PID_Pro2_Wired | 0x3010 | Pro 2 Wired Controller | Standard64 | internal-fallback | medium | |
| PID_Ultimate_PC | 0x3011 | Ultimate PC Controller | Standard64 | internal-fallback | medium | |
| PID_Ultimate2_4 | 0x3012 | Ultimate 2.4G Controller | Standard64 | internal-fallback | medium | |
| PID_Ultimate2_4RR | 0x3013 | Ultimate 2.4G Adapter | Standard64 | internal-fallback | medium | |
| PID_UltimateBT | 0x6007 | Ultimate Wireless Controller | Standard64 | vendor-language-map | high | |
| PID_UltimateBTRR | 0x3106 | Ultimate Wireless Adapter | Standard64 | internal-fallback | medium | |
| PID_JP | 0x5200 | Retro Mechanical Keyboard | JpHandshake | vendor-language-map | high | |
| PID_JPUSB | 0x5201 | Retro Mechanical Keyboard Receiver | JpHandshake | vendor-language-map | high | |
| PID_NUMPAD | 0x5203 | Retro 18 Mechanical Numpad | Standard64 | vendor-language-map | high | |
| PID_NUMPADRR | 0x5204 | Retro 18 Adapter | Standard64 | vendor-language-map | high | |
| PID_QINGCHUN2 | 0x310a | Ultimate 2C Controller | DInput | official-web | high | |
| PID_QINGCHUN2RR | 0x301c | Ultimate 2C Wireless Adapter | DInput | vendor-language-map | high | |
| PID_Xinput | 0x310b | Unconfirmed Interface Name (PID_Xinput) | DInput | internal-fallback | low | |
| PID_Pro3 | 0x6009 | Pro 3 Bluetooth Gamepad | DInput | vendor-language-map | high | |
| PID_Pro3USB | 0x600a | Pro 3 Bluetooth Adapter | DInput | vendor-language-map | high | |
| PID_Pro3DOCK | 0x600d | Charging Dock for Pro 3S Gamepad | Standard64 | vendor-language-map | high | |
| PID_108JP | 0x5209 | Retro 108 Mechanical Keyboard | JpHandshake | official-web | high | |
| PID_108JPUSB | 0x520a | Retro 108 Mechanical Adapter | JpHandshake | vendor-language-map | high | |
| PID_XBOXJP | 0x2028 | Retro 87 Mechanical Keyboard - Xbox Edition | JpHandshake | official-web | high | |
| PID_XBOXJPUSB | 0x202e | Retro 87 Mechanical Keyboard Adapter - Xbox Edition | JpHandshake | vendor-language-map | high | |
| PID_NGCDIY | 0x5750 | Mod Kit for NGC Controller | Standard64 | vendor-language-map | high | |
| PID_NGCRR | 0x902a | Retro Receiver for NGC | Standard64 | vendor-language-map | high | |
| PID_Ultimate2 | 0x6012 | Ultimate 2 Wireless Controller | DInput | official-web | high | |
| PID_Ultimate2RR | 0x6013 | Ultimate 2 Wireless Adapter | DInput | vendor-language-map | high | |
| PID_UltimateBT2 | 0x600f | Ultimate 2 Bluetooth Controller | DInput | official-web | high | |
| PID_UltimateBT2RR | 0x6011 | Ultimate 2 Bluetooth Adapter | DInput | vendor-language-map | high | |
| PID_Mouse | 0x5205 | Retro R8 Mouse | Standard64 | official-web | high | PID_ASLGMouse |
| PID_MouseRR | 0x5206 | Retro R8 Adapter | Standard64 | vendor-language-map | high | |
| PID_SaturnRR | 0x902b | Retro Receiver for Saturn | Standard64 | vendor-language-map | high | |
| PID_UltimateBT2C | 0x301a | Ultimate 2C Bluetooth Controller | Standard64 | official-web | high | |
| PID_Lashen | 0x301e | Ultimate Mobile Gaming Controller | Standard64 | vendor-language-map | high | |
| PID_HitBox | 0x600b | Arcade Controller | DInput | official-web | high | |
| PID_HitBoxRR | 0x600c | Arcade Controller Adapter | DInput | vendor-language-map | high | |
| PID_N64BT | 0x3019 | 64 Bluetooth Controller | Standard64 | official-web | high | |
| PID_N64 | 0x3004 | 64 2.4G Wireless Controller | Standard64 | vendor-language-map | high | |
| PID_N64RR | 0x9028 | Retro Receiver for N64 | Standard64 | vendor-language-map | high | |
| PID_XBOXUK | 0x3026 | Retro 87 Mechanical Keyboard - Xbox (UK) | Standard64 | vendor-language-map | high | |
| PID_XBOXUKUSB | 0x3027 | Retro 87 Mechanical Keyboard Adapter - Xbox (UK) | Standard64 | vendor-language-map | high | |
| PID_LashenX | 0x200b | Ultimate Mobile Gaming Controller For Xbox | Standard64 | vendor-language-map | high | |
| PID_68JP | 0x203a | Retro 68 Keyboard - N40 | JpHandshake | vendor-language-map | high | |
| PID_68JPUSB | 0x2049 | Retro 68 Keyboard Adapter - N40 | JpHandshake | vendor-language-map | high | |
| PID_N64JoySticks | 0x3021 | Joystick v2 for N64 Controller | Standard64 | vendor-language-map | high | |
| PID_DoubleSuper | 0x203e | Wireless Dual Super Button | Standard64 | vendor-language-map | high | |
| PID_Cube2RR | 0x2056 | Retro Cube 2 Adapter - N Edition | Standard64 | vendor-language-map | high | |
| PID_Cube2 | 0x2039 | Retro Cube 2 Speaker - N Edition | Standard64 | vendor-language-map | high | |
| PID_ASLGJP | 0x205a | Riviera Keyboard | JpHandshake | vendor-language-map | high | |

## Notes

- Canonical rows are unique by PID. Do not duplicate a PID to reflect a marketing alias.
- Name-source evidence is indexed in `/Users/brooklyn/data/8bitdo/cleanroom/process/device_name_sources.md`.
- Alias names live in `/Users/brooklyn/data/8bitdo/cleanroom/spec/alias_index.md` and stay out of the primary PID rows unless they become the canonical public name.
