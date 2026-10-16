package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// These tests run the actual Bubbletea program loop (Init/Update/View, real
// tea.Cmd scheduling) against an in-memory virtual terminal — there is no
// /dev/tty in this sandbox, so this is the only way to exercise the running
// program rather than just unit-testing Update() in isolation. Every
// scenario here is end-to-end: real key/nav messages in, real rendered
// frames out.
//
// tm.Output() is a one-shot draining stream, not a replayable log: each
// waitForOutput call fully drains everything currently buffered, including
// content that arrived after the match. Two waitForOutput calls in a row
// with no Send in between will only ever succeed for the first one — the
// second is looking at bytes from *after* whatever the first already
// consumed, and nothing new is being written since nothing changed. Every
// waitForOutput below is therefore preceded by something that just changed
// (construction, a Send, or a nav-channel write) since the previous one;
// where a render produces several checkable strings at once, only the most
// specific one is checked; this was found the hard way — an earlier draft
// had two adjacent checks against the same frame and the second one hung
// for the full 5s timeout every time.
//
// Mock-mode device order is always [JP108 (full), Ultimate2 (full),
// candidate device (candidate-readonly)] before the tier sort, which is
// already stable, so it stays in that order. actionsForSelectedDevice() for
// a full-tier, non-candidate device is always
// [Diagnose, Mapping Editor, Firmware Update, Settings, Quit] — the two
// index facts every scenario below relies on. The real mock device names
// (core.mockDevice) are "PID_108JP", "PID_Ultimate2", and "PID_Xcloud".

func newTeatestModel(t *testing.T, settingsPath string, width, height int) (*teatest.TestModel, chan input.NavEvent, *core.OpenBitdoCore) {
	t.Helper()
	c := core.New(core.Config{MockMode: true, ProgressIntervalMs: 1})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	navCh := make(chan input.NavEvent)
	model := NewModel(ctx, cancel, c, input.StartResult{Events: navCh}, Options{
		SettingsPath: settingsPath, Settings: defaultSettings(), MockMode: true,
	})
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(width, height))
	t.Cleanup(func() { _ = tm.Quit() })
	// WithInitialTermSize's own Send races the program's Run() goroutine
	// starting up and can be silently dropped; send it again explicitly so
	// the first frame reliably renders instead of "starting…".
	tm.Send(tea.WindowSizeMsg{Width: width, Height: height})
	return tm, navCh, c
}

func waitForOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte(substr))
	}, teatest.WithCheckInterval(10*time.Millisecond), teatest.WithDuration(5*time.Second))
}

// waitForAllOutputs checks several substrings against the *same* buffered
// frame in one WaitFor call. tm.Output() drains on each call (see the package
// doc comment above), so multiple substrings that all come from one render
// must be checked together here rather than via back-to-back waitForOutput
// calls — the second of which would see nothing new and hang for the full
// timeout.
func waitForAllOutputs(t *testing.T, tm *teatest.TestModel, substrs ...string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		for _, s := range substrs {
			if !bytes.Contains(bts, []byte(s)) {
				return false
			}
		}
		return true
	}, teatest.WithCheckInterval(10*time.Millisecond), teatest.WithDuration(5*time.Second))
}

func pressRune(tm *teatest.TestModel, r rune) {
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// TestTeatest_OverviewRendersAndDeviceKeyMoves: the app starts on the first
// device's Overview, and d moves down the device list — landing on the
// candidate-readonly device surfaces its plain-language explanation (the
// GitHub issue #15 fix), which only renders once that device is selected.
func TestTeatest_OverviewRendersAndDeviceKeyMoves(t *testing.T) {
	tm, _, _ := newTeatestModel(t, filepath.Join(t.TempDir(), "config.toml"), 100, 30)
	waitForAllOutputs(t, tm, "DEVICES", "Retro 108 Mechanical", "You can")

	pressRune(tm, 'd')
	pressRune(tm, 'd')
	waitForOutput(t, tm, "This model is recognised")
}

// TestTeatest_GamepadNavDrivesSameNavigationAsKeyboard: simulated gamepad
// events, fed through the same channel internal/input would use, reach the
// same navigation a keyboard does. Button 3 steps through devices and the
// d-pad steps through sections, so a controller alone can get everywhere.
func TestTeatest_GamepadNavDrivesSameNavigationAsKeyboard(t *testing.T) {
	tm, navCh, _ := newTeatestModel(t, filepath.Join(t.TempDir(), "config.toml"), 100, 30)
	waitForOutput(t, tm, "Retro 108 Mechanical")

	navCh <- input.NavEvent{Kind: input.EventButtonDown, Button: 3}
	navCh <- input.NavEvent{Kind: input.EventButtonDown, Button: 3}
	waitForOutput(t, tm, "This model is recognised")

	navCh <- input.NavEvent{Kind: input.EventDPadChanged, DPad: input.DirRight}
	waitForOutput(t, tm, "All checks") // only the Checks tab renders this
}

// TestTeatest_KeyboardEditorAssignsThroughThePicker: on the keyboard's
// Mapping tab, enter on a key opens the target picker, typing narrows it,
// and enter assigns. The row then shows the new target with the change mark
// ("*"), which only an actual draft change produces.
func TestTeatest_KeyboardEditorAssignsThroughThePicker(t *testing.T) {
	tm, _, _ := newTeatestModel(t, filepath.Join(t.TempDir(), "config.toml"), 100, 30)
	waitForOutput(t, tm, "Retro 108 Mechanical")

	pressRune(tm, '3') // the Mapping tab, for the keyboard selected by default
	waitForAllOutputs(t, tm, "Key mapping", "A button")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // the A button row is selected first
	waitForOutput(t, tm, "Assign A button")
	for _, r := range "f13" {
		pressRune(tm, r)
	}
	waitForOutput(t, tm, "› F13")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "*A button      → F13")
}

// TestTeatest_FirmwareIsNotOfferedIn010: firmware cannot run in v0.0.3, so
// it is listed under "Not yet" with the release's label and is not something
// the cursor can reach: walking down the "You can" list ends on the last
// available action, and enter runs that one.
func TestTeatest_FirmwareIsNotOfferedIn010(t *testing.T) {
	tm, _, _ := newTeatestModel(t, filepath.Join(t.TempDir(), "config.toml"), 100, 30)
	waitForAllOutputs(t, tm, "Not yet", "Update firmware", "Deferred in 0.0.3")

	for range 6 { // more presses than there are actions
		tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	}
	waitForOutput(t, tm, "› Remap keys")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "Key mapping") // the mapping editor, not a firmware dialog
}

// TestTeatest_SettingsTogglePersistsAcrossReload: toggling a setting writes
// it to disk, and loading that same path back (a fresh read, as a restart
// would do) reflects the persisted value. Only "Settings saved." is
// checked after the toggle (not "Advanced Mode: true" first) since that
// confirmation only renders once the async save completes, strictly after
// the toggle itself — finding it is proof both already happened.
func TestTeatest_SettingsTogglePersistsAcrossReload(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "config.toml")
	tm, _, _ := newTeatestModel(t, settingsPath, 100, 30)
	waitForOutput(t, tm, "Retro 108 Mechanical Keyboard")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}) // settings is a key, not a device action
	waitForOutput(t, tm, "› Advanced mode    off")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // toggle Advanced Mode (settingsCursor starts at 0)
	waitForOutput(t, tm, "Settings saved.")

	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("expected settings file to exist after toggling: %v", err)
	}
	loaded, warning := LoadSettings(settingsPath)
	if warning != "" {
		t.Fatalf("unexpected warning reloading persisted settings: %s", warning)
	}
	if !loaded.AdvancedMode {
		t.Fatal("expected advanced_mode=true to survive a reload from disk, matching what a restart would read")
	}
}

// TestTeatest_WriteLockForcesRecoveryAndBlocksNavigation: once a write-lock
// condition trips, Recovery takes over end-to-end through the real program
// loop — any ordinary navigation key must not escape it. The lock is
// pre-set on the model before wrapping it in teatest (equivalent to
// reaching that state via a failed mapping/firmware write mid-session);
// route()'s forced-takeover is what's under test here — proving it fires
// through the real live message loop, not just Update() called directly —
// not how the lock gets engaged in the first place, or that further
// navigation can't escape it once there (both already covered directly
// against handleMappingApplyResult/route in app_test.go; re-checking the
// "further navigation" half here would mean asserting on a render that
// bubbletea may legitimately skip writing when nothing visible changed).
func TestTeatest_WriteLockForcesRecoveryAndBlocksNavigation(t *testing.T) {
	c := core.New(core.Config{MockMode: true, ProgressIntervalMs: 1})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	navCh := make(chan input.NavEvent)
	model := NewModel(ctx, cancel, c, input.StartResult{Events: navCh}, Options{
		SettingsPath: filepath.Join(t.TempDir(), "config.toml"), Settings: defaultSettings(), MockMode: true,
	})
	model.writeLockUntilRestart = true
	model.recoveryReason = "Simulated failure for the takeover test."

	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(100, 30))
	t.Cleanup(func() { _ = tm.Quit() })
	tm.Send(tea.WindowSizeMsg{Width: 100, Height: 30}) // see newTeatestModel's comment on the same Send

	// Any ordinary key routes through route(), which must redirect to
	// Recovery before dispatching to whatever screen-specific handler.
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	waitForOutput(t, tm, "Simulated failure for the takeover test.")
}
