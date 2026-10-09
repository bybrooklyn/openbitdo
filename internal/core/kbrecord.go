package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"reflect"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// This file is the core's side of the keyboards that keep their whole
// profile in one record: Retro 87 (Xbox edition and its UK sibling), Retro
// 68 and the Riviera keyboard. A profile is every key's assignment, the
// lock options, volume, sleep timers, lighting and recorded macros. The
// record's layout is in
// docs/clean-room-evidence/dossiers/2028/keyboard_record.toml; none of it
// has been exchanged with a real keyboard yet.

const (
	// RecordKeySlots is how many entries a keyboard's key table has. Not
	// every entry is a key; see RecordKeyboardProfile.Keys.
	RecordKeySlots = 120
	// RecordMacroSlotsMax is the most macros any of the keyboards holds.
	RecordMacroSlotsMax = 10

	kbRecInUse = 0x20200902 // marks the record, or a macro, as set

	kbRecOffFlag      = 0x000
	kbRecOffName      = 0x004 // 32 bytes
	kbRecOffProfileOn = 0x024
	kbRecOffSleep     = 0x026 // Retro 68 and Riviera only
	kbRecOffLightsOff = 0x027 // Retro 68 and Riviera only
	kbRecOffKeys      = 0x028 // 120 entries of 12: key u8, target u32 at 4, kind u32 at 8
	kbRecOffLocks     = 0x5c8
	kbRecOffVolume    = 0x5c9
	kbRecOffTheme     = 0x5ca
	kbRecOffLightIdle = 0x5cc // u32, written only when a profile is created
	kbRecOffPerKey    = 0x5f9
	kbRecOffDynamic   = 0x5fa

	kbRecKeyEntry = 12

	// Values a key's target takes that are not HID usages.
	kbRecValueFn  = 242
	kbRecValueOff = 243

	// Kinds of target, as stored.
	kbRecKindNone  = 0
	kbRecKindKey   = 1
	kbRecKindMedia = 2
	kbRecKindMouse = 3
	kbRecKindMacro = 4

	// A macro is a 48-byte header at the start of its slot and four-byte
	// steps from 64 bytes in.
	kbRecMacroHeader = 48
	kbRecMacroSteps  = 64

	// A colour block is nine bytes of settings, three bytes per light, and
	// a closing byte.
	kbRecLightsHead = 9
)

// kbRecordLayout is what differs between the keyboard models. The record
// itself is laid out the same on all of them.
type kbRecordLayout struct {
	// macroSlots macros of macroSlotSize bytes each; a slot has room for
	// macroCapacity steps, of which the vendor application writes at most
	// macroLimit.
	macroSlots, macroSlotSize int
	macroCapacity, macroLimit int
	// lights is how many lights the per-key colour block covers.
	lights int
	// iso: the key table has the extra ISO key at 108, which moves the
	// port buttons up by one. The default assignment of each key depends
	// on which national layout the keyboard is, which is not read here.
	iso bool
	// timers: the record holds a sleep timer and a lights-off timer, a
	// macro has a play mode, and Fn cannot be assigned.
	timers bool
	// fewThemes: no loop or colour ripple theme, and themes are numbered
	// without them.
	fewThemes bool
}

var (
	kbRecordLayoutRetro87   = kbRecordLayout{macroSlots: 10, macroSlotSize: 4096, macroCapacity: 1008, macroLimit: 1008, lights: 91}
	kbRecordLayoutRetro87UK = kbRecordLayout{macroSlots: 10, macroSlotSize: 4096, macroCapacity: 1008, macroLimit: 1008, lights: 92, iso: true}
	kbRecordLayoutRetro68   = kbRecordLayout{macroSlots: 8, macroSlotSize: 1024, macroCapacity: 240, macroLimit: 200, lights: 72, timers: true}
	kbRecordLayoutRiviera   = kbRecordLayout{macroSlots: 8, macroSlotSize: 1024, macroCapacity: 240, macroLimit: 200, lights: 72, timers: true, fewThemes: true}
)

func kbRecordLayoutFor(vidPid protocol.VidPid) (kbRecordLayout, bool) {
	switch vidPid.PID {
	case 0x2028, 0x202e:
		return kbRecordLayoutRetro87, true
	case 0x3026, 0x3027:
		return kbRecordLayoutRetro87UK, true
	case 0x203a, 0x2049:
		return kbRecordLayoutRetro68, true
	case 0x205a:
		return kbRecordLayoutRiviera, true
	}
	return kbRecordLayout{}, false
}

func (l kbRecordLayout) lightsSize() int { return kbRecLightsHead + 3*l.lights + 1 }

// RecordKey is one entry of a keyboard's key table that is a key.
type RecordKey struct {
	// Index is the key's place in the table, and in
	// RecordKeyboardProfile.Targets.
	Index int
	Name  string
	// Usage is the HID usage the key sends as printed on a US layout; 0
	// for a port button.
	Usage byte
	// Port marks the buttons that do nothing until assigned: the A and B
	// buttons and the K1-K8 buttons plugged into the keyboard's ports.
	Port bool
}

// keys lists the key table's entries. Not every model has a physical key
// for each one (a Retro 68 has no function row); an assignment to a key
// that is not there is stored and has no effect.
func (l kbRecordLayout) keys() []RecordKey {
	keys := make([]RecordKey, 0, RecordKeySlots)
	for i := 0; i < 8; i++ {
		keys = append(keys, RecordKey{Index: i, Name: hidKeyNames[byte(0xe0+i)], Usage: byte(0xe0 + i)})
	}
	keys = append(keys, RecordKey{Index: 9, Name: "A button", Port: true}, RecordKey{Index: 10, Name: "B button", Port: true})
	// From 12 to 107 an entry's usage is its index less eight; 58 (usage
	// 50) is not a key.
	for i := 12; i <= 107; i++ {
		if i != 58 {
			keys = append(keys, RecordKey{Index: i, Name: hidKeyNames[byte(i-8)], Usage: byte(i - 8)})
		}
	}
	ports := 108
	if l.iso {
		keys = append(keys, RecordKey{Index: 108, Name: "ISO key", Usage: 0x64})
		ports = 109
	}
	for i := 0; i < 8; i++ {
		keys = append(keys, RecordKey{Index: ports + i, Name: fmt.Sprintf("K%d", i+1), Port: true})
	}
	return keys
}

// RecordTargetKind is what kind of thing a key is assigned to.
type RecordTargetKind int

const (
	// RecordTargetDefault: the key is itself; a port button does nothing.
	RecordTargetDefault RecordTargetKind = iota
	// RecordTargetKey: a keyboard key, a modifier, or a key with one
	// modifier held.
	RecordTargetKey
	// RecordTargetMedia: a media or system key.
	RecordTargetMedia
	// RecordTargetMouse: a mouse button or one step of the wheel.
	RecordTargetMouse
	// RecordTargetMacro: plays the macro in a slot.
	RecordTargetMacro
	// RecordTargetOff: the key does nothing.
	RecordTargetOff
	// RecordTargetFn: the key is the Fn key.
	RecordTargetFn
	// RecordTargetUnknown: something this program does not understand. It
	// is kept as stored and cannot be assigned.
	RecordTargetUnknown
)

// Mouse actions a key can be assigned.
const (
	RecordMouseLeft        byte = 232
	RecordMouseMiddle      byte = 233
	RecordMouseRight       byte = 234
	RecordMouseButton5     byte = 235
	RecordMouseButton4     byte = 236
	RecordMouseDoubleClick byte = 237
	RecordMouseScrollUp    byte = 238
	RecordMouseScrollDown  byte = 239
	RecordMouseScrollLeft  byte = 240
	RecordMouseScrollRight byte = 241
)

// RecordMediaKeys are the media and system keys a key can be assigned, as
// HID consumer-page usages.
var RecordMediaKeys = map[uint16]string{
	234: "Volume down", 233: "Volume up", 226: "Mute", 205: "Play/Pause", 182: "Previous track", 181: "Next track",
	394: "Mail", 402: "Calculator", 547: "Browser", 112: "Brightness down", 111: "Brightness up",
}

// RecordKeyTarget is what one key is assigned to.
type RecordKeyTarget struct {
	Kind RecordTargetKind
	// Modifier and Key are HID keyboard usages, for RecordTargetKey.
	// Either may be zero: a modifier alone, a key alone, or both.
	Modifier, Key byte
	// Media is a HID consumer-page usage from RecordMediaKeys.
	Media uint16
	// Mouse is one of the RecordMouse* actions.
	Mouse byte
	// Macro is the slot of the macro the key plays.
	Macro int

	// What an unknown target was stored as.
	rawKind, rawValue uint32
}

func kbRecModifier(usage uint32) bool { return usage >= 0xe0 && usage <= 0xe7 }

func decodeRecordKeyTarget(layout kbRecordLayout, entry []byte) RecordKeyTarget {
	value, kind := binary.LittleEndian.Uint32(entry[4:]), binary.LittleEndian.Uint32(entry[8:])
	switch kind {
	case kbRecKindNone, kbRecKindKey:
		switch {
		case kind == kbRecKindNone && value == uint32(entry[0]):
			return RecordKeyTarget{}
		case value == kbRecValueOff:
			return RecordKeyTarget{Kind: RecordTargetOff}
		case value == kbRecValueFn:
			return RecordKeyTarget{Kind: RecordTargetFn}
		case kbRecModifier(value):
			return RecordKeyTarget{Kind: RecordTargetKey, Modifier: byte(value)}
		case value >= 4 && value < 0xe0:
			return RecordKeyTarget{Kind: RecordTargetKey, Key: byte(value)}
		case value <= 0xffff && kbRecModifier(value&0xff) && value>>8 >= 4 && value>>8 < 0xe0:
			return RecordKeyTarget{Kind: RecordTargetKey, Modifier: byte(value), Key: byte(value >> 8)}
		}
	case kbRecKindMedia:
		if value >= 1 && value <= 0xffff {
			return RecordKeyTarget{Kind: RecordTargetMedia, Media: uint16(value)}
		}
	case kbRecKindMouse:
		if value >= uint32(RecordMouseLeft) && value <= uint32(RecordMouseScrollRight) {
			return RecordKeyTarget{Kind: RecordTargetMouse, Mouse: byte(value)}
		}
	case kbRecKindMacro:
		if slot := int(value) / layout.macroSlotSize; int(value)%layout.macroSlotSize == 0 && slot < layout.macroSlots {
			return RecordKeyTarget{Kind: RecordTargetMacro, Macro: slot}
		}
	}
	return RecordKeyTarget{Kind: RecordTargetUnknown, rawKind: kind, rawValue: value}
}

// wire gives the target as stored for a key whose own code is code.
func (t RecordKeyTarget) wire(layout kbRecordLayout, key RecordKey, code byte) (value, kind uint32, err error) {
	switch t.Kind {
	case RecordTargetDefault:
		return uint32(code), kbRecKindNone, nil
	case RecordTargetOff:
		if key.Port { // a port button that does nothing is at its default
			return uint32(code), kbRecKindNone, nil
		}
		return kbRecValueOff, kbRecKindKey, nil
	case RecordTargetFn:
		if layout.timers {
			return 0, 0, fmt.Errorf("the Fn key cannot be assigned on this keyboard")
		}
		return kbRecValueFn, kbRecKindKey, nil
	case RecordTargetKey:
		if t.Modifier != 0 && !kbRecModifier(uint32(t.Modifier)) {
			return 0, 0, fmt.Errorf("%#02x is not a modifier key", t.Modifier)
		}
		if _, named := hidKeyNames[t.Key]; t.Key != 0 && t.Key != 0x64 && (!named || kbRecModifier(uint32(t.Key))) {
			return 0, 0, fmt.Errorf("%#02x is not a key this keyboard can send", t.Key)
		}
		switch {
		case t.Key == 0 && t.Modifier == 0:
			return 0, 0, fmt.Errorf("no key was named")
		case t.Key == 0:
			value = uint32(t.Modifier)
		case t.Modifier == 0:
			value = uint32(t.Key)
		default:
			return uint32(t.Key)<<8 | uint32(t.Modifier), kbRecKindKey, nil
		}
		if value == uint32(code) { // the key as itself is stored as its default
			return value, kbRecKindNone, nil
		}
		return value, kbRecKindKey, nil
	case RecordTargetMedia:
		if _, ok := RecordMediaKeys[t.Media]; !ok {
			return 0, 0, fmt.Errorf("%#x is not a media key this keyboard can send", t.Media)
		}
		return uint32(t.Media), kbRecKindMedia, nil
	case RecordTargetMouse:
		if t.Mouse < RecordMouseLeft || t.Mouse > RecordMouseScrollRight {
			return 0, 0, fmt.Errorf("%d is not a mouse action", t.Mouse)
		}
		return uint32(t.Mouse), kbRecKindMouse, nil
	case RecordTargetMacro:
		if t.Macro < 0 || t.Macro >= layout.macroSlots {
			return 0, 0, fmt.Errorf("macro slot %d is outside 1-%d", t.Macro+1, layout.macroSlots)
		}
		return uint32(t.Macro * layout.macroSlotSize), kbRecKindMacro, nil
	}
	return 0, 0, fmt.Errorf("an assignment this program does not understand cannot be given to a key")
}

// Lighting themes.
const (
	RecordThemeOff          = 0
	RecordThemeKeyResonance = 1
	RecordThemeStarlight    = 2
	RecordThemeSingle       = 3
	RecordThemeLoop         = 4
	RecordThemeColorRipple  = 5
	RecordThemeBreathing    = 6
	RecordThemeRipple       = 7
	// RecordThemes is how many themes there are, counting off.
	RecordThemes = 8
)

// RecordTheme is one lighting theme's settings. A theme has only some of
// them; the others read as zero and cannot be set.
type RecordTheme struct {
	// Brightness is 0 to 255.
	Brightness int
	// Speed is 1 to 10, as the vendor application numbers it.
	Speed int
	// Count is how many lights twinkle at once, 5 to 100 (starlight).
	Count int
	// Direction is the way a colour ripple travels: 0 across, 1 across
	// reversed, 2 down, 3 down reversed, 4 outwards, 5 inwards.
	Direction int
	// Color and Background are 0xRRGGBB. Color is the colour of a single,
	// breathing, key resonance or starlight theme; Background is what the
	// keys not lit by a ripple, key resonance or starlight theme show.
	Color, Background uint32
}

// kbRecThemeSection is where a theme's settings are in the record, and
// where each setting is in the section (-1: the theme has none). Brightness
// is always the first byte.
type kbRecThemeSection struct {
	offset, size                                    int
	speed, count, direction, flag, colour, backdrop int
}

var kbRecThemeSections = [RecordThemes]kbRecThemeSection{
	RecordThemeSingle:       {0x5d0, 5, -1, -1, -1, 1, 2, -1},
	RecordThemeLoop:         {0x5d5, 2, 1, -1, -1, -1, -1, -1},
	RecordThemeColorRipple:  {0x5d7, 3, 1, -1, 2, -1, -1, -1},
	RecordThemeBreathing:    {0x5da, 6, 1, -1, -1, 2, 3, -1},
	RecordThemeRipple:       {0x5e0, 6, 1, -1, -1, 2, -1, 3},
	RecordThemeKeyResonance: {0x5e6, 9, 1, -1, -1, 2, 6, 3},
	RecordThemeStarlight:    {0x5ef, 10, 1, 2, -1, 3, 7, 4},
}

func rgbAt(raw []byte) uint32 { return uint32(raw[0])<<16 | uint32(raw[1])<<8 | uint32(raw[2]) }

func putRGB(raw []byte, colour uint32) {
	raw[0], raw[1], raw[2] = byte(colour>>16), byte(colour>>8), byte(colour)
}

func (s kbRecThemeSection) decode(record []byte) RecordTheme {
	if s.size == 0 {
		return RecordTheme{}
	}
	raw := record[s.offset : s.offset+s.size]
	theme := RecordTheme{Brightness: int(raw[0])}
	if s.speed >= 0 {
		theme.Speed = 11 - int(raw[s.speed]) // stored counting down from 10
	}
	if s.count >= 0 {
		theme.Count = int(raw[s.count])
	}
	if s.direction >= 0 {
		theme.Direction = int(raw[s.direction])
	}
	if s.colour >= 0 {
		theme.Color = rgbAt(raw[s.colour:])
	}
	if s.backdrop >= 0 {
		theme.Background = rgbAt(raw[s.backdrop:])
	}
	return theme
}

// kbRecLightSettings sets the settings of a theme, or of the per-key
// colour block, that differ from was into raw. A setting that did not
// change keeps its stored byte, whatever it is.
func kbRecLightSettings(raw []byte, edited, was RecordTheme, speed, count, direction int) error {
	if edited.Brightness != was.Brightness {
		if edited.Brightness < 0 || edited.Brightness > 255 {
			return fmt.Errorf("brightness %d is outside 0-255", edited.Brightness)
		}
		raw[0] = byte(edited.Brightness)
	}
	for _, setting := range []struct {
		name          string
		value, was    int
		at, low, high int
		stored        func(int) byte
	}{
		{"speed", edited.Speed, was.Speed, speed, 1, 10, func(v int) byte { return byte(11 - v) }},
		{"light count", edited.Count, was.Count, count, 5, 100, func(v int) byte { return byte(v) }},
		{"direction", edited.Direction, was.Direction, direction, 0, 5, func(v int) byte { return byte(v) }},
	} {
		if setting.value == setting.was {
			continue
		}
		if setting.at < 0 {
			return fmt.Errorf("it has no %s to set", setting.name)
		}
		if setting.value < setting.low || setting.value > setting.high {
			return fmt.Errorf("%s %d is outside %d-%d", setting.name, setting.value, setting.low, setting.high)
		}
		raw[setting.at] = setting.stored(setting.value)
	}
	return nil
}

func (s kbRecThemeSection) encode(record []byte, edited, was RecordTheme) ([]byte, error) {
	if s.size == 0 {
		return nil, fmt.Errorf("it has no settings")
	}
	raw := append([]byte(nil), record[s.offset:s.offset+s.size]...)
	if err := kbRecLightSettings(raw, edited, was, s.speed, s.count, s.direction); err != nil {
		return nil, err
	}
	for _, colour := range []struct {
		name       string
		value, was uint32
		at         int
	}{{"colour", edited.Color, was.Color, s.colour}, {"background colour", edited.Background, was.Background, s.backdrop}} {
		if colour.value == colour.was {
			continue
		}
		if colour.at < 0 {
			return nil, fmt.Errorf("it has no %s to set", colour.name)
		}
		if colour.value > 0xffffff {
			return nil, fmt.Errorf("%s %#x is not 0xRRGGBB", colour.name, colour.value)
		}
		putRGB(raw[colour.at:], colour.value)
	}
	if s.flag >= 0 {
		raw[s.flag] = 1 // the vendor application always stores 1 here
	}
	return raw, nil
}

// Effects the per-key colours can be shown with.
const (
	RecordPerKeyOff       = 0
	RecordPerKeySteady    = 1
	RecordPerKeyBreathing = 2
	RecordPerKeyStarlight = 3
)

// RecordPerKeyLights is the per-key colour block: a colour for every light
// and how they are shown.
type RecordPerKeyLights struct {
	// Effect is one of the RecordPerKey* values.
	Effect int
	// Brightness (0-255), Speed (1-10) and Count (5-100) are as a theme's.
	Brightness, Speed, Count int
	// Colors is each light's colour, 0xRRGGBB, in the keyboard's own light
	// order. Which key each light is under is not known: the wide key at
	// positions 3 to 7 has five lights, and the vendor application numbers
	// its keys in an order this program has no record of.
	Colors []uint32
}

func decodeKbRecordLights(block []byte, layout kbRecordLayout) RecordPerKeyLights {
	lights := RecordPerKeyLights{
		Brightness: int(block[0]), Effect: int(block[2]), Speed: 11 - int(block[3]), Count: int(block[4]),
		Colors: make([]uint32, layout.lights),
	}
	for i := range lights.Colors {
		lights.Colors[i] = rgbAt(block[kbRecLightsHead+3*i:])
	}
	return lights
}

// encodeKbRecordLights gives the colour block holding edited, starting from
// the stored block so the bytes this program does not model are kept.
func encodeKbRecordLights(stored []byte, layout kbRecordLayout, edited, was RecordPerKeyLights) ([]byte, error) {
	block := append([]byte(nil), stored...)
	if edited.Effect != was.Effect {
		if edited.Effect < RecordPerKeyOff || edited.Effect > RecordPerKeyStarlight {
			return nil, fmt.Errorf("effect %d is not one of off, steady, breathing or starlight", edited.Effect)
		}
		block[2] = byte(edited.Effect)
	}
	settings := func(l RecordPerKeyLights) RecordTheme {
		return RecordTheme{Brightness: l.Brightness, Speed: l.Speed, Count: l.Count}
	}
	if err := kbRecLightSettings(block, settings(edited), settings(was), 3, 4, -1); err != nil {
		return nil, err
	}
	if len(edited.Colors) != layout.lights {
		return nil, fmt.Errorf("%d colours were given for %d lights", len(edited.Colors), layout.lights)
	}
	for i, colour := range edited.Colors {
		if colour > 0xffffff {
			return nil, fmt.Errorf("colour %#x is not 0xRRGGBB", colour)
		}
		putRGB(block[kbRecLightsHead+3*i:], colour)
	}
	// The vendor application always stores 1 in the second and last bytes.
	block[1], block[len(block)-1] = 1, 1
	return block, nil
}

// RecordLighting is a keyboard's lighting.
type RecordLighting struct {
	// Theme is the theme in use, one of the RecordTheme* values.
	Theme int
	// Themes holds every theme's settings, indexed by the RecordTheme*
	// values. The keyboard keeps them all, whichever is in use.
	Themes [RecordThemes]RecordTheme
	// PerKeyOn shows the per-key colours instead of the theme.
	PerKeyOn bool
	PerKey   RecordPerKeyLights
	// WindowsDynamic hands the lights to Windows Dynamic Lighting.
	WindowsDynamic bool
}

func (l kbRecordLayout) themeFromWire(stored byte) int {
	if l.fewThemes && (stored == 4 || stored == 5) {
		return int(stored) + 2 // numbered without loop and colour ripple
	}
	return int(stored)
}

func (l kbRecordLayout) themeToWire(theme int) (byte, error) {
	if theme < RecordThemeOff || theme >= RecordThemes {
		return 0, fmt.Errorf("%d is not a lighting theme", theme)
	}
	if !l.fewThemes {
		return byte(theme), nil
	}
	switch theme {
	case RecordThemeLoop, RecordThemeColorRipple:
		return 0, fmt.Errorf("this keyboard has no loop or colour ripple theme")
	case RecordThemeBreathing, RecordThemeRipple:
		return byte(theme - 2), nil
	}
	return byte(theme), nil
}

// RecordKeyboardProfile is everything a record keyboard's profile holds.
type RecordKeyboardProfile struct {
	// InUse is whether the keyboard holds a profile. Without one it
	// behaves as its defaults, which is what the rest of this then shows;
	// applying a change creates the profile.
	InUse bool
	// ProfileOn is whether the profile is switched on with the keyboard's
	// Profile button. It is changed on the keyboard, not from here.
	ProfileOn bool
	// Name is the profile's name, at most 16 characters.
	Name string
	// Targets is what each entry of the key table is assigned to. The
	// entries that are keys are listed by Keys.
	Targets [RecordKeySlots]RecordKeyTarget
	Locks   KeyboardLocks
	// Volume is the keyboard's volume level, 1 (quietest) to 5.
	Volume int
	// SleepMinutes is how long the keyboard waits before switching itself
	// off and LightsOffMinutes before switching its lights off: 5 to 30 in
	// steps of 5, the lights no later than the keyboard. Only where
	// HasTimers.
	SleepMinutes, LightsOffMinutes int
	Lighting                       RecordLighting
	// Macros holds the macro in each slot some key plays, up to MacroSlots
	// of them. A slot no key plays is free and reads as empty.
	Macros [RecordMacroSlotsMax]RecordMacro

	// HasTimers says this model has the sleep and lights-off timers and a
	// play mode for each macro.
	HasTimers bool
	// MacroSlots and MacroStepLimit are how many macros this model holds
	// and how long each may be. LightCount is how many lights the per-key
	// colours cover.
	MacroSlots, MacroStepLimit, LightCount int

	// record and lights are what this was decoded from. Writes start from
	// them so everything this program does not model is preserved.
	record, lights []byte
	layout         kbRecordLayout
}

// Keys lists the entries of the key table that are keys.
func (p RecordKeyboardProfile) Keys() []RecordKey { return p.layout.keys() }

// defaultKbRecord is the record the vendor application writes for a new
// profile, keeping the bytes of current that a new profile leaves alone:
// the profile switch, the byte after it, the timers, and on a model with
// timers the volume.
func defaultKbRecord(layout kbRecordLayout, current []byte) []byte {
	record := make([]byte, protocol.KbRecordSize)
	copy(record[kbRecOffProfileOn:kbRecOffKeys], current[kbRecOffProfileOn:kbRecOffKeys])
	for _, key := range layout.keys() {
		code := key.Usage
		if key.Port {
			code = kbRecValueOff
		}
		entry := record[kbRecOffKeys+key.Index*kbRecKeyEntry:]
		entry[0] = code
		binary.LittleEndian.PutUint32(entry[4:], uint32(code))
	}
	record[kbRecOffVolume] = 2
	if layout.timers {
		record[kbRecOffVolume] = current[kbRecOffVolume]
	}
	record[kbRecOffTheme] = RecordThemeKeyResonance
	binary.LittleEndian.PutUint32(record[kbRecOffLightIdle:], 300)
	// Every theme starts at full brightness, speed 8 (stored 3) and orange.
	const orange = 0xffa500
	for theme, section := range kbRecThemeSections {
		if section.size == 0 {
			continue
		}
		raw := record[section.offset : section.offset+section.size]
		raw[0] = 255
		if section.speed >= 0 {
			raw[section.speed] = 3
		}
		if section.count >= 0 {
			raw[section.count] = 5
		}
		if section.flag >= 0 {
			raw[section.flag] = 1
		}
		switch {
		case theme == RecordThemeRipple:
			putRGB(raw[section.backdrop:], orange)
		case section.colour >= 0:
			putRGB(raw[section.colour:], orange)
		}
	}
	return record
}

// base is the record a write starts from: the stored one, or the defaults
// when the keyboard holds no profile.
func (p RecordKeyboardProfile) base() []byte {
	if p.InUse {
		return append([]byte(nil), p.record...)
	}
	return defaultKbRecord(p.layout, p.record)
}

func decodeKbRecordProfile(record, lights []byte, layout kbRecordLayout) (RecordKeyboardProfile, error) {
	if len(record) != protocol.KbRecordSize {
		return RecordKeyboardProfile{}, fmt.Errorf("configuration record is %d bytes, expected %d", len(record), protocol.KbRecordSize)
	}
	if len(lights) != layout.lightsSize() {
		return RecordKeyboardProfile{}, fmt.Errorf("colour block is %d bytes, expected %d", len(lights), layout.lightsSize())
	}
	profile := RecordKeyboardProfile{
		InUse:     binary.LittleEndian.Uint32(record[kbRecOffFlag:]) == kbRecInUse,
		ProfileOn: record[kbRecOffProfileOn] == 1,
		HasTimers: layout.timers, MacroSlots: layout.macroSlots, MacroStepLimit: layout.macroLimit, LightCount: layout.lights,
		record: append([]byte(nil), record...), lights: append([]byte(nil), lights...), layout: layout,
	}
	shown := profile.base()
	profile.Name = decodePadName(shown[kbRecOffName : kbRecOffName+padNameLen])
	for i := range profile.Targets {
		profile.Targets[i] = decodeRecordKeyTarget(layout, shown[kbRecOffKeys+i*kbRecKeyEntry:])
	}
	profile.Locks = keyboardLocksFromFlags(shown[kbRecOffLocks])
	profile.Volume = int(shown[kbRecOffVolume])
	if layout.timers {
		profile.SleepMinutes, profile.LightsOffMinutes = int(shown[kbRecOffSleep]), int(shown[kbRecOffLightsOff])
	}
	profile.Lighting = RecordLighting{
		Theme:          layout.themeFromWire(shown[kbRecOffTheme]),
		PerKeyOn:       shown[kbRecOffPerKey] == 1,
		PerKey:         decodeKbRecordLights(lights, layout),
		WindowsDynamic: shown[kbRecOffDynamic] == 1,
	}
	for theme, section := range kbRecThemeSections {
		profile.Lighting.Themes[theme] = section.decode(shown)
	}
	return profile, nil
}

// kbSpan is a span of the record that changed.
type kbSpan struct{ offset, length int }

func timerOK(minutes int) bool { return minutes >= 5 && minutes <= 30 && minutes%5 == 0 }

// encodeKbRecord writes what edited changes from was into record, which is
// was's base, and returns the spans it changed. Each span is one the vendor
// application also writes on its own: a key's entry, the name, a theme's
// settings, or a single byte. A setting equal to what was read is left
// alone, whatever it holds, so applying an unedited profile writes nothing.
func encodeKbRecord(record []byte, layout kbRecordLayout, edited, was RecordKeyboardProfile) ([]kbSpan, error) {
	var changed []kbSpan
	put := func(offset int, section ...byte) {
		if !bytes.Equal(record[offset:offset+len(section)], section) {
			copy(record[offset:], section)
			changed = append(changed, kbSpan{offset, len(section)})
		}
	}
	flagByte := func(on bool) byte {
		if on {
			return 1
		}
		return 0
	}

	if edited.Name != was.Name {
		name, err := encodePadName(edited.Name)
		if err != nil {
			return nil, err
		}
		if edited.Name == "" {
			return nil, fmt.Errorf("a profile needs a name")
		}
		put(kbRecOffName, name...)
	}
	keys := map[int]RecordKey{}
	for _, key := range layout.keys() {
		keys[key.Index] = key
	}
	for i, target := range edited.Targets {
		if target == was.Targets[i] {
			continue
		}
		key, ok := keys[i]
		if !ok {
			return nil, fmt.Errorf("there is no key at table entry %d", i)
		}
		entry := append([]byte(nil), record[kbRecOffKeys+i*kbRecKeyEntry:kbRecOffKeys+(i+1)*kbRecKeyEntry]...)
		// The key's own code is kept as stored: on some national layouts
		// it is not the one the table's order suggests.
		if entry[0] == 0 {
			entry[0] = key.Usage
			if key.Port {
				entry[0] = kbRecValueOff
			}
		}
		value, kind, err := target.wire(layout, key, entry[0])
		if err != nil {
			return nil, fmt.Errorf("%s: %v", key.Name, err)
		}
		binary.LittleEndian.PutUint32(entry[4:], value)
		binary.LittleEndian.PutUint32(entry[8:], kind)
		put(kbRecOffKeys+i*kbRecKeyEntry, entry...)
	}
	if edited.Locks != was.Locks {
		const lockBits = protocol.JP108LockWinKey | protocol.JP108LockAltTab | protocol.JP108LockAltF4
		put(kbRecOffLocks, record[kbRecOffLocks]&^lockBits|edited.Locks.flags())
	}
	if edited.Volume != was.Volume {
		if edited.Volume < 1 || edited.Volume > 5 {
			return nil, fmt.Errorf("volume level %d is outside 1-5", edited.Volume)
		}
		put(kbRecOffVolume, byte(edited.Volume))
	}
	if edited.SleepMinutes != was.SleepMinutes || edited.LightsOffMinutes != was.LightsOffMinutes {
		if !layout.timers {
			return nil, fmt.Errorf("this keyboard has no sleep timers to set")
		}
		if !timerOK(edited.SleepMinutes) || !timerOK(edited.LightsOffMinutes) || edited.LightsOffMinutes > edited.SleepMinutes {
			return nil, fmt.Errorf("timers of %d and %d minutes are not 5 to 30 in steps of 5 with the lights going off no later than the keyboard",
				edited.SleepMinutes, edited.LightsOffMinutes)
		}
		put(kbRecOffSleep, byte(edited.SleepMinutes))
		put(kbRecOffLightsOff, byte(edited.LightsOffMinutes))
	}

	light, wasLight := edited.Lighting, was.Lighting
	if light.PerKeyOn != wasLight.PerKeyOn {
		put(kbRecOffPerKey, flagByte(light.PerKeyOn))
	}
	if light.Theme != wasLight.Theme {
		stored, err := layout.themeToWire(light.Theme)
		if err != nil {
			return nil, err
		}
		put(kbRecOffTheme, stored)
	}
	for theme, section := range kbRecThemeSections {
		if light.Themes[theme] == wasLight.Themes[theme] {
			continue
		}
		if _, err := layout.themeToWire(theme); err != nil {
			return nil, err
		}
		raw, err := section.encode(record, light.Themes[theme], wasLight.Themes[theme])
		if err != nil {
			return nil, fmt.Errorf("lighting theme %d: %v", theme, err)
		}
		put(section.offset, raw...)
	}
	if light.WindowsDynamic != wasLight.WindowsDynamic {
		put(kbRecOffDynamic, flagByte(light.WindowsDynamic))
	}
	return changed, nil
}

func supportsRecordKeyboard(vidPid protocol.VidPid) bool {
	_, known := kbRecordLayoutFor(vidPid)
	return known && protocol.DeviceProfileFor(vidPid).Capability.SupportsRecordKeyboard
}

// kbRecordSession opens a session and turns the keyboard's live key reports
// off, as the vendor application does before it reads or writes. Nothing
// here has been tried on a real keyboard, so one is only read in advanced
// mode and only written once the caller has been through the write-unlock
// ceremony.
func (c *OpenBitdoCore) kbRecordSession(ctx context.Context, vidPid protocol.VidPid, unlocked bool) (*protocol.DeviceSession, kbRecordLayout, error) {
	layout, _ := kbRecordLayoutFor(vidPid)
	if !supportsRecordKeyboard(vidPid) {
		return nil, layout, errPolicyDenied(ReasonUnsupportedPid, "keyboard profiles of this kind are not supported for %s", vidPid)
	}
	if c.transportOverride == nil {
		if c.config.MockMode {
			return nil, layout, errPolicyDenied(ReasonFeatureUnavailable, "there is no simulated keyboard of this kind")
		}
		if !c.AdvancedMode() {
			return nil, layout, errPolicyDenied(ReasonExperimentalRequired,
				"reading this keyboard's profile is not hardware-confirmed yet; turn on advanced mode to try it")
		}
	}
	config := protocol.SessionConfig{
		Experimental: true, CandidateWriteUnlock: unlocked,
		RetryPolicy: protocol.DefaultRetryPolicy(), TimeoutProfile: protocol.DefaultTimeoutProfile(), TraceEnabled: true,
	}
	session, perr := protocol.NewDeviceSession(ctx, c.transportFor(vidPid), vidPid, config)
	if perr != nil {
		return nil, layout, errProtocol(perr)
	}
	if perr := session.KbRecordSetKeyReports(ctx, false); perr != nil {
		_ = session.Close()
		return nil, layout, errProtocol(perr)
	}
	return session, layout, nil
}

// RecordKeyboardReadProfile reads a record keyboard's whole profile.
func (c *OpenBitdoCore) RecordKeyboardReadProfile(ctx context.Context, vidPid protocol.VidPid) (RecordKeyboardProfile, error) {
	session, layout, err := c.kbRecordSession(ctx, vidPid, false)
	if err != nil {
		return RecordKeyboardProfile{}, err
	}
	defer func() { _ = session.Close() }()
	return readKbRecordProfile(ctx, session, layout)
}

func readKbRecordProfile(ctx context.Context, session *protocol.DeviceSession, layout kbRecordLayout) (RecordKeyboardProfile, error) {
	record, err := session.KbRecordRead(ctx, 0, protocol.KbRecordSize)
	if err != nil {
		return RecordKeyboardProfile{}, errProtocol(err)
	}
	lights, err := session.KbRecordReadLights(ctx, layout.lightsSize())
	if err != nil {
		return RecordKeyboardProfile{}, errProtocol(err)
	}
	profile, err := decodeKbRecordProfile(record, lights, layout)
	if err != nil {
		return RecordKeyboardProfile{}, errProtocol(err)
	}
	for _, target := range profile.Targets {
		if target.Kind != RecordTargetMacro || len(profile.Macros[target.Macro].Steps) > 0 {
			continue
		}
		if profile.Macros[target.Macro], err = readKbRecordMacro(ctx, session, layout, target.Macro); err != nil {
			return RecordKeyboardProfile{}, errProtocol(err)
		}
	}
	return profile, nil
}

// RecordKeyboardApply writes an edited profile to the keyboard: it reads
// the profile first and keeps it as a backup, writes only what changed,
// and reads each part back. If a readback does not match, or a step fails
// after something was written, what was there before is written back.
//
// These keyboards are read-only until the write-unlock ceremony has been
// gone through; policy says whether it has.
func (c *OpenBitdoCore) RecordKeyboardApply(ctx context.Context, vidPid protocol.VidPid, edited RecordKeyboardProfile, policy RuntimeUnlockPolicy) (WriteRecoveryReport, error) {
	if c.transportOverride == nil && (!c.AdvancedMode() || !policy.AdvancedMode || !policy.AcknowledgedRisk || !policy.UnlockFilePresent) {
		return WriteRecoveryReport{}, errPolicyDenied(ReasonNotHardwareConfirmed,
			"writing this keyboard's profile is not hardware-confirmed; it needs advanced mode, the write-risk acknowledgement and the keyboard's unlock file")
	}
	session, layout, err := c.kbRecordSession(ctx, vidPid, true)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	defer func() { _ = session.Close() }()

	before, err := readKbRecordProfile(ctx, session, layout)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	record := before.base()
	spans, encodeErr := encodeKbRecord(record, layout, edited, before)
	if encodeErr != nil {
		return WriteRecoveryReport{}, errInvalidState("%v", encodeErr)
	}
	lights := before.lights
	if !reflect.DeepEqual(edited.Lighting.PerKey, before.Lighting.PerKey) {
		if lights, encodeErr = encodeKbRecordLights(before.lights, layout, edited.Lighting.PerKey, before.Lighting.PerKey); encodeErr != nil {
			return WriteRecoveryReport{}, errInvalidState("per-key lights: %v", encodeErr)
		}
	}
	lightsChanged := !bytes.Equal(lights, before.lights)
	macros, encodeErr := planKbRecordMacros(layout, edited, before)
	if encodeErr != nil {
		return WriteRecoveryReport{}, errInvalidState("%v", encodeErr)
	}

	unchanged := len(spans) == 0 && !lightsChanged && len(macros) == 0
	if !before.InUse && !unchanged {
		// A keyboard without a profile is given a whole one, as the
		// vendor application does, rather than single fields.
		if layout.iso {
			return WriteRecoveryReport{}, errInvalidState("this keyboard holds no profile yet, and one cannot be created here: its default key assignments depend on its national layout")
		}
		if edited.Name == "" {
			return WriteRecoveryReport{}, errInvalidState("a new profile needs a name")
		}
		binary.LittleEndian.PutUint32(record[kbRecOffFlag:], kbRecInUse)
		spans = []kbSpan{{0, len(record)}}
	}
	backupID := c.storeBackup(vidPid, configBackupPayload{kind: backupRecordKeyboard, recordKeyboard: before})
	report := WriteRecoveryReport{BackupID: backupID, HasBackupID: true}
	if unchanged {
		report.WriteApplied = true
		return report, nil
	}

	// The record goes first, then the colours, then each macro: the order
	// the vendor application applies a profile in.
	applyErr := writeKbRecordSpans(ctx, session, record, spans)
	if applyErr == nil && lightsChanged {
		applyErr = writeKbRecordLights(ctx, session, lights)
	}
	for i := 0; i < len(macros) && applyErr == nil; i++ {
		applyErr = writeKbRecordMacro(ctx, session, layout, macros[i].slot, macros[i].header, macros[i].steps)
	}
	if applyErr == nil {
		report.WriteApplied = true
		return report, nil
	}
	report.RollbackAttempted, report.WriteError = true, applyErr.Error()
	rollbackErr := writeKbRecordSpans(ctx, session, before.record, spans)
	if rollbackErr == nil && lightsChanged {
		rollbackErr = writeKbRecordLights(ctx, session, before.lights)
	}
	// A slot no key played before is left as it is: nothing plays it once
	// the key table is back.
	for i := 0; i < len(macros) && rollbackErr == nil; i++ {
		if was := macros[i].was; was != nil {
			rollbackErr = writeKbRecordMacro(ctx, session, layout, macros[i].slot, was.header, was.steps)
		}
	}
	if rollbackErr != nil {
		report.RollbackError = rollbackErr.Error()
		return report, nil
	}
	report.RollbackSucceeded = true
	return report, nil
}

// writeKbRecordSpans writes the given spans of record and checks the
// keyboard now holds them.
func writeKbRecordSpans(ctx context.Context, session *protocol.DeviceSession, record []byte, spans []kbSpan) error {
	for _, span := range spans {
		if err := session.KbRecordWriteRange(ctx, record, span.offset, span.length); err != nil {
			return fmt.Errorf("write at %#x: %w", span.offset, err)
		}
	}
	for _, span := range spans {
		got, err := session.KbRecordRead(ctx, span.offset, span.length)
		if err != nil {
			return fmt.Errorf("readback failed: %w", err)
		}
		if !bytes.Equal(got, record[span.offset:span.offset+span.length]) {
			return fmt.Errorf("readback mismatch at %#x: the keyboard did not keep what was written", span.offset)
		}
	}
	return nil
}

// writeKbRecordLights writes the per-key colour block and checks the
// keyboard now holds it.
func writeKbRecordLights(ctx context.Context, session *protocol.DeviceSession, block []byte) error {
	if err := session.KbRecordWriteLights(ctx, block); err != nil {
		return fmt.Errorf("per-key lights: %w", err)
	}
	got, err := session.KbRecordReadLights(ctx, len(block))
	if err != nil {
		return fmt.Errorf("readback of the per-key lights failed: %w", err)
	}
	if !bytes.Equal(got, block) {
		return fmt.Errorf("readback mismatch for the per-key lights: the keyboard did not keep what was written")
	}
	return nil
}

// restoreKbRecordBackup writes a backed-up profile back wherever the
// keyboard's now differs from it.
func (c *OpenBitdoCore) restoreKbRecordBackup(ctx context.Context, vidPid protocol.VidPid, backup RecordKeyboardProfile) error {
	session, layout, err := c.kbRecordSession(ctx, vidPid, true)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	current, err := readKbRecordProfile(ctx, session, layout)
	if err != nil {
		return err
	}
	// Runs of differing bytes, leaving the profile switch to the keyboard.
	var spans []kbSpan
	differs := func(i int) bool { return i != kbRecOffProfileOn && current.record[i] != backup.record[i] }
	for i := 0; i < len(backup.record); i++ {
		if !differs(i) {
			continue
		}
		start := i
		for i < len(backup.record) && differs(i) {
			i++
		}
		spans = append(spans, kbSpan{start, i - start})
	}
	if len(spans) > 0 {
		if err := writeKbRecordSpans(ctx, session, backup.record, spans); err != nil {
			return errProtocol(err)
		}
	}
	if !bytes.Equal(current.lights, backup.lights) {
		if err := writeKbRecordLights(ctx, session, backup.lights); err != nil {
			return errProtocol(err)
		}
	}
	macros, planErr := planKbRecordMacros(layout, backup, current)
	if planErr != nil {
		return errInvalidState("%v", planErr)
	}
	for _, macro := range macros {
		if err := writeKbRecordMacro(ctx, session, layout, macro.slot, macro.header, macro.steps); err != nil {
			return errProtocol(err)
		}
	}
	return nil
}

// What a macro step does.
const (
	RecordStepPress   byte = 1
	RecordStepRelease byte = 2
	RecordStepWait    byte = 3
)

// RecordMacroStep is one step of a macro: a key going down or up, or a
// wait.
type RecordMacroStep struct {
	// Kind is RecordStepPress, RecordStepRelease or RecordStepWait.
	Kind byte
	// Key is the HID usage of the key pressed or released.
	Key byte
	// Millis is how long a wait lasts, up to 60000.
	Millis int
}

// How a macro is played, on the keyboards that have the choice (see
// RecordKeyboardProfile.HasTimers).
const (
	RecordMacroToggle = 0 // press to start, press again to stop
	RecordMacroHold   = 1 // runs while the key is held
	RecordMacroOnce   = 2 // runs through once
	RecordMacroReplay = 3 // press to start over
)

// RecordMacro is a recorded sequence of key presses that a key plays. The
// key that plays it is the one whose target names its slot.
type RecordMacro struct {
	// Name is at most 16 characters.
	Name  string
	Steps []RecordMacroStep
	// Repeat is how many times it plays, 1 to 99; 0 repeats until stopped.
	Repeat int
	// IntervalMs is the pause between repeats, up to 60000.
	IntervalMs int
	// Mode is one of the RecordMacro* play modes.
	Mode int
}

func (m RecordMacro) validate(layout kbRecordLayout) error {
	if len(m.Steps) == 0 || len(m.Steps) > layout.macroLimit {
		return fmt.Errorf("it has %d steps; a macro holds 1 to %d", len(m.Steps), layout.macroLimit)
	}
	for i, step := range m.Steps {
		if step.Millis < 0 || step.Millis > 60000 {
			return fmt.Errorf("step %d lasts %d ms, outside 0-60000", i+1, step.Millis)
		}
		switch step.Kind {
		case RecordStepPress, RecordStepRelease:
			if _, named := hidKeyNames[step.Key]; !named && step.Key != 0x64 {
				return fmt.Errorf("step %d names key %#02x, which is not a key this keyboard can send", i+1, step.Key)
			}
		case RecordStepWait:
		default:
			return fmt.Errorf("step %d is neither a press, a release nor a wait", i+1)
		}
	}
	if m.Repeat < 0 || m.Repeat > 99 || m.IntervalMs < 0 || m.IntervalMs > 60000 {
		return fmt.Errorf("repeating %d times every %d ms is outside 0-99 times and 0-60000 ms", m.Repeat, m.IntervalMs)
	}
	if m.Mode < RecordMacroToggle || m.Mode > RecordMacroReplay || (m.Mode != RecordMacroToggle && !layout.timers) {
		return fmt.Errorf("play mode %d is not one this keyboard has", m.Mode)
	}
	_, err := encodePadName(m.Name)
	return err
}

// kbMacroImage is a macro as stored: the header at the start of its slot
// and the steps that follow 64 bytes in.
type kbMacroImage struct{ header, steps []byte }

// encodeKbRecordMacro gives the stored form of a macro played by the key at
// table entry trigger.
func encodeKbRecordMacro(trigger int, m RecordMacro) *kbMacroImage {
	header := make([]byte, kbRecMacroHeader)
	binary.LittleEndian.PutUint32(header, kbRecInUse)
	header[4] = byte(trigger)
	name, _ := encodePadName(m.Name)
	copy(header[5:], name)
	binary.LittleEndian.PutUint16(header[38:], uint16(len(m.Steps)))
	binary.LittleEndian.PutUint16(header[40:], uint16(m.IntervalMs))
	header[42], header[43] = byte(m.Repeat), byte(m.Mode)
	steps := make([]byte, 4*len(m.Steps))
	for i, step := range m.Steps {
		steps[4*i], steps[4*i+1] = step.Kind, step.Key
		binary.LittleEndian.PutUint16(steps[4*i+2:], uint16(step.Millis))
	}
	return &kbMacroImage{header, steps}
}

// kbRecordMacroStepCount is how many steps a stored header says its macro
// has; zero when the slot holds no macro.
func kbRecordMacroStepCount(layout kbRecordLayout, header []byte) int {
	count := int(binary.LittleEndian.Uint16(header[38:]))
	if binary.LittleEndian.Uint32(header) != kbRecInUse || count > layout.macroCapacity {
		return 0
	}
	return count
}

func decodeKbRecordMacro(header, steps []byte) RecordMacro {
	macro := RecordMacro{
		Name:       decodePadName(header[5 : 5+padNameLen]),
		IntervalMs: int(binary.LittleEndian.Uint16(header[40:])),
		Repeat:     int(header[42]), Mode: int(header[43]),
		Steps: make([]RecordMacroStep, len(steps)/4),
	}
	for i := range macro.Steps {
		macro.Steps[i] = RecordMacroStep{Kind: steps[4*i], Key: steps[4*i+1], Millis: int(binary.LittleEndian.Uint16(steps[4*i+2:]))}
	}
	return macro
}

// readKbRecordMacro reads the macro in a slot; a slot that holds none reads
// as an empty macro.
func readKbRecordMacro(ctx context.Context, session *protocol.DeviceSession, layout kbRecordLayout, slot int) (RecordMacro, error) {
	at := slot * layout.macroSlotSize
	header, err := session.KbRecordReadMacroData(ctx, at, kbRecMacroHeader)
	if err != nil {
		return RecordMacro{}, err
	}
	count := kbRecordMacroStepCount(layout, header)
	if count == 0 {
		return RecordMacro{}, nil
	}
	steps, err := session.KbRecordReadMacroData(ctx, at+kbRecMacroSteps, 4*count)
	if err != nil {
		return RecordMacro{}, err
	}
	return decodeKbRecordMacro(header, steps), nil
}

// kbRecordMacroImages gives, by slot, the stored form of every macro a key
// of profile plays, and how many keys play each slot. A slot whose macro
// has no steps has no image.
func kbRecordMacroImages(profile RecordKeyboardProfile) (map[int]*kbMacroImage, map[int]int) {
	images, players := map[int]*kbMacroImage{}, map[int]int{}
	for i, target := range profile.Targets {
		if target.Kind != RecordTargetMacro {
			continue
		}
		players[target.Macro]++
		if _, seen := images[target.Macro]; !seen && len(profile.Macros[target.Macro].Steps) > 0 {
			images[target.Macro] = encodeKbRecordMacro(i, profile.Macros[target.Macro])
		}
	}
	return images, players
}

// kbMacroWrite is a macro to store, and what its slot held before when a
// key played that.
type kbMacroWrite struct {
	slot          int
	header, steps []byte
	was           *kbMacroImage
}

// planKbRecordMacros lists the macros to store so the keyboard's become
// edited's: those a key plays whose stored form differs from before's. A
// slot no key plays any more is left as it is, as the vendor application
// leaves it.
func planKbRecordMacros(layout kbRecordLayout, edited, before RecordKeyboardProfile) ([]kbMacroWrite, error) {
	want, players := kbRecordMacroImages(edited)
	have, played := kbRecordMacroImages(before)
	var writes []kbMacroWrite
	for slot := 0; slot < layout.macroSlots; slot++ {
		if players[slot] == 0 {
			continue
		}
		image, was := want[slot], have[slot]
		if image == nil && was == nil && played[slot] > 0 {
			continue // a key played an empty slot when read; untouched since
		}
		if image != nil && was != nil && bytes.Equal(image.header, was.header) && bytes.Equal(image.steps, was.steps) {
			continue
		}
		if players[slot] > 1 {
			return nil, fmt.Errorf("macro %d: only one key can play a macro", slot+1)
		}
		if err := edited.Macros[slot].validate(layout); err != nil {
			return nil, fmt.Errorf("macro %d: %v", slot+1, err)
		}
		writes = append(writes, kbMacroWrite{slot: slot, header: image.header, steps: image.steps, was: was})
	}
	return writes, nil
}

// writeKbRecordMacro stores a macro in a slot the way the vendor
// application does (the slot is prepared, then the header and the steps
// are written) and checks the keyboard now holds it.
func writeKbRecordMacro(ctx context.Context, session *protocol.DeviceSession, layout kbRecordLayout, slot int, header, steps []byte) error {
	at := slot * layout.macroSlotSize
	parts := []struct {
		offset int
		data   []byte
	}{{at, header}, {at + kbRecMacroSteps, steps}}
	if err := session.KbRecordPrepareMacro(ctx, at); err != nil {
		return fmt.Errorf("macro %d: %w", slot+1, err)
	}
	for _, part := range parts {
		if err := session.KbRecordWriteMacroData(ctx, part.offset, part.data); err != nil {
			return fmt.Errorf("macro %d: %w", slot+1, err)
		}
	}
	for _, part := range parts {
		got, err := session.KbRecordReadMacroData(ctx, part.offset, len(part.data))
		if err != nil {
			return fmt.Errorf("readback of macro %d failed: %w", slot+1, err)
		}
		if !bytes.Equal(got, part.data) {
			return fmt.Errorf("readback mismatch for macro %d: the keyboard did not keep what was written", slot+1)
		}
	}
	return nil
}
