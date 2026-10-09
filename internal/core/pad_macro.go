package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// A controller macro is a recording: a list of moments, each holding some
// buttons and stick directions for a length of time, played back when its
// trigger button is pressed. A slot holds up to four.

const (
	// PadMacros is how many macros a slot holds.
	PadMacros = protocol.U2MacrosPerSlot
	// PadMacroMaxSteps is the longest macro a controller holds.
	PadMacroMaxSteps = 200

	padMacroSection   = 216
	padMacroHeader    = 52
	padMacroStepBytes = 10
)

// PadStick is a stick direction held during a macro step.
type PadStick uint16

// Stick directions. The low byte is X (0x00 left, 0x7f centre, 0xff right)
// and the high byte Y (0x00 up, 0x7f centre, 0xff down).
const (
	PadStickCentre    PadStick = 0x7f7f
	PadStickUp        PadStick = 0x007f
	PadStickDown      PadStick = 0xff7f
	PadStickLeft      PadStick = 0x7f00
	PadStickRight     PadStick = 0x7fff
	PadStickUpLeft    PadStick = 0x0000
	PadStickUpRight   PadStick = 0x00ff
	PadStickDownLeft  PadStick = 0xff00
	PadStickDownRight PadStick = 0xffff
)

var padStickNames = []struct {
	stick PadStick
	name  string
}{
	{PadStickCentre, "centre"}, {PadStickUp, "up"}, {PadStickUpRight, "up-right"}, {PadStickRight, "right"},
	{PadStickDownRight, "down-right"}, {PadStickDown, "down"}, {PadStickDownLeft, "down-left"},
	{PadStickLeft, "left"}, {PadStickUpLeft, "up-left"},
}

// PadSticks lists the stick directions in menu order.
func PadSticks() []PadStick {
	out := make([]PadStick, len(padStickNames))
	for i, entry := range padStickNames {
		out[i] = entry.stick
	}
	return out
}

func (s PadStick) String() string {
	for _, entry := range padStickNames {
		if entry.stick == s {
			return entry.name
		}
	}
	return fmt.Sprintf("%#04x", uint16(s))
}

// PadMacroStep is one moment of a macro.
type PadMacroStep struct {
	// Millis is how long the moment lasts.
	Millis int
	// Buttons is the buttons held, as the low sixteen bits of their
	// PadTarget values (PadA | PadR1, ...).
	Buttons uint16
	Left    PadStick
	Right   PadStick
}

// padMacroStepButtons are the buttons a step can hold, in menu order.
var padMacroStepButtons = []PadTarget{PadA, PadB, PadX, PadY, PadL1, PadR1, PadL2, PadR2, PadL3, PadR3,
	PadUp, PadDown, PadLeft, PadRight, PadSelect, PadStart}

// PadMacroStepButtons lists the buttons a macro step can hold.
func PadMacroStepButtons() []PadTarget { return append([]PadTarget(nil), padMacroStepButtons...) }

// String describes a step: "A+R1 for 50 ms", "left stick up for 200 ms",
// "nothing for 30 ms".
func (s PadMacroStep) String() string {
	var parts []string
	for _, button := range padMacroStepButtons {
		if s.Buttons&uint16(button) != 0 {
			parts = append(parts, button.String())
		}
	}
	if s.Left != PadStickCentre {
		parts = append(parts, "left stick "+s.Left.String())
	}
	if s.Right != PadStickCentre {
		parts = append(parts, "right stick "+s.Right.String())
	}
	if len(parts) == 0 {
		parts = []string{"nothing"}
	}
	return fmt.Sprintf("%s for %d ms", strings.Join(parts, "+"), s.Millis)
}

// PadMacro is one recorded macro. The zero value is an empty macro slot.
type PadMacro struct {
	Name string
	// Trigger is the button that plays the macro.
	Trigger PadTarget
	Steps   []PadMacroStep
	// Repeat and IntervalMillis are stored as given: how many times the
	// macro plays and the pause between plays.
	Repeat         int
	IntervalMillis int
}

// Empty reports whether the macro slot holds nothing.
func (m PadMacro) Empty() bool { return len(m.Steps) == 0 }

// PadMacroTriggers are the buttons that can play a macro, in menu order.
var PadMacroTriggers = []PadTarget{
	PadPaddle1, PadPaddle2, padMotionP3, padMotionP4,
	PadA, PadB, PadX, PadY, PadL1, PadR1, PadL2, PadR2, PadL3, PadR3,
	PadUp, PadDown, PadLeft, PadRight, PadSelect, PadStart, PadStar,
}

func padMacroTrigger(target PadTarget) bool {
	for _, allowed := range PadMacroTriggers {
		if target == allowed {
			return true
		}
	}
	return false
}

// Validate reports the first reason a controller could not hold the macro.
func (m PadMacro) Validate() error { return m.validate(padMacroTrigger) }

// validate is Validate with the model's own idea of which buttons can play
// a macro.
func (m PadMacro) validate(canTrigger func(PadTarget) bool) error {
	if m.Empty() {
		return nil
	}
	if len(m.Steps) > PadMacroMaxSteps {
		return fmt.Errorf("a macro holds at most %d steps, this one has %d", PadMacroMaxSteps, len(m.Steps))
	}
	if !canTrigger(m.Trigger) {
		return fmt.Errorf("%s cannot play a macro", PadMotionButtonName(m.Trigger))
	}
	if _, err := encodePadName(m.Name); err != nil {
		return err
	}
	if m.Name == "" {
		return fmt.Errorf("a macro needs a name")
	}
	if m.Repeat < 0 || m.IntervalMillis < 0 || m.IntervalMillis > 60000 {
		return fmt.Errorf("repeat count or interval is out of range")
	}
	for i, step := range m.Steps {
		if step.Millis < 1 || step.Millis > 0xffff {
			return fmt.Errorf("step %d lasts %d ms, outside 1-65535", i+1, step.Millis)
		}
	}
	// The last moment must let go of everything, or it would stay held.
	if last := m.Steps[len(m.Steps)-1]; last.Buttons != 0 || last.Left != PadStickCentre || last.Right != PadStickCentre {
		return fmt.Errorf("the last step still holds something; end with a step that holds nothing")
	}
	return nil
}

func (m PadMacro) encodeSteps() []byte {
	out := make([]byte, 0, len(m.Steps)*padMacroStepBytes)
	for _, step := range m.Steps {
		out = binary.LittleEndian.AppendUint16(out, uint16(step.Millis))
		out = binary.LittleEndian.AppendUint16(out, step.Buttons)
		out = binary.LittleEndian.AppendUint16(out, 0) // analog trigger values: unused, triggers are button bits
		out = binary.LittleEndian.AppendUint16(out, uint16(step.Left))
		out = binary.LittleEndian.AppendUint16(out, uint16(step.Right))
	}
	return out
}

func decodePadMacroSteps(data []byte) []PadMacroStep {
	steps := make([]PadMacroStep, 0, len(data)/padMacroStepBytes)
	for i := 0; i+padMacroStepBytes <= len(data); i += padMacroStepBytes {
		steps = append(steps, PadMacroStep{
			Millis:  int(binary.LittleEndian.Uint16(data[i:])),
			Buttons: binary.LittleEndian.Uint16(data[i+2:]),
			Left:    PadStick(binary.LittleEndian.Uint16(data[i+6:])),
			Right:   PadStick(binary.LittleEndian.Uint16(data[i+8:])),
		})
	}
	return steps
}

// encodePadMacroSection is a slot's macro section of the record: which of
// the four macros exist, with each one's name, trigger and length.
func encodePadMacroSection(layout padLayout, platform byte, macros [PadMacros]PadMacro) []byte {
	section := make([]byte, padMacroSection)
	binary.LittleEndian.PutUint32(section, padInUse)
	for j, macro := range macros {
		header := section[8+j*padMacroHeader:][:padMacroHeader]
		if macro.Empty() {
			continue
		}
		section[4]++
		name, _ := encodePadName(macro.Name)
		copy(header, name)
		header[32] = platform
		binary.LittleEndian.PutUint16(header[34:], uint16(len(macro.Steps)))
		binary.LittleEndian.PutUint16(header[36:], uint16(j*protocol.U2MacroRegion))
		binary.LittleEndian.PutUint32(header[40:], uint32(layout.wireTrigger(macro.Trigger)))
		binary.LittleEndian.PutUint32(header[44:], uint32(macro.Repeat))
		binary.LittleEndian.PutUint32(header[48:], uint32(macro.IntervalMillis))
	}
	return section
}

// readPadMacros reads every slot's macros: headers from the record, steps
// from macro storage.
func readPadMacros(ctx context.Context, session *protocol.DeviceSession, record []byte, layout padLayout, platform byte) ([PadSlots][PadMacros]PadMacro, error) {
	var macros [PadSlots][PadMacros]PadMacro
	for slot := 0; slot < layout.slots; slot++ {
		section := record[layout.macros+slot*padMacroSection:][:padMacroSection]
		if binary.LittleEndian.Uint32(section) != padInUse {
			continue
		}
		for j := 0; j < PadMacros; j++ {
			header := section[8+j*padMacroHeader:][:padMacroHeader]
			count := int(binary.LittleEndian.Uint16(header[34:]))
			trigger := layout.wireTrigger(PadTarget(binary.LittleEndian.Uint32(header[40:])))
			if count == 0 || count > PadMacroMaxSteps || !layout.macroTrigger(trigger) {
				continue
			}
			data, err := session.U2ReadMacroData(ctx, platform, byte(slot), j*protocol.U2MacroRegion, count*padMacroStepBytes)
			if err != nil {
				return macros, fmt.Errorf("slot %d macro %d: %w", slot+1, j+1, err)
			}
			macros[slot][j] = PadMacro{
				Name: decodePadName(header[:padNameLen]), Trigger: trigger, Steps: decodePadMacroSteps(data),
				Repeat:         int(binary.LittleEndian.Uint32(header[44:])),
				IntervalMillis: int(binary.LittleEndian.Uint32(header[48:])),
			}
		}
	}
	return macros, nil
}

// writePadMacroSteps erases and rewrites the storage of each macro in slot
// that differs from was, commits, and reads the steps back.
func writePadMacroSteps(ctx context.Context, session *protocol.DeviceSession, platform byte, slot int, now, was [PadMacros]PadMacro) error {
	wrote := false
	for j := range now {
		if reflect.DeepEqual(now[j].Steps, was[j].Steps) || now[j].Empty() {
			continue
		}
		offset := j * protocol.U2MacroRegion
		if err := session.U2EraseMacroData(ctx, platform, byte(slot), offset, protocol.U2MacroRegion); err != nil {
			return fmt.Errorf("macro %d: erase: %w", j+1, err)
		}
		if err := session.U2WriteMacroData(ctx, platform, byte(slot), offset, now[j].encodeSteps()); err != nil {
			return fmt.Errorf("macro %d: %w", j+1, err)
		}
		wrote = true
	}
	if !wrote {
		return nil
	}
	if err := session.U2Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	for j := range now {
		if reflect.DeepEqual(now[j].Steps, was[j].Steps) || now[j].Empty() {
			continue
		}
		want := now[j].encodeSteps()
		got, err := session.U2ReadMacroData(ctx, platform, byte(slot), j*protocol.U2MacroRegion, len(want))
		if err != nil {
			return fmt.Errorf("macro %d: readback failed: %w", j+1, err)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("macro %d: readback mismatch: the controller did not keep the steps", j+1)
		}
	}
	return nil
}

// macroTrigger reports whether a button can play a macro on this model: an
// Arcade Controller Pro adds its fifth extra button.
func (l padLayout) macroTrigger(target PadTarget) bool {
	return padMacroTrigger(target) || (l.arcadePro() && target == padArcadeP5)
}

// wireTrigger converts a macro trigger between this program's numbering
// and the model's; the swap is its own inverse.
func (l padLayout) wireTrigger(trigger PadTarget) PadTarget {
	if l.swapPaddleTriggers {
		switch trigger {
		case PadPaddle1:
			return PadPaddle2
		case PadPaddle2:
			return PadPaddle1
		}
	}
	return trigger
}
