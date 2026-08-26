package core

// DedicatedButtonID is one of the 10 JP108 dedicated-mapping buttons.
type DedicatedButtonID int

const (
	ButtonA DedicatedButtonID = iota
	ButtonB
	ButtonK1
	ButtonK2
	ButtonK3
	ButtonK4
	ButtonK5
	ButtonK6
	ButtonK7
	ButtonK8
)

// AllDedicatedButtons lists every JP108 dedicated button in wire-index order.
var AllDedicatedButtons = []DedicatedButtonID{
	ButtonA, ButtonB, ButtonK1, ButtonK2, ButtonK3, ButtonK4, ButtonK5, ButtonK6, ButtonK7, ButtonK8,
}

// WireIndex returns the protocol byte index for this button.
func (b DedicatedButtonID) WireIndex() byte { return byte(b) }

// DedicatedButtonFromWireIndex resolves a protocol byte index back to a button.
func DedicatedButtonFromWireIndex(value byte) (DedicatedButtonID, bool) {
	if int(value) < len(AllDedicatedButtons) {
		return AllDedicatedButtons[value], true
	}
	return 0, false
}

// U2ButtonID is one of the 17 Ultimate2 core buttons (incl. d-pad).
type U2ButtonID int

const (
	U2A U2ButtonID = iota
	U2B
	U2X
	U2Y
	U2L1
	U2R1
	U2L2
	U2R2
	U2L3
	U2R3
	U2Select
	U2Start
	U2Home
	U2DPadUp
	U2DPadDown
	U2DPadLeft
	U2DPadRight
)

// AllU2Buttons lists every Ultimate2 button in wire-index order.
var AllU2Buttons = []U2ButtonID{
	U2A, U2B, U2X, U2Y, U2L1, U2R1, U2L2, U2R2, U2L3, U2R3,
	U2Select, U2Start, U2Home, U2DPadUp, U2DPadDown, U2DPadLeft, U2DPadRight,
}

// WireIndex returns the protocol byte index for this button.
func (b U2ButtonID) WireIndex() byte { return byte(b) }

// U2ButtonFromWireIndex resolves a protocol byte index back to a button.
func U2ButtonFromWireIndex(value byte) (U2ButtonID, bool) {
	if int(value) < len(AllU2Buttons) {
		return AllU2Buttons[value], true
	}
	return 0, false
}

// U2SlotID is one of the 3 Ultimate2 config slots.
type U2SlotID int

const (
	U2Slot1 U2SlotID = iota + 1
	U2Slot2
	U2Slot3
)

// WireValue returns the protocol byte value for this slot.
func (s U2SlotID) WireValue() byte { return byte(s) }

// U2SlotFromWireValue resolves a protocol byte value to a slot, defaulting
// to Slot1 for any unrecognized value (matches Rust's from_wire_value).
func U2SlotFromWireValue(value byte) U2SlotID {
	switch value {
	case 2:
		return U2Slot2
	case 3:
		return U2Slot3
	default:
		return U2Slot1
	}
}
