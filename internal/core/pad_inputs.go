package core

import (
	"fmt"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// PadInput is one physical input of a controller that can be reassigned.
type PadInput struct {
	Name string
	// self is what the input does unassigned.
	self PadTarget
}

// Functions an input can be assigned, as the controller numbers them.
const (
	PadNone       PadTarget = 0x00000000
	PadStart      PadTarget = 0x00000001
	PadL3         PadTarget = 0x00000002
	PadR3         PadTarget = 0x00000004
	PadSelect     PadTarget = 0x00000008
	PadX          PadTarget = 0x00000010
	PadY          PadTarget = 0x00000020
	PadRight      PadTarget = 0x00000040
	PadLeft       PadTarget = 0x00000080
	PadDown       PadTarget = 0x00000100
	PadUp         PadTarget = 0x00000200
	PadL1         PadTarget = 0x00000400
	PadR1         PadTarget = 0x00000800
	PadB          PadTarget = 0x00001000
	PadA          PadTarget = 0x00002000
	PadL2         PadTarget = 0x00004000
	PadR2         PadTarget = 0x00008000
	PadStar       PadTarget = 0x00010000
	PadHome       PadTarget = 0x00020000
	PadScreenshot PadTarget = 0x00400000
	PadTurbo      PadTarget = 0x00800000
	PadAutoTurbo  PadTarget = 0x01000000
	PadPaddle1    PadTarget = 0x02000000
	PadPaddle2    PadTarget = 0x04000000
	PadSwap       PadTarget = 0x08000000
	PadRSUp       PadTarget = 0x08000001
	PadRSDown     PadTarget = 0x08000002
	PadRSLeft     PadTarget = 0x08000004
	PadRSRight    PadTarget = 0x08000008
	PadLSUp       PadTarget = 0x08000010
	PadLSDown     PadTarget = 0x08000020
	PadLSLeft     PadTarget = 0x08000040
	PadLSRight    PadTarget = 0x08000080
)

// PadInputs are a controller's assignable inputs, in the order its button
// map stores them.
var PadInputs = [PadButtons]PadInput{
	{"A", PadA}, {"B", PadB}, {"X", PadX}, {"Y", PadY},
	{"L1", PadL1}, {"R1", PadR1}, {"L2", PadL2}, {"R2", PadR2},
	{"L3", PadL3}, {"R3", PadR3}, {"Select", PadSelect}, {"Start", PadStart},
	{"Star", PadStar}, {"Home", PadHome},
	{"D-pad Up", PadUp}, {"D-pad Down", PadDown}, {"D-pad Left", PadLeft}, {"D-pad Right", PadRight},
	{"Back paddle P1", PadNone}, {"Back paddle P2", PadNone}, {"Extra button L4", PadNone}, {"Extra button R4", PadNone},
}

// defaultFor is what the input does with no profile on the given platform.
// On XInput the face buttons follow the Xbox layout, so A and B, and X and
// Y, trade places; on Switch the Star button takes screenshots.
func (in PadInput) defaultFor(platform byte) PadTarget {
	switch {
	case platform == protocol.U2PlatformXInput:
		switch in.self {
		case PadA:
			return PadB
		case PadB:
			return PadA
		case PadX:
			return PadY
		case PadY:
			return PadX
		}
	case platform == protocol.U2PlatformSwitch && in.self == PadStar:
		return PadScreenshot
	}
	return in.self
}

var padTargetNames = []struct {
	target PadTarget
	name   string
}{
	{PadNone, "(none)"},
	{PadA, "A"}, {PadB, "B"}, {PadX, "X"}, {PadY, "Y"},
	{PadL1, "L1"}, {PadR1, "R1"}, {PadL2, "L2"}, {PadR2, "R2"}, {PadL3, "L3"}, {PadR3, "R3"},
	{PadSelect, "Select"}, {PadStart, "Start"}, {PadHome, "Home"}, {PadStar, "Star"}, {PadScreenshot, "Screenshot"},
	{PadUp, "D-pad Up"}, {PadDown, "D-pad Down"}, {PadLeft, "D-pad Left"}, {PadRight, "D-pad Right"},
	{PadLSUp, "Left Stick Up"}, {PadLSDown, "Left Stick Down"}, {PadLSLeft, "Left Stick Left"}, {PadLSRight, "Left Stick Right"},
	{PadRSUp, "Right Stick Up"}, {PadRSDown, "Right Stick Down"}, {PadRSLeft, "Right Stick Left"}, {PadRSRight, "Right Stick Right"},
	{PadPaddle1, "Paddle P1"}, {PadPaddle2, "Paddle P2"},
	{PadTurbo, "Turbo (hold)"}, {PadAutoTurbo, "Auto Turbo"}, {PadSwap, "Swap"},
}

// PadTargets lists every function an input can be assigned, in menu order.
func PadTargets() []PadTarget {
	targets := make([]PadTarget, len(padTargetNames))
	for i, entry := range padTargetNames {
		targets[i] = entry.target
	}
	return targets
}

func padTargetKnown(target PadTarget) bool {
	for _, entry := range padTargetNames {
		if entry.target == target {
			return true
		}
	}
	return false
}

// String names a function the way a person would.
func (t PadTarget) String() string {
	for _, entry := range padTargetNames {
		if entry.target == t {
			return entry.name
		}
	}
	return fmt.Sprintf("Function %#08x", uint32(t))
}

// DefaultTarget is what input i does on this profile's platform when it has
// no assignment.
func (p PadProfile) DefaultTarget(i int) PadTarget {
	return p.layout.defaultTarget(i, p.Platform)
}
