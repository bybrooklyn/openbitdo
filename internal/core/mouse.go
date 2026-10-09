package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"unicode/utf16"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// This file is the core's side of the mice: the Retro R8 (by cable and
// through its 2.4G receiver) and the Riviera mouse. A profile is the
// buttons' assignments, left-handed mode, the DPI stages, polling rate,
// lift-off distance, the wheel's settings and, on a Retro R8, the macros
// four of its buttons can play. The two mice store it differently: a Retro
// R8 has a command for each setting, a Riviera mouse one 140-byte record.
// Both are described in docs/clean-room-evidence/dossiers/5205/mouse.toml;
// none of it has been exchanged with a real mouse yet.

const (
	// MouseDpiStages is how many DPI stages a mouse has.
	MouseDpiStages = protocol.MouseDpiStages
	// MouseMacroSlots is how many macros a Retro R8 holds, one for each of
	// its last four buttons.
	MouseMacroSlots = 4
	// MouseMacroStepLimit is how long a macro the vendor application
	// records; the mouse has room for one more step.
	MouseMacroStepLimit = 79
	// MouseMacroUntilReleased is a macro's Repeat when it plays for as long
	// as its button is held.
	MouseMacroUntilReleased = 0xffff

	mouseDpiMin  = 50
	mouseDpiStep = 50

	// Where a Riviera mouse's record keeps things.
	mouseRecOffFlag       = 0x00
	mouseRecOffName       = 0x04 // 32 bytes
	mouseRecOffStage      = 0x25
	mouseRecOffDpi        = 0x26 // six entries of 5: on u8, X u16, Y u16
	mouseRecOffWheelDir   = 0x44
	mouseRecOffWheelSpeed = 0x45
	mouseRecOffRate       = 0x46 // u16, in Hz
	mouseRecOffLiftOff    = 0x48
	mouseRecOffLeftHanded = 0x49
	mouseRecOffButtons    = 0x50 // five entries of 12, as a record keyboard's keys

	mouseRecDpiEntry = 5
)

// Functions a Retro R8 button can be given: entries of the mouse's function
// table. These are the ones with a plain meaning; MouseFunctionOffered says
// which others the vendor application offers.
const (
	MouseFunctionNone        uint32 = 0 // the button does nothing
	MouseFunctionLeft        uint32 = 1
	MouseFunctionRight       uint32 = 2
	MouseFunctionMiddle      uint32 = 3
	MouseFunctionButton4     uint32 = 4
	MouseFunctionButton5     uint32 = 5
	MouseFunctionDoubleClick uint32 = 36
)

// mouseFunctionsOffered are the spans of the function table the vendor
// application lets a button be given: mouse buttons, media keys, wheel
// steps, shifted symbols, browser, brightness, the number pad, menu, the
// letters, digits, function and editing keys, modifiers, and F13 to F24.
var mouseFunctionsOffered = [][2]uint32{
	{0, 5}, {7, 9}, {11, 16}, {32, 36}, {71, 78}, {83, 83}, {88, 89}, {109, 125},
	{127, 207}, {209, 209}, {211, 215}, {230, 230}, {241, 252},
}

// MouseFunctionOffered reports whether the vendor application offers a
// Retro R8 button this function. The table has others (DPI and polling
// rate switches among them) that it never assigns; they are not accepted
// here either.
func MouseFunctionOffered(function uint32) bool {
	for _, span := range mouseFunctionsOffered {
		if function >= span[0] && function <= span[1] {
			return true
		}
	}
	return false
}

// MouseButton is one of a mouse's buttons that has an assignment.
type MouseButton struct {
	// Index is the button's place in MouseProfile.Assignments.
	Index int
	Name  string
	// Fixed: the mouse stores an assignment for it, but the vendor
	// application offers no way to change it, and neither does this.
	Fixed bool
	// MacroSlot is the place in MouseProfile.Macros of the macro the button
	// can play, or -1.
	MacroSlot int

	// number is a Retro R8 button's number, and normal the function it has
	// in a new profile; code is a Riviera button's own code.
	number int
	normal uint32
	code   byte
}

// mouseLayout is what differs between the mice.
type mouseLayout struct {
	// record: the mouse keeps one record (Riviera) rather than a setting
	// per command (Retro R8).
	record bool
	// nameChars is how long a profile name may be.
	nameChars int
	buttons   []MouseButton
	// rates are the polling rates offered, in Hz.
	rates []int
	// dpiMax is the highest DPI a stage is given.
	dpiMax int
	// splitXY: a stage can have a Y value of its own.
	splitXY bool
	// liftOff: the lift-off distance can be set.
	liftOff bool
	// macros: buttons can play macros.
	macros bool
}

var (
	// Which physical button is 4, 5, 6 or 7 is not established: the vendor
	// application's own names for them disagree with each other.
	mouseLayoutRetro = mouseLayout{
		nameChars: protocol.MouseNameLen / 2,
		buttons: []MouseButton{
			{Index: 0, Name: "Right button", MacroSlot: -1, number: 1, normal: MouseFunctionRight},
			{Index: 1, Name: "Wheel button", MacroSlot: -1, number: 2, normal: MouseFunctionMiddle},
			{Index: 2, Name: "Button 4", MacroSlot: 0, number: 3, normal: MouseFunctionButton5},
			{Index: 3, Name: "Button 5", MacroSlot: 1, number: 4, normal: MouseFunctionButton4},
			{Index: 4, Name: "Button 6", MacroSlot: 2, number: 5, normal: MouseFunctionNone},
			{Index: 5, Name: "Button 7", MacroSlot: 3, number: 6, normal: MouseFunctionNone},
		},
		rates:  []int{250, 500, 1000, 2000, 4000, 8000},
		dpiMax: 26000, splitXY: true, liftOff: true, macros: true,
	}
	// The vendor application's slider for a Riviera stage stops at 12000
	// although its text box takes 26000; the lower is used here. It hides
	// the lift-off setting for this mouse.
	mouseLayoutRiviera = mouseLayout{
		record:    true,
		nameChars: padNameLen / 2,
		buttons: []MouseButton{
			{Index: 0, Name: "Left button", Fixed: true, MacroSlot: -1, code: RecordMouseLeft},
			{Index: 1, Name: "Right button", MacroSlot: -1, code: RecordMouseRight},
			{Index: 2, Name: "Wheel button", MacroSlot: -1, code: RecordMouseMiddle},
			{Index: 3, Name: "Button 4", Fixed: true, MacroSlot: -1, code: RecordMouseButton4},
			{Index: 4, Name: "Button 5", Fixed: true, MacroSlot: -1, code: RecordMouseButton5},
		},
		rates:  []int{250, 500, 1000},
		dpiMax: 12000,
	}

	// rivieraMouseTargets makes a Riviera button's assignment read and
	// written as a Riviera keyboard key's is, with no macros to name.
	rivieraMouseTargets = kbRecordLayout{macroSlotSize: 1024, timers: true}

	mouseDefaultDpi = [MouseDpiStages]uint16{800, 1200, 1600, 2400, 3200, 6400}
)

func mouseLayoutFor(vidPid protocol.VidPid) (mouseLayout, bool) {
	switch vidPid.PID {
	case 0x5205, mouseReceiverPID:
		return mouseLayoutRetro, true
	case 0x205d:
		return mouseLayoutRiviera, true
	}
	return mouseLayout{}, false
}

// mouseReceiverPID is the Retro R8's 2.4G receiver.
const mouseReceiverPID = 0x5206

// MouseAssignment is what one button does. A Retro R8 and a Riviera mouse
// name what a button can do differently, so each has its own fields.
type MouseAssignment struct {
	// Function is what a Retro R8 button does: an entry of the mouse's
	// function table (see MouseFunctionOffered).
	Function uint32
	// Macro makes a Retro R8 button play the macro in its slot instead; its
	// Function is then the one a new profile gives it.
	Macro bool
	// Target is what a Riviera button does, said the way a Riviera
	// keyboard's key is. Macros and Fn cannot be assigned.
	Target RecordKeyTarget
}

// MouseDpiStage is one DPI stage.
type MouseDpiStage struct {
	// Enabled: the DPI button stops at this stage.
	Enabled bool
	// X is the stage's DPI, 50 to MouseProfile.DpiMax in steps of 50.
	X int
	// SplitXY gives the stage a vertical DPI of its own, Y. Without it Y is
	// stored and not used. Only where MouseProfile.HasSplitXY; a Riviera
	// mouse's Y always follows X.
	SplitXY bool
	Y       int
}

// What a macro step does.
const (
	MouseStepKeyDown    byte = 10
	MouseStepKeyUp      byte = 2
	MouseStepButtonDown byte = 15
	MouseStepButtonUp   byte = 7
)

// MouseMacroStep is one step of a Retro R8 macro.
type MouseMacroStep struct {
	// Kind is one of the MouseStep* kinds.
	Kind byte
	// Code is a HID keyboard usage for a key (8417 is #), or 0 to 4 for the
	// left, right and wheel buttons and the two side buttons.
	Code uint16
	// DelayMs is a wait that goes with the step, up to 60000. The vendor
	// application stores 10 for a step with no wait.
	DelayMs int
}

// MouseMacro is the macro a Retro R8 button plays when its assignment says
// so.
type MouseMacro struct {
	// Name is at most ten characters: the mouse stores fifteen, but only
	// ten can be read back.
	Name  string
	Steps []MouseMacroStep
	// Repeat is how many times it plays, 1 to 99, or
	// MouseMacroUntilReleased.
	Repeat int
	// IntervalMs is the wait between two plays, up to 60000.
	IntervalMs int
}

// MouseProfile is everything a mouse's profile holds.
type MouseProfile struct {
	// InUse is whether the mouse holds a profile. Without one the rest
	// shows the defaults; applying a change creates the profile, which
	// needs a name.
	InUse bool
	// Name is the profile's name, at most NameLimit characters.
	Name       string
	LeftHanded bool
	// Assignments is what each button does, in the order of Buttons.
	Assignments []MouseAssignment
	Dpi         [MouseDpiStages]MouseDpiStage
	// ActiveStage is the DPI stage in use, from 0. It must be enabled.
	ActiveStage int
	// PollingRate is in Hz, one of Rates.
	PollingRate int
	// LiftOff is the lift-off distance in millimetres, 1 or 2. It can be
	// changed only where HasLiftOff.
	LiftOff int
	// WheelSpeed is 0 (slowest) to 14.
	WheelSpeed int
	// WheelNatural turns the wheel's direction round.
	WheelNatural bool
	// Macros holds the macro of each button that plays one. A button that
	// does not play its macro reads as having none.
	Macros [MouseMacroSlots]MouseMacro

	// What this model offers.
	NameLimit  int
	Rates      []int
	DpiMax     int
	HasSplitXY bool
	HasLiftOff bool
	HasMacros  bool

	// What this was decoded from. Writes start from it so everything this
	// program does not model is preserved: retro for a Retro R8, record
	// for a Riviera mouse.
	retro  mouseRetroState
	record []byte
	layout mouseLayout
}

// Buttons lists the mouse's buttons that have an assignment.
func (p MouseProfile) Buttons() []MouseButton {
	return append([]MouseButton(nil), p.layout.buttons...)
}

// mouseRetroState is a Retro R8's settings as its commands carry them.
type mouseRetroState struct {
	name       [protocol.MouseNameLen]byte
	buttons    [protocol.MouseButtons]protocol.MouseButtonEntry
	leftHanded byte
	liftOff    byte
	wheelSpeed byte
	wheelDir   byte
	dpi        [2]protocol.MouseDpi
	stage      byte
	rate       protocol.MousePollingRate
	// macros holds the macro of each button that plays one, its name cut
	// to the part that can be read back.
	macros [MouseMacroSlots]protocol.MouseMacro
}

func (s mouseRetroState) inUse() bool { return s.name[0] != 0 || s.name[1] != 0 }

// defaultMouseRetroState is what the vendor application shows for a Retro
// R8 without a profile, and writes when one is created.
func defaultMouseRetroState() mouseRetroState {
	state := mouseRetroState{liftOff: 1}
	for i, button := range mouseLayoutRetro.buttons {
		state.buttons[i].Function = button.normal
	}
	state.dpi[protocol.MouseAxisX] = protocol.MouseDpi{Mask: 0x3f, Values: mouseDefaultDpi}
	state.dpi[protocol.MouseAxisY] = protocol.MouseDpi{Values: mouseDefaultDpi}
	state.rate = protocol.MousePollingRate{Count: 6, Current: 1, Rates: [6]uint16{250, 500, 1000, 2000, 4000, 8000}}
	return state
}

// mouseRetroRates are the rates a Retro R8 offers: the vendor application
// names them by their place, 250 Hz first, whatever the mouse's own table
// says. A mouse that reports fewer than six is offered only that many.
func mouseRetroRates(rate protocol.MousePollingRate) []int {
	rates := mouseLayoutRetro.rates
	if rate.Count >= 1 && int(rate.Count) < len(rates) {
		rates = rates[:rate.Count]
	}
	return append([]int(nil), rates...)
}

func (l mouseLayout) newProfile() MouseProfile {
	return MouseProfile{
		Assignments: make([]MouseAssignment, len(l.buttons)),
		NameLimit:   l.nameChars, Rates: append([]int(nil), l.rates...), DpiMax: l.dpiMax,
		HasSplitXY: l.splitXY, HasLiftOff: l.liftOff, HasMacros: l.macros, layout: l,
	}
}

func decodeMouseRetro(read mouseRetroState) MouseProfile {
	profile := mouseLayoutRetro.newProfile()
	profile.InUse, profile.retro = read.inUse(), read
	state := read
	if !profile.InUse {
		state = defaultMouseRetroState()
		profile.retro = state
	}
	profile.Name = decodePadName(state.name[:])
	profile.LeftHanded = state.leftHanded == 1
	for i, entry := range state.buttons {
		profile.Assignments[i] = MouseAssignment{Function: entry.Function, Macro: entry.Macro == 1}
	}
	x, y := state.dpi[protocol.MouseAxisX], state.dpi[protocol.MouseAxisY]
	for i := range profile.Dpi {
		profile.Dpi[i] = MouseDpiStage{
			Enabled: x.Mask&(1<<i) != 0, X: int(x.Values[i]),
			SplitXY: y.Mask&(1<<i) != 0, Y: int(y.Values[i]),
		}
	}
	profile.ActiveStage = int(state.stage)
	profile.Rates = mouseRetroRates(state.rate)
	if int(state.rate.Current) < len(mouseLayoutRetro.rates) {
		profile.PollingRate = mouseLayoutRetro.rates[state.rate.Current]
	}
	profile.LiftOff = int(state.liftOff)
	profile.WheelSpeed = min(14, int(state.wheelSpeed))
	profile.WheelNatural = state.wheelDir == 1
	for slot, macro := range state.macros {
		if len(macro.Records) == 0 {
			continue
		}
		decoded := MouseMacro{Name: decodePadName(macro.Name), Repeat: int(macro.Cycles), IntervalMs: int(macro.IntervalMs)}
		for _, record := range macro.Records {
			decoded.Steps = append(decoded.Steps, MouseMacroStep{Kind: record.State, Code: record.Code, DelayMs: int(record.Timer)})
		}
		profile.Macros[slot] = decoded
	}
	return profile
}

// encodeMouseName gives name as UTF-16 big-endian in size bytes.
func encodeMouseName(name string, size int) ([]byte, error) {
	units := utf16.Encode([]rune(name))
	if len(units) > size/2 {
		return nil, fmt.Errorf("a name holds at most %d characters", size/2)
	}
	raw := make([]byte, size)
	for i, unit := range units {
		binary.BigEndian.PutUint16(raw[i*2:], unit)
	}
	return raw, nil
}

func mouseDpiOK(value, limit int) bool {
	return value >= mouseDpiMin && value <= limit && value%mouseDpiStep == 0
}

func mouseFlag(on bool) byte {
	if on {
		return 1
	}
	return 0
}

// checkMouseCommon refuses what neither mouse can be given, comparing
// edited with was, the profile as it was read: anything unchanged is left
// alone, whatever it holds.
func checkMouseCommon(layout mouseLayout, edited, was MouseProfile) error {
	if len(edited.Assignments) != len(layout.buttons) {
		return fmt.Errorf("this mouse has %d buttons to assign, not %d", len(layout.buttons), len(edited.Assignments))
	}
	if edited.Name != was.Name && edited.Name == "" {
		return fmt.Errorf("a profile needs a name")
	}
	enabled := 0
	for i, stage := range edited.Dpi {
		if stage.Enabled {
			enabled++
		}
		before := was.Dpi[i]
		if stage.X != before.X && !mouseDpiOK(stage.X, layout.dpiMax) {
			return fmt.Errorf("stage %d: %d DPI is not %d to %d in steps of %d", i+1, stage.X, mouseDpiMin, layout.dpiMax, mouseDpiStep)
		}
		if !layout.splitXY && (stage.SplitXY || (stage.Y != before.Y && stage.Y != stage.X)) {
			return fmt.Errorf("stage %d: this mouse has no separate vertical DPI", i+1)
		}
		if layout.splitXY && (stage.Y != before.Y || (stage.SplitXY && !before.SplitXY)) && !mouseDpiOK(stage.Y, layout.dpiMax) {
			return fmt.Errorf("stage %d: a vertical DPI of %d is not %d to %d in steps of %d", i+1, stage.Y, mouseDpiMin, layout.dpiMax, mouseDpiStep)
		}
	}
	if edited.Dpi != was.Dpi && enabled == 0 {
		return fmt.Errorf("at least one DPI stage has to stay enabled")
	}
	// The stage in use has to be one the DPI button stops at. The vendor
	// application moves to another when it is switched off; here that is
	// the caller's to say.
	active := edited.ActiveStage
	if active != was.ActiveStage && (active < 0 || active >= MouseDpiStages) {
		return fmt.Errorf("DPI stage %d is outside 1-%d", active+1, MouseDpiStages)
	}
	if active >= 0 && active < MouseDpiStages && !edited.Dpi[active].Enabled &&
		(active != was.ActiveStage || was.Dpi[active].Enabled) {
		return fmt.Errorf("DPI stage %d is in use and cannot be one that is switched off", active+1)
	}
	if edited.PollingRate != was.PollingRate {
		offered := false
		for _, hz := range was.Rates {
			offered = offered || hz == edited.PollingRate
		}
		if !offered {
			return fmt.Errorf("%d Hz is not a polling rate this mouse offers", edited.PollingRate)
		}
	}
	if edited.LiftOff != was.LiftOff {
		if !layout.liftOff {
			return fmt.Errorf("this mouse's lift-off distance cannot be set")
		}
		if edited.LiftOff != 1 && edited.LiftOff != 2 {
			return fmt.Errorf("a lift-off distance of %d mm is neither 1 nor 2", edited.LiftOff)
		}
	}
	if edited.WheelSpeed != was.WheelSpeed && (edited.WheelSpeed < 0 || edited.WheelSpeed > 14) {
		return fmt.Errorf("wheel speed %d is outside 0-14", edited.WheelSpeed)
	}
	return nil
}

func (m MouseMacro) validate() error {
	if len(m.Steps) == 0 || len(m.Steps) > MouseMacroStepLimit {
		return fmt.Errorf("it has %d steps; a macro holds 1 to %d", len(m.Steps), MouseMacroStepLimit)
	}
	for i, step := range m.Steps {
		if step.DelayMs < 0 || step.DelayMs > 60000 {
			return fmt.Errorf("step %d waits %d ms, outside 0-60000", i+1, step.DelayMs)
		}
		switch step.Kind {
		case MouseStepKeyDown, MouseStepKeyUp:
			// The keys the vendor application records: the main block,
			// the number pad, the modifiers, and # as a shifted 3.
			key := step.Code >= 4 && step.Code <= 99 && step.Code != 50
			if !key && (step.Code < 0xe0 || step.Code > 0xe7) && step.Code != 0x20e1 {
				return fmt.Errorf("step %d names key %#02x, which is not a key a macro can press", i+1, step.Code)
			}
		case MouseStepButtonDown, MouseStepButtonUp:
			if step.Code > 4 {
				return fmt.Errorf("step %d names mouse button %d, outside 0-4", i+1, step.Code)
			}
		default:
			return fmt.Errorf("step %d is neither a key nor a mouse button going down or up", i+1)
		}
	}
	if (m.Repeat < 1 || m.Repeat > 99) && m.Repeat != MouseMacroUntilReleased {
		return fmt.Errorf("playing %d times is neither 1 to 99 nor until the button is released", m.Repeat)
	}
	if m.IntervalMs < 0 || m.IntervalMs > 60000 {
		return fmt.Errorf("a wait of %d ms between plays is outside 0-60000", m.IntervalMs)
	}
	return nil
}

func (m MouseMacro) wire() (protocol.MouseMacro, error) {
	if err := m.validate(); err != nil {
		return protocol.MouseMacro{}, err
	}
	name, err := encodeMouseName(m.Name, protocol.MouseMacroNameRead)
	if err != nil {
		return protocol.MouseMacro{}, err
	}
	macro := protocol.MouseMacro{Cycles: uint16(m.Repeat), IntervalMs: uint16(m.IntervalMs), Name: name}
	for _, step := range m.Steps {
		macro.Records = append(macro.Records, protocol.MouseMacroRecord{State: step.Kind, Code: step.Code, Timer: uint16(step.DelayMs)})
	}
	return macro, nil
}

// encodeMouseRetro gives the settings a Retro R8 should hold once edited is
// applied over was, the profile as it was read.
func encodeMouseRetro(edited, was MouseProfile) (mouseRetroState, error) {
	layout := mouseLayoutRetro
	state := was.retro
	if err := checkMouseCommon(layout, edited, was); err != nil {
		return state, err
	}
	if edited.Name != was.Name {
		name, err := encodeMouseName(edited.Name, protocol.MouseNameLen)
		if err != nil {
			return state, err
		}
		copy(state.name[:], name)
	}
	if edited.LeftHanded != was.LeftHanded {
		state.leftHanded = mouseFlag(edited.LeftHanded)
	}
	for slot, macro := range edited.Macros {
		if reflect.DeepEqual(macro, was.Macros[slot]) {
			continue
		}
		state.macros[slot] = protocol.MouseMacro{}
		if len(macro.Steps) == 0 {
			continue
		}
		encoded, err := macro.wire()
		if err != nil {
			return state, fmt.Errorf("macro %d: %v", slot+1, err)
		}
		state.macros[slot] = encoded
	}
	for i, button := range layout.buttons {
		assignment := edited.Assignments[i]
		if assignment.Target != (RecordKeyTarget{}) {
			return state, fmt.Errorf("%s: this mouse's buttons are given a function, not a key target", button.Name)
		}
		if assignment.Function != was.Assignments[i].Function || assignment.Macro != was.Assignments[i].Macro {
			switch {
			case assignment.Macro && button.MacroSlot < 0:
				return state, fmt.Errorf("%s cannot play a macro", button.Name)
			case assignment.Macro:
				// A button that plays its macro keeps its usual function.
				state.buttons[i] = protocol.MouseButtonEntry{Macro: 1, Function: button.normal}
			case !MouseFunctionOffered(assignment.Function):
				return state, fmt.Errorf("%s: function %d is not one a button can be given", button.Name, assignment.Function)
			default:
				state.buttons[i] = protocol.MouseButtonEntry{Function: assignment.Function}
			}
		}
		if button.MacroSlot < 0 {
			continue
		}
		// A macro is stored only for a button that plays it.
		plays, steps := state.buttons[i].Macro == 1, len(state.macros[button.MacroSlot].Records)
		if plays && steps == 0 {
			return state, fmt.Errorf("%s is set to play a macro, and it has none", button.Name)
		}
		if !plays && steps > 0 && !reflect.DeepEqual(edited.Macros[button.MacroSlot], was.Macros[button.MacroSlot]) {
			return state, fmt.Errorf("%s has a macro and is not set to play it", button.Name)
		}
	}
	x, y := &state.dpi[protocol.MouseAxisX], &state.dpi[protocol.MouseAxisY]
	for i, stage := range edited.Dpi {
		before, bit := was.Dpi[i], byte(1)<<i
		if stage.Enabled != before.Enabled {
			x.Mask = x.Mask&^bit | mouseFlag(stage.Enabled)<<i
		}
		if stage.SplitXY != before.SplitXY {
			y.Mask = y.Mask&^bit | mouseFlag(stage.SplitXY)<<i
		}
		if stage.X != before.X {
			x.Values[i] = uint16(stage.X)
		}
		if stage.Y != before.Y {
			y.Values[i] = uint16(stage.Y)
		}
	}
	if edited.ActiveStage != was.ActiveStage {
		state.stage = byte(edited.ActiveStage)
	}
	if edited.PollingRate != was.PollingRate {
		for place, hz := range layout.rates {
			if hz == edited.PollingRate {
				state.rate.Current = byte(place)
			}
		}
	}
	if edited.LiftOff != was.LiftOff {
		state.liftOff = byte(edited.LiftOff)
	}
	if edited.WheelSpeed != was.WheelSpeed {
		state.wheelSpeed = byte(edited.WheelSpeed)
	}
	if edited.WheelNatural != was.WheelNatural {
		state.wheelDir = mouseFlag(edited.WheelNatural)
	}
	return state, nil
}

// What a Retro R8 is written in: each part is one command of the vendor
// application's, in the order it applies a whole profile.
const (
	mousePartName = iota
	mousePartLeftHanded
	mousePartButton // index: the button's place
	mousePartLiftOff
	mousePartWheelSpeed
	mousePartWheelDir
	mousePartDpi // index: the axis
	mousePartRate
	mousePartStage
	mousePartMacro // index: the macro's slot
)

type mouseRetroPart struct{ kind, index int }

func mouseMacroSame(a, b protocol.MouseMacro) bool {
	return a.Cycles == b.Cycles && a.IntervalMs == b.IntervalMs && bytes.Equal(a.Name, b.Name) && reflect.DeepEqual(a.Records, b.Records)
}

func mouseRateSame(a, b protocol.MousePollingRate) bool {
	// The sixth rate is only written, and so only kept, when there are six.
	if a.Count != 6 {
		a.Rates[5], b.Rates[5] = 0, 0
	}
	return a == b
}

// mouseRetroParts lists the parts in which want differs from have, or with
// all every part of a profile, for a mouse that holds none.
func mouseRetroParts(want, have mouseRetroState, all bool) []mouseRetroPart {
	var parts []mouseRetroPart
	add := func(differs bool, kind, index int) {
		if differs || all {
			parts = append(parts, mouseRetroPart{kind, index})
		}
	}
	add(want.name != have.name, mousePartName, 0)
	add(want.leftHanded != have.leftHanded, mousePartLeftHanded, 0)
	for i := range want.buttons {
		add(want.buttons[i] != have.buttons[i], mousePartButton, i)
	}
	add(want.liftOff != have.liftOff, mousePartLiftOff, 0)
	add(want.wheelSpeed != have.wheelSpeed, mousePartWheelSpeed, 0)
	add(want.wheelDir != have.wheelDir, mousePartWheelDir, 0)
	for axis := range want.dpi {
		add(want.dpi[axis] != have.dpi[axis], mousePartDpi, axis)
	}
	add(!mouseRateSame(want.rate, have.rate), mousePartRate, 0)
	// The stage in use is sent with its X value, so it is sent again when
	// that value changes, and, as the vendor application does, when a stage
	// is switched on or off or given a Y value of its own.
	if stage := int(want.stage); stage < MouseDpiStages {
		x, wasX := want.dpi[protocol.MouseAxisX], have.dpi[protocol.MouseAxisX]
		if want.stage != have.stage || x.Mask != wasX.Mask || x.Values[stage] != wasX.Values[stage] ||
			want.dpi[protocol.MouseAxisY].Mask != have.dpi[protocol.MouseAxisY].Mask {
			parts = append(parts, mouseRetroPart{mousePartStage, 0})
		}
	}
	for _, button := range mouseLayoutRetro.buttons {
		slot := button.MacroSlot
		if slot < 0 || want.buttons[button.Index].Macro != 1 || len(want.macros[slot].Records) == 0 {
			continue
		}
		if all || have.buttons[button.Index].Macro != 1 || !mouseMacroSame(want.macros[slot], have.macros[slot]) {
			parts = append(parts, mouseRetroPart{mousePartMacro, slot})
		}
	}
	return parts
}

func mouseKept(what string, kept bool, err error) error {
	if err != nil {
		return fmt.Errorf("readback of %s failed: %w", what, err)
	}
	if !kept {
		return fmt.Errorf("readback mismatch for %s: the mouse did not keep what was written", what)
	}
	return nil
}

// writeMouseRetroPart writes one part of state and checks the mouse now
// holds it.
func writeMouseRetroPart(ctx context.Context, session *protocol.DeviceSession, part mouseRetroPart, state mouseRetroState) error {
	setting := func(what string, which protocol.MouseSetting, value byte) error {
		if err := session.MouseWriteSetting(ctx, which, value); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		got, err := session.MouseReadSetting(ctx, which)
		return mouseKept(what, got == value, err)
	}
	switch part.kind {
	case mousePartName:
		if err := session.MouseWriteProfileName(ctx, state.name[:]); err != nil {
			return fmt.Errorf("profile name: %w", err)
		}
		got, err := session.MouseReadProfileName(ctx)
		return mouseKept("the profile name", bytes.Equal(got, state.name[:]), err)
	case mousePartLeftHanded:
		return setting("left-handed mode", protocol.MouseLeftHanded, state.leftHanded)
	case mousePartLiftOff:
		return setting("the lift-off distance", protocol.MouseLiftOff, state.liftOff)
	case mousePartWheelSpeed:
		return setting("the wheel speed", protocol.MouseWheelSpeed, state.wheelSpeed)
	case mousePartWheelDir:
		return setting("the wheel direction", protocol.MouseWheelDirection, state.wheelDir)
	case mousePartButton:
		button := mouseLayoutRetro.buttons[part.index]
		if err := session.MouseWriteButton(ctx, button.number, state.buttons[part.index]); err != nil {
			return fmt.Errorf("%s: %w", button.Name, err)
		}
		// Buttons are read in pairs from an odd number.
		first := button.number - (button.number+1)%2
		got, err := session.MouseReadButtons(ctx, first)
		return mouseKept(button.Name, got[button.number-first] == state.buttons[part.index], err)
	case mousePartDpi:
		if err := session.MouseWriteDpi(ctx, part.index, state.dpi[part.index]); err != nil {
			return fmt.Errorf("DPI stages: %w", err)
		}
		got, err := session.MouseReadDpi(ctx, part.index)
		return mouseKept("the DPI stages", got == state.dpi[part.index], err)
	case mousePartRate:
		if err := session.MouseWritePollingRate(ctx, state.rate); err != nil {
			return fmt.Errorf("polling rate: %w", err)
		}
		got, err := session.MouseReadPollingRate(ctx)
		return mouseKept("the polling rate", mouseRateSame(state.rate, got), err)
	case mousePartStage:
		if int(state.stage) >= MouseDpiStages {
			return fmt.Errorf("DPI stage in use: there is no stage %d", int(state.stage)+1)
		}
		if err := session.MouseWriteDpiStage(ctx, int(state.stage), state.dpi[protocol.MouseAxisX].Values[state.stage]); err != nil {
			return fmt.Errorf("DPI stage in use: %w", err)
		}
		got, err := session.MouseReadDpiStage(ctx)
		return mouseKept("the DPI stage in use", got == state.stage, err)
	case mousePartMacro:
		button := mouseLayoutRetro.buttons[part.index+protocol.MouseMacroFirstButton-1]
		if err := session.MouseWriteMacro(ctx, button.number, state.macros[part.index]); err != nil {
			return fmt.Errorf("macro of %s: %w", button.Name, err)
		}
		got, err := session.MouseReadMacro(ctx, button.number)
		return mouseKept("the macro of "+button.Name, mouseMacroSame(got, state.macros[part.index]), err)
	}
	return fmt.Errorf("there is no such part of a mouse profile")
}

// readMouseRetro reads a Retro R8's settings in the order the vendor
// application does. When the name says the mouse holds no profile nothing
// else is asked for, as there.
func readMouseRetro(ctx context.Context, session *protocol.DeviceSession) (mouseRetroState, error) {
	var state mouseRetroState
	name, err := session.MouseReadProfileName(ctx)
	if err != nil {
		return state, err
	}
	copy(state.name[:], name)
	if !state.inUse() {
		return state, nil
	}
	if state.leftHanded, err = session.MouseReadSetting(ctx, protocol.MouseLeftHanded); err != nil {
		return state, err
	}
	for first := 1; first <= protocol.MouseButtons; first += 2 {
		pair, err := session.MouseReadButtons(ctx, first)
		if err != nil {
			return state, err
		}
		state.buttons[first-1], state.buttons[first] = pair[0], pair[1]
	}
	if state.liftOff, err = session.MouseReadSetting(ctx, protocol.MouseLiftOff); err != nil {
		return state, err
	}
	if state.wheelSpeed, err = session.MouseReadSetting(ctx, protocol.MouseWheelSpeed); err != nil {
		return state, err
	}
	if state.wheelDir, err = session.MouseReadSetting(ctx, protocol.MouseWheelDirection); err != nil {
		return state, err
	}
	for axis := range state.dpi {
		if state.dpi[axis], err = session.MouseReadDpi(ctx, axis); err != nil {
			return state, err
		}
	}
	if state.rate, err = session.MouseReadPollingRate(ctx); err != nil {
		return state, err
	}
	for _, button := range mouseLayoutRetro.buttons {
		if button.MacroSlot < 0 || state.buttons[button.Index].Macro != 1 {
			continue
		}
		if state.macros[button.MacroSlot], err = session.MouseReadMacro(ctx, button.number); err != nil {
			return state, err
		}
	}
	// The vendor application asks for the stage in use last, when its DPI
	// screen is opened.
	if state.stage, err = session.MouseReadDpiStage(ctx); err != nil {
		return state, err
	}
	return state, nil
}

// defaultMouseRecord is the record the vendor application writes for a new
// Riviera profile: every stage on at the usual values, 500 Hz, and every
// button as itself.
func defaultMouseRecord() []byte {
	record := make([]byte, protocol.MouseRecordSize)
	for i, dpi := range mouseDefaultDpi {
		entry := record[mouseRecOffDpi+i*mouseRecDpiEntry:]
		entry[0] = 1
		binary.LittleEndian.PutUint16(entry[1:], dpi)
		binary.LittleEndian.PutUint16(entry[3:], dpi)
	}
	binary.LittleEndian.PutUint16(record[mouseRecOffRate:], 500)
	record[mouseRecOffLiftOff] = 1
	for i, button := range mouseLayoutRiviera.buttons {
		entry := record[mouseRecOffButtons+i*kbRecKeyEntry:]
		entry[0] = button.code
		binary.LittleEndian.PutUint32(entry[4:], uint32(button.code))
	}
	return record
}

// mouseRecordBase is the record a write starts from: the stored one, or the
// defaults when the mouse holds no profile.
func mouseRecordBase(record []byte) []byte {
	if binary.LittleEndian.Uint32(record[mouseRecOffFlag:]) == kbRecInUse {
		return append([]byte(nil), record...)
	}
	return defaultMouseRecord()
}

func decodeMouseRecord(record []byte) (MouseProfile, error) {
	if len(record) != protocol.MouseRecordSize {
		return MouseProfile{}, fmt.Errorf("configuration record is %d bytes, expected %d", len(record), protocol.MouseRecordSize)
	}
	layout := mouseLayoutRiviera
	profile := layout.newProfile()
	profile.InUse = binary.LittleEndian.Uint32(record[mouseRecOffFlag:]) == kbRecInUse
	profile.record = append([]byte(nil), record...)
	shown := mouseRecordBase(record)
	profile.Name = decodePadName(shown[mouseRecOffName : mouseRecOffName+padNameLen])
	profile.LeftHanded = shown[mouseRecOffLeftHanded] == 1
	for i := range layout.buttons {
		profile.Assignments[i].Target = decodeRecordKeyTarget(rivieraMouseTargets, shown[mouseRecOffButtons+i*kbRecKeyEntry:])
	}
	for i := range profile.Dpi {
		entry := shown[mouseRecOffDpi+i*mouseRecDpiEntry:]
		profile.Dpi[i] = MouseDpiStage{
			Enabled: entry[0] == 1,
			X:       int(binary.LittleEndian.Uint16(entry[1:])), Y: int(binary.LittleEndian.Uint16(entry[3:])),
		}
	}
	profile.ActiveStage = int(shown[mouseRecOffStage])
	profile.PollingRate = int(binary.LittleEndian.Uint16(shown[mouseRecOffRate:]))
	profile.LiftOff = int(shown[mouseRecOffLiftOff])
	profile.WheelSpeed = int(shown[mouseRecOffWheelSpeed])
	profile.WheelNatural = shown[mouseRecOffWheelDir] == 1
	return profile, nil
}

// encodeMouseRecord writes what edited changes from was into record, which
// is was's base, and returns the spans it changed. Each span is one the
// vendor application also writes on its own: the name, a stage, a button's
// entry, or a single setting.
func encodeMouseRecord(record []byte, edited, was MouseProfile) ([]kbSpan, error) {
	layout := mouseLayoutRiviera
	if err := checkMouseCommon(layout, edited, was); err != nil {
		return nil, err
	}
	var changed []kbSpan
	put := func(offset int, section ...byte) {
		if !bytes.Equal(record[offset:offset+len(section)], section) {
			copy(record[offset:], section)
			changed = append(changed, kbSpan{offset, len(section)})
		}
	}
	if edited.Name != was.Name {
		name, err := encodePadName(edited.Name)
		if err != nil {
			return nil, err
		}
		put(mouseRecOffName, name...)
	}
	if edited.ActiveStage != was.ActiveStage {
		put(mouseRecOffStage, byte(edited.ActiveStage))
	}
	for i, stage := range edited.Dpi {
		if stage.Enabled == was.Dpi[i].Enabled && stage.X == was.Dpi[i].X {
			continue
		}
		// The vertical value always follows the horizontal one.
		entry := []byte{mouseFlag(stage.Enabled), 0, 0, 0, 0}
		binary.LittleEndian.PutUint16(entry[1:], uint16(stage.X))
		binary.LittleEndian.PutUint16(entry[3:], uint16(stage.X))
		put(mouseRecOffDpi+i*mouseRecDpiEntry, entry...)
	}
	if edited.WheelNatural != was.WheelNatural {
		put(mouseRecOffWheelDir, mouseFlag(edited.WheelNatural))
	}
	if edited.WheelSpeed != was.WheelSpeed {
		put(mouseRecOffWheelSpeed, byte(edited.WheelSpeed))
	}
	if edited.PollingRate != was.PollingRate {
		put(mouseRecOffRate, byte(edited.PollingRate), byte(edited.PollingRate>>8))
	}
	if edited.LeftHanded != was.LeftHanded {
		put(mouseRecOffLeftHanded, mouseFlag(edited.LeftHanded))
	}
	for _, macro := range edited.Macros {
		if macro.Name != "" || len(macro.Steps) > 0 {
			return nil, fmt.Errorf("this mouse has no macros")
		}
	}
	for i, button := range layout.buttons {
		assignment := edited.Assignments[i]
		if assignment.Function != 0 || assignment.Macro {
			return nil, fmt.Errorf("%s: this mouse's buttons are given a key target, not a function", button.Name)
		}
		target := assignment.Target
		if target == was.Assignments[i].Target {
			continue
		}
		if button.Fixed {
			return nil, fmt.Errorf("%s cannot be reassigned", button.Name)
		}
		if target.Kind == RecordTargetMacro {
			return nil, fmt.Errorf("%s: this mouse has no macros", button.Name)
		}
		// A button given its own click is at its default.
		if target.Kind == RecordTargetMouse && target.Mouse == button.code {
			target = RecordKeyTarget{}
		}
		at := mouseRecOffButtons + i*kbRecKeyEntry
		entry := append([]byte(nil), record[at:at+kbRecKeyEntry]...)
		if entry[0] == 0 {
			entry[0] = button.code
		}
		value, kind, err := target.wire(rivieraMouseTargets, RecordKey{Name: button.Name, Usage: entry[0]}, entry[0])
		if err != nil {
			return nil, fmt.Errorf("%s: %v", button.Name, err)
		}
		binary.LittleEndian.PutUint32(entry[4:], value)
		binary.LittleEndian.PutUint32(entry[8:], kind)
		put(at, entry...)
	}
	return changed, nil
}

// writeMouseRecordSpans writes the given spans of record and checks the
// mouse now holds them.
func writeMouseRecordSpans(ctx context.Context, session *protocol.DeviceSession, record []byte, spans []kbSpan) error {
	for _, span := range spans {
		if err := session.MouseRecordWriteRange(ctx, record, span.offset, span.length); err != nil {
			return fmt.Errorf("write at %#x: %w", span.offset, err)
		}
	}
	for _, span := range spans {
		got, err := session.MouseRecordRead(ctx, span.offset, span.length)
		if err != nil {
			return fmt.Errorf("readback failed: %w", err)
		}
		if !bytes.Equal(got, record[span.offset:span.offset+span.length]) {
			return fmt.Errorf("readback mismatch at %#x: the mouse did not keep what was written", span.offset)
		}
	}
	return nil
}

func supportsMouse(vidPid protocol.VidPid) bool {
	_, known := mouseLayoutFor(vidPid)
	return known && protocol.DeviceProfileFor(vidPid).Capability.SupportsMouse
}

// openMouseSession opens a session on a mouse. Nothing here has been tried
// on a real one, so a mouse is only read in advanced mode and only written
// once the caller has been through the write-unlock ceremony.
func (c *OpenBitdoCore) openMouseSession(ctx context.Context, vidPid protocol.VidPid, unlocked bool) (*protocol.DeviceSession, mouseLayout, error) {
	layout, _ := mouseLayoutFor(vidPid)
	if !supportsMouse(vidPid) {
		return nil, layout, errPolicyDenied(ReasonUnsupportedPid, "mouse profiles are not supported for %s", vidPid)
	}
	if c.transportOverride == nil {
		if c.config.MockMode {
			return nil, layout, errPolicyDenied(ReasonFeatureUnavailable, "there is no simulated mouse")
		}
		if !c.AdvancedMode() {
			return nil, layout, errPolicyDenied(ReasonExperimentalRequired,
				"reading this mouse's profile is not hardware-confirmed yet; turn on advanced mode to try it")
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
	return session, layout, nil
}

// mouseSession is openMouseSession for reading or writing a profile: a
// receiver is first asked whether its mouse is there, as the vendor
// application does before it offers the mouse at all.
func (c *OpenBitdoCore) mouseSession(ctx context.Context, vidPid protocol.VidPid, unlocked bool) (*protocol.DeviceSession, mouseLayout, error) {
	session, layout, err := c.openMouseSession(ctx, vidPid, unlocked)
	if err != nil || vidPid.PID != mouseReceiverPID {
		return session, layout, err
	}
	linked, perr := session.MouseReceiverLinked(ctx)
	if perr != nil {
		_ = session.Close()
		return nil, layout, errProtocol(perr)
	}
	if !linked {
		_ = session.Close()
		return nil, layout, errInvalidState("no mouse is linked to this receiver; switch the mouse on")
	}
	return session, layout, nil
}

// MouseReceiverLinked asks a Retro R8 receiver whether a mouse is linked to
// it.
func (c *OpenBitdoCore) MouseReceiverLinked(ctx context.Context, vidPid protocol.VidPid) (bool, error) {
	if vidPid.PID != mouseReceiverPID {
		return false, errPolicyDenied(ReasonUnsupportedPid, "%s is not a mouse receiver", vidPid)
	}
	session, _, err := c.openMouseSession(ctx, vidPid, false)
	if err != nil {
		return false, err
	}
	defer func() { _ = session.Close() }()
	linked, perr := session.MouseReceiverLinked(ctx)
	if perr != nil {
		return false, errProtocol(perr)
	}
	return linked, nil
}

// MouseReadProfile reads a mouse's whole profile.
func (c *OpenBitdoCore) MouseReadProfile(ctx context.Context, vidPid protocol.VidPid) (MouseProfile, error) {
	session, layout, err := c.mouseSession(ctx, vidPid, false)
	if err != nil {
		return MouseProfile{}, err
	}
	defer func() { _ = session.Close() }()
	return readMouseProfile(ctx, session, layout)
}

func readMouseProfile(ctx context.Context, session *protocol.DeviceSession, layout mouseLayout) (MouseProfile, error) {
	if layout.record {
		record, err := session.MouseRecordRead(ctx, 0, protocol.MouseRecordSize)
		if err != nil {
			return MouseProfile{}, errProtocol(err)
		}
		profile, err := decodeMouseRecord(record)
		if err != nil {
			return MouseProfile{}, errProtocol(err)
		}
		return profile, nil
	}
	state, err := readMouseRetro(ctx, session)
	if err != nil {
		return MouseProfile{}, errProtocol(err)
	}
	return decodeMouseRetro(state), nil
}

// MouseApply writes an edited profile to the mouse: it reads the profile
// first and keeps it as a backup, writes only what changed, and reads each
// part back. If a readback does not match, or a step fails after something
// was written, what was there before is written back.
//
// A mouse is read-only until the write-unlock ceremony has been gone
// through; policy says whether it has.
func (c *OpenBitdoCore) MouseApply(ctx context.Context, vidPid protocol.VidPid, edited MouseProfile, policy RuntimeUnlockPolicy) (WriteRecoveryReport, error) {
	if c.transportOverride == nil && (!c.AdvancedMode() || !policy.AdvancedMode || !policy.AcknowledgedRisk || !policy.UnlockFilePresent) {
		return WriteRecoveryReport{}, errPolicyDenied(ReasonNotHardwareConfirmed,
			"writing this mouse's profile is not hardware-confirmed; it needs advanced mode, the write-risk acknowledgement and the mouse's unlock file")
	}
	session, layout, err := c.mouseSession(ctx, vidPid, true)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	defer func() { _ = session.Close() }()

	before, err := readMouseProfile(ctx, session, layout)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	// apply writes the change and undo puts back what was there; both are
	// worked out before anything is sent.
	var apply, undo func() error
	if layout.record {
		record := mouseRecordBase(before.record)
		spans, encodeErr := encodeMouseRecord(record, edited, before)
		if encodeErr != nil {
			return WriteRecoveryReport{}, errInvalidState("%v", encodeErr)
		}
		if !before.InUse && len(spans) > 0 {
			// A mouse without a profile is given a whole one, as the
			// vendor application does, rather than single fields.
			if edited.Name == "" {
				return WriteRecoveryReport{}, errInvalidState("a new profile needs a name")
			}
			binary.LittleEndian.PutUint32(record[mouseRecOffFlag:], kbRecInUse)
			spans = []kbSpan{{0, len(record)}}
		}
		if len(spans) > 0 {
			apply = func() error { return writeMouseRecordSpans(ctx, session, record, spans) }
			undo = func() error { return writeMouseRecordSpans(ctx, session, before.record, spans) }
		}
	} else {
		want, encodeErr := encodeMouseRetro(edited, before)
		if encodeErr != nil {
			return WriteRecoveryReport{}, errInvalidState("%v", encodeErr)
		}
		parts := mouseRetroParts(want, before.retro, false)
		if !before.InUse && len(parts) > 0 {
			if !want.inUse() {
				return WriteRecoveryReport{}, errInvalidState("a new profile needs a name")
			}
			parts = mouseRetroParts(want, before.retro, true)
		}
		if len(parts) > 0 {
			done := 0
			apply = func() error {
				for ; done < len(parts); done++ {
					if err := writeMouseRetroPart(ctx, session, parts[done], want); err != nil {
						return err
					}
				}
				return nil
			}
			undo = func() error { return undoMouseRetro(ctx, session, parts[:min(done+1, len(parts))], before) }
		}
	}

	backupID := c.storeBackup(vidPid, configBackupPayload{kind: backupMouse, mouse: before})
	report := WriteRecoveryReport{BackupID: backupID, HasBackupID: true}
	if apply == nil {
		report.WriteApplied = true
		return report, nil
	}
	applyErr := apply()
	if applyErr == nil {
		report.WriteApplied = true
		return report, nil
	}
	report.RollbackAttempted, report.WriteError = true, applyErr.Error()
	if rollbackErr := undo(); rollbackErr != nil {
		report.RollbackError = rollbackErr.Error()
		return report, nil
	}
	report.RollbackSucceeded = true
	return report, nil
}

// undoMouseRetro puts the given parts of a Retro R8 back as before holds
// them. A mouse that held no profile has it cleared instead: its settings
// were never read, so there is nothing to write back.
func undoMouseRetro(ctx context.Context, session *protocol.DeviceSession, parts []mouseRetroPart, before MouseProfile) error {
	if !before.InUse {
		if err := session.MouseClearProfile(ctx); err != nil {
			return fmt.Errorf("clearing the profile: %w", err)
		}
		name, err := session.MouseReadProfileName(ctx)
		if err != nil {
			return mouseKept("the cleared profile", false, err)
		}
		return mouseKept("the cleared profile", name[0] == 0 && name[1] == 0, nil)
	}
	for _, part := range parts {
		// A macro no button played before is left as it is: nothing plays
		// it once the button's assignment is back.
		if part.kind == mousePartMacro && len(before.retro.macros[part.index].Records) == 0 {
			continue
		}
		if err := writeMouseRetroPart(ctx, session, part, before.retro); err != nil {
			return err
		}
	}
	return nil
}

// restoreMouseBackup writes a backed-up profile back wherever the mouse's
// now differs from it.
func (c *OpenBitdoCore) restoreMouseBackup(ctx context.Context, vidPid protocol.VidPid, backup MouseProfile) error {
	session, layout, err := c.mouseSession(ctx, vidPid, true)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	current, err := readMouseProfile(ctx, session, layout)
	if err != nil {
		return err
	}
	if layout.record {
		var spans []kbSpan
		for i := 0; i < len(backup.record); i++ {
			if current.record[i] == backup.record[i] {
				continue
			}
			start := i
			for i < len(backup.record) && current.record[i] != backup.record[i] {
				i++
			}
			spans = append(spans, kbSpan{start, i - start})
		}
		if len(spans) == 0 {
			return nil
		}
		if err := writeMouseRecordSpans(ctx, session, backup.record, spans); err != nil {
			return errProtocol(err)
		}
		return nil
	}
	if !backup.InUse {
		if !current.InUse {
			return nil
		}
		if err := undoMouseRetro(ctx, session, nil, backup); err != nil {
			return errProtocol(err)
		}
		return nil
	}
	for _, part := range mouseRetroParts(backup.retro, current.retro, !current.InUse) {
		if err := writeMouseRetroPart(ctx, session, part, backup.retro); err != nil {
			return errProtocol(err)
		}
	}
	return nil
}
