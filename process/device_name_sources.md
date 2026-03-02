# Device Name Sources

This index lists sanitized naming sources used to build `/Users/brooklyn/data/8bitdo/cleanroom/spec/device_name_catalog.md`.

## Primary dirty-room references
- `DR-1`: Decompiled PID constants table (`VIDPID`) in `/Users/brooklyn/data/8bitdo/decompiled_dll/8BitDo_Ultimate_Software_V2.decompiled.cs` around line 194289.
- `DR-2`: Decompiled device name resolver (`getName()`) in `/Users/brooklyn/data/8bitdo/decompiled_dll/8BitDo_Ultimate_Software_V2.decompiled.cs` around line 12249.
- `DR-3`: Decompiled language map (`LanguageTools`) in `/Users/brooklyn/data/8bitdo/decompiled_dll/8BitDo_Ultimate_Software_V2.decompiled.cs` around line 206277.

## Official 8BitDo web cross-check references
- `WEB-1`: [8BitDo product catalog](https://www.8bitdo.com/#Products)
- `WEB-2`: [Ultimate 2 Wireless Controller](https://www.8bitdo.com/ultimate-2-wireless-controller/)
- `WEB-3`: [Ultimate 2 Bluetooth Controller](https://www.8bitdo.com/ultimate-2-bluetooth-controller/)
- `WEB-4`: [Ultimate 2C Wireless Controller](https://www.8bitdo.com/ultimate-2c-wireless-controller/)
- `WEB-5`: [Ultimate 2C Bluetooth Controller](https://www.8bitdo.com/ultimate-2c-bluetooth-controller/)
- `WEB-6`: [Pro 2 Bluetooth Controller](https://www.8bitdo.com/pro2/)
- `WEB-7`: [Retro 108 Mechanical Keyboard](https://www.8bitdo.com/retro-108-mechanical-keyboard/)
- `WEB-8`: [Retro 87 Mechanical Keyboard - Xbox Edition](https://www.8bitdo.com/retro-87-mechanical-keyboard-xbox/)
- `WEB-9`: [Retro R8 Mouse - Xbox Edition](https://www.8bitdo.com/retro-r8-mouse-xbox/)
- `WEB-10`: [Arcade Controller](https://www.8bitdo.com/arcade-controller/)
- `WEB-11`: [64 Bluetooth Controller](https://www.8bitdo.com/64-controller/)
- `WEB-12`: [Retro Mechanical Keyboard N Edition](https://www.8bitdo.com/retro-mechanical-keyboard/)

## Confidence policy
- `high`: direct match in `DR-2/DR-3` and/or product page exact-name match.
- `medium`: strong inferred match from product family naming with at least one source anchor.
- `low`: internal fallback name because no confident public marketing name was found.

## Low-confidence naming convention
- Canonical wording for low-confidence rows is:
  - `Unconfirmed Internal Device (PID_*)`
  - `Unconfirmed Variant Name (PID_*)`
  - `Unconfirmed Interface Name (PID_*)`
