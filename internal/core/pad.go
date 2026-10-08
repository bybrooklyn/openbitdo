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
	// PadSlots is how many profile slots a controller holds.
	PadSlots = 3
	// PadButtons is how many inputs a slot's button map covers.
	PadButtons = 22

	padInUse = 0x20200911 // marks a slot or one of its sections as set

	padOffFlags     = 0x000
	padOffCRC       = 0x00c
	padOffPlatform  = 0x010
	padOffActive    = 0x012
	padOffName      = 0x014 // 32 bytes per slot
	padOffVibration = 0x074 // 12: flag, two float32
	padOffSticks    = 0x098 // 8: flag, two (start, end)
	padOffTriggers  = 0x0b0 // 8: flag, two (start, end)
	padOffOptions   = 0x0c8 // 8: flag, option word
	padOffButtons   = 0x0e0 // 92: flag, 22 targets

	padMotionOff = 0x20190000 // the motion section's flag when motion is off

	padNameLen = 32
)

// padLayout is where a controller model keeps the sections of its record
// that follow the button map. The sections before it are in the same place
// on every model.
type padLayout struct {
	size int
	// macros is the recorded-macro section: 216 bytes per slot.
	macros int
	// motion (12 per slot), then tracing (12), fire (16) and custom (100)
	// lights; zero when the model has none.
	motion, tracing, fire, custom int
	// swapPaddleTriggers: this model numbers P1 and P2 the other way round
	// when one plays a macro.
	swapPaddleTriggers bool
}

var (
	padLayoutU2   = padLayout{size: protocol.U2RecordSize, macros: 0x1f4, motion: 0x494, tracing: 0x4b8, fire: 0x4dc, custom: 0x50c}
	padLayoutU2BT = padLayout{size: protocol.U2BTRecordSize, macros: 0x68c, motion: 0x92c, tracing: 0x950, fire: 0x974, custom: 0x9a4, swapPaddleTriggers: true}
	padLayoutPro3 = padLayout{size: protocol.Pro3RecordSize, macros: 0x68c}
)

func padLayoutFor(vidPid protocol.VidPid) padLayout {
	switch vidPid.PID {
	case 0x600f, 0x6011:
		return padLayoutU2BT
	case 0x6009:
		return padLayoutPro3
	}
	return padLayoutU2
}

func (l padLayout) hasMotion() bool { return l.motion != 0 }
func (l padLayout) hasLights() bool { return l.tracing != 0 }

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
}

// PadProfile is a controller's configuration for one platform.
type PadProfile struct {
	// Platform is the platform bank this was read from.
	Platform byte
	// ActiveSlot is the slot the controller is using, 0-2. It is changed
	// on the controller, not from here.
	ActiveSlot int
	Slots      [PadSlots]PadSlot
	// LightEffect is the stick-ring effect in use: one of the
	// protocol.U2Light* values. It belongs to the controller, not a slot.
	LightEffect byte
	// Macros are each slot's four recorded macros.
	Macros [PadSlots][PadMacros]PadMacro

	// HasMotion and HasLights say whether this model has a motion sensor
	// mapping and stick-ring lights to configure.
	HasMotion, HasLights bool

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
		Platform:   byte(binary.LittleEndian.Uint16(record[padOffPlatform:])),
		ActiveSlot: int(binary.LittleEndian.Uint16(record[padOffActive:])),
		record:     append([]byte(nil), record...),
		layout:     layout, HasMotion: layout.hasMotion(), HasLights: layout.hasLights(),
	}
	if profile.ActiveSlot >= PadSlots {
		profile.ActiveSlot = 0
	}
	for i := range profile.Slots {
		slot := defaultPadSlot(profile.Platform)
		slot.InUse = flagAt(record, padOffFlags+i*4)
		if slot.InUse {
			slot.Name = decodePadName(record[padOffName+i*padNameLen:][:padNameLen])
		}
		// Each section counts only when its own flag says it is set.
		if at := padOffButtons + i*92; flagAt(record, at) {
			for b := range slot.Buttons {
				slot.Buttons[b] = PadTarget(binary.LittleEndian.Uint32(record[at+4+b*4:]))
			}
		}
		if at := padOffSticks + i*8; flagAt(record, at) {
			slot.LeftStick = PadRange{record[at+4], record[at+5]}
			slot.RightStick = PadRange{record[at+6], record[at+7]}
		}
		if at := padOffTriggers + i*8; flagAt(record, at) {
			slot.LeftTrigger = PadRange{record[at+4], record[at+5]}
			slot.RightTrigger = PadRange{record[at+6], record[at+7]}
		}
		if at := padOffVibration + i*12; flagAt(record, at) {
			slot.VibrationLeft = vibrationLevel(math.Float32frombits(binary.LittleEndian.Uint32(record[at+4:])))
			slot.VibrationRight = vibrationLevel(math.Float32frombits(binary.LittleEndian.Uint32(record[at+8:])))
		}
		if at := padOffOptions + i*8; flagAt(record, at) {
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
func encodePadSlot(record []byte, layout padLayout, i int, slot, was PadSlot) ([]padRange, error) {
	if err := slot.validate(); err != nil {
		return nil, err
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
		put(padOffName+i*padNameLen, name)
	}
	if slot.Buttons != was.Buttons {
		section := append([]byte(nil), flag...)
		for _, target := range slot.Buttons {
			section = binary.LittleEndian.AppendUint32(section, uint32(target))
		}
		put(padOffButtons+i*92, section)
	}
	if slot.LeftStick != was.LeftStick || slot.RightStick != was.RightStick {
		put(padOffSticks+i*8, append(append([]byte(nil), flag...),
			slot.LeftStick.Start, slot.LeftStick.End, slot.RightStick.Start, slot.RightStick.End))
	}
	if slot.LeftTrigger != was.LeftTrigger || slot.RightTrigger != was.RightTrigger {
		put(padOffTriggers+i*8, append(append([]byte(nil), flag...),
			slot.LeftTrigger.Start, slot.LeftTrigger.End, slot.RightTrigger.Start, slot.RightTrigger.End))
	}
	if slot.VibrationLeft != was.VibrationLeft || slot.VibrationRight != was.VibrationRight {
		section := append([]byte(nil), flag...)
		section = binary.LittleEndian.AppendUint32(section, math.Float32bits(float32(slot.VibrationLeft)*0.2))
		section = binary.LittleEndian.AppendUint32(section, math.Float32bits(float32(slot.VibrationRight)*0.2))
		put(padOffVibration+i*12, section)
	}
	if slot.Options != was.Options {
		put(padOffOptions+i*8, binary.LittleEndian.AppendUint32(append([]byte(nil), flag...), slot.Options))
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
func (c *OpenBitdoCore) padSession(ctx context.Context, addr PadAddress) (session *protocol.DeviceSession, platform byte, done func(), err error) {
	vidPid := addr.product()
	if !supportsPadProfile(vidPid) {
		return nil, 0, nil, errPolicyDenied(ReasonUnsupportedPid, "controller profiles are not supported for %s", vidPid)
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
	platform, perr = padPlatform(ctx, session, addr)
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
	session, _, done, err := c.padSession(ctx, addr)
	if err != nil {
		return PadProfile{}, err
	}
	defer done()
	return readPadProfile(ctx, session, padLayoutFor(addr.product()))
}

func readPadProfile(ctx context.Context, session *protocol.DeviceSession, layout padLayout) (PadProfile, error) {
	record, err := session.U2ReadRecord(ctx, layout.size)
	if err != nil {
		return PadProfile{}, errProtocol(err)
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
	session, platform, done, err := c.padSession(ctx, addr)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	defer done()
	if edited.Platform != platform {
		return WriteRecoveryReport{}, errInvalidState("the controller's mode switch moved since this profile was read; reload it")
	}

	layout := padLayoutFor(vidPid)
	before, err := readPadProfile(ctx, session, layout)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	record := append([]byte(nil), before.record...)
	var spans []padRange
	for i := range edited.Slots {
		changed, err := encodePadSlot(record, layout, i, edited.Slots[i], before.Slots[i])
		if err != nil {
			return WriteRecoveryReport{}, errInvalidState("slot %d: %v", i+1, err)
		}
		spans = append(spans, changed...)
	}
	macroSlots := map[int]bool{}
	for slot := range edited.Macros {
		if reflect.DeepEqual(edited.Macros[slot], before.Macros[slot]) {
			continue
		}
		for j, macro := range edited.Macros[slot] {
			if err := macro.Validate(); err != nil {
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

	var applyErr error
	// Steps go first: a header must never describe steps that are not there.
	for slot := 0; slot < PadSlots && applyErr == nil; slot++ {
		if macroSlots[slot] {
			applyErr = writePadMacroSteps(ctx, session, platform, slot, edited.Macros[slot], before.Macros[slot])
		}
	}
	if applyErr == nil && len(spans) > 0 {
		applyErr = writePadSpans(ctx, session, record, spans)
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
		rollbackErr = writePadSpans(ctx, session, before.record, spans)
	}
	for slot := 0; slot < PadSlots && rollbackErr == nil; slot++ {
		if macroSlots[slot] {
			rollbackErr = writePadMacroSteps(ctx, session, platform, slot, before.Macros[slot], edited.Macros[slot])
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
// controller now holds them.
func writePadSpans(ctx context.Context, session *protocol.DeviceSession, record []byte, spans []padRange) error {
	for _, span := range spans {
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
	session, platform, done, err := c.padSession(ctx, addr)
	if err != nil {
		return err
	}
	defer done()
	if byte(binary.LittleEndian.Uint16(backup[padOffPlatform:])) != platform {
		return errInvalidState("this backup is for the other position of the controller's mode switch")
	}
	layout := padLayoutFor(vidPid)
	if len(backup) != layout.size {
		return errInvalidState("this backup is for a different controller model")
	}
	current, perr := session.U2ReadRecord(ctx, layout.size)
	if perr != nil {
		return errProtocol(perr)
	}
	// Runs of differing bytes, leaving the header's crc, platform and
	// active slot (0x0c-0x13) to the controller.
	var spans []padRange
	for i := 0; i < len(backup); i++ {
		if (i >= padOffCRC && i < padOffName) || current[i] == backup[i] {
			continue
		}
		start := i
		for i < len(backup) && current[i] != backup[i] && (i < padOffCRC || i >= padOffName) {
			i++
		}
		spans = append(spans, padRange{start, i - start})
	}
	now, err2 := readPadMacros(ctx, session, current, layout, platform)
	if err2 != nil {
		return errProtocol(err2)
	}
	for slot := 0; slot < PadSlots; slot++ {
		if err := writePadMacroSteps(ctx, session, platform, slot, macros[slot], now[slot]); err != nil {
			return errProtocol(err)
		}
	}
	if len(spans) == 0 {
		return nil
	}
	if err := writePadSpans(ctx, session, backup, spans); err != nil {
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
	}
	return target.String()
}

// padPlatform works out which platform's record a controller is using. An
// Ultimate 2's mode switch chooses between XInput and DInput; a Pro 3
// reached directly is on DInput or Switch; an Ultimate 2 Bluetooth reached
// directly is always on Switch.
func padPlatform(ctx context.Context, session *protocol.DeviceSession, addr PadAddress) (byte, error) {
	vidPid := addr.product()
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
