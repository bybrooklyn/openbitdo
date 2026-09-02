package core

import "testing"

func TestU2FunctionValuesAreDistinctSingleBits(t *testing.T) {
	all := []U2Function{
		U2FuncA, U2FuncB, U2FuncX, U2FuncY, U2FuncL1, U2FuncR1, U2FuncL2, U2FuncR2,
		U2FuncL3, U2FuncR3, U2FuncSelect, U2FuncStart,
		U2FuncDPadUp, U2FuncDPadDown, U2FuncDPadLeft, U2FuncDPadRight,
		U2FuncStickUp, U2FuncStickDown, U2FuncStickLeft, U2FuncStickRight,
		U2FuncStickUpLeft, U2FuncStickUpRight, U2FuncStickDownLeft, U2FuncStickDownRight,
		U2FuncHome, U2FuncMenu, U2FuncScreenshot, U2FuncTurboA, U2FuncTurboB,
		U2FuncButtonSwap, U2FuncActAsPaddle1, U2FuncActAsPaddle2,
	}
	if len(all) != 32 {
		t.Fatalf("expected 32 catalog entries (evidence says ~30), got %d", len(all))
	}
	seen := make(map[U2Function]bool, len(all))
	for _, f := range all {
		if f == 0 {
			t.Fatalf("catalog entry has zero value (collides with U2FuncNone)")
		}
		// A power of two has exactly one bit set: f & (f-1) == 0.
		if f&(f-1) != 0 {
			t.Fatalf("catalog entry %d is not a single bit", f)
		}
		if seen[f] {
			t.Fatalf("duplicate bit value %d", f)
		}
		seen[f] = true
	}
}

func TestU2FuncDPadDiagonalCombinesCardinalBits(t *testing.T) {
	diag := U2FuncDPadDiagonal(U2FuncDPadUp, U2FuncDPadRight)
	if diag&U2FuncDPadUp == 0 || diag&U2FuncDPadRight == 0 {
		t.Fatalf("diagonal %d does not contain both cardinal bits", diag)
	}
	if diag&U2FuncDPadDown != 0 || diag&U2FuncDPadLeft != 0 {
		t.Fatalf("diagonal %d unexpectedly contains an unrelated cardinal bit", diag)
	}
	if diag == U2FuncDPadUp || diag == U2FuncDPadRight {
		t.Fatalf("diagonal %d equals one of its components instead of combining them", diag)
	}
}

// TestU2FunctionAssignableAsPaddleTarget locks in the real, asymmetric
// device restriction the dirty-room evidence found: only "act as paddle 1"
// and "act as paddle 2" are valid remap targets for another input, even
// though paddles 3 and 4 can themselves be assigned any function. Every
// other catalog entry -- including, notably, DPad/stick directions and the
// paddle-3/4-*targeting* functions if the catalog ever grows to include
// them -- must be rejected as a paddle-emulation target.
func TestU2FunctionAssignableAsPaddleTarget(t *testing.T) {
	allowed := map[U2Function]bool{
		U2FuncActAsPaddle1: true,
		U2FuncActAsPaddle2: true,
	}
	all := []U2Function{
		U2FuncNone, U2FuncA, U2FuncB, U2FuncX, U2FuncY, U2FuncL1, U2FuncR1,
		U2FuncL2, U2FuncR2, U2FuncL3, U2FuncR3, U2FuncSelect, U2FuncStart,
		U2FuncDPadUp, U2FuncDPadDown, U2FuncDPadLeft, U2FuncDPadRight,
		U2FuncStickUp, U2FuncHome, U2FuncMenu, U2FuncScreenshot,
		U2FuncTurboA, U2FuncTurboB, U2FuncButtonSwap,
		U2FuncActAsPaddle1, U2FuncActAsPaddle2,
	}
	for _, f := range all {
		got := f.AssignableAsPaddleTarget()
		want := allowed[f]
		if got != want {
			t.Errorf("U2Function(%d).AssignableAsPaddleTarget() = %v, want %v", f, got, want)
		}
	}
}

func TestU2PaddleSlotIndexMatchesEvidence(t *testing.T) {
	want := map[U2PaddleID]int{
		U2Paddle1: 18, U2Paddle2: 19, U2Paddle3: 20, U2Paddle4: 21,
	}
	for p, idx := range want {
		if got := p.SlotIndex(); got != idx {
			t.Errorf("%v.SlotIndex() = %d, want %d", p, got, idx)
		}
	}
	if len(AllU2Paddles) != 4 {
		t.Fatalf("expected 4 paddles, got %d", len(AllU2Paddles))
	}
}

func TestU2PaddleString(t *testing.T) {
	if got := U2Paddle3.String(); got != "Paddle3" {
		t.Errorf("U2Paddle3.String() = %q, want %q", got, "Paddle3")
	}
	if got := U2PaddleID(99).String(); got != "?" {
		t.Errorf("out-of-range paddle String() = %q, want %q", got, "?")
	}
}
