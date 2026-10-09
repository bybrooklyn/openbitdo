package core

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// This file is what sets a first-generation Ultimate Bluetooth Controller
// apart from the other controllers. Its record is the same kind as theirs,
// with three slots, but:
//
//   - its button map covers 20 inputs (it has no L4 or R4), so the sections
//     after the map sit earlier, and it keeps the four face buttons in the
//     order B, A, Y, X;
//   - it has no mode switch to ask about: its user says whether the Switch
//     or the XInput record is wanted;
//   - it is only ever sent its whole record;
//   - a macro step on its XInput record holds the triggers as analog values.
//
// The layout is in docs/clean-room-evidence/dossiers/6012/u2_adv.toml under
// [ultimate_bluetooth_first_generation]. None of it has been tried on a
// controller: everything here is inferred from the vendor's software, and
// what a button may be changed to is held to what that software can itself
// produce.

// padPlatformUnchosen is the chosen platform when none was given.
const padPlatformUnchosen = -1

var padUltimateBTPlatforms = []byte{protocol.U2PlatformSwitch, protocol.U2PlatformXInput}

// PadPlatformChoices lists the platforms a controller keeps a record for
// when it is its user, not the controller, who says which one to open:
// Switch and XInput on a first-generation Ultimate Bluetooth. It is nil for
// every other model, which says itself which platform it is on. A profile of
// a model with choices is read with PadReadProfileOn.
func PadPlatformChoices(addr PadAddress) []byte {
	if !padLayoutFor(addr.product()).ultimateBT {
		return nil
	}
	return append([]byte(nil), padUltimateBTPlatforms...)
}

func checkPadPlatformChoice(chosen int) error {
	if chosen == padPlatformUnchosen {
		return errInvalidState("this controller keeps one record for Switch and one for XInput and does not say which it is using; say which to open")
	}
	for _, platform := range padUltimateBTPlatforms {
		if chosen == int(platform) {
			return nil
		}
	}
	return errInvalidState("platform %d is not one this controller keeps a record for (Switch or XInput)", chosen)
}

// PadReadProfileOn reads the record a controller keeps for platform, on a
// model that leaves that choice to its user (see PadPlatformChoices).
func (c *OpenBitdoCore) PadReadProfileOn(ctx context.Context, addr PadAddress, platform byte) (PadProfile, error) {
	if PadPlatformChoices(addr) == nil {
		return PadProfile{}, errInvalidState("this controller says itself which platform it is on; there is no choosing one")
	}
	return c.padReadProfile(ctx, addr, int(platform))
}

// stored is where the button map keeps input i, and equally which input
// sits at place i of the map: on a first-generation Ultimate Bluetooth A
// and B, and X and Y, are stored the other way round.
func (l padLayout) stored(i int) int {
	if l.ultimateBT && i < 4 {
		return i ^ 1
	}
	return i
}

// defaultTarget is what input i does on platform when its slot has no button
// map. Every model's unset map holds the same values in the same places, so
// on a first-generation Ultimate Bluetooth, whose places are other inputs,
// A and B (and X and Y) trade functions on Switch and not on XInput: the
// reverse of the other controllers.
func (l padLayout) defaultTarget(i int, platform byte) PadTarget {
	return PadInputs[l.stored(i)].defaultFor(platform)
}

func (l padLayout) defaultSlot(platform byte) PadSlot {
	slot := defaultPadSlot(platform)
	for i := range slot.Buttons {
		slot.Buttons[i] = l.defaultTarget(i, platform)
	}
	return slot
}

// holdsPlatform checks that a record read after choosing a platform is that
// platform's. Where the controller was not asked which platform it is on,
// its record is the only thing that says the choice took.
func (l padLayout) holdsPlatform(record []byte, platform byte) error {
	if !l.ultimateBT {
		return nil
	}
	if got := binary.LittleEndian.Uint16(record[l.platform:]); got != uint16(platform) {
		return errInvalidState("the controller answered with the record of platform %d when asked for platform %d's", got, platform)
	}
	return nil
}

// analogMacroTriggers reports whether macro steps on this platform hold the
// triggers as analog values rather than button bits.
func (l padLayout) analogMacroTriggers(platform byte) bool {
	return l.ultimateBT && platform == protocol.U2PlatformXInput
}

// macroStorage is a macro's steps as they are sent to macro storage. The
// vendor's software sends a first-generation Ultimate Bluetooth's in whole
// reports of 32 bytes; the fill is the value storage holds once erased.
func (l padLayout) macroStorage(steps []byte) []byte {
	if !l.ultimateBT {
		return steps
	}
	for len(steps)%32 != 0 {
		steps = append(steps, 0xff)
	}
	return steps
}

const (
	padUltimateBTStar = 12
	padUltimateBTHome = 13
)

// padUltimateBTTargets are what any of the controller's buttons can be
// assigned.
var padUltimateBTTargets = []PadTarget{
	PadA, PadB, PadX, PadY, PadL1, PadL2, PadL3, PadR1, PadR2, PadR3, PadSelect, PadStart, PadHome,
	PadUp, PadDown, PadLeft, PadRight, PadNone,
}

// padUltimateBTStarTargets are what the Star button can be assigned besides:
// its own function, auto turbo and stick directions. Left Stick Left is not
// among them: the vendor's software cannot produce it for this controller.
var padUltimateBTStarTargets = []PadTarget{
	PadLSUp, PadLSDown, PadLSRight, PadRSUp, PadRSDown, PadRSLeft, PadRSRight, PadStar, PadAutoTurbo,
}

// PadUltimateBTTargets lists what input i (indexed through PadInputs) of a
// first-generation Ultimate Bluetooth can be assigned. The Home button
// cannot be reassigned, and the controller has no L4 or R4.
func PadUltimateBTTargets(i int) []PadTarget {
	if i < 0 || i >= padLayoutUltimateBT.inputs || i == padUltimateBTHome {
		return nil
	}
	targets := append([]PadTarget(nil), padUltimateBTTargets...)
	if i == padUltimateBTStar {
		targets = append(targets, padUltimateBTStarTargets...)
	}
	return targets
}

// validateUltimateBTSlot reports the first thing in slot that a
// first-generation Ultimate Bluetooth could not be given. Only what differs
// from was is judged, so a value the controller already holds never stands
// in the way of changing something else.
func validateUltimateBTSlot(platform byte, slot, was PadSlot) error {
	layout := padLayoutUltimateBT
	for i, target := range slot.Buttons {
		switch {
		case target == was.Buttons[i]:
		case i >= layout.inputs:
			return fmt.Errorf("this controller has no %s", PadInputs[i].Name)
		// Putting an input back to what it does by default is always possible.
		case target == layout.defaultTarget(i, platform) || padTargetIn(PadUltimateBTTargets(i), target):
		default:
			return fmt.Errorf("%s cannot be assigned %s on this controller", PadInputs[i].Name, target)
		}
	}
	// The vendor's software sets the two strengths on the Switch record. On
	// the XInput record it sets a range instead, except on one firmware
	// version, and this program neither models that range nor knows the
	// version.
	if platform != protocol.U2PlatformSwitch && (slot.VibrationLeft != was.VibrationLeft || slot.VibrationRight != was.VibrationRight) {
		return fmt.Errorf("vibration strength is set on this controller's Switch record only")
	}
	return nil
}
