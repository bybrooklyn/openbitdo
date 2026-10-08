package core

import (
	"fmt"
	"strings"
)

// A keyboard macro is a recorded sequence of key presses, releases and
// pauses that one key plays back. A Retro 108 holds up to eight.

const (
	// KeyMacroSlots is how many macros a keyboard holds.
	KeyMacroSlots = 8
	// KeyMacroMaxSteps is the longest macro a keyboard holds.
	KeyMacroMaxSteps = 200
	// KeyMacroMaxDelay is the longest pause, and the longest interval
	// between repeats, in milliseconds.
	KeyMacroMaxDelay = 60000
	// KeyMacroForever as a repeat count plays the macro until the key is
	// pressed again.
	KeyMacroForever = 255
)

// KeyMacroStepKind is what one step of a macro does.
type KeyMacroStepKind int

const (
	// StepPress presses a key and keeps it down.
	StepPress KeyMacroStepKind = iota
	// StepRelease lets a key go.
	StepRelease
	// StepWait pauses.
	StepWait
)

// KeyMacroStep is one step of a macro.
type KeyMacroStep struct {
	Kind KeyMacroStepKind
	// Usage is the HID keyboard usage, for a press or release.
	Usage byte
	// Millis is how long to pause, for a wait.
	Millis int
}

// KeyMacro is a macro and the key that plays it.
type KeyMacro struct {
	// Key is the id of the key that plays the macro.
	Key  byte
	Name string
	// Steps are played in order.
	Steps []KeyMacroStep
	// Repeat is how many times the macro plays per key press: 1-99, or
	// KeyMacroForever.
	Repeat int
	// IntervalMillis is the pause between repeats.
	IntervalMillis int
}

// Step opcodes as the keyboard stores them: three bytes per step.
const (
	macroOpWait        byte = 0x0f
	macroOpKeyDown     byte = 0x81
	macroOpKeyUp       byte = 0x01
	macroOpModDown     byte = 0x83
	macroOpModUp       byte = 0x03
	macroForeverFlag   byte = 0x20
	macroValueHeader        = 4
	macroValueLeadByte byte = 0x01
)

func isModifierUsage(usage byte) bool { return usage >= 0xe0 && usage <= 0xe7 }

// Validate reports the first reason the keyboard could not hold the macro.
func (m KeyMacro) Validate() error {
	if len(m.Steps) == 0 {
		return fmt.Errorf("a macro needs at least one step")
	}
	steps := len(m.Steps)
	if m.hasInterval() {
		steps++ // the interval is stored as a final step
	}
	if steps > KeyMacroMaxSteps {
		return fmt.Errorf("a macro holds at most %d steps, this one has %d", KeyMacroMaxSteps, steps)
	}
	if m.Repeat != KeyMacroForever && (m.Repeat < 1 || m.Repeat > 99) {
		return fmt.Errorf("repeat count %d is not 1-99 or forever", m.Repeat)
	}
	if m.IntervalMillis < 0 || m.IntervalMillis > KeyMacroMaxDelay {
		return fmt.Errorf("interval of %d ms is outside 0-%d", m.IntervalMillis, KeyMacroMaxDelay)
	}
	down := map[byte]bool{}
	for i, step := range m.Steps {
		switch step.Kind {
		case StepWait:
			if step.Millis < 1 || step.Millis > KeyMacroMaxDelay {
				return fmt.Errorf("step %d: a pause of %d ms is outside 1-%d", i+1, step.Millis, KeyMacroMaxDelay)
			}
		case StepPress, StepRelease:
			if step.Usage == 0 {
				return fmt.Errorf("step %d names no key", i+1)
			}
			if step.Kind == StepRelease && !down[step.Usage] {
				return fmt.Errorf("step %d releases %s, which is not held at that point", i+1, keyName(step.Usage))
			}
			down[step.Usage] = step.Kind == StepPress
		default:
			return fmt.Errorf("step %d is of an unknown kind", i+1)
		}
	}
	// A key left down would stay down after the macro ends.
	for usage, held := range down {
		if held {
			return fmt.Errorf("%s is pressed and never released", keyName(usage))
		}
	}
	return nil
}

func (m KeyMacro) hasInterval() bool { return m.IntervalMillis > 0 || m.Repeat != 1 }

// encodeValue is the macro as the keyboard stores it: a four-byte header
// (a lead byte, the repeat count, the forever flag, the step count), then
// three bytes per step. The interval between repeats travels as a final
// wait step.
func (m KeyMacro) encodeValue() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	steps := append([]KeyMacroStep(nil), m.Steps...)
	if m.hasInterval() {
		steps = append(steps, KeyMacroStep{Kind: StepWait, Millis: m.IntervalMillis})
	}
	value := []byte{macroValueLeadByte, 0, 0, byte(len(steps))}
	if m.Repeat == KeyMacroForever {
		value[2] = macroForeverFlag
	} else {
		value[1] = byte(m.Repeat)
	}
	for _, step := range steps {
		switch step.Kind {
		case StepWait:
			value = append(value, macroOpWait, byte(step.Millis), byte(step.Millis>>8))
		case StepPress:
			op := macroOpKeyDown
			if isModifierUsage(step.Usage) {
				op = macroOpModDown
			}
			value = append(value, op, step.Usage, 0)
		case StepRelease:
			op := macroOpKeyUp
			if isModifierUsage(step.Usage) {
				op = macroOpModUp
			}
			value = append(value, op, step.Usage, 0)
		}
	}
	return value, nil
}

// decodeKeyMacroValue reads a stored macro value back into steps, a repeat
// count and an interval.
func decodeKeyMacroValue(value []byte) (KeyMacro, error) {
	if len(value) < macroValueHeader {
		return KeyMacro{}, fmt.Errorf("macro value is %d bytes, shorter than its header", len(value))
	}
	macro := KeyMacro{Repeat: int(value[1])}
	if value[2] == macroForeverFlag {
		macro.Repeat = KeyMacroForever
	}
	if macro.Repeat == 0 {
		macro.Repeat = 1
	}
	count := int(value[3])
	body := value[macroValueHeader:]
	if len(body) < count*3 {
		return KeyMacro{}, fmt.Errorf("macro declares %d steps but only %d bytes of steps arrived", count, len(body))
	}
	for i := 0; i < count; i++ {
		op, a, b := body[i*3], body[i*3+1], body[i*3+2]
		switch op {
		case macroOpWait:
			macro.Steps = append(macro.Steps, KeyMacroStep{Kind: StepWait, Millis: int(a) | int(b)<<8})
		case macroOpKeyDown, macroOpModDown:
			macro.Steps = append(macro.Steps, KeyMacroStep{Kind: StepPress, Usage: a})
		case macroOpKeyUp, macroOpModUp:
			macro.Steps = append(macro.Steps, KeyMacroStep{Kind: StepRelease, Usage: a})
		default:
			// Refuse rather than drop it: a macro read becomes the backup.
			return KeyMacro{}, fmt.Errorf("step %d has opcode %#02x, which is not understood", i+1, op)
		}
	}
	// A trailing wait on a repeating macro is the interval between repeats.
	if n := len(macro.Steps); n > 1 && macro.Steps[n-1].Kind == StepWait && macro.Repeat != 1 {
		macro.IntervalMillis = macro.Steps[n-1].Millis
		macro.Steps = macro.Steps[:n-1]
	}
	return macro, nil
}

// String describes a step the way a person would: "press Left Ctrl",
// "release C", "wait 50 ms".
func (s KeyMacroStep) String() string {
	switch s.Kind {
	case StepPress:
		return "press " + keyName(s.Usage)
	case StepRelease:
		return "release " + keyName(s.Usage)
	}
	return fmt.Sprintf("wait %d ms", s.Millis)
}

// Summary is a one-line description of what a macro types, for lists:
// the keys pressed, in order, with held modifiers joined by "+".
func (m KeyMacro) Summary() string {
	var parts []string
	var held []string
	for _, step := range m.Steps {
		switch {
		case step.Kind == StepPress && isModifierUsage(step.Usage):
			held = append(held, keyName(step.Usage))
		case step.Kind == StepPress:
			parts = append(parts, strings.Join(append(append([]string(nil), held...), keyName(step.Usage)), "+"))
		case step.Kind == StepRelease && isModifierUsage(step.Usage):
			for i, name := range held {
				if name == keyName(step.Usage) {
					held = append(held[:i], held[i+1:]...)
					break
				}
			}
		}
	}
	if len(parts) == 0 {
		return "(no key presses)"
	}
	return strings.Join(parts, " ")
}

// TypeKeys builds the steps that tap each usage in turn, with a short pause
// between key down and key up: the simplest useful macro.
func TypeKeys(pauseMillis int, usages ...byte) []KeyMacroStep {
	var steps []KeyMacroStep
	for _, usage := range usages {
		steps = append(steps, KeyMacroStep{Kind: StepPress, Usage: usage})
		if pauseMillis > 0 {
			steps = append(steps, KeyMacroStep{Kind: StepWait, Millis: pauseMillis})
		}
		steps = append(steps, KeyMacroStep{Kind: StepRelease, Usage: usage})
	}
	return steps
}
