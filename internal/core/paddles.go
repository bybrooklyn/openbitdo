package core

// U2PaddleID is one of the Ultimate2's 4 back paddle inputs.
//
// Sanitized dirty-room evidence (static analysis of the official vendor
// software, cross-validated across two independent builds) describes these
// as occupying indices 18-21 of a 22-slot button-map array, each holding a
// uint32 "function" bitmask — confirmed correct by a dedicated
// reconciliation pass, see docs/clean-room-evidence/dossiers/6012/u2_core.toml,
// RESOLVED_encoding_mismatch. This type and U2Function are wired into
// U2CoreProfile/U2ButtonMapping/U2PaddleMapping and the mock/UI paths.
// Real (non-mock) protocol read/write remains hard-blocked, not because of
// this type, but because the 88-byte payload needs multi-report chunked
// transfer whose exact paging scheme isn't yet confirmed by any evidence —
// see OPEN_QUESTION_chunking_mechanism in the same dossier and
// internal/protocol's U2ReadButtonMap/U2WriteButtonMap.
type U2PaddleID int

const (
	U2Paddle1 U2PaddleID = iota
	U2Paddle2
	U2Paddle3
	U2Paddle4
)

// AllU2Paddles lists every paddle in the evidence's slot order (18-21).
var AllU2Paddles = []U2PaddleID{U2Paddle1, U2Paddle2, U2Paddle3, U2Paddle4}

var u2PaddleNames = [...]string{"Paddle1", "Paddle2", "Paddle3", "Paddle4"}

// String returns this paddle's short display name (e.g. "Paddle3").
func (p U2PaddleID) String() string {
	if int(p) >= 0 && int(p) < len(u2PaddleNames) {
		return u2PaddleNames[p]
	}
	return "?"
}

// SlotIndex returns this paddle's position in the 22-slot button-map array
// described by the dirty-room evidence (18, 19, 20, or 21) — distinct from
// WireIndex-style helpers elsewhere in this package, named differently on
// purpose: this index is not yet backed by a working protocol call, and
// giving it the same method name as U2ButtonID.WireIndex would imply it is.
func (p U2PaddleID) SlotIndex() int { return 18 + int(p) }

// U2Function is one entry in the sanitized dirty-room catalog of ~30
// bitmask constants a button/paddle slot can be bound to. Names below are
// this session's own choices, not vendor-source names (those were
// correctly excluded from the sanitized report this type is built from).
type U2Function uint32

const (
	U2FuncNone U2Function = 0

	// Core face/shoulder buttons (12 of the ~30 catalog values).
	U2FuncA U2Function = 1 << (iota - 1)
	U2FuncB
	U2FuncX
	U2FuncY
	U2FuncL1
	U2FuncR1
	U2FuncL2
	U2FuncR2
	U2FuncL3
	U2FuncR3
	U2FuncSelect
	U2FuncStart

	// D-pad: 4 cardinal directions. Evidence: the 4 diagonals are each two
	// cardinal bits OR'd together, not their own distinct catalog entries —
	// modeled below as a helper (U2FuncDPadDiagonal), not more constants.
	U2FuncDPadUp
	U2FuncDPadDown
	U2FuncDPadLeft
	U2FuncDPadRight

	// Analog stick acting as a D-pad: 8 directions (4 cardinal + 4 diagonal
	// reported as their own distinct bits here, per the evidence — unlike
	// the real D-pad's diagonals, which are OR'd cardinals).
	U2FuncStickUp
	U2FuncStickDown
	U2FuncStickLeft
	U2FuncStickRight
	U2FuncStickUpLeft
	U2FuncStickUpRight
	U2FuncStickDownLeft
	U2FuncStickDownRight

	U2FuncHome
	U2FuncMenu
	U2FuncScreenshot
	U2FuncTurboA
	U2FuncTurboB
	U2FuncButtonSwap

	// The two functions this file actually exists for.
	U2FuncActAsPaddle1
	U2FuncActAsPaddle2
)

// U2FuncDPadDiagonal returns the OR'd bitmask for a D-pad diagonal, per the
// evidence's description of how the 4 diagonals are represented (two
// cardinal-direction bits combined, not distinct catalog entries).
func U2FuncDPadDiagonal(vertical, horizontal U2Function) U2Function {
	return vertical | horizontal
}

// AssignableTo reports whether this function can legally be bound to the
// given paddle. Evidence: "act as paddle 3"/"act as paddle 4" bitmask
// values exist in the catalog (so paddles 3 and 4 can themselves be
// assigned any function), but no *other* input can ever be remapped to
// emulate paddle 3 or 4 — only U2FuncActAsPaddle1/2 are valid remap
// targets. This is a real device restriction to preserve, not an
// analysis gap to "complete."
//
// This only governs what a *button* can be remapped to say "act as paddle
// N" — it says nothing about what a paddle itself can be assigned (any
// function in the catalog, including U2FuncNone to leave it unbound).
func (f U2Function) AssignableAsPaddleTarget() bool {
	return f == U2FuncActAsPaddle1 || f == U2FuncActAsPaddle2
}
