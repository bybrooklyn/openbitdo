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

// dedicatedButtonNames mirrors u2TargetLabel's approach in internal/tui for
// the physical-button (not remap-target) side: a small, explicit name table
// rather than relying on Stringer codegen for 10 fixed values.
var dedicatedButtonNames = [...]string{"A", "B", "K1", "K2", "K3", "K4", "K5", "K6", "K7", "K8"}

// String returns this button's short display name (e.g. "K3"). Used by the
// Mapping Editor's row labels and controller diagram — before this existed,
// %v formatting fell back to the raw underlying int, showing "0".."9"
// instead of a name.
func (b DedicatedButtonID) String() string {
	if int(b) >= 0 && int(b) < len(dedicatedButtonNames) {
		return dedicatedButtonNames[b]
	}
	return "?"
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

// U2ButtonID is one of the 17 named Ultimate2 core buttons (incl. d-pad).
//
// The confirmed 22-slot wire array (see U2PaddleID and
// docs/clean-room-evidence/dossiers/6012/u2_core.toml,
// RESOLVED_encoding_mismatch) has 18 core button/D-pad slots at indices
// 0-17, not 17 — this type names only 17 of them. The dirty-room evidence
// establishes that an 18th core slot exists but does not identify which
// physical input it corresponds to; rather than guess, this codebase
// leaves it unnamed. Since real button-map reads/writes are currently
// hard-blocked entirely (see internal/protocol's U2ReadButtonMap/
// U2WriteButtonMap), this has no behavioral effect today — noted here so a
// future implementer doesn't mistake "17 named buttons" for "18 confirmed
// slots, all identified."
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

// u2ButtonNames mirrors u2TargetLabel's naming exactly (internal/tui) for
// the physical-button side, since AllU2Buttons and the target-usage table
// (0x0100.."A"..0x0110.."DPadRight") name the same 17 logical positions.
var u2ButtonNames = [...]string{
	"A", "B", "X", "Y", "L1", "R1", "L2", "R2", "L3", "R3",
	"Select", "Start", "Home", "DPadUp", "DPadDown", "DPadLeft", "DPadRight",
}

// String returns this button's short display name (e.g. "L2"). Used by the
// Mapping Editor's row labels and controller diagram — before this existed,
// %v formatting fell back to the raw underlying int, showing "0".."16"
// instead of a name.
func (b U2ButtonID) String() string {
	if int(b) >= 0 && int(b) < len(u2ButtonNames) {
		return u2ButtonNames[b]
	}
	return "?"
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
