package core

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// This file is what an Arcade Controller Pro has that the other controllers
// do not: two more inputs, lights under thirteen of its buttons, and combos
// (one button pressing several). Its record is the same kind as theirs but
// holds two slots, so every section is somewhere else; the layout is in
// docs/clean-room-evidence/dossiers/6012/u2_adv.toml under
// [arcade_controller_pro].
//
// None of it has been tried on a controller: everything here is inferred
// from the vendor's software, and what a setting may be changed to is held
// to what that software can itself produce.

const (
	arcadeProPID    = protocol.ArcadeProPID
	arcadeProAltPID = protocol.ArcadeProAltPID

	// PadExtraButtons is how many inputs an Arcade Controller Pro has
	// beyond PadInputs.
	PadExtraButtons = 2
	// PadCombos is how many combos a slot holds; PadComboButtons how many
	// buttons one presses at most.
	PadCombos       = 5
	PadComboButtons = 6
	// PadButtonLEDs is how many buttons have a light under them;
	// PadButtonEffects how many light effects there are, each with its own
	// table of colours and brightnesses.
	PadButtonLEDs    = 13
	PadButtonEffects = 7

	padButtonLightSection = 376
	padComboSection       = 164
	padComboEntry         = 32

	// padArcadeP5 is the fifth extra button as a macro trigger and as the
	// button a combo is on.
	padArcadeP5 PadTarget = 0x00100000
)

// PadExtraInputs are an Arcade Controller Pro's inputs after PadInputs, in
// the order its button map stores them.
var PadExtraInputs = [PadExtraButtons]PadInput{{"Extra button P5", PadNone}, {"Screenshot button", PadScreenshot}}

func defaultPadExtraButtons() [PadExtraButtons]PadTarget {
	var out [PadExtraButtons]PadTarget
	for i, input := range PadExtraInputs {
		out[i] = input.self
	}
	return out
}

// padArcadeProInputCodes is how a combo names the button it is on: one code
// per input, in button-map order.
var padArcadeProInputCodes = [PadButtons + PadExtraButtons]PadTarget{
	PadA, PadB, PadX, PadY, PadL1, PadR1, PadL2, PadR2, PadL3, PadR3, PadSelect, PadStart,
	PadStar, PadHome, PadUp, PadDown, PadLeft, PadRight,
	PadPaddle1, PadPaddle2, padMotionP3, padMotionP4, padArcadeP5, PadScreenshot,
}

const (
	padArcadeProStar       = 12
	padArcadeProHome       = 13
	padArcadeProScreenshot = PadButtons + 1
)

// padArcadeProTargets are what any of the controller's buttons can be
// assigned: no Home, turbo or screenshot.
var padArcadeProTargets = []PadTarget{
	PadA, PadB, PadX, PadY, PadL1, PadL2, PadL3, PadR1, PadR2, PadR3, PadSelect, PadStart,
	PadUp, PadDown, PadLeft, PadRight,
	PadLSUp, PadLSDown, PadLSLeft, PadLSRight, PadRSUp, PadRSDown, PadRSLeft, PadRSRight, PadNone,
}

// PadArcadeProTargets lists what input i (indexed through PadInputs and then
// PadExtraInputs) of an Arcade Controller Pro can be assigned on a
// platform. The Star and Home buttons cannot be reassigned; the Screenshot
// button can also be Star, and on Switch take a screenshot.
func PadArcadeProTargets(i int, platform byte) []PadTarget {
	if i < 0 || i >= len(padArcadeProInputCodes) || i == padArcadeProStar || i == padArcadeProHome {
		return nil
	}
	targets := append([]PadTarget(nil), padArcadeProTargets...)
	if i == padArcadeProScreenshot {
		targets = append(targets, PadStar)
		if platform == protocol.U2PlatformSwitch {
			targets = append(targets, PadScreenshot)
		}
	}
	return targets
}

// padComboMembers are the buttons a combo can press.
var padComboMembers = []PadTarget{PadA, PadB, PadX, PadY, PadL1, PadL2, PadL3, PadR1, PadR2, PadR3, PadSelect, PadStart}

// PadComboMembers lists the buttons a combo can press.
func PadComboMembers() []PadTarget { return append([]PadTarget(nil), padComboMembers...) }

// PadCombo makes one button press several at once, in place of what the
// button map assigns it. The zero value is an unused combo.
type PadCombo struct {
	// Input is the button the combo is on, indexed through PadInputs and
	// then PadExtraInputs.
	Input int
	// Buttons are the buttons pressed, from the front; PadNone ends them.
	Buttons [PadComboButtons]PadTarget
}

// Empty reports whether the combo is unused.
func (c PadCombo) Empty() bool { return c.Buttons[0] == PadNone }

// Button light effects.
const (
	PadButtonLightCycle     byte = 0 // shown by the controller, not something to select
	PadButtonLightEcho      byte = 1 // lights a button as it is pressed
	PadButtonLightBreathing byte = 2
	PadButtonLightFixed     byte = 3
	PadButtonLightFlow      byte = 4
	PadButtonLightRipple    byte = 5
	PadButtonLightOff       byte = 6
)

// PadButtonLEDNames names the button over each light, in the order the
// tables of PadButtonLights list them.
var PadButtonLEDNames = [PadButtonLEDs]string{"P5", "Y", "B", "Up", "Left", "X", "A", "Down", "R1", "R2", "Right", "L1", "L2"}

// PadButtonLights are a slot's button lights. Every effect has its own
// table of colours and brightnesses, indexed by effect and then by light.
type PadButtonLights struct {
	// Effect is the PadButtonLight* effect in use.
	Effect byte
	// Brightness is 0 to 100; Colors are 0xRRGGBB.
	Brightness [PadButtonEffects][PadButtonLEDs]byte
	Colors     [PadButtonEffects][PadButtonLEDs]uint32
	// Speeds are 1 to 10: the second is breathing's, the third flow's and
	// the fourth ripple's. The first belongs to the cycle effect.
	Speeds [4]byte
	// EffectBrightness is 0 to 100: the first is breathing's as a whole,
	// the second the cycle effect's.
	EffectBrightness [2]byte
}

// tables is the lights without the effect in use, which is written apart.
func (l PadButtonLights) tables() PadButtonLights {
	l.Effect = 0
	return l
}

// decodePadButtonLights reads a slot's light section: flag u32, effect u8,
// 7 x 13 brightnesses, 7 x 13 colours (r, g, b), four speeds, two effect
// brightnesses and a byte this program leaves alone. The effect counts
// whatever the flag says; the tables only when it is set.
func decodePadButtonLights(section []byte) PadButtonLights {
	lights := PadButtonLights{Effect: section[4], Speeds: [4]byte{5, 5, 5, 5}, EffectBrightness: [2]byte{100, 100}}
	set := flagAt(section, 0)
	for effect := 0; effect < PadButtonEffects; effect++ {
		for led := 0; led < PadButtonLEDs; led++ {
			lights.Brightness[effect][led], lights.Colors[effect][led] = 100, 0xff0000
			if set {
				rgb := section[96+(effect*PadButtonLEDs+led)*3:]
				lights.Brightness[effect][led] = section[5+effect*PadButtonLEDs+led]
				lights.Colors[effect][led] = uint32(rgb[0])<<16 | uint32(rgb[1])<<8 | uint32(rgb[2])
			}
		}
	}
	if set {
		copy(lights.Speeds[:], section[369:373])
		copy(lights.EffectBrightness[:], section[373:375])
	}
	return lights
}

// encodeTables is the light section from its fifth byte to the last but
// one: everything but the flag, the effect and the final byte.
func (l PadButtonLights) encodeTables() []byte {
	out := make([]byte, 0, padButtonLightSection-6)
	for _, row := range l.Brightness {
		out = append(out, row[:]...)
	}
	for _, row := range l.Colors {
		for _, colour := range row {
			out = append(out, byte(colour>>16), byte(colour>>8), byte(colour))
		}
	}
	out = append(out, l.Speeds[:]...)
	return append(out, l.EffectBrightness[:]...)
}

// decodePadCombos reads a slot's combo section: flag u32, then five
// entries of flag u32, the code of the button the combo is on, and six
// codes of the buttons it presses. An entry on a button this program does
// not know is left out.
func decodePadCombos(section []byte) [PadCombos]PadCombo {
	var combos [PadCombos]PadCombo
	if !flagAt(section, 0) {
		return combos
	}
	n := 0
	for e := 0; e < PadCombos; e++ {
		entry := section[4+e*padComboEntry:][:padComboEntry]
		input := padArcadeProInput(PadTarget(binary.LittleEndian.Uint32(entry[4:])))
		if !flagAt(entry, 0) || input < 0 {
			continue
		}
		combo := PadCombo{Input: input}
		for k := range combo.Buttons {
			combo.Buttons[k] = PadTarget(binary.LittleEndian.Uint32(entry[8+k*4:]))
		}
		if !combo.Empty() {
			combos[n] = combo
			n++
		}
	}
	return combos
}

// encodePadCombos is a slot's combo section: the combos in use from the
// first entry on, the rest of it zero.
func encodePadCombos(combos [PadCombos]PadCombo) []byte {
	section := make([]byte, padComboSection)
	binary.LittleEndian.PutUint32(section, padInUse)
	n := 0
	for _, combo := range combos {
		if combo.Empty() {
			continue
		}
		entry := section[4+n*padComboEntry:][:padComboEntry]
		binary.LittleEndian.PutUint32(entry, padInUse)
		binary.LittleEndian.PutUint32(entry[4:], uint32(padArcadeProInputCodes[combo.Input]))
		for k, button := range combo.Buttons {
			binary.LittleEndian.PutUint32(entry[8+k*4:], uint32(button))
		}
		n++
	}
	return section
}

// padArcadeProInput is the input a combo's button code names, or -1. Star
// and Home cannot carry a combo.
func padArcadeProInput(code PadTarget) int {
	for i, known := range padArcadeProInputCodes {
		if known == code && i != padArcadeProStar && i != padArcadeProHome {
			return i
		}
	}
	return -1
}

func padTargetIn(list []PadTarget, target PadTarget) bool {
	for _, entry := range list {
		if entry == target {
			return true
		}
	}
	return false
}

// validateArcadeProSlot reports the first thing in slot that an Arcade
// Controller Pro could not be given. Only what differs from was is judged,
// so a value the controller already holds never stands in the way of
// changing something else.
func validateArcadeProSlot(platform byte, slot, was PadSlot) error {
	for i := range padArcadeProInputCodes {
		var input PadInput
		var target, old PadTarget
		if i < PadButtons {
			input, target, old = PadInputs[i], slot.Buttons[i], was.Buttons[i]
		} else {
			input, target, old = PadExtraInputs[i-PadButtons], slot.ExtraButtons[i-PadButtons], was.ExtraButtons[i-PadButtons]
		}
		// Putting an input back to what it does by default is always possible.
		if target == old || target == input.defaultFor(platform) || padTargetIn(PadArcadeProTargets(i, platform), target) {
			continue
		}
		return fmt.Errorf("%s cannot be assigned %s on this controller", input.Name, target)
	}

	l, w := slot.ButtonLights, was.ButtonLights
	if l.Effect != w.Effect && (l.Effect < PadButtonLightEcho || l.Effect > PadButtonLightOff) {
		return fmt.Errorf("button light effect %d is not one that can be selected (1-6)", l.Effect)
	}
	for effect := range l.Brightness {
		for led := range l.Brightness[effect] {
			if b := l.Brightness[effect][led]; b != w.Brightness[effect][led] && b > 100 {
				return fmt.Errorf("button light brightness %d is outside 0-100", b)
			}
			if c := l.Colors[effect][led]; c != w.Colors[effect][led] && c > 0xffffff {
				return fmt.Errorf("colour %#x is not 0xRRGGBB", c)
			}
		}
	}
	for i, speed := range l.Speeds {
		if speed != w.Speeds[i] && (speed < 1 || speed > 10) {
			return fmt.Errorf("button light speed %d is outside 1-10", speed)
		}
	}
	for i, b := range l.EffectBrightness {
		if b != w.EffectBrightness[i] && b > 100 {
			return fmt.Errorf("button light brightness %d is outside 0-100", b)
		}
	}

	if slot.Combos == was.Combos {
		return nil
	}
	onInput := map[int]bool{}
	for n, combo := range slot.Combos {
		if combo.Empty() {
			continue
		}
		if combo.Input < 0 || combo.Input >= len(padArcadeProInputCodes) || padArcadeProInput(padArcadeProInputCodes[combo.Input]) < 0 {
			return fmt.Errorf("combo %d is not on a button that can carry one", n+1)
		}
		if onInput[combo.Input] {
			return fmt.Errorf("combo %d is on a button that already has a combo", n+1)
		}
		onInput[combo.Input] = true
		pressed, ended := map[PadTarget]bool{}, false
		for _, button := range combo.Buttons {
			switch {
			case button == PadNone:
				ended = true
			case ended:
				return fmt.Errorf("combo %d has a gap in its buttons", n+1)
			case !padTargetIn(padComboMembers, button):
				return fmt.Errorf("combo %d cannot press %s", n+1, button)
			case pressed[button]:
				return fmt.Errorf("combo %d presses %s twice", n+1, button)
			}
			pressed[button] = true
		}
	}
	return nil
}

// validateArcadeProMacros refuses a button that both carries a combo and
// plays a macro: the vendor's software clears one when it sets the other.
func validateArcadeProMacros(slot PadSlot, macros [PadMacros]PadMacro) error {
	for _, combo := range slot.Combos {
		if combo.Empty() || combo.Input < 0 || combo.Input >= len(padArcadeProInputCodes) {
			continue
		}
		for j, macro := range macros {
			if !macro.Empty() && macro.Trigger == padArcadeProInputCodes[combo.Input] {
				return fmt.Errorf("macro %d plays from a button that carries a combo", j+1)
			}
		}
	}
	return nil
}

// beginArcadeProSync marks a configuration session as started, which the
// vendor's software does before it writes anything; the returned function
// marks it over. Reading needs neither.
func beginArcadeProSync(ctx context.Context, session *protocol.DeviceSession) (func(), error) {
	if err := session.ArcadeProSetSync(ctx, true); err != nil {
		return nil, errProtocol(err)
	}
	return func() { _ = session.ArcadeProSetSync(context.WithoutCancel(ctx), false) }, nil
}
