package tui

import (
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

// TestMappingDraft_JP108AndU2PresetsAreDistinctTables guards against the
// two preset tables being accidentally merged back into one shared list —
// JP108 targets are raw HID keyboard-usage IDs, U2 targets are the device's
// own 17 logical button-target IDs, a completely different value space.
func TestMappingDraft_JP108AndU2PresetsAreDistinctTables(t *testing.T) {
	if len(jp108Presets) != 16 {
		t.Fatalf("expected 16 JP108 presets (reducer.rs JP108_PRESETS), got %d", len(jp108Presets))
	}
	if len(u2Presets) != 17 {
		t.Fatalf("expected 17 U2 presets (reducer.rs U2_PRESETS), got %d", len(u2Presets))
	}
	if jp108Presets[0] != 0x0004 || jp108Presets[len(jp108Presets)-1] != 0x00e1 {
		t.Fatalf("JP108 preset table doesn't match reducer.rs's exact values: %#v", jp108Presets)
	}
	if u2Presets[0] != 0x0100 || u2Presets[len(u2Presets)-1] != 0x0110 {
		t.Fatalf("U2 preset table doesn't match reducer.rs's exact values: %#v", u2Presets)
	}
	if u2TargetLabel(0x0100) != "A" || u2TargetLabel(0x0110) != "DPadRight" {
		t.Fatal("u2TargetLabel doesn't match mapping_editor.rs's u2_target_label")
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
