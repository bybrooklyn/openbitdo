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

// dedicatedButtonNames is a small, explicit name table for the physical
// buttons, rather than Stringer codegen for 10 fixed values.
var dedicatedButtonNames = [...]string{"A", "B", "K1", "K2", "K3", "K4", "K5", "K6", "K7", "K8"}

// String returns this button's short display name (e.g. "K3"). Used by the
// Mapping Editor's row labels — before this existed,
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
