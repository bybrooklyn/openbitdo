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
		profile.Slots[s] = core.DefaultPadSlot(profile.Platform)
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

func TestPadEditorMotionAndLights(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)
	m.mockMode = true
	next, cmd := m.navigate(screenMapping, 1)
	m = drainCmds(t, next, cmd)

	// Motion: steering a stick picks a sensible enabling button by itself.
	m.mapping.cursor = padRowIndex(t, "Motion steers")
	if value, _ := m.padRowText(padRows[m.mapping.cursor]); value != "nothing (off)" {
		t.Fatalf("motion should start off, got %q", value)
	}
	m = press(t, m, "right", "right")
	motion := m.mapping.pad.draft.Slots[0].Motion
	if motion.Target != core.PadMotionLeftStick || motion.Button != core.PadR2 {
		t.Fatalf("motion = %+v", motion)
	}
	m.mapping.cursor = padRowIndex(t, "Motion button works by")
	m = press(t, m, "enter")
	m.mapping.cursor = padRowIndex(t, "Motion sensitivity")
	for i := 0; i < 12; i++ {
		m = press(t, m, "right")
	}
	if got := m.mapping.pad.draft.Slots[0].Motion; !got.Toggle || got.Sensitivity != 10 {
		t.Fatalf("motion = %+v", got)
	}

	// Lights: the effect belongs to the controller; colours are typed as
	// hex or stepped through swatches.
	m.mapping.cursor = padRowIndex(t, "Stick lights")
	m = press(t, m, "right", "right", "right")
	if m.mapping.pad.draft.LightEffect != protocol.U2LightCustom {
		t.Fatalf("light effect = %d", m.mapping.pad.draft.LightEffect)
	}
	m.mapping.cursor = padRowIndex(t, "Left ring light 1")
	m = press(t, m, "enter", "ff8800", "enter")
	m.mapping.cursor = padRowIndex(t, "Right ring light 12")
	m = press(t, m, "right")
	lights := m.mapping.pad.draft.Slots[0].Lights
	if lights.Custom[0] != 0xff8800 || lights.Custom[23] != 0xff0000 {
		t.Fatalf("custom lights = %06x %06x", lights.Custom[0], lights.Custom[23])
	}
	m.mapping.cursor = padRowIndex(t, "Tracing colour")
	m = press(t, m, "enter", "nothex", "enter")
	if !strings.Contains(m.mapping.statusMsg, "six hex digits") || m.mapping.pad.draft.Slots[0].Lights.TracingColor != 0 {
		t.Fatalf("a bad colour should be refused with a hint, status=%q", m.mapping.statusMsg)
	}
	// The profile name still takes text after a colour was typed.
	m.mapping.cursor = padRowIndex(t, "Profile name")
	m = press(t, m, "enter", "glow", "enter")
	if m.mapping.pad.draft.Slots[0].Name != "glow" {
		t.Fatalf("name = %q", m.mapping.pad.draft.Slots[0].Name)
	}

	m.mapping.cursor = len(padRows)
	nextModel, cmd := m.Update(*keyMsg("enter"))
	m = drainCmds(t, nextModel.(Model), cmd)
	if m.mapping.dirty() || !strings.Contains(m.mapping.statusMsg, "Applied and verified") {
		t.Fatalf("expected a verified apply, status=%q", m.mapping.statusMsg)
	}
	profile, err := c.PadReadProfile(m.ctx, m.mapping.device.VidPid)
	if err != nil || profile.LightEffect != protocol.U2LightCustom || profile.Slots[0].Lights.Custom[0] != 0xff8800 ||
		profile.Slots[0].Motion.Target != core.PadMotionLeftStick || !profile.Slots[0].Motion.Toggle {
		t.Fatalf("the controller should hold the motion and light settings: %+v err=%v", profile.Slots[0].Motion, err)
	}
}

func TestPadMacroEditorBuildsSavesAndApplies(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)
	m.mockMode = true
	next, cmd := m.navigate(screenMapping, 1)
	m = drainCmds(t, next, cmd)

	m.mapping.cursor = padRowIndex(t, "Macro 2")
	m = press(t, m, "enter")
	if !m.mapping.pad.macro.open || !strings.Contains(ansi.Strip(m.View()), "Macro 2 of slot 1") {
		t.Fatal("enter on a macro row should open its editor")
	}
	// Tap A, then on the held step also hold R1 and push the left stick up,
	// and make it longer. Letters here are commands, not shell shortcuts.
	m = press(t, m, "t", "enter")
	editor := m.mapping.pad.macro
	if len(editor.macro.Steps) != 2 || editor.macro.Steps[0].Buttons != uint16(core.PadA) || editor.macro.Steps[1].Buttons != 0 {
		t.Fatalf("a tap should add a held step and a released step: %+v", editor.macro.Steps)
	}
	m.mapping.pad.macro.cursor = padMacroHeadRows // the held step
	m = press(t, m, "b", "down", "down", "down", "down", "down", "enter", "l", "right", "right")
	step := m.mapping.pad.macro.macro.Steps[0]
	if step.Buttons != uint16(core.PadA|core.PadR1) || step.Left != core.PadStickUp || step.Millis != 70 {
		t.Fatalf("held step = %+v", step)
	}

	// Saving needs a name; a macro that ends holding something is refused.
	m.mapping.pad.macro.cursor = padMacroHeadRows + 2 // Save
	m = press(t, m, "enter")
	if !m.mapping.pad.macro.open || !strings.Contains(m.mapping.pad.macro.problem, "name") {
		t.Fatalf("saving without a name should say so: %q", m.mapping.pad.macro.problem)
	}
	m.mapping.pad.macro.cursor = padMacroRowName
	m = press(t, m, "enter", "burst", "enter")
	m.mapping.pad.macro.cursor = padMacroRowTrigger
	m = press(t, m, "right") // Back paddle P2
	m.mapping.pad.macro.cursor = padMacroHeadRows + 2
	m = press(t, m, "enter")
	if m.mapping.pad.macro.open || !m.mapping.dirty() {
		t.Fatalf("expected the macro to save into the draft: %q", m.mapping.pad.macro.problem)
	}
	if value, changed := m.padRowText(padRows[padRowIndex(t, "Macro 2")]); !changed || value != "burst: Back paddle P2 plays 2 steps" {
		t.Fatalf("macro row reads %q", value)
	}

	m.mapping.cursor = len(padRows)
	nextModel, cmd := m.Update(*keyMsg("enter"))
	m = drainCmds(t, nextModel.(Model), cmd)
	if m.mapping.dirty() || !strings.Contains(m.mapping.statusMsg, "Applied and verified") {
		t.Fatalf("expected a verified apply, status=%q", m.mapping.statusMsg)
	}
	profile, err := c.PadReadProfile(m.ctx, m.mapping.device.VidPid)
	got := profile.Macros[0][1]
	if err != nil || got.Name != "burst" || got.Trigger != core.PadPaddle2 || len(got.Steps) != 2 || got.Steps[0].Left != core.PadStickUp {
		t.Fatalf("the controller should hold the macro: %+v err=%v", got, err)
	}

	// Removing it is an edit too.
	m.mapping.cursor = padRowIndex(t, "Macro 2")
	m = press(t, m, "enter")
	m.mapping.pad.macro.cursor = padMacroHeadRows + 2 + 1 // Remove This Macro
	m = press(t, m, "enter")
	if !m.mapping.pad.draft.Macros[0][1].Empty() || !m.mapping.dirty() {
		t.Fatal("removing the macro should empty it in the draft")
	}
}
