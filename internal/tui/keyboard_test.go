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

// keyboardMapping is the keyboard editor with a freshly read, unconfigured
// keyboard: no profile, nothing assigned, volume 3.
func keyboardMapping() mappingState {
	profile := core.KeyboardProfile{Mappings: map[byte]core.KeyTarget{}, Volume: 3}
	return mappingState{
		kind:   core.KindJP108,
		device: core.AppDevice{Name: "PID_108JP", DisplayName: "Retro 108", VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x5209}},
		kb:     keyboardState{loaded: cloneKeyboardProfile(profile), draft: cloneKeyboardProfile(profile)},
	}
}

func keyboardModel(t *testing.T, width, height int) Model {
	t.Helper()
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.width, m.height = width, height
	m.screen = screenMapping
	m.mapping = keyboardMapping()
	return m
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, key := range keys {
		var msg tea.KeyMsg
		if named := keyMsg(key); named.Type != tea.KeyRunes || len([]rune(key)) == 1 {
			msg = *named
		} else {
			switch key {
			case "tab":
				msg = tea.KeyMsg{Type: tea.KeyTab}
			case "backspace":
				msg = tea.KeyMsg{Type: tea.KeyBackspace}
			case "delete":
				msg = tea.KeyMsg{Type: tea.KeyDelete}
			default:
				for _, r := range key {
					next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
					m = next.(Model)
				}
				continue
			}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func TestKeyboardEditorListsEveryKeyAndSetting(t *testing.T) {
	if len(keyboardRows) != 116 {
		t.Fatalf("expected 111 keys and 5 settings, got %d rows", len(keyboardRows))
	}
	// The dedicated buttons come first, then the settings, then the rest.
	if keyboardRows[0].key.Name != "A button" || keyboardRows[10].kind != kbRowLockWin || keyboardRows[13].kind != kbRowVolume {
		t.Fatalf("unexpected row order: %+v %+v %+v", keyboardRows[0], keyboardRows[10], keyboardRows[13])
	}
	m := keyboardModel(t, 100, 30)
	plain := ansi.Strip(m.View())
	for _, want := range []string{"Key mapping", "A button", "(none)", "Apply Changes", "no profile yet"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q:\n%s", want, plain)
		}
	}
}

func TestKeyboardPickerAssignsKeysMediaMouseAndModifiers(t *testing.T) {
	m := keyboardModel(t, 100, 30)

	// A plain key, found by typing.
	m = press(t, m, "enter", "f13")
	if !m.mapping.kb.picking || !strings.Contains(ansi.Strip(m.View()), "Assign A button") {
		t.Fatal("expected the picker to be open for the A button")
	}
	m = press(t, m, "enter")
	if got := m.mapping.kb.draft.Mappings[233]; got != core.KeyTargetKeyOf(0x68) {
		t.Fatalf("A button draft = %+v, want F13", got)
	}

	// A key with a modifier: tab steps the modifier, shown as "Held with".
	m = press(t, m, "down", "enter", "tab", "tab")
	if !strings.Contains(ansi.Strip(m.View()), "Held with: Left Shift") {
		t.Fatalf("expected the modifier line to say Left Shift:\n%s", ansi.Strip(m.View()))
	}
	m = press(t, m, "1", "enter")
	if got := m.mapping.kb.draft.Mappings[232]; got != (core.KeyTarget{Kind: core.TargetKey, Modifier: 0xe1, Key: 0x1e}) {
		t.Fatalf("B button draft = %+v, want Left Shift+1", got)
	}

	// A media key and a mouse button. Typing q or ? here is text, not a shortcut.
	m = press(t, m, "down", "enter", "mute", "enter", "down", "enter", "left click", "enter")
	if m.mapping.kb.draft.Mappings[240] != (core.KeyTarget{Kind: core.TargetMedia, Media: 0x00e2}) {
		t.Fatalf("K1 draft = %+v, want Mute", m.mapping.kb.draft.Mappings[240])
	}
	if m.mapping.kb.draft.Mappings[241] != (core.KeyTarget{Kind: core.TargetMouse, Buttons: core.MouseLeft}) {
		t.Fatalf("K2 draft = %+v, want Left Click", m.mapping.kb.draft.Mappings[241])
	}
	m = press(t, m, "enter", "q?")
	if !m.mapping.kb.picking || m.mapping.kb.pickFilter != "q?" || m.modal.active {
		t.Fatal("typing in the picker must be text: q must not quit and ? must not open help")
	}
	m = press(t, m, "esc")
	if m.mapping.kb.picking || m.screen != screenMapping {
		t.Fatal("esc should close the picker and stay in the editor")
	}

	changes := m.mapping.kb.changes()
	if len(changes.Mappings) != 4 || changes.Locks != nil || changes.Volume != nil {
		t.Fatalf("expected exactly the four assignments as changes, got %+v", changes)
	}
}

func TestKeyboardSettingsRowsAndDefaults(t *testing.T) {
	m := keyboardModel(t, 100, 30)
	m.mapping.cursor = 10 // Lock Win key
	m = press(t, m, "enter", "down", "down", "right", "down", "right", "right", "right", "right")
	draft := m.mapping.kb.draft
	if !draft.Locks.WinKey || draft.Locks.AltTab || !draft.Locks.AltF4 || draft.Volume != 5 {
		t.Fatalf("unexpected settings after edits: %+v volume=%d", draft.Locks, draft.Volume)
	}
	changes := m.mapping.kb.changes()
	if changes.Locks == nil || changes.Volume == nil || *changes.Volume != 5 {
		t.Fatalf("expected lock and volume changes, got %+v", changes)
	}

	// An ordinary key reads "itself" until assigned, and delete puts it back
	// without leaving a change behind.
	m.mapping.cursor = 15
	key := keyboardRows[15].key
	if _, value, _ := m.keyboardRowText(keyboardRows[15]); value != "itself" {
		t.Fatalf("an unassigned ordinary key should read \"itself\", got %q", value)
	}
	m = press(t, m, "right")
	if _, explicit := m.mapping.kb.draft.Target(key); !explicit {
		t.Fatal("expected stepping to assign the key")
	}
	m = press(t, m, "delete")
	if _, changed := m.mapping.kb.changes().Mappings[key.ID]; changed {
		t.Fatal("a key put back to itself is not a change")
	}
}

func TestKeyboardUndoResetAndDiscard(t *testing.T) {
	m := keyboardModel(t, 100, 30)
	if m.mapping.dirty() {
		t.Fatal("a fresh draft must not be dirty")
	}
	m = press(t, m, "right")
	if !m.mapping.dirty() || !m.mapping.canUndo() {
		t.Fatal("expected an edit to make the draft dirty and undoable")
	}

	m.mapping.cursor = len(keyboardRows) + 1 // Undo
	m = press(t, m, "enter")
	if m.mapping.dirty() {
		t.Fatal("undoing the only edit should leave the draft clean")
	}

	m.mapping.cursor = 0
	m = press(t, m, "right")
	m.mapping.cursor = len(keyboardRows) + 2 // Reset
	m = press(t, m, "enter")
	if m.mapping.dirty() || !m.mapping.canUndo() {
		t.Fatal("reset should clean the draft and be undoable itself")
	}

	// Leaving with unapplied changes asks first.
	m.mapping.cursor = 0
	m = press(t, m, "right", "esc")
	if !m.modal.active || m.screen != screenMapping {
		t.Fatal("esc with unapplied changes should ask before discarding")
	}
	m = press(t, m, "enter") // Discard
	if m.screen != screenDevices {
		t.Fatalf("discard should return to Overview, got screen %v", m.screen)
	}
}

func TestKeyboardApplyWritesTheDraftToTheKeyboard(t *testing.T) {
	// Mock mode's keyboard is a simulated Retro 108 behind the real protocol
	// code, so this goes editor -> core -> wire frames -> stored mapping.
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)
	next, cmd := m.navigate(screenMapping, 0)
	m = drainCmds(t, next, cmd)
	if m.mapping.loading || m.mapping.err != nil {
		t.Fatalf("expected the keyboard profile to load: err=%v", m.mapping.err)
	}

	m = press(t, m, "enter", "f13", "enter")
	m.mapping.cursor = len(keyboardRows) // Apply
	nextModel, cmd := m.Update(*keyMsg("enter"))
	m = drainCmds(t, nextModel.(Model), cmd)
	if m.mapping.dirty() || !strings.Contains(m.mapping.statusMsg, "Applied and verified") {
		t.Fatalf("expected a verified apply, status=%q dirty=%v", m.mapping.statusMsg, m.mapping.dirty())
	}
	if !strings.Contains(m.mapping.statusMsg, "third small button") {
		t.Fatalf("expected the reminder about the profile light: %q", m.mapping.statusMsg)
	}

	profile, err := c.KeyboardReadProfile(m.ctx, m.mapping.device.VidPid)
	if err != nil || profile.Mappings[233] != core.KeyTargetKeyOf(0x68) || profile.Name == "" {
		t.Fatalf("the keyboard should now hold A = F13 in a named profile: %+v err=%v", profile, err)
	}
}

func TestKeyboardRowsAreClickable(t *testing.T) {
	m := keyboardModel(t, 100, 30)
	row := renderedRowContaining(t, m.View(), "K3")
	next, cmd := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: m.width - 30, Y: row})
	m = next.(Model)
	if cmd != nil || keyboardRows[m.mapping.cursor].key.Name != "K3" {
		t.Fatalf("clicking a key row should select it, got row %d", m.mapping.cursor)
	}

	m = press(t, m, "right") // make the draft dirty so Apply does something
	apply := renderedRowContaining(t, m.View(), "Apply Changes")
	next, cmd = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: m.width - 30, Y: apply})
	if cmd == nil || !next.(Model).mapping.applying {
		t.Fatal("clicking Apply Changes should start the apply")
	}
}

func TestKeyboardRenameAndErase(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)
	next, cmd := m.navigate(screenMapping, 0)
	m = drainCmds(t, next, cmd)

	// Assign a key and name the profile; q while typing is a letter.
	m = press(t, m, "enter", "f13", "enter")
	m.mapping.cursor = 14 // Profile name
	m = press(t, m, "enter", "quiet", "enter")
	if m.mapping.kb.draft.Name != "quiet" || m.mapping.kb.naming {
		t.Fatalf("expected the draft name to be set, got %q", m.mapping.kb.draft.Name)
	}
	m.mapping.cursor = len(keyboardRows)
	nextModel, cmd := m.Update(*keyMsg("enter"))
	m = drainCmds(t, nextModel.(Model), cmd)
	profile, err := c.KeyboardReadProfile(m.ctx, m.mapping.device.VidPid)
	if err != nil || profile.Name != "quiet" || profile.Mappings[233] != core.KeyTargetKeyOf(0x68) {
		t.Fatalf("expected a renamed profile that kept its mapping: %+v err=%v", profile, err)
	}

	// Erase asks first, then empties the keyboard and reloads the editor.
	m = press(t, m, "X")
	if !m.modal.active {
		t.Fatal("erase must ask first")
	}
	// Cancel is focused: a stray enter erases nothing.
	if !m.modal.focusCancel {
		t.Fatal("the erase prompt should start on Cancel")
	}
	m = press(t, m, "left")
	nextModel, cmd = m.Update(*keyMsg("enter"))
	m = drainCmds(t, nextModel.(Model), cmd)
	profile, err = c.KeyboardReadProfile(m.ctx, m.mapping.device.VidPid)
	if err != nil || profile.Name != "" || len(profile.Mappings) != 0 {
		t.Fatalf("expected an empty keyboard after erase: %+v err=%v", profile, err)
	}
	if len(m.mapping.kb.loaded.Mappings) != 0 || m.mapping.dirty() {
		t.Fatal("the editor should show the erased keyboard")
	}
}

func TestMacroEditorBuildsSavesAndAppliesAMacro(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)
	next, cmd := m.navigate(screenMapping, 0)
	m = drainCmds(t, next, cmd)

	// On K1: hold Left Ctrl, tap C, let go of Left Ctrl, pause, tap V.
	m.mapping.cursor = 2
	if keyboardRows[2].key.Name != "K1" {
		t.Fatalf("row 2 is %q", keyboardRows[2].key.Name)
	}
	m = press(t, m, "m")
	if !m.mapping.kb.macro.open || !strings.Contains(ansi.Strip(m.View()), "Macro on K1") {
		t.Fatal("m should open the macro editor for the selected key")
	}
	// Letters typed here are commands or search text, never shell shortcuts.
	m = press(t, m, "p", "left ctrl", "enter", "a", "c", "enter", "r", "left ctrl", "enter", "w", "right", "a", "v", "enter")
	editor := m.mapping.kb.macro
	if len(editor.macro.Steps) != 7 || editor.macro.Steps[4] != (core.KeyMacroStep{Kind: core.StepWait, Millis: 60}) {
		t.Fatalf("steps = %+v", editor.macro.Steps)
	}
	if got := editor.macro.Summary(); got != "Left Ctrl+C V" {
		t.Fatalf("summary = %q", got)
	}

	// Saving needs a name; a macro that leaves a key held cannot be saved.
	m.mapping.kb.macro.cursor = macroHeadRows + 7 // Save
	m = press(t, m, "enter")
	if !m.mapping.kb.macro.open || !strings.Contains(m.mapping.kb.macro.problem, "name") {
		t.Fatalf("saving without a name should say so, got %q", m.mapping.kb.macro.problem)
	}
	m.mapping.kb.macro.cursor = macroRowName
	m = press(t, m, "enter", "copy", "enter")
	m.mapping.kb.macro.cursor = macroRowRepeat
	m = press(t, m, "right", "right")
	m.mapping.kb.macro.cursor = macroHeadRows + 7
	m = press(t, m, "enter")
	if m.mapping.kb.macro.open {
		t.Fatalf("expected the macro to save, problem=%q", m.mapping.kb.macro.problem)
	}
	if _, value, changed := m.keyboardRowText(keyboardRows[2]); !changed || !strings.Contains(value, "macro: copy") {
		t.Fatalf("the key row should show its macro, got %q", value)
	}

	m.mapping.cursor = len(keyboardRows) // Apply
	nextModel, cmd := m.Update(*keyMsg("enter"))
	m = drainCmds(t, nextModel.(Model), cmd)
	if m.mapping.dirty() || !strings.Contains(m.mapping.statusMsg, "Applied and verified") {
		t.Fatalf("expected a verified apply, status=%q", m.mapping.statusMsg)
	}
	profile, err := c.KeyboardReadProfile(m.ctx, m.mapping.device.VidPid)
	if err != nil || profile.Macros[240].Name != "copy" || profile.Macros[240].Repeat != 3 || len(profile.Macros[240].Steps) != 7 {
		t.Fatalf("the keyboard should hold the macro on K1: %+v err=%v", profile.Macros, err)
	}

	// Assigning the key something else drops its macro.
	m.mapping.cursor = 2
	m = press(t, m, "enter", "f13", "enter")
	if _, still := m.mapping.kb.draft.Macros[240]; still {
		t.Fatal("a key with a mapping must not keep its macro")
	}
	if changes := m.mapping.kb.changes(); changes.Macros == nil || changes.Macros[240] != nil {
		t.Fatalf("expected the change set to remove K1's macro, got %+v", changes.Macros)
	}
}

func TestMacroEditorRefusesAHeldKeyAndCancelsCleanly(t *testing.T) {
	m := keyboardModel(t, 100, 30)
	m.mapping.cursor = 0
	m = press(t, m, "m", "p", "a", "enter")
	m.mapping.kb.macro.cursor = macroRowName
	m = press(t, m, "enter", "stuck", "enter")
	m.mapping.kb.macro.cursor = macroHeadRows + 1 // Save
	m = press(t, m, "enter")
	if !m.mapping.kb.macro.open || !strings.Contains(m.mapping.kb.macro.problem, "never released") {
		t.Fatalf("a macro that leaves a key down must not save: %q", m.mapping.kb.macro.problem)
	}
	m = press(t, m, "esc")
	if m.mapping.kb.macro.open || m.mapping.dirty() || m.screen != screenMapping {
		t.Fatal("esc should leave the macro editor without changing the draft")
	}
}

func TestRealKeyboardMacroEditorIsGatedUntilConfirmed(t *testing.T) {
	m := keyboardModel(t, 100, 30)
	m.core = core.New(core.Config{}) // a real keyboard, not --mock
	m = press(t, m, "m")
	if m.mapping.kb.macro.open || !strings.Contains(m.mapping.statusMsg, "advanced mode") {
		t.Fatalf("expected the macro editor to stay shut with a reason, status=%q", m.mapping.statusMsg)
	}
}
