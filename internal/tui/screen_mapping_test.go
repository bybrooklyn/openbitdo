package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// TestMappingDraft_UndoAndReset ports Rust's mapping_draft_undo_and_reset:
// adjusting a mapping marks the draft dirty; Undo restores the prior value;
// adjusting again and Reset also restores to the loaded baseline.
func TestMappingDraft_UndoAndReset(t *testing.T) {
	loaded := []core.DedicatedButtonMapping{{Button: core.ButtonA, TargetHIDUsage: 0x0004}}
	m := Model{mapping: mappingState{
		kind:        core.KindJP108,
		jp108Loaded: append([]core.DedicatedButtonMapping(nil), loaded...),
		jp108Draft:  append([]core.DedicatedButtonMapping(nil), loaded...),
	}}
	m.screen = screenMapping

	if m.mapping.dirty() {
		t.Fatal("fresh draft must not be dirty")
	}

	next, _ := m.updateMapping(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if !m.mapping.dirty() {
		t.Fatal("expected draft dirty after adjusting a mapping")
	}
	if m.mapping.jp108Draft[0].TargetHIDUsage == loaded[0].TargetHIDUsage {
		t.Fatal("expected the target HID usage to actually change")
	}

	// Undo (virtual row buttonRows+1) restores the single edit.
	m.mapping.cursor = m.mapping.rowCount() - 2
	next, _ = m.updateMapping(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.mapping.dirty() {
		t.Fatal("expected draft clean after undoing the only edit")
	}
	if !equalJP108(m.mapping.jp108Draft, loaded) {
		t.Fatal("expected undo to restore the exact loaded mapping")
	}

	// Adjust again, then Reset (virtual row buttonRows+2) — also restores.
	m.mapping.cursor = 0
	next, _ = m.updateMapping(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if !m.mapping.dirty() {
		t.Fatal("expected draft dirty after a second adjustment")
	}

	m.mapping.cursor = m.mapping.rowCount() - 1
	next, _ = m.updateMapping(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.mapping.dirty() {
		t.Fatal("expected draft clean after Reset")
	}
	if !equalJP108(m.mapping.jp108Draft, loaded) {
		t.Fatal("expected reset to restore the exact loaded mapping")
	}

	// Reset itself pushed a snapshot, so it must be undoable too (mirrors
	// Rust's mapping_reset, which pushes before overwriting current).
	if !m.mapping.canUndo() {
		t.Fatal("expected Reset to be undoable, matching Rust's mapping_reset semantics")
	}
}

// TestMappingDraft_JP108PresetsTable guards the JP108 raw-HID-usage-ID
// preset table (unaffected by the U2 button-map encoding fix).
func TestMappingDraft_JP108PresetsTable(t *testing.T) {
	if len(jp108Presets) != 16 {
		t.Fatalf("expected 16 JP108 presets (reducer.rs JP108_PRESETS), got %d", len(jp108Presets))
	}
	if jp108Presets[0] != 0x0004 || jp108Presets[len(jp108Presets)-1] != 0x00e1 {
		t.Fatalf("JP108 preset table doesn't match reducer.rs's exact values: %#v", jp108Presets)
	}
}

// TestMappingDraft_U2FunctionCycleCoversWholeCatalog guards the U2 function
// cycle table against drifting out of sync with core.U2Function's catalog —
// JP108 targets are raw HID usage IDs, U2 targets are single-bit function
// bitmasks (see docs/clean-room-evidence/dossiers/6012/u2_core.toml), a
// completely different value space, so these stay separate tables.
func TestMappingDraft_U2FunctionCycleCoversWholeCatalog(t *testing.T) {
	// core.U2FuncNone plus the 32 catalog values paddles_test.go's
	// TestU2FunctionValuesAreDistinctSingleBits already locks in.
	if len(u2FunctionCycle) != 33 {
		t.Fatalf("expected 33 entries (None + 32 catalog values), got %d", len(u2FunctionCycle))
	}
	seen := make(map[core.U2Function]bool, len(u2FunctionCycle))
	for _, f := range u2FunctionCycle {
		if seen[f] {
			t.Fatalf("duplicate entry %v in u2FunctionCycle", f)
		}
		seen[f] = true
		if _, ok := u2FunctionLabels[f]; !ok {
			t.Fatalf("u2FunctionCycle entry %v has no label in u2FunctionLabels", f)
		}
	}
	if u2FunctionLabel(core.U2FuncA) != "A" || u2FunctionLabel(core.U2FuncDPadRight) != "DPad Right" {
		t.Fatal("u2FunctionLabel doesn't return the expected friendly names")
	}
	if got := u2FunctionLabel(core.U2Function(0xdeadbeef)); got != "0xdeadbeef" {
		t.Fatalf("expected hex fallback for an unknown value, got %q", got)
	}
}

func TestMappingDraft_CycleU2FunctionWrapsAndFallsBackToStart(t *testing.T) {
	if got := cycleU2Function(core.U2Function(0xdeadbeef), 0); got != u2FunctionCycle[0] {
		t.Fatalf("expected fallback to index 0 for an unrecognized value, got %v", got)
	}
	last := u2FunctionCycle[len(u2FunctionCycle)-1]
	if got := cycleU2Function(last, 1); got != u2FunctionCycle[0] {
		t.Fatalf("expected wraparound past the end, got %v", got)
	}
	if got := cycleU2Function(u2FunctionCycle[0], -1); got != last {
		t.Fatalf("expected wraparound past the start, got %v", got)
	}
}

// TestMappingDraft_CycleWrapsAndFallsBackToStart mirrors reducer.rs's
// cycle_from_table: an unrecognized current value falls back to index 0
// before applying delta, and cycling wraps around both ends of the table.
func TestMappingDraft_CycleWrapsAndFallsBackToStart(t *testing.T) {
	if got := cycleFromTable(jp108Presets, 0xffff, 0); got != jp108Presets[0] {
		t.Fatalf("expected fallback to index 0 for an unrecognized value, got 0x%04x", got)
	}
	last := jp108Presets[len(jp108Presets)-1]
	if got := cycleFromTable(jp108Presets, last, 1); got != jp108Presets[0] {
		t.Fatalf("expected wraparound past the end, got 0x%04x", got)
	}
	if got := cycleFromTable(jp108Presets, jp108Presets[0], -1); got != last {
		t.Fatalf("expected wraparound past the start, got 0x%04x", got)
	}
}

// u2TestProfile builds a minimal loaded U2CoreProfile for the unit tests
// below: 2 buttons, 2 paddles — enough to exercise button-vs-paddle row
// branching without needing the full 17/4 mock lists.
func u2TestProfile() core.U2CoreProfile {
	return core.U2CoreProfile{
		Slot: core.U2Slot1,
		Mappings: []core.U2ButtonMapping{
			{Button: core.U2A, Target: core.U2FuncA},
			{Button: core.U2B, Target: core.U2FuncB},
		},
		PaddleMappings: []core.U2PaddleMapping{
			{Paddle: core.U2Paddle1, Target: core.U2FuncNone},
			{Paddle: core.U2Paddle2, Target: core.U2FuncNone},
		},
	}
}

// TestMappingDraft_U2RowCountIncludesPaddleRows locks in rowCount's new
// shape: button rows + paddle rows + the 3 virtual action rows.
func TestMappingDraft_U2RowCountIncludesPaddleRows(t *testing.T) {
	loaded := u2TestProfile()
	s := mappingState{kind: core.KindUltimate2, u2Loaded: loaded, u2Draft: loaded}
	if got, want := s.rowCount(), 2+2+3; got != want {
		t.Fatalf("rowCount() = %d, want %d (2 buttons + 2 paddles + 3 action rows)", got, want)
	}
}

// TestMappingDraft_U2CycleAffectsCorrectRowKind proves cycleMappingCursor
// routes a button-row cursor to Mappings and a paddle-row cursor to
// PaddleMappings — the bug this test guards against is the cursor arithmetic
// silently mutating the wrong slice (or panicking) once paddle rows were
// appended after button rows.
func TestMappingDraft_U2CycleAffectsCorrectRowKind(t *testing.T) {
	loaded := u2TestProfile()
	m := Model{mapping: mappingState{kind: core.KindUltimate2, u2Loaded: loaded, u2Draft: cloneU2Profile(loaded)}}
	m.screen = screenMapping

	m.mapping.cursor = 0 // button row (U2A)
	m.cycleMappingCursor(1)
	if m.mapping.u2Draft.Mappings[0].Target == loaded.Mappings[0].Target {
		t.Fatal("expected button row 0's target to change")
	}
	if m.mapping.u2Draft.PaddleMappings[0].Target != loaded.PaddleMappings[0].Target {
		t.Fatal("cycling a button row must not touch paddle mappings")
	}

	m.mapping.cursor = 2 // first paddle row (U2Paddle1) -- index 2 = len(Mappings)
	m.cycleMappingCursor(1)
	if m.mapping.u2Draft.PaddleMappings[0].Target == loaded.PaddleMappings[0].Target {
		t.Fatal("expected paddle row 0's target to change")
	}
	if m.mapping.u2Draft.Mappings[1].Target != loaded.Mappings[1].Target {
		t.Fatal("cycling a paddle row must not touch untouched button mappings")
	}
}

// TestMappingDraft_U2DirtyDetectsPaddleOnlyChange guards equalU2Paddles/
// dirty(): a draft that only differs from Loaded in its paddle assignments
// (no button changes at all) must still be reported dirty, or Apply would
// stay permanently disabled for a paddle-only edit.
func TestMappingDraft_U2DirtyDetectsPaddleOnlyChange(t *testing.T) {
	loaded := u2TestProfile()
	s := mappingState{kind: core.KindUltimate2, u2Loaded: loaded, u2Draft: cloneU2Profile(loaded)}
	if s.dirty() {
		t.Fatal("fresh draft must not be dirty")
	}
	s.u2Draft.PaddleMappings[0].Target = core.U2FuncActAsPaddle2
	if !s.dirty() {
		t.Fatal("expected dirty() to detect a paddle-only change")
	}
}

// TestMappingDraft_U2MappingsUnavailableRendersWithoutPanicking covers the
// real-hardware state (button-map read blocked pending confirmed wire
// chunking -- see internal/protocol's U2ReadButtonMap): Mappings and
// PaddleMappings are both empty, rowCount() collapses to just the 3 action
// rows, and the view must render a clear explanation rather than panicking
// on an empty-slice index or silently looking like "no changes available"
// with no explanation.
func TestMappingDraft_U2MappingsUnavailableRendersWithoutPanicking(t *testing.T) {
	profile := core.U2CoreProfile{Slot: core.U2Slot1, MappingsUnavailable: "chunking not yet confirmed"}
	m := Model{width: 100, mapping: mappingState{
		kind: core.KindUltimate2, device: core.AppDevice{Name: "Test U2"},
		u2Loaded: profile, u2Draft: profile,
	}}
	m.screen = screenMapping

	if got, want := m.mapping.rowCount(), 3; got != want {
		t.Fatalf("rowCount() = %d, want %d (no button/paddle rows when unavailable)", got, want)
	}

	view := m.viewMapping(30)
	if !strings.Contains(view, "isn't available yet") {
		t.Fatalf("expected the view to explain why mappings are unavailable, got:\n%s", view)
	}
	if !strings.Contains(view, "chunking not yet confirmed") {
		t.Fatalf("expected the view to surface the specific reason, got:\n%s", view)
	}
	if !strings.Contains(view, "Apply Changes") {
		t.Fatal("expected the Apply/Undo/Reset action rows to still render")
	}
}

// TestTeatest_U2PaddleRemapDraftAndApply runs the real Bubbletea program
// loop (not just Update() in isolation) proving a paddle remap can actually
// be drafted and applied end to end in mock mode -- Ultimate2's real
// (non-mock) button-map write is hard-blocked pending confirmed wire
// chunking (see internal/protocol's U2WriteButtonMap), but mock mode never
// reaches that block (U2ReadCoreProfile/U2ApplyCoreProfileWithRecovery's
// MockMode branches short-circuit before ever calling it), so this covers
// the case that's actually usable today.
//
// Uses a tall (60-row) terminal deliberately, not this file's usual 30:
// the Ultimate2 Mapping Editor's 21 button/paddle rows plus diagram/action
// rows/help text exceed a 30-row terminal's available body height, and
// investigation found that once this screen's rendered content overflows
// stylePanel's Height() constraint, bubbletea's eventLoop stops picking up
// any further tm.Send messages in this test harness (reproduced with a
// bare WindowSizeMsg resend loop and zero interaction beyond the initial
// render; confirmed pre-existing and unrelated to this change via git
// stash bisection against the 17-button-only model that predates the
// paddle rows added here; confirmed the render function itself has no
// bug via 50 direct, non-bubbletea calls to viewMapping completing
// instantly). Root cause is presumably in lipgloss's height-clipping
// interacting with bubbletea's tea.WithANSICompressor() frame diffing,
// not this package's code -- not fixed here, out of scope for the button-
// map encoding fix this pass exists for, and unconfirmed whether it
// affects a real terminal (which doesn't go through teatest's virtual
// output buffer/ANSI compressor at all) or is confined to this test
// harness. Flagged for a future pass; a real fix likely means adding
// scrolling to the Mapping Editor's row list rather than just avoiding
// the trigger here.
func TestTeatest_U2PaddleRemapDraftAndApply(t *testing.T) {
	tm, _, _ := newTeatestModel(t, filepath.Join(t.TempDir(), "config.toml"), 100, 60)
	waitForOutput(t, tm, "PID_108JP")

	tm.Send(tea.KeyMsg{Type: tea.KeyDown}) // JP108 -> Ultimate2 (mock device order)
	waitForOutput(t, tm, "PID_Ultimate2")
	tm.Send(tea.KeyMsg{Type: tea.KeyRight}) // into actions pane, Diagnose(0)
	waitForOutput(t, tm, "› Diagnose")
	tm.Send(tea.KeyMsg{Type: tea.KeyDown}) // Diagnose(0) -> Mapping Editor(1)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForAllOutputs(t, tm, "Ultimate2 Core Mapping", "Paddle1")

	for range core.AllU2Buttons { // move the cursor past all 17 button rows onto Paddle1's row
		tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	}
	waitForOutput(t, tm, "› Paddle1        → (none)  (←/→ to change)")

	tm.Send(tea.KeyMsg{Type: tea.KeyRight}) // cycle Paddle1's target off "(none)"
	waitForOutput(t, tm, "› Paddle1        → A  (←/→ to change)")

	for range core.AllU2Paddles { // move the cursor down onto Apply
		tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	}
	waitForOutput(t, tm, "› Apply Changes")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "Applied and verified.")
}
