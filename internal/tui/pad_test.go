package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// padMapping is the controller editor with a freshly read, unconfigured
// controller on its DInput position.
func padMapping() mappingState {
	profile := core.PadProfile{Platform: protocol.U2PlatformDInput}
	for s := range profile.Slots {
		slot := &profile.Slots[s]
		for i := range slot.Buttons {
			slot.Buttons[i] = profile.DefaultTarget(i)
		}
		slot.LeftStick, slot.RightStick = core.PadRange{End: 128}, core.PadRange{End: 128}
		slot.LeftTrigger, slot.RightTrigger = core.PadRange{End: 255}, core.PadRange{End: 255}
		slot.VibrationLeft, slot.VibrationRight = 5, 5
	}
	return mappingState{
		kind:   core.KindUltimate2,
		device: core.AppDevice{Name: "PID_Ultimate2", DisplayName: "Ultimate 2", VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012}},
		pad:    padEditor{loaded: profile, draft: profile},
	}
}

func padModel(t *testing.T) Model {
	t.Helper()
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.width, m.height = 100, 30
	m.screen = screenMapping
	m.mapping = padMapping()
	return m
}

func padRowIndex(t *testing.T, label string) int {
	t.Helper()
	for i, row := range padRows {
		if row.label == label {
			return i
		}
	}
	t.Fatalf("no row labelled %q", label)
	return 0
}

func TestPadEditorShowsPaddlesFirst(t *testing.T) {
	m := padModel(t)
	plain := ansi.Strip(m.View())
	for _, want := range []string{"Controller profile", "DInput position", "1 of 3", "(the one in use)", "Back paddle P1", "(none)", "Apply Changes"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q:\n%s", want, plain)
		}
	}
	if padRows[2].label != "Back paddle P1" || padRows[5].label != "Extra button R4" || padRows[6].label != "A" {
		t.Fatalf("unexpected row order: %q %q %q", padRows[2].label, padRows[5].label, padRows[6].label)
	}
}

func TestPadEditorAssignsRangesOptionsAndNames(t *testing.T) {
	m := padModel(t)

	// A paddle through the picker; typing q is text, not quit.
	m.mapping.cursor = padRowIndex(t, "Back paddle P1")
	m = press(t, m, "enter", "left stick up")
	if !m.mapping.pad.picking || m.modal.active {
		t.Fatal("expected the picker to be open and taking text")
	}
	m = press(t, m, "enter")
	if got := m.mapping.pad.draft.Slots[0].Buttons[18]; got != core.PadLSUp {
		t.Fatalf("P1 = %v, want Left Stick Up", got)
	}

	// Ranges: a start cannot pass its end, and [ ] step by ten.
	m.mapping.cursor = padRowIndex(t, "Left stick starts at")
	m = press(t, m, "]", "right", "left", "left", "left")
	if got := m.mapping.pad.draft.Slots[0].LeftStick; got != (core.PadRange{Start: 8, End: 128}) {
		t.Fatalf("left stick = %+v", got)
	}
	m.mapping.cursor = padRowIndex(t, "Right trigger full at")
	for i := 0; i < 40; i++ {
		m = press(t, m, "[")
	}
	if got := m.mapping.pad.draft.Slots[0].RightTrigger; got != (core.PadRange{Start: 0, End: 1}) {
		t.Fatalf("a range's end must stop above its start, got %+v", got)
	}

	// Vibration stops at off; options toggle, and the d-pad swap excludes
	// the left-stick edits.
	m.mapping.cursor = padRowIndex(t, "Left vibration")
	for i := 0; i < 8; i++ {
		m = press(t, m, "left")
	}
	if m.mapping.pad.draft.Slots[0].VibrationLeft != 0 {
		t.Fatal("vibration should bottom out at off")
	}
	m.mapping.cursor = padRowIndex(t, "Invert left stick Y")
	m = press(t, m, "enter")
	m.mapping.cursor = padRowIndex(t, "Swap d-pad and left stick")
	m = press(t, m, "enter")
	if got := m.mapping.pad.draft.Slots[0].Options; got != core.PadSwapDpadStick {
		t.Fatalf("options = %#x, want only the d-pad swap", got)
	}

	// A name, on another slot; switching slots is not an edit.
	m.mapping.cursor = padRowIndex(t, "Slot")
	undo := len(m.mapping.pad.undo)
	m = press(t, m, "right")
	if m.mapping.pad.slot != 1 || len(m.mapping.pad.undo) != undo {
		t.Fatal("stepping the slot should only change which slot is shown")
	}
	m.mapping.cursor = padRowIndex(t, "Profile name")
	m = press(t, m, "enter", "quick", "enter")
	if m.mapping.pad.draft.Slots[1].Name != "quick" || m.mapping.pad.draft.Slots[0].Name != "" {
		t.Fatalf("names: %q %q", m.mapping.pad.draft.Slots[0].Name, m.mapping.pad.draft.Slots[1].Name)
	}

	// Delete puts a button back; undo and reset work on the whole draft.
	m.mapping.cursor = padRowIndex(t, "Slot")
	m = press(t, m, "left")
	m.mapping.cursor = padRowIndex(t, "Back paddle P1")
	m = press(t, m, "delete")
	if m.mapping.pad.draft.Slots[0].Buttons[18] != core.PadNone {
		t.Fatal("delete should put the paddle back to doing nothing")
	}
	m.mapping.cursor = len(padRows) + 2 // Reset
	m = press(t, m, "enter")
	if m.mapping.dirty() || !m.mapping.canUndo() {
		t.Fatal("reset should clean the draft and be undoable")
	}
}

func TestPadApplyWritesTheDraftToTheController(t *testing.T) {
	// Mock mode's controller is a simulated Ultimate 2 behind the real
	// protocol code: editor -> core -> wire frames -> committed record.
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)
	m.mockMode = true // as --mock sets it; a real controller's editor is still gated
	next, cmd := m.navigate(screenMapping, 1)
	m = drainCmds(t, next, cmd)
	if m.mapping.kind != core.KindUltimate2 || m.mapping.loading || m.mapping.err != nil {
		t.Fatalf("expected the controller profile to load: kind=%v err=%v unavailable=%q", m.mapping.kind, m.mapping.err, m.mapping.unavailable)
	}

	m.mapping.cursor = padRowIndex(t, "Back paddle P2")
	m = press(t, m, "enter", "hom", "enter") // "home" would be read as the Home key
	m.mapping.cursor = len(padRows)          // Apply
	nextModel, cmd := m.Update(*keyMsg("enter"))
	m = drainCmds(t, nextModel.(Model), cmd)
	if m.mapping.dirty() || !strings.Contains(m.mapping.statusMsg, "Applied and verified") {
		t.Fatalf("expected a verified apply, status=%q", m.mapping.statusMsg)
	}
	profile, err := c.PadReadProfile(m.ctx, m.mapping.device.VidPid)
	if err != nil || profile.Slots[profile.ActiveSlot].Buttons[19] != core.PadHome || !profile.Slots[profile.ActiveSlot].InUse {
		t.Fatalf("the controller should now hold P2 = Home: err=%v", err)
	}
}

func TestPadRowsAreClickable(t *testing.T) {
	m := padModel(t)
	row := renderedRowContaining(t, m.View(), "Extra button L4")
	next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: m.width - 30, Y: row})
	m = next.(Model)
	if padRows[m.mapping.cursor].label != "Extra button L4" {
		t.Fatalf("clicking a row should select it, got row %d", m.mapping.cursor)
	}
	m = press(t, m, "right")
	apply := renderedRowContaining(t, m.View(), "Apply Changes")
	next, cmd := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: m.width - 30, Y: apply})
	if cmd == nil || !next.(Model).mapping.applying {
		t.Fatal("clicking Apply Changes should start the apply")
	}
}

func TestRealControllerEditorNeedsAdvancedModeUntilConfirmed(t *testing.T) {
	device := core.AppDevice{SupportTier: protocol.TierFull,
		Capability: protocol.PidCapability{SupportsU2ButtonMap: true, SupportsU2SlotConfig: true}}
	if got := mappingDisabledReason(device, false, false, false); got != "button-map framing not hardware-confirmed" {
		t.Fatalf("a real controller should be gated by default, got %q", got)
	}
	if got := mappingDisabledReason(device, false, true, false); got != "" {
		t.Fatalf("advanced mode should open the editor, got %q", got)
	}
}

func TestProfileFilesSaveAndLoadInBothEditors(t *testing.T) {
	// Controller: save slot 1, change it, load the file back.
	m := padModel(t)
	m.mapping.cursor = padRowIndex(t, "Back paddle P1")
	m = press(t, m, "right") // P1 -> A
	saved := m.mapping.pad.draft.Slots[0]
	m = press(t, m, "E")
	if !strings.Contains(m.mapping.statusMsg, "Saved to ") || !strings.Contains(m.mapping.statusMsg, filepath.Join("profiles", "ultimate-2", "profile.toml")) {
		t.Fatalf("status = %q", m.mapping.statusMsg)
	}
	// A second save asks before replacing, then replaces.
	m = press(t, m, "E")
	if !strings.Contains(m.mapping.statusMsg, "already exists") {
		t.Fatalf("expected a replace prompt, got %q", m.mapping.statusMsg)
	}
	m = press(t, m, "E")
	if !strings.Contains(m.mapping.statusMsg, "Saved to ") {
		t.Fatalf("expected the second press to replace, got %q", m.mapping.statusMsg)
	}
	m = press(t, m, "right", "right", "I")
	if !m.mapping.files.open || !strings.Contains(ansi.Strip(m.View()), "profile.toml") {
		t.Fatal("I should list the saved profile")
	}
	m = press(t, m, "enter")
	got := m.mapping.pad.draft.Slots[0]
	saved.InUse = true // a loaded slot is one in use
	if got != saved || m.mapping.files.open {
		t.Fatalf("loading should restore the saved slot:\n got %+v\nwant %+v", got, saved)
	}
	if !m.mapping.canUndo() {
		t.Fatal("loading a file should be undoable")
	}

	// Keyboard: same keys, its own directory; a controller file is refused.
	k := keyboardModel(t, 100, 30)
	k.settingsPath = m.settingsPath
	k = press(t, k, "I")
	if k.mapping.files.open || !strings.Contains(k.mapping.statusMsg, "No saved profiles yet") {
		t.Fatalf("the keyboard has no saved profiles yet: %q", k.mapping.statusMsg)
	}
	k = press(t, k, "right", "E") // A button -> first target, then save
	if !strings.Contains(k.mapping.statusMsg, filepath.Join("profiles", "retro-108")) {
		t.Fatalf("status = %q", k.mapping.statusMsg)
	}
	want := k.mapping.kb.draft.Mappings[233]
	k = press(t, k, "right", "I", "enter")
	if k.mapping.kb.draft.Mappings[233] != want {
		t.Fatalf("loading should restore the saved mapping, got %+v", k.mapping.kb.draft.Mappings[233])
	}
}
