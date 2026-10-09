package core

import (
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// KeyboardKey is one physical key that can be assigned.
type KeyboardKey struct {
	// ID is the keyboard's id for the key. For an ordinary key it is the
	// key's HID usage; the dedicated buttons have ids of their own.
	ID   byte
	Name string
	// Dedicated marks the buttons that do nothing until assigned (A, B and
	// the K1-K8 ports). Every other key defaults to itself.
	Dedicated bool
	// Types is the usage the key sends unassigned, when that is not its
	// id: a Retro Mechanical Keyboard numbers its modifier keys 100-106.
	Types byte
}

// Default is what the key does with no assignment.
func (k KeyboardKey) Default() KeyTarget {
	if k.Dedicated {
		return KeyTarget{}
	}
	if k.Types != 0 {
		return KeyTargetKeyOf(k.Types)
	}
	return KeyTargetKeyOf(k.ID) // a modifier is itself as a modifier
}

// hidKeyNames names HID keyboard usages (USB HID Usage Tables, Keyboard/
// Keypad page), for the keys a Retro 108 has and the keys it can be told to
// send.
var hidKeyNames = map[byte]string{
	0x04: "A", 0x05: "B", 0x06: "C", 0x07: "D", 0x08: "E", 0x09: "F", 0x0a: "G", 0x0b: "H", 0x0c: "I",
	0x0d: "J", 0x0e: "K", 0x0f: "L", 0x10: "M", 0x11: "N", 0x12: "O", 0x13: "P", 0x14: "Q", 0x15: "R",
	0x16: "S", 0x17: "T", 0x18: "U", 0x19: "V", 0x1a: "W", 0x1b: "X", 0x1c: "Y", 0x1d: "Z",
	0x1e: "1", 0x1f: "2", 0x20: "3", 0x21: "4", 0x22: "5", 0x23: "6", 0x24: "7", 0x25: "8", 0x26: "9", 0x27: "0",
	0x28: "Enter", 0x29: "Esc", 0x2a: "Backspace", 0x2b: "Tab", 0x2c: "Space",
	0x2d: "-", 0x2e: "=", 0x2f: "[", 0x30: "]", 0x31: "\\", 0x33: ";", 0x34: "'", 0x35: "`", 0x36: ",", 0x37: ".", 0x38: "/",
	0x39: "Caps Lock",
	0x3a: "F1", 0x3b: "F2", 0x3c: "F3", 0x3d: "F4", 0x3e: "F5", 0x3f: "F6", 0x40: "F7", 0x41: "F8",
	0x42: "F9", 0x43: "F10", 0x44: "F11", 0x45: "F12",
	0x46: "Print Screen", 0x47: "Scroll Lock", 0x48: "Pause", 0x49: "Insert", 0x4a: "Home", 0x4b: "Page Up",
	0x4c: "Delete", 0x4d: "End", 0x4e: "Page Down", 0x4f: "Right", 0x50: "Left", 0x51: "Down", 0x52: "Up",
	0x53: "Num Lock", 0x54: "Num /", 0x55: "Num *", 0x56: "Num -", 0x57: "Num +", 0x58: "Num Enter",
	0x59: "Num 1", 0x5a: "Num 2", 0x5b: "Num 3", 0x5c: "Num 4", 0x5d: "Num 5", 0x5e: "Num 6", 0x5f: "Num 7",
	0x60: "Num 8", 0x61: "Num 9", 0x62: "Num 0", 0x63: "Num .", 0x65: "Menu",
	0x68: "F13", 0x69: "F14", 0x6a: "F15", 0x6b: "F16", 0x6c: "F17", 0x6d: "F18", 0x6e: "F19", 0x6f: "F20",
	0x70: "F21", 0x71: "F22", 0x72: "F23", 0x73: "F24",
	0xe0: "Left Ctrl", 0xe1: "Left Shift", 0xe2: "Left Alt", 0xe3: "Left Win",
	0xe4: "Right Ctrl", 0xe5: "Right Shift", 0xe6: "Right Alt", 0xe7: "Right Win",
}

// retro108Remappable are the ordinary keys of a Retro 108 that can be
// reassigned, in keyboard order. Num Lock, Menu, Fn and the right Win key
// are not in the keyboard's remappable set.
var retro108Remappable = []byte{
	0x29, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
	0x35, 0x1e, 0x1f, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x2d, 0x2e, 0x2a,
	0x2b, 0x14, 0x1a, 0x08, 0x15, 0x17, 0x1c, 0x18, 0x0c, 0x12, 0x13, 0x2f, 0x30, 0x31,
	0x39, 0x04, 0x16, 0x07, 0x09, 0x0a, 0x0b, 0x0d, 0x0e, 0x0f, 0x33, 0x34, 0x28,
	0x1d, 0x1b, 0x06, 0x19, 0x05, 0x11, 0x10, 0x36, 0x37, 0x38, 0x2c,
	0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x52, 0x50, 0x51, 0x4f,
	0x54, 0x55, 0x56, 0x5f, 0x60, 0x61, 0x57, 0x5c, 0x5d, 0x5e, 0x59, 0x5a, 0x5b, 0x58, 0x62, 0x63,
	0xe0, 0xe1, 0xe2, 0xe3, 0xe4, 0xe5, 0xe6,
}

// Retro108Keys lists every assignable key of a Retro 108: the ten dedicated
// buttons first (they are what most people come to set), then the ordinary
// keys in keyboard order.
var Retro108Keys = buildRetro108Keys()

func buildRetro108Keys() []KeyboardKey {
	keys := []KeyboardKey{
		{ID: 233, Name: "A button", Dedicated: true}, {ID: 232, Name: "B button", Dedicated: true},
		{ID: 240, Name: "K1", Dedicated: true}, {ID: 241, Name: "K2", Dedicated: true},
		{ID: 238, Name: "K3", Dedicated: true}, {ID: 239, Name: "K4", Dedicated: true},
		{ID: 236, Name: "K5", Dedicated: true}, {ID: 237, Name: "K6", Dedicated: true},
		{ID: 234, Name: "K7", Dedicated: true}, {ID: 235, Name: "K8", Dedicated: true},
	}
	for _, usage := range retro108Remappable {
		keys = append(keys, KeyboardKey{ID: usage, Name: hidKeyNames[usage]})
	}
	return keys
}

// KeyboardKeyByID finds an assignable key by its id.
func KeyboardKeyByID(id byte) (KeyboardKey, bool) {
	for _, key := range Retro108Keys {
		if key.ID == id {
			return key, true
		}
	}
	return KeyboardKey{}, false
}

// mediaKeyNames names the media and system keys offered as targets (HID
// Consumer page usages).
var mediaKeyNames = map[uint16]string{
	0x00e9: "Volume Up", 0x00ea: "Volume Down", 0x00e2: "Mute",
	0x00cd: "Play/Pause", 0x00b5: "Next Track", 0x00b6: "Previous Track", 0x00b7: "Stop",
	0x006f: "Brightness Up", 0x0070: "Brightness Down",
	0x018a: "Mail", 0x0192: "Calculator", 0x0194: "My Computer", 0x0223: "Browser Home",
	0x0224: "Browser Back", 0x0225: "Browser Forward", 0x0227: "Browser Refresh", 0x0221: "Search",
}

// Mouse buttons a key can press, as the keyboard encodes them.
const (
	MouseLeft        byte = 0x01
	MouseRight       byte = 0x02
	MouseMiddle      byte = 0x04
	MouseBack        byte = 0x08
	MouseForward     byte = 0x10
	MouseDoubleClick byte = 0x80
)

var mouseButtonNames = []struct {
	bit  byte
	name string
}{
	{MouseLeft, "Left Click"}, {MouseRight, "Right Click"}, {MouseMiddle, "Middle Click"},
	{MouseBack, "Mouse Back"}, {MouseForward, "Mouse Forward"}, {MouseDoubleClick, "Double Click"},
}

// String names a target the way a person would: "Left Shift+1", "Volume Up",
// "Left Click", "(none)".
func (t KeyTarget) String() string {
	switch t.Kind {
	case TargetKey:
		var parts []string
		if t.Modifier != 0 {
			parts = append(parts, keyName(t.Modifier))
		}
		if t.Key != 0 {
			parts = append(parts, keyName(t.Key))
		}
		if len(parts) == 0 {
			return "(none)"
		}
		return strings.Join(parts, "+")
	case TargetMedia:
		if name, ok := mediaKeyNames[t.Media]; ok {
			return name
		}
		return fmt.Sprintf("Media key %#04x", t.Media)
	case TargetMouse:
		var parts []string
		for _, button := range mouseButtonNames {
			if t.Buttons&button.bit != 0 {
				parts = append(parts, button.name)
			}
		}
		switch {
		case t.Wheel > 0:
			parts = append(parts, "Wheel Up")
		case t.Wheel < 0:
			parts = append(parts, "Wheel Down")
		}
		if len(parts) == 0 {
			return "(none)"
		}
		return strings.Join(parts, "+")
	}
	return "(none)"
}

func keyName(usage byte) string {
	if name, ok := hidKeyNames[usage]; ok {
		return name
	}
	return fmt.Sprintf("Key %#02x", usage)
}

// KeyTargetChoice is one entry in the list of things a key can be assigned.
type KeyTargetChoice struct {
	Target KeyTarget
	Group  string // "Keys", "Media", "Mouse"
}

// KeyTargetChoices lists everything a key can be assigned to, grouped. Any
// key choice can additionally be combined with a modifier.
func KeyTargetChoices() []KeyTargetChoice {
	choices := []KeyTargetChoice{{Target: KeyTarget{}, Group: "Keys"}}
	for usage := 0x04; usage <= 0xe7; usage++ {
		if _, ok := hidKeyNames[byte(usage)]; ok {
			choices = append(choices, KeyTargetChoice{Target: KeyTargetKeyOf(byte(usage)), Group: "Keys"})
		}
	}
	for _, usage := range []uint16{0x00e9, 0x00ea, 0x00e2, 0x00cd, 0x00b5, 0x00b6, 0x00b7, 0x006f, 0x0070,
		0x018a, 0x0192, 0x0194, 0x0223, 0x0224, 0x0225, 0x0227, 0x0221} {
		choices = append(choices, KeyTargetChoice{Target: KeyTarget{Kind: TargetMedia, Media: usage}, Group: "Media"})
	}
	for _, button := range mouseButtonNames {
		choices = append(choices, KeyTargetChoice{Target: KeyTarget{Kind: TargetMouse, Buttons: button.bit}, Group: "Mouse"})
	}
	choices = append(choices,
		KeyTargetChoice{Target: KeyTarget{Kind: TargetMouse, Wheel: 2}, Group: "Mouse"},
		KeyTargetChoice{Target: KeyTarget{Kind: TargetMouse, Wheel: -2}, Group: "Mouse"},
	)
	return choices
}

// ModifierChoices are the modifiers a key target can be combined with; 0 is
// no modifier.
var ModifierChoices = []byte{0, 0xe0, 0xe1, 0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7}

// RetroKeyboardKeys lists every assignable key of a Retro Mechanical
// Keyboard (0x5200): the ten dedicated buttons, then the ordinary keys. It
// has no numpad, and its own ids for the modifiers and dedicated buttons.
var RetroKeyboardKeys = buildRetroKeyboardKeys()

func buildRetroKeyboardKeys() []KeyboardKey {
	keys := []KeyboardKey{
		{ID: 109, Name: "A button", Dedicated: true}, {ID: 108, Name: "B button", Dedicated: true},
		{ID: 116, Name: "K1", Dedicated: true}, {ID: 117, Name: "K2", Dedicated: true},
		{ID: 114, Name: "K3", Dedicated: true}, {ID: 115, Name: "K4", Dedicated: true},
		{ID: 112, Name: "K5", Dedicated: true}, {ID: 113, Name: "K6", Dedicated: true},
		{ID: 110, Name: "K7", Dedicated: true}, {ID: 111, Name: "K8", Dedicated: true},
	}
	for _, usage := range retro108Remappable {
		switch {
		case usage >= 0x54 && usage <= 0x63: // no numpad
		case usage >= 0xe0 && usage <= 0xe6:
			keys = append(keys, KeyboardKey{ID: 100 + (usage - 0xe0), Name: hidKeyNames[usage], Types: usage})
		default:
			keys = append(keys, KeyboardKey{ID: usage, Name: hidKeyNames[usage]})
		}
	}
	return keys
}

// keyboardLayout is what differs between keyboard models that share the
// per-key protocol.
type keyboardLayout struct {
	keys []KeyboardKey
	// fKeyShift is added to the usages of F13-F24 as targets: a Retro
	// Mechanical Keyboard numbers them 118-129 rather than 104-115.
	fKeyShift byte
}

func keyboardLayoutFor(vidPid protocol.VidPid) keyboardLayout {
	if vidPid.PID == 0x5200 {
		return keyboardLayout{keys: RetroKeyboardKeys, fKeyShift: 118 - 0x68}
	}
	return keyboardLayout{keys: Retro108Keys}
}

// KeyboardKeysFor lists the assignable keys of the keyboard with this id.
func KeyboardKeysFor(vidPid protocol.VidPid) []KeyboardKey { return keyboardLayoutFor(vidPid).keys }

func (l keyboardLayout) key(id byte) (KeyboardKey, bool) {
	for _, key := range l.keys {
		if key.ID == id {
			return key, true
		}
	}
	return KeyboardKey{}, false
}

// defaultOf is what key id does unassigned on this keyboard.
func (l keyboardLayout) defaultOf(id byte) KeyTarget {
	if key, ok := l.key(id); ok {
		return key.Default()
	}
	return KeyTarget{}
}

// toWire and fromWire translate a target to and from this keyboard's
// numbering of F13-F24.
func (l keyboardLayout) toWire(t KeyTarget) protocol.JP108Mapping {
	if t.Kind == TargetKey && t.Key >= 0x68 && t.Key <= 0x73 {
		t.Key += l.fKeyShift
	}
	return t.wire()
}

func (l keyboardLayout) fromWire(m protocol.JP108Mapping) (KeyTarget, error) {
	t, err := keyTargetFromWire(m)
	if err == nil && l.fKeyShift != 0 && t.Kind == TargetKey && t.Key >= 0x68+l.fKeyShift && t.Key <= 0x73+l.fKeyShift {
		t.Key -= l.fKeyShift
	}
	return t, err
}
