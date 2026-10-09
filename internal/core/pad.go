package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"unicode/utf16"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// This file is the controller side of the core: an Ultimate 2's whole
// configuration (three profile slots, each with a button map, stick and
// trigger ranges, vibration strength and option switches), read from and
// written to the controller's configuration record. The record's layout is
// in docs/clean-room-evidence/dossiers/6012/u2_adv.toml.

const (
	// PadSlots is how many profile slots a controller holds at most. An
	// Arcade Controller Pro holds two; see PadProfile.SlotCount.
	PadSlots = 3
	// PadButtons is how many inputs a slot's button map covers. An Arcade
	// Controller Pro has two more; see PadSlot.ExtraButtons.
	PadButtons = 22

	padInUse = 0x20200911 // marks a slot or one of its sections as set

	padOffFlags = 0x000 // one flag per slot

	padMotionOff = 0x20190000 // the motion section's flag when motion is off

	padNameLen = 32
)

// padFront is the front of a record: the header and the sections every
// model has, which follow one another in the same order everywhere. Where
// each one starts depends on how many slots the record has, and how long
// the button map is on how many inputs it covers.
type padFront struct {
	slots, inputs int
	crc           int // u32; then the platform and the active slot, u16 each
	platform      int
	active        int
	names         int // 32 bytes per slot
	vibration     int // 12: flag, two float32
	sticks        int // 8: flag, two (start, end)
	triggers      int // 8: flag, two (start, end)
	options       int // 8: flag, option word
	buttons       int // flag, then one target per input
}

var (
	padFrontThreeSlots = padFront{slots: 3, inputs: PadButtons, crc: 0x00c, platform: 0x010, active: 0x012,
		names: 0x014, vibration: 0x074, sticks: 0x098, triggers: 0x0b0, options: 0x0c8, buttons: 0x0e0}
	// A first-generation Ultimate Bluetooth: 20 inputs, so everything after
	// the button map sits earlier than on the other three-slot models.
	padFrontTwentyInputs = padFront{slots: 3, inputs: PadButtons - 2, crc: 0x00c, platform: 0x010, active: 0x012,
		names: 0x014, vibration: 0x074, sticks: 0x098, triggers: 0x0b0, options: 0x0c8, buttons: 0x0e0}
	// An Arcade Controller Pro: two slots, 24 inputs.
	padFrontTwoSlots = padFront{slots: 2, inputs: PadButtons + PadExtraButtons, crc: 0x008, platform: 0x00c, active: 0x00e,
		names: 0x010, vibration: 0x050, sticks: 0x068, triggers: 0x078, options: 0x088, buttons: 0x098}
)

// buttonSection is the size of one slot's button map.
func (f padFront) buttonSection() int { return 4 + f.inputs*4 }

// padLayout is where a controller model keeps the sections of its record.
type padLayout struct {
	padFront
	size int
	// macros is the recorded-macro section: 216 bytes per slot.
	macros int
	// motion (12 per slot), then tracing (12), fire (16) and custom (100)
	// lights; zero when the model has none.
	motion, tracing, fire, custom int
	// swapPaddleTriggers: this model numbers P1 and P2 the other way round
	// when one plays a macro.
	swapPaddleTriggers bool
	// arcade: a leverless arcade controller. It has no sticks, triggers or
	// vibration to tune, and a setting for opposite directions pressed at
	// once instead.
	arcade bool
	// buttonLights (376 per slot) and combos (164 per slot): an Arcade
	// Controller Pro's button lights and its buttons that press several at
	// once; zero when the model has none.
	buttonLights, combos int
	// ultimateBT: a first-generation Ultimate Bluetooth; see
	// pad_ultimate_bt.go for everything that sets it apart.
	ultimateBT bool
}

var (
	padLayoutU2   = padLayout{padFront: padFrontThreeSlots, size: protocol.U2RecordSize, macros: 0x1f4, motion: 0x494, tracing: 0x4b8, fire: 0x4dc, custom: 0x50c}
	padLayoutU2BT = padLayout{padFront: padFrontThreeSlots, size: protocol.U2BTRecordSize, macros: 0x68c, motion: 0x92c, tracing: 0x950, fire: 0x974, custom: 0x9a4, swapPaddleTriggers: true}
	padLayoutPro3 = padLayout{padFront: padFrontThreeSlots, size: protocol.Pro3RecordSize, macros: 0x68c}
	// An Arcade Controller's record is laid out as a Pro 3's.
	padLayoutArcade = padLayout{padFront: padFrontThreeSlots, size: protocol.Pro3RecordSize, macros: 0x68c, arcade: true}
	// An Arcade Controller Pro's is its own: see pad_arcade_pro.go.
	padLayoutArcadePro = padLayout{padFront: padFrontTwoSlots, size: protocol.ArcadeProRecordSize, macros: 0x470, arcade: true,
		buttonLights: 0x630, combos: 0x920}
	// A first-generation Ultimate Bluetooth's: hot-key macros at 0x1dc,
	// recorded macros at 0x674, xinput rumble at 0x8fc.
	padLayoutUltimateBT = padLayout{padFront: padFrontTwentyInputs, size: protocol.UltimateBTRecordSize, macros: 0x674, ultimateBT: true}
)

func padLayoutFor(vidPid protocol.VidPid) padLayout {
	switch vidPid.PID {
	case 0x600f, 0x6011:
		return padLayoutU2BT
	case 0x6009:
		return padLayoutPro3
	case 0x600b, 0x600c:
		return padLayoutArcade
	case arcadeProPID, arcadeProAltPID:
		return padLayoutArcadePro
	case protocol.UltimateBTPID, protocol.UltimateBTAdapterPID:
		return padLayoutUltimateBT
	}
	return padLayoutU2
}

func (l padLayout) hasMotion() bool { return l.motion != 0 }
func (l padLayout) hasLights() bool { return l.tracing != 0 }

// arcadePro: the model is an Arcade Controller Pro.
func (l padLayout) arcadePro() bool { return l.combos != 0 }

// PadTarget is what a controller input is assigned to: one value from the
// controller's own function list (see PadTargets).
type PadTarget uint32

// PadRange is the part of a stick's or trigger's travel that is used: below
// Start reads as nothing, End and above as full.
type PadRange struct{ Start, End byte }

// Option switches a slot can have on.
const (
	PadInvertLeftX   uint32 = 0x0001
	PadInvertLeftY   uint32 = 0x0002
	PadInvertRightX  uint32 = 0x0004
	PadInvertRightY  uint32 = 0x0008
	PadSwapSticks    uint32 = 0x0010
	PadSwapTriggers  uint32 = 0x0080
	PadSwapDpadStick uint32 = 0x0100

	// On an arcade controller: which direction wins when two opposite
	// ones are pressed together. At most one is set; none means neither
	// direction registers.
	PadSOCDUpWins    uint32 = 0x10000
	PadSOCDFirstWins uint32 = 0x20000
	PadSOCDLastWins  uint32 = 0x40000
	PadSOCDMask      uint32 = PadSOCDUpWins | PadSOCDFirstWins | PadSOCDLastWins
)

// Where motion (tilting the controller) is sent.
const (
	PadMotionOff        byte = 0
	PadMotionRightStick byte = 1
	PadMotionLeftStick  byte = 2
)

// PadMotion maps the controller's motion sensor onto a stick while a
// button enables it.
type PadMotion struct {
	// Target is PadMotionOff, PadMotionRightStick or PadMotionLeftStick.
	Target byte
	// Button enables motion: held down, or pressed once to toggle.
	Button PadTarget
	Toggle bool
	// Sensitivity is 1 (least) to 10; DeadZone is 0 to 100.
	Sensitivity, DeadZone int
}

// PadLEDs is how many LEDs the two stick rings have: twelve each, left
// ring first, each ring starting at the bottom and going clockwise.
const PadLEDs = 24

// PadLights are a slot's stick-ring colours, as 0xRRGGBB, for each of the
// three effects. Which effect shows is PadProfile.LightEffect.
type PadLights struct {
	TracingColor, TracingBackground uint32
	FireColor, FireBackground       uint32
	// FireSpeed is 1 to 15.
	FireSpeed int
	Custom    [PadLEDs]uint32
}

// PadSlot is one profile slot.
type PadSlot struct {
	// InUse is whether the slot holds a profile. An unused slot behaves as
	// the controller's defaults.
	InUse bool
	Name  string
	// Buttons is the button map, indexed as PadInputs.
	Buttons [PadButtons]PadTarget
	// LeftStick and RightStick are the used range of each stick (0-128);
	// LeftTrigger and RightTrigger of each trigger (0-255).
	LeftStick, RightStick     PadRange
	LeftTrigger, RightTrigger PadRange
	// VibrationLeft and VibrationRight are motor strengths, 0 (off) to 5.
	VibrationLeft, VibrationRight int
	// Options is the slot's option switches (the Pad* bits). Bits this
	// program does not name are kept as read.
	Options uint32
	Motion  PadMotion
	Lights  PadLights

	// The rest is an Arcade Controller Pro's alone, and left at its zero
	// value on every other model. ExtraButtons continues the button map,
	// indexed as PadExtraInputs.
	ExtraButtons [PadExtraButtons]PadTarget
	Combos       [PadCombos]PadCombo
	ButtonLights PadButtonLights
}

// PadProfile is a controller's configuration for one platform.
type PadProfile struct {
	// Platform is the platform bank this was read from.
	Platform byte
	// ActiveSlot is the slot the controller is using, counted from 0. It
	// is changed on the controller, not from here.
	ActiveSlot int
	// SlotCount is how many of Slots (and of Macros) this model has: the
	// first two on an Arcade Controller Pro, all three otherwise. The rest
	// are not on the controller, and changing one is refused.
	SlotCount int
	Slots     [PadSlots]PadSlot
	// LightEffect is the stick-ring effect in use: one of the
	// protocol.U2Light* values. It belongs to the controller, not a slot.
	LightEffect byte
	// Macros are each slot's four recorded macros.
	Macros [PadSlots][PadMacros]PadMacro

	// HasMotion and HasLights say whether this model has a motion sensor
	// mapping and stick-ring lights to configure.
	HasMotion, HasLights bool
	// Arcade says this is a leverless arcade controller: no sticks,
	// triggers or vibration to tune, and the opposite-directions setting.
	Arcade bool
	// ArcadePro says it is an Arcade Controller Pro: two slots, two more
	// inputs, button lights and combos.
	ArcadePro bool
	// UltimateBT says it is a first-generation Ultimate Bluetooth; see
	// PadUltimateBTTargets for what its buttons can be assigned.
	UltimateBT bool
	// ButtonCount is how many of a slot's Buttons this model has: the
	// first 20 on a first-generation Ultimate Bluetooth, which has no L4
	// or R4, and all of them otherwise. Changing one past it is refused.
	ButtonCount int

	// record is the raw record this was decoded from. Writes start from
	// it so everything this program does not model is preserved.
	record []byte
	layout padLayout
}

func decodePadName(raw []byte) string {
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		unit := binary.BigEndian.Uint16(raw[i:])
		if unit == 0 {
			break
		}
		if unit == 0xffff || raw[i] == 0xff || raw[i+1] == 0xff {
			return "" // erased flash
		}
		units = append(units, unit)
	}
	return string(utf16.Decode(units))
}

func encodePadName(name string) ([]byte, error) {
	units := utf16.Encode([]rune(name))
	if len(units) > padNameLen/2 {
		return nil, fmt.Errorf("a profile name holds at most %d characters", padNameLen/2)
	}
	raw := make([]byte, padNameLen)
	for i, unit := range units {
		binary.BigEndian.PutUint16(raw[i*2:], unit)
	}
	return raw, nil
}

func vibrationLevel(strength float32) int {
	if math.IsNaN(float64(strength)) {
		return 5
	}
	// Stored as level/5; a small tolerance absorbs float rounding.
	return max(0, min(5, int(strength*5+0.01)))
}

func flagAt(record []byte, offset int) bool {
	return binary.LittleEndian.Uint32(record[offset:]) == padInUse
}

// DefaultPadSlot is how a slot behaves when nothing is set: every input is
// itself and ranges are full.
func DefaultPadSlot(platform byte) PadSlot { return defaultPadSlot(platform) }

func defaultPadSlot(platform byte) PadSlot {
	slot := PadSlot{
		LeftStick: PadRange{0, 128}, RightStick: PadRange{0, 128},
		LeftTrigger: PadRange{0, 255}, RightTrigger: PadRange{0, 255},
		VibrationLeft: 5, VibrationRight: 5,
		Motion: PadMotion{Sensitivity: 5, DeadZone: 40},
		Lights: PadLights{FireSpeed: 7},
	}
	for i, input := range PadInputs {
		slot.Buttons[i] = input.defaultFor(platform)
	}
	return slot
}

func decodePadProfile(record []byte, layout padLayout) (PadProfile, error) {
	if len(record) != layout.size {
		return PadProfile{}, fmt.Errorf("configuration record is %d bytes, expected %d", len(record), layout.size)
	}
	profile := PadProfile{
		Platform:   byte(binary.LittleEndian.Uint16(record[layout.platform:])),
		ActiveSlot: int(binary.LittleEndian.Uint16(record[layout.active:])),
		SlotCount:  layout.slots,
		record:     append([]byte(nil), record...),
		layout:     layout, HasMotion: layout.hasMotion(), HasLights: layout.hasLights(), Arcade: layout.arcade,
		ArcadePro: layout.arcadePro(), UltimateBT: layout.ultimateBT, ButtonCount: min(layout.inputs, PadButtons),
	}
	if profile.ActiveSlot >= layout.slots {
		profile.ActiveSlot = 0
	}
	for i := range profile.Slots {
		slot := layout.defaultSlot(profile.Platform)
		if i >= layout.slots {
			// Not on this model: left as a slot with nothing set.
			profile.Slots[i] = slot
			continue
		}
		slot.InUse = flagAt(record, padOffFlags+i*4)
		if slot.InUse {
			slot.Name = decodePadName(record[layout.names+i*padNameLen:][:padNameLen])
		}
		if layout.arcadePro() {
			slot.ExtraButtons = defaultPadExtraButtons()
		}
		// Each section counts only when its own flag says it is set.
		if at := layout.buttons + i*layout.buttonSection(); flagAt(record, at) {
			for b := 0; b < min(layout.inputs, PadButtons); b++ {
				slot.Buttons[layout.stored(b)] = PadTarget(binary.LittleEndian.Uint32(record[at+4+b*4:]))
			}
			for b := PadButtons; b < layout.inputs; b++ {
				slot.ExtraButtons[b-PadButtons] = PadTarget(binary.LittleEndian.Uint32(record[at+4+b*4:]))
			}
		}
		if layout.arcadePro() {
			slot.ButtonLights = decodePadButtonLights(record[layout.buttonLights+i*padButtonLightSection:][:padButtonLightSection])
			slot.Combos = decodePadCombos(record[layout.combos+i*padComboSection:][:padComboSection])
		}
		if at := layout.sticks + i*8; flagAt(record, at) {
			slot.LeftStick = PadRange{record[at+4], record[at+5]}
			slot.RightStick = PadRange{record[at+6], record[at+7]}
		}
		if at := layout.triggers + i*8; flagAt(record, at) {
			slot.LeftTrigger = PadRange{record[at+4], record[at+5]}
			slot.RightTrigger = PadRange{record[at+6], record[at+7]}
		}
		if at := layout.vibration + i*12; flagAt(record, at) {
			slot.VibrationLeft = vibrationLevel(math.Float32frombits(binary.LittleEndian.Uint32(record[at+4:])))
			slot.VibrationRight = vibrationLevel(math.Float32frombits(binary.LittleEndian.Uint32(record[at+8:])))
		}
		if at := layout.options + i*8; flagAt(record, at) {
			slot.Options = binary.LittleEndian.Uint32(record[at+4:])
		}
		if at := layout.motion + i*12; layout.hasMotion() && flagAt(record, at) && record[at+11] != PadMotionOff {
			slot.Motion = PadMotion{
				Target: record[at+11], Button: PadTarget(binary.LittleEndian.Uint32(record[at+4:])),
				Toggle:      record[at+8] == 2,
				Sensitivity: max(1, min(10, int(record[at+9]))), DeadZone: min(100, int(record[at+10])),
			}
		}
		if at := layout.tracing + i*12; layout.hasLights() && flagAt(record, at) {
			slot.Lights.TracingColor = binary.LittleEndian.Uint32(record[at+4:]) & 0xffffff
			slot.Lights.TracingBackground = binary.LittleEndian.Uint32(record[at+8:]) & 0xffffff
		}
		if at := layout.fire + i*16; layout.hasLights() && flagAt(record, at) {
			slot.Lights.FireColor = binary.LittleEndian.Uint32(record[at+4:]) & 0xffffff
			slot.Lights.FireBackground = binary.LittleEndian.Uint32(record[at+8:]) & 0xffffff
			slot.Lights.FireSpeed = max(1, min(15, int(record[at+12])))
		}
		if at := layout.custom + i*100; layout.hasLights() && flagAt(record, at) {
			for led := range slot.Lights.Custom {
				slot.Lights.Custom[led] = binary.LittleEndian.Uint32(record[at+4+led*4:]) & 0xffffff
			}
		}
		profile.Slots[i] = slot
	}
	return profile, nil
}

func (s PadSlot) validate() error {
	for _, r := range []struct {
		name string
		r    PadRange
		max  byte
	}{{"left stick", s.LeftStick, 128}, {"right stick", s.RightStick, 128},
		{"left trigger", s.LeftTrigger, 255}, {"right trigger", s.RightTrigger, 255}} {
		if r.r.Start >= r.r.End || r.r.End > r.max {
			return fmt.Errorf("%s range %d-%d is not within 0-%d", r.name, r.r.Start, r.r.End, r.max)
		}
	}
	if s.VibrationLeft < 0 || s.VibrationLeft > 5 || s.VibrationRight < 0 || s.VibrationRight > 5 {
		return fmt.Errorf("vibration strength %d/%d is outside 0-5", s.VibrationLeft, s.VibrationRight)
	}
	for i, target := range s.Buttons {
		if !padTargetKnown(target) {
			return fmt.Errorf("%s is assigned %#08x, which is not a function this controller has", PadInputs[i].Name, uint32(target))
		}
	}
	if m := s.Motion; m.Target > PadMotionLeftStick || m.Sensitivity < 1 || m.Sensitivity > 10 || m.DeadZone < 0 || m.DeadZone > 100 {
		return fmt.Errorf("motion settings are out of range (target %d, sensitivity %d, dead zone %d)", m.Target, m.Sensitivity, m.DeadZone)
	}
	if m := s.Motion; m.Target != PadMotionOff && !PadMotionButton(m.Button) {
		return fmt.Errorf("%s cannot be the button that enables motion", m.Button)
	}
	if s.Lights.FireSpeed < 1 || s.Lights.FireSpeed > 15 {
		return fmt.Errorf("fire ring speed %d is outside 1-15", s.Lights.FireSpeed)
	}
	for _, colour := range append([]uint32{s.Lights.TracingColor, s.Lights.TracingBackground, s.Lights.FireColor, s.Lights.FireBackground}, s.Lights.Custom[:]...) {
		if colour > 0xffffff {
			return fmt.Errorf("colour %#x is not 0xRRGGBB", colour)
		}
	}
	_, err := encodePadName(s.Name)
	return err
}

// padRange is a span of the record that changed.
type padRange struct{ offset, length int }

// encodePadSlot writes slot i into record and returns the spans it changed.
// A section equal to what the record already decodes to is left alone, so
// applying an unedited profile writes nothing.
func encodePadSlot(record []byte, layout padLayout, platform byte, i int, slot, was PadSlot) ([]padRange, error) {
	if err := slot.validate(); err != nil {
		return nil, err
	}
	if layout.arcadePro() {
		if err := validateArcadeProSlot(platform, slot, was); err != nil {
			return nil, err
		}
	} else if slot.ExtraButtons != was.ExtraButtons || slot.Combos != was.Combos || slot.ButtonLights != was.ButtonLights {
		return nil, fmt.Errorf("this controller has no extra inputs, combos or button lights")
	}
	if layout.ultimateBT {
		if err := validateUltimateBTSlot(platform, slot, was); err != nil {
			return nil, err
		}
	}
	var changed []padRange
	put := func(offset int, section []byte) {
		if !bytes.Equal(record[offset:offset+len(section)], section) {
			copy(record[offset:], section)
			changed = append(changed, padRange{offset, len(section)})
		}
	}
	flag := binary.LittleEndian.AppendUint32(nil, padInUse)

	if slot.Name != was.Name {
		name, _ := encodePadName(slot.Name)
		put(layout.names+i*padNameLen, name)
	}
	if slot.Buttons != was.Buttons || slot.ExtraButtons != was.ExtraButtons {
		section := append([]byte(nil), flag...)
		for b := 0; b < min(layout.inputs, PadButtons); b++ {
			section = binary.LittleEndian.AppendUint32(section, uint32(slot.Buttons[layout.stored(b)]))
		}
		for _, target := range slot.ExtraButtons[:max(0, layout.inputs-PadButtons)] {
			section = binary.LittleEndian.AppendUint32(section, uint32(target))
		}
		put(layout.buttons+i*layout.buttonSection(), section)
	}
	if layout.arcadePro() {
		at := layout.buttonLights + i*padButtonLightSection
		if l, w := slot.ButtonLights, was.ButtonLights; l.tables() != w.tables() {
			// The flag and the tables; the effect byte is written below,
			// and the last byte is not this program's to set.
			put(at, flag)
			put(at+5, l.encodeTables())
		}
		if slot.ButtonLights.Effect != was.ButtonLights.Effect {
			put(at+4, []byte{slot.ButtonLights.Effect})
		}
		if slot.Combos != was.Combos {
			put(layout.combos+i*padComboSection, encodePadCombos(slot.Combos))
		}
	}
	if slot.LeftStick != was.LeftStick || slot.RightStick != was.RightStick {
		put(layout.sticks+i*8, append(append([]byte(nil), flag...),
			slot.LeftStick.Start, slot.LeftStick.End, slot.RightStick.Start, slot.RightStick.End))
	}
	if slot.LeftTrigger != was.LeftTrigger || slot.RightTrigger != was.RightTrigger {
		put(layout.triggers+i*8, append(append([]byte(nil), flag...),
			slot.LeftTrigger.Start, slot.LeftTrigger.End, slot.RightTrigger.Start, slot.RightTrigger.End))
	}
	if slot.VibrationLeft != was.VibrationLeft || slot.VibrationRight != was.VibrationRight {
		section := append([]byte(nil), flag...)
		section = binary.LittleEndian.AppendUint32(section, math.Float32bits(float32(slot.VibrationLeft)*0.2))
		section = binary.LittleEndian.AppendUint32(section, math.Float32bits(float32(slot.VibrationRight)*0.2))
		put(layout.vibration+i*12, section)
	}
	if slot.Options != was.Options {
		put(layout.options+i*8, binary.LittleEndian.AppendUint32(append([]byte(nil), flag...), slot.Options))
	}
	if socd := slot.Options & PadSOCDMask; socd&(socd-1) != 0 || (socd != 0 && !layout.arcade) {
		return nil, fmt.Errorf("the opposite-directions setting takes one choice, on an arcade controller only")
	}
	if (slot.Motion != was.Motion && !layout.hasMotion()) || (slot.Lights != was.Lights && !layout.hasLights()) {
		return nil, fmt.Errorf("this controller has no motion or light settings")
	}
	if slot.Motion != was.Motion {
		section := binary.LittleEndian.AppendUint32(nil, padMotionOff)
		if m := slot.Motion; m.Target == PadMotionOff {
			// Off is stored as the controller's own "off" values.
			section = append(section, 0, 0, 0, 0, 0, 5, 40, PadMotionOff)
		} else {
			mode := byte(1)
			if m.Toggle {
				mode = 2
			}
			section = binary.LittleEndian.AppendUint32(append([]byte(nil), flag...), uint32(m.Button))
			section = append(section, mode, byte(m.Sensitivity), byte(m.DeadZone), m.Target)
		}
		put(layout.motion+i*12, section)
	}
	if l, w := slot.Lights, was.Lights; l.TracingColor != w.TracingColor || l.TracingBackground != w.TracingBackground {
		section := binary.LittleEndian.AppendUint32(append([]byte(nil), flag...), l.TracingColor)
		put(layout.tracing+i*12, binary.LittleEndian.AppendUint32(section, l.TracingBackground))
	}
	if l, w := slot.Lights, was.Lights; l.FireColor != w.FireColor || l.FireBackground != w.FireBackground || l.FireSpeed != w.FireSpeed {
		section := binary.LittleEndian.AppendUint32(append([]byte(nil), flag...), l.FireColor)
		section = binary.LittleEndian.AppendUint32(section, l.FireBackground)
		put(layout.fire+i*16, append(section, byte(l.FireSpeed), 0, 0, 0))
	}
	if slot.Lights.Custom != was.Lights.Custom {
		section := append([]byte(nil), flag...)
		for _, colour := range slot.Lights.Custom {
			section = binary.LittleEndian.AppendUint32(section, colour)
		}
		put(layout.custom+i*100, section)
	}
	// A slot with anything set in it is in use.
	if len(changed) > 0 || slot.InUse != was.InUse {
		mark := make([]byte, 4)
		if slot.InUse || len(changed) > 0 {
			mark = flag
		}
		put(padOffFlags+i*4, mark)
	}
	return changed, nil
}

// PadAddress is how to reach a controller: the id it enumerates under and,
// when that id is shared between products, the product it said it is.
type PadAddress struct {
	Enumerated protocol.VidPid
	Product    protocol.VidPid
}

// PadAddressOf is the address of a listed device.
func PadAddressOf(device AppDevice) PadAddress {
	return PadAddress{Enumerated: device.VidPid, Product: device.Product}
}

// product is the product whose commands and record layout apply.
func (a PadAddress) product() protocol.VidPid {
	if a.Product.PID != 0 {
		return a.Product
	}
	// An Arcade Controller Pro has a second id of its own, which the
	// vendor's software treats as the first.
	if a.Enumerated.PID == arcadeProAltPID {
		return protocol.VidPid{VID: a.Enumerated.VID, PID: arcadeProPID}
	}
	return a.Enumerated
}

// shared reports whether the controller was reached under the shared id.
func (a PadAddress) shared() bool { return a.Product.PID != 0 && a.Product != a.Enumerated }

func supportsPadProfile(vidPid protocol.VidPid) bool {
	return protocol.DeviceProfileFor(vidPid).Capability.SupportsU2SlotConfig
}

// padSession opens a session and gets the controller ready to exchange its
// record: checks it is connected, pauses its input reports and selects the
// platform its mode switch is on. done resumes input reports and closes.
// chosen is the platform asked for on a model that leaves the choice to its
// user (see PadPlatformChoices), or padPlatformUnchosen.
func (c *OpenBitdoCore) padSession(ctx context.Context, addr PadAddress, chosen int) (session *protocol.DeviceSession, platform byte, done func(), err error) {
	vidPid := addr.product()
	if !supportsPadProfile(vidPid) {
		return nil, 0, nil, errPolicyDenied(ReasonUnsupportedPid, "controller profiles are not supported for %s", vidPid)
	}
	choosing := padLayoutFor(vidPid).ultimateBT
	if choosing {
		// Refused before anything is sent: there is nothing to ask the
		// controller that would settle it.
		if err := checkPadPlatformChoice(chosen); err != nil {
			return nil, 0, nil, err
		}
	}
	config := protocol.SessionConfig{
		AllowUnsafe: true, BrickRiskAck: true, Experimental: true,
		RetryPolicy: protocol.DefaultRetryPolicy(), TimeoutProfile: protocol.DefaultTimeoutProfile(), TraceEnabled: true,
	}
	session, perr := protocol.NewDeviceSessionAs(ctx, c.transportFor(vidPid), addr.Enumerated, vidPid, config)
	if perr != nil {
		return nil, 0, nil, errProtocol(perr)
	}
	fail := func(err error) (*protocol.DeviceSession, byte, func(), error) {
		_ = session.Close()
		return nil, 0, nil, err
	}
	// Only an Ultimate 2 is asked whether it is connected: its receiver
	// answers while the controller is off. The others are reached directly.
	if vidPid.PID == 0x6012 || vidPid.PID == 0x6013 {
		connected, perr := session.U2Connected(ctx)
		if perr != nil {
			return fail(errProtocol(perr))
		}
		if !connected {
			return fail(errInvalidState("the controller is off or not connected to its receiver"))
		}
	}
	if perr := session.U2SetInputReports(ctx, false); perr != nil {
		return fail(errProtocol(perr))
	}
	done = func() {
		_ = session.U2SetInputReports(context.WithoutCancel(ctx), true)
		_ = session.Close()
	}
	if choosing {
		platform = byte(chosen)
	} else {
		platform, perr = padPlatform(ctx, session, addr)
	}
	if perr == nil {
		perr = session.U2SelectPlatform(ctx, platform)
	}
	if perr != nil {
		done()
		return nil, 0, nil, errProtocol(perr)
	}
	return session, platform, done, nil
}

// PadReadProfile reads a controller's configuration for the platform its
// mode switch is on.
func (c *OpenBitdoCore) PadReadProfile(ctx context.Context, vidPid protocol.VidPid) (PadProfile, error) {
	return c.PadReadProfileAt(ctx, PadAddress{Enumerated: vidPid})
}

// PadReadProfileAt is PadReadProfile for a controller that may have been
// reached under the shared controller id.
func (c *OpenBitdoCore) PadReadProfileAt(ctx context.Context, addr PadAddress) (PadProfile, error) {
	return c.padReadProfile(ctx, addr, padPlatformUnchosen)
}

func (c *OpenBitdoCore) padReadProfile(ctx context.Context, addr PadAddress, chosen int) (PadProfile, error) {
	session, platform, done, err := c.padSession(ctx, addr, chosen)
	if err != nil {
		return PadProfile{}, err
	}
	defer done()
	return readPadProfile(ctx, session, padLayoutFor(addr.product()), platform)
}

// readPadProfile reads the record of the platform that was selected.
func readPadProfile(ctx context.Context, session *protocol.DeviceSession, layout padLayout, platform byte) (PadProfile, error) {
	record, err := session.U2ReadRecord(ctx, layout.size)
	if err != nil {
		return PadProfile{}, errProtocol(err)
	}
	if err := layout.holdsPlatform(record, platform); err != nil {
		return PadProfile{}, err
	}
	profile, err := decodePadProfile(record, layout)
	if err != nil {
		return PadProfile{}, errProtocol(err)
	}
	if layout.hasLights() {
		if profile.LightEffect, err = session.U2LightEffect(ctx); err != nil {
			return PadProfile{}, errProtocol(err)
		}
	}
	if profile.Macros, err = readPadMacros(ctx, session, record, layout, profile.Platform); err != nil {
		return PadProfile{}, errProtocol(err)
	}
	return profile, nil
}

// PadApply writes an edited profile to the controller: it reads the record
// first and keeps it as a backup, writes only the sections that changed,
// commits, and reads the record back. If the readback does not match, or a
// step fails after something was written, the backup is written back.
func (c *OpenBitdoCore) PadApply(ctx context.Context, vidPid protocol.VidPid, edited PadProfile) (WriteRecoveryReport, error) {
	return c.PadApplyAt(ctx, PadAddress{Enumerated: vidPid}, edited)
}

// PadApplyAt is PadApply for a controller that may have been reached under
// the shared controller id.
func (c *OpenBitdoCore) PadApplyAt(ctx context.Context, addr PadAddress, edited PadProfile) (WriteRecoveryReport, error) {
	vidPid := addr.product()
	// Nothing past the receiver's connection query has been exchanged with
	// a real controller yet, so real writes stay behind advanced mode.
	if !c.config.MockMode && c.transportOverride == nil && !c.AdvancedMode() {
		return WriteRecoveryReport{}, errPolicyDenied(ReasonUnsupportedPid,
			"writing controller profiles is not hardware-confirmed yet; turn on advanced mode to try it")
	}
	layout := padLayoutFor(vidPid)
	chosen := padPlatformUnchosen
	if layout.ultimateBT {
		// The profile says which platform's record it was read from.
		chosen = int(edited.Platform)
	}
	session, platform, done, err := c.padSession(ctx, addr, chosen)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	defer done()
	if edited.Platform != platform {
		return WriteRecoveryReport{}, errInvalidState("the controller's mode switch moved since this profile was read; reload it")
	}

	before, err := readPadProfile(ctx, session, layout, platform)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	record := append([]byte(nil), before.record...)
	var spans []padRange
	for i := range edited.Slots {
		if i >= layout.slots {
			if edited.Slots[i] != before.Slots[i] || !reflect.DeepEqual(edited.Macros[i], before.Macros[i]) {
				return WriteRecoveryReport{}, errInvalidState("slot %d: this controller has %d profile slots", i+1, layout.slots)
			}
			continue
		}
		changed, err := encodePadSlot(record, layout, platform, i, edited.Slots[i], before.Slots[i])
		if err != nil {
			return WriteRecoveryReport{}, errInvalidState("slot %d: %v", i+1, err)
		}
		if layout.arcadePro() {
			if err := validateArcadeProMacros(edited.Slots[i], edited.Macros[i]); err != nil {
				return WriteRecoveryReport{}, errInvalidState("slot %d: %v", i+1, err)
			}
		}
		spans = append(spans, changed...)
	}
	macroSlots := map[int]bool{}
	for slot := 0; slot < layout.slots; slot++ {
		if reflect.DeepEqual(edited.Macros[slot], before.Macros[slot]) {
			continue
		}
		for j, macro := range edited.Macros[slot] {
			if err := macro.validate(layout.macroTrigger); err != nil {
				return WriteRecoveryReport{}, errInvalidState("slot %d macro %d: %v", slot+1, j+1, err)
			}
		}
		macroSlots[slot] = true
		section := encodePadMacroSection(layout, platform, edited.Macros[slot])
		at := layout.macros + slot*padMacroSection
		if !bytes.Equal(record[at:at+padMacroSection], section) {
			copy(record[at:], section)
			spans = append(spans, padRange{at, padMacroSection})
		}
	}
	backupID := c.storeBackup(addr.Enumerated, configBackupPayload{kind: backupPad, padRecord: before.record, padMacros: before.Macros, padProduct: addr.Product})
	report := WriteRecoveryReport{BackupID: backupID, HasBackupID: true}
	effect := edited.LightEffect
	if effect == 0 || !layout.hasLights() {
		effect = before.LightEffect // a profile built without one leaves it alone
	}
	if len(spans) == 0 && len(macroSlots) == 0 && effect == before.LightEffect {
		report.WriteApplied = true
		return report, nil
	}
	if layout.arcadePro() {
		endSync, err := beginArcadeProSync(ctx, session)
		if err != nil {
			return report, err
		}
		defer endSync()
	}

	var applyErr error
	// Steps go first: a header must never describe steps that are not there.
	for slot := 0; slot < PadSlots && applyErr == nil; slot++ {
		if macroSlots[slot] {
			applyErr = writePadMacroSteps(ctx, session, layout, platform, slot, edited.Macros[slot], before.Macros[slot])
		}
	}
	if applyErr == nil && len(spans) > 0 {
		applyErr = writePadSpans(ctx, session, layout, record, spans)
	}
	if applyErr == nil && effect != before.LightEffect {
		applyErr = setPadLightEffect(ctx, session, effect)
	}
	if applyErr == nil {
		report.WriteApplied = true
		return report, nil
	}
	report.RollbackAttempted, report.WriteError = true, applyErr.Error()
	var rollbackErr error
	if len(spans) > 0 {
		rollbackErr = writePadSpans(ctx, session, layout, before.record, spans)
	}
	for slot := 0; slot < PadSlots && rollbackErr == nil; slot++ {
		if macroSlots[slot] {
			rollbackErr = writePadMacroSteps(ctx, session, layout, platform, slot, before.Macros[slot], edited.Macros[slot])
		}
	}
	if rollbackErr == nil && effect != before.LightEffect {
		rollbackErr = setPadLightEffect(ctx, session, before.LightEffect)
	}
	if rollbackErr != nil {
		report.RollbackError = rollbackErr.Error()
		return report, nil
	}
	report.RollbackSucceeded = true
	return report, nil
}

// writePadSpans writes the given spans of record, commits, and checks the
// controller now holds them. A model that is only ever sent its whole
// record gets all of it; the spans are still what is checked.
func writePadSpans(ctx context.Context, session *protocol.DeviceSession, layout padLayout, record []byte, spans []padRange) error {
	sent := spans
	if layout.ultimateBT {
		sent = []padRange{{0, len(record)}}
	}
	for _, span := range sent {
		if err := session.U2WriteRecordRange(ctx, record, span.offset, span.length); err != nil {
			return fmt.Errorf("write at %#x: %w", span.offset, err)
		}
	}
	if err := session.U2Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	got, err := session.U2ReadRecord(ctx, len(record))
	if err != nil {
		return fmt.Errorf("readback failed: %w", err)
	}
	for _, span := range spans {
		if !bytes.Equal(got[span.offset:span.offset+span.length], record[span.offset:span.offset+span.length]) {
			return fmt.Errorf("readback mismatch at %#x: the controller did not keep what was written", span.offset)
		}
	}
	return nil
}

// restorePadBackup writes a backed-up record back wherever the controller's
// record now differs from it.
func (c *OpenBitdoCore) restorePadBackup(ctx context.Context, addr PadAddress, backup []byte, macros [PadSlots][PadMacros]PadMacro) error {
	vidPid := addr.product()
	layout := padLayoutFor(vidPid)
	chosen := padPlatformUnchosen
	if layout.ultimateBT {
		// The backup says which platform's record it is.
		if len(backup) != layout.size {
			return errInvalidState("this backup is for a different controller model")
		}
		chosen = int(binary.LittleEndian.Uint16(backup[layout.platform:]))
	}
	session, platform, done, err := c.padSession(ctx, addr, chosen)
	if err != nil {
		return err
	}
	defer done()
	if len(backup) > layout.active && byte(binary.LittleEndian.Uint16(backup[layout.platform:])) != platform {
		return errInvalidState("this backup is for the other position of the controller's mode switch")
	}
	if len(backup) != layout.size {
		return errInvalidState("this backup is for a different controller model")
	}
	current, perr := session.U2ReadRecord(ctx, layout.size)
	if perr != nil {
		return errProtocol(perr)
	}
	if err := layout.holdsPlatform(current, platform); err != nil {
		return err
	}
	// The header's crc, platform and active slot stay the controller's, in
	// what is written as in what is compared.
	backup = append([]byte(nil), backup...)
	copy(backup[layout.crc:layout.names], current[layout.crc:layout.names])
	// Runs of differing bytes, leaving the header's crc, platform and
	// active slot (the eight bytes before the names) to the controller.
	var spans []padRange
	for i := 0; i < len(backup); i++ {
		if (i >= layout.crc && i < layout.names) || current[i] == backup[i] {
			continue
		}
		start := i
		for i < len(backup) && current[i] != backup[i] && (i < layout.crc || i >= layout.names) {
			i++
		}
		spans = append(spans, padRange{start, i - start})
	}
	now, err2 := readPadMacros(ctx, session, current, layout, platform)
	if err2 != nil {
		return errProtocol(err2)
	}
	if layout.arcadePro() {
		endSync, err := beginArcadeProSync(ctx, session)
		if err != nil {
			return err
		}
		defer endSync()
	}
	for slot := 0; slot < layout.slots; slot++ {
		if err := writePadMacroSteps(ctx, session, layout, platform, slot, macros[slot], now[slot]); err != nil {
			return errProtocol(err)
		}
	}
	if len(spans) == 0 {
		return nil
	}
	if err := writePadSpans(ctx, session, layout, backup, spans); err != nil {
		return errProtocol(err)
	}
	return nil
}

// setPadLightEffect selects the stick-ring effect and reads it back.
func setPadLightEffect(ctx context.Context, session *protocol.DeviceSession, effect byte) error {
	if err := session.U2SetLightEffect(ctx, effect); err != nil {
		return fmt.Errorf("light effect: %w", err)
	}
	if got, err := session.U2LightEffect(ctx); err != nil || got != effect {
		return fmt.Errorf("readback mismatch for the light effect: set %d, the controller reports %d (%v)", effect, got, err)
	}
	return nil
}

// PadMotionButton reports whether target can be the button that enables
// motion: a face button, d-pad direction, shoulder, trigger, stick click
// or one of the four back buttons.
func PadMotionButton(target PadTarget) bool {
	for _, allowed := range PadMotionButtons {
		if target == allowed {
			return true
		}
	}
	return false
}

// Back buttons as the motion setting names them.
const (
	padMotionP3 PadTarget = 0x00200000
	padMotionP4 PadTarget = 0x40000000
)

// PadMotionButtons are the buttons that can enable motion, in menu order.
var PadMotionButtons = []PadTarget{
	PadR2, PadL2, PadR1, PadL1, PadA, PadB, PadX, PadY, PadUp, PadDown, PadLeft, PadRight,
	PadL3, PadR3, PadPaddle1, PadPaddle2, padMotionP3, padMotionP4,
}

// PadMotionButtonName names a motion-enable button. The four back buttons
// are named by where they are rather than by the function they can send.
func PadMotionButtonName(target PadTarget) string {
	switch target {
	case PadPaddle1:
		return "Back paddle P1"
	case PadPaddle2:
		return "Back paddle P2"
	case padMotionP3:
		return "Extra button L4"
	case padMotionP4:
		return "Extra button R4"
	case padArcadeP5:
		return "Extra button P5"
	}
	return target.String()
}

// padPlatform works out which platform's record a controller is using. An
// Ultimate 2's mode switch chooses between XInput and DInput; a Pro 3
// reached directly is on DInput or Switch; an Ultimate 2 Bluetooth reached
// directly is always on Switch.
func padPlatform(ctx context.Context, session *protocol.DeviceSession, addr PadAddress) (byte, error) {
	vidPid := addr.product()
	if vidPid.PID == 0x600b || vidPid.PID == 0x600c {
		return session.ArcadePlatform(ctx)
	}
	if vidPid.PID == arcadeProPID {
		return session.ArcadeProPlatform(ctx)
	}
	isU2 := vidPid.PID == 0x6012 || vidPid.PID == 0x6013
	// Under the shared id a Pro 3 or Ultimate 2 Bluetooth is in XInput
	// mode; an Ultimate 2 still says where its switch is.
	if addr.shared() && !isU2 {
		return protocol.U2PlatformXInput, nil
	}
	if vidPid.PID == 0x600f || vidPid.PID == 0x6011 {
		return protocol.U2PlatformSwitch, nil
	}
	platform, err := session.U2PhysicalPlatform(ctx)
	if err != nil {
		return 0, err
	}
	if vidPid.PID == 0x6009 {
		if platform == protocol.U2PlatformXInput { // the switch's first position
			return protocol.U2PlatformDInput, nil
		}
		return protocol.U2PlatformSwitch, nil
	}
	return platform, nil
}
