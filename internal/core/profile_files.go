package core

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Profiles saved to files: a keyboard's whole profile, or one controller
// slot, as a small TOML file a person can read. Loading one only fills an
// editor's draft; nothing reaches a device until that draft is applied.

const profileFileVersion = 1

// keyboardProfileFile is a keyboard profile on disk. Keys and usages are
// written by number and by name; the number is what is read back.
type keyboardProfileFile struct {
	OpenBitdo int              `toml:"openbitdo_profile"`
	Device    string           `toml:"device"`
	Name      string           `toml:"name"`
	Volume    int              `toml:"volume"`
	LockWin   bool             `toml:"lock_win_key"`
	LockTab   bool             `toml:"lock_alt_tab"`
	LockF4    bool             `toml:"lock_alt_f4"`
	Mappings  []keyMappingFile `toml:"mapping"`
	Macros    []keyMacroFile   `toml:"macro"`
}

type keyMappingFile struct {
	Key      int    `toml:"key"`
	KeyName  string `toml:"key_name"`
	Kind     string `toml:"kind"` // "key", "media", "mouse", "none"
	Modifier int    `toml:"modifier,omitempty"`
	Usage    int    `toml:"usage,omitempty"`
	Media    int    `toml:"media,omitempty"`
	Buttons  int    `toml:"buttons,omitempty"`
	Wheel    int    `toml:"wheel,omitempty"`
	Does     string `toml:"does"`
}

type keyMacroFile struct {
	Key      int      `toml:"key"`
	KeyName  string   `toml:"key_name"`
	Name     string   `toml:"name"`
	Repeat   int      `toml:"repeat"`
	Interval int      `toml:"interval_ms"`
	Steps    []string `toml:"steps"`
}

const profileDeviceKeyboard = "retro-108"

func keyNameByID(id byte) string {
	if key, ok := KeyboardKeyByID(id); ok {
		return key.Name
	}
	return fmt.Sprintf("key %d", id)
}

func sortedKeyIDs[V any](m map[byte]V) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	return ids
}

// EncodeKeyboardProfile renders a keyboard profile as a profile file.
func EncodeKeyboardProfile(profile KeyboardProfile) ([]byte, error) {
	file := keyboardProfileFile{
		OpenBitdo: profileFileVersion, Device: profileDeviceKeyboard, Name: profile.Name, Volume: profile.Volume,
		LockWin: profile.Locks.WinKey, LockTab: profile.Locks.AltTab, LockF4: profile.Locks.AltF4,
	}
	for _, id := range sortedKeyIDs(profile.Mappings) {
		target := profile.Mappings[byte(id)]
		entry := keyMappingFile{Key: id, KeyName: keyNameByID(byte(id)), Does: target.String()}
		switch target.Kind {
		case TargetKey:
			entry.Kind, entry.Modifier, entry.Usage = "key", int(target.Modifier), int(target.Key)
		case TargetMedia:
			entry.Kind, entry.Media = "media", int(target.Media)
		case TargetMouse:
			entry.Kind, entry.Buttons, entry.Wheel = "mouse", int(target.Buttons), int(target.Wheel)
		default:
			entry.Kind = "none"
		}
		file.Mappings = append(file.Mappings, entry)
	}
	for _, id := range sortedKeyIDs(profile.Macros) {
		macro := profile.Macros[byte(id)]
		entry := keyMacroFile{Key: id, KeyName: keyNameByID(byte(id)), Name: macro.Name, Repeat: macro.Repeat, Interval: macro.IntervalMillis}
		for _, step := range macro.Steps {
			switch step.Kind {
			case StepPress:
				entry.Steps = append(entry.Steps, fmt.Sprintf("press %d %s", step.Usage, keyName(step.Usage)))
			case StepRelease:
				entry.Steps = append(entry.Steps, fmt.Sprintf("release %d %s", step.Usage, keyName(step.Usage)))
			case StepWait:
				entry.Steps = append(entry.Steps, fmt.Sprintf("wait %d", step.Millis))
			}
		}
		file.Macros = append(file.Macros, entry)
	}
	var out bytes.Buffer
	if err := toml.NewEncoder(&out).Encode(file); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// DecodeKeyboardProfile reads a profile file back, refusing anything a
// keyboard could not hold: it is about to become an editor's draft.
func DecodeKeyboardProfile(data []byte) (KeyboardProfile, error) {
	var file keyboardProfileFile
	meta, err := toml.Decode(string(data), &file)
	if err != nil {
		return KeyboardProfile{}, fmt.Errorf("not a readable profile file: %w", err)
	}
	if file.OpenBitdo != profileFileVersion || file.Device != profileDeviceKeyboard {
		return KeyboardProfile{}, fmt.Errorf("not a Retro 108 profile (device %q, version %d)", file.Device, file.OpenBitdo)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return KeyboardProfile{}, fmt.Errorf("unknown setting %q", undecoded[0].String())
	}
	if file.Volume < 1 || file.Volume > 5 {
		return KeyboardProfile{}, fmt.Errorf("volume %d is outside 1-5", file.Volume)
	}
	inByte := func(what string, v int) error {
		if v < 0 || v > 255 {
			return fmt.Errorf("%s %d is outside 0-255", what, v)
		}
		return nil
	}
	profile := KeyboardProfile{
		Name: file.Name, Volume: file.Volume, Mappings: map[byte]KeyTarget{}, Macros: map[byte]KeyMacro{},
		Locks: KeyboardLocks{WinKey: file.LockWin, AltTab: file.LockTab, AltF4: file.LockF4},
	}
	for _, entry := range file.Mappings {
		if _, known := KeyboardKeyByID(byte(entry.Key)); !known || entry.Key > 255 || entry.Key < 0 {
			return KeyboardProfile{}, fmt.Errorf("key %d is not a key this keyboard has", entry.Key)
		}
		var target KeyTarget
		switch entry.Kind {
		case "key":
			if err := inByte("modifier", entry.Modifier); err != nil {
				return KeyboardProfile{}, err
			}
			if err := inByte("usage", entry.Usage); err != nil {
				return KeyboardProfile{}, err
			}
			if entry.Modifier != 0 && !isModifierUsage(byte(entry.Modifier)) {
				return KeyboardProfile{}, fmt.Errorf("modifier %d is not a modifier key", entry.Modifier)
			}
			target = KeyTarget{Kind: TargetKey, Modifier: byte(entry.Modifier), Key: byte(entry.Usage)}
		case "media":
			if entry.Media < 1 || entry.Media > 0xffff {
				return KeyboardProfile{}, fmt.Errorf("media usage %d is outside 1-65535", entry.Media)
			}
			target = KeyTarget{Kind: TargetMedia, Media: uint16(entry.Media)}
		case "mouse":
			if err := inByte("mouse buttons", entry.Buttons); err != nil {
				return KeyboardProfile{}, err
			}
			if entry.Wheel < -127 || entry.Wheel > 127 {
				return KeyboardProfile{}, fmt.Errorf("wheel step %d is outside -127..127", entry.Wheel)
			}
			target = KeyTarget{Kind: TargetMouse, Buttons: byte(entry.Buttons), Wheel: int8(entry.Wheel)}
		case "none":
		default:
			return KeyboardProfile{}, fmt.Errorf("key %d has an unknown kind %q", entry.Key, entry.Kind)
		}
		profile.Mappings[byte(entry.Key)] = target
	}
	if len(file.Macros) > KeyMacroSlots {
		return KeyboardProfile{}, fmt.Errorf("%d macros, but a keyboard holds %d", len(file.Macros), KeyMacroSlots)
	}
	for _, entry := range file.Macros {
		if _, known := KeyboardKeyByID(byte(entry.Key)); !known || entry.Key > 255 || entry.Key < 0 {
			return KeyboardProfile{}, fmt.Errorf("macro key %d is not a key this keyboard has", entry.Key)
		}
		macro := KeyMacro{Key: byte(entry.Key), Name: entry.Name, Repeat: entry.Repeat, IntervalMillis: entry.Interval}
		for i, text := range entry.Steps {
			var verb string
			var n int
			if _, err := fmt.Sscanf(text, "%s %d", &verb, &n); err != nil {
				return KeyboardProfile{}, fmt.Errorf("macro %q step %d (%q) is not \"press N\", \"release N\" or \"wait N\"", entry.Name, i+1, text)
			}
			switch verb {
			case "press", "release":
				if err := inByte("usage", n); err != nil {
					return KeyboardProfile{}, err
				}
				kind := StepPress
				if verb == "release" {
					kind = StepRelease
				}
				macro.Steps = append(macro.Steps, KeyMacroStep{Kind: kind, Usage: byte(n)})
			case "wait":
				macro.Steps = append(macro.Steps, KeyMacroStep{Kind: StepWait, Millis: n})
			default:
				return KeyboardProfile{}, fmt.Errorf("macro %q step %d: unknown action %q", entry.Name, i+1, verb)
			}
		}
		if err := macro.Validate(); err != nil {
			return KeyboardProfile{}, fmt.Errorf("macro %q: %w", entry.Name, err)
		}
		if n := len([]rune(macro.Name)); n < 1 || n > 14 {
			return KeyboardProfile{}, fmt.Errorf("macro on key %d: a macro name is 1-14 characters", entry.Key)
		}
		profile.Macros[macro.Key] = macro
	}
	return profile, nil
}

// padSlotFile is one controller slot on disk.
type padSlotFile struct {
	OpenBitdo int             `toml:"openbitdo_profile"`
	Device    string          `toml:"device"`
	Platform  int             `toml:"platform"`
	Name      string          `toml:"name"`
	Buttons   []padButtonFile `toml:"button"`
	Sticks    padRangesFile   `toml:"sticks"`
	Triggers  padRangesFile   `toml:"triggers"`
	Vibration struct {
		Left  int `toml:"left"`
		Right int `toml:"right"`
	} `toml:"vibration"`
	Options uint32 `toml:"options"`
}

type padButtonFile struct {
	Input  string `toml:"input"`
	Target uint32 `toml:"target"`
	Does   string `toml:"does"`
}

type padRangesFile struct {
	LeftStart  int `toml:"left_start"`
	LeftEnd    int `toml:"left_end"`
	RightStart int `toml:"right_start"`
	RightEnd   int `toml:"right_end"`
}

const profileDevicePad = "ultimate-2"

// EncodePadSlot renders one controller slot as a profile file.
func EncodePadSlot(platform byte, slot PadSlot) ([]byte, error) {
	file := padSlotFile{OpenBitdo: profileFileVersion, Device: profileDevicePad, Platform: int(platform), Name: slot.Name, Options: slot.Options}
	for i, target := range slot.Buttons {
		file.Buttons = append(file.Buttons, padButtonFile{Input: PadInputs[i].Name, Target: uint32(target), Does: target.String()})
	}
	file.Sticks = padRangesFile{int(slot.LeftStick.Start), int(slot.LeftStick.End), int(slot.RightStick.Start), int(slot.RightStick.End)}
	file.Triggers = padRangesFile{int(slot.LeftTrigger.Start), int(slot.LeftTrigger.End), int(slot.RightTrigger.Start), int(slot.RightTrigger.End)}
	file.Vibration.Left, file.Vibration.Right = slot.VibrationLeft, slot.VibrationRight
	var out bytes.Buffer
	if err := toml.NewEncoder(&out).Encode(file); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// DecodePadSlot reads a controller slot file back. A slot saved on one
// platform is refused on another: the face-button defaults differ, so the
// same file would mean something else.
func DecodePadSlot(data []byte, platform byte) (PadSlot, error) {
	var file padSlotFile
	meta, err := toml.Decode(string(data), &file)
	if err != nil {
		return PadSlot{}, fmt.Errorf("not a readable profile file: %w", err)
	}
	if file.OpenBitdo != profileFileVersion || file.Device != profileDevicePad {
		return PadSlot{}, fmt.Errorf("not an Ultimate 2 profile (device %q, version %d)", file.Device, file.OpenBitdo)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return PadSlot{}, fmt.Errorf("unknown setting %q", undecoded[0].String())
	}
	if file.Platform != int(platform) {
		return PadSlot{}, fmt.Errorf("this profile was saved for the other position of the controller's mode switch")
	}
	if len(file.Buttons) != PadButtons {
		return PadSlot{}, fmt.Errorf("%d buttons listed, expected %d", len(file.Buttons), PadButtons)
	}
	byteRange := func(start, end int) (PadRange, error) {
		if start < 0 || end > 255 || start > 255 || end < 0 {
			return PadRange{}, fmt.Errorf("range %d-%d is outside 0-255", start, end)
		}
		return PadRange{byte(start), byte(end)}, nil
	}
	slot := PadSlot{InUse: true, Name: file.Name, Options: file.Options,
		VibrationLeft: file.Vibration.Left, VibrationRight: file.Vibration.Right}
	for i, entry := range file.Buttons {
		if entry.Input != PadInputs[i].Name {
			return PadSlot{}, fmt.Errorf("button %d is %q, expected %q", i+1, entry.Input, PadInputs[i].Name)
		}
		slot.Buttons[i] = PadTarget(entry.Target)
	}
	if slot.LeftStick, err = byteRange(file.Sticks.LeftStart, file.Sticks.LeftEnd); err != nil {
		return PadSlot{}, err
	}
	if slot.RightStick, err = byteRange(file.Sticks.RightStart, file.Sticks.RightEnd); err != nil {
		return PadSlot{}, err
	}
	if slot.LeftTrigger, err = byteRange(file.Triggers.LeftStart, file.Triggers.LeftEnd); err != nil {
		return PadSlot{}, err
	}
	if slot.RightTrigger, err = byteRange(file.Triggers.RightStart, file.Triggers.RightEnd); err != nil {
		return PadSlot{}, err
	}
	if err := slot.validate(); err != nil {
		return PadSlot{}, err
	}
	return slot, nil
}

// ProfileFileName turns a profile name into a file name: letters, digits,
// dashes and underscores, never empty, never a path.
func ProfileFileName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "profile.toml"
	}
	return b.String() + ".toml"
}

// WriteProfileFile saves data under dir, creating it. It will not replace
// a file unless overwrite is set.
func WriteProfileFile(dir, name string, data []byte, overwrite bool) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, ProfileFileName(name))
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if overwrite {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return path, err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return path, err
	}
	return path, file.Close()
}

// ListProfileFiles lists the profile files in dir, by name. A missing
// directory is an empty list.
func ListProfileFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".toml") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}
