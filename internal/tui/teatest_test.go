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

// TestTeatest_DashboardRendersAndKeyboardNavMoves: the app starts, renders
// the device dashboard, and keyboard Down actually moves the device-list
// selection — landing on the candidate-readonly device surfaces its
// plain-language tier explanation (the GitHub issue #15 fix), which can
// only render once that device is actually selected.
func TestTeatest_DashboardRendersAndKeyboardNavMoves(t *testing.T) {
	tm, _, _ := newTeatestModel(t, filepath.Join(t.TempDir(), "config.toml"), 100, 30)
	waitForOutput(t, tm, "PID_108JP") // initial frame: header, device list, and detail panel together

	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	waitForOutput(t, tm, "Not hardware-confirmed yet")
}

// TestTeatest_GamepadNavDrivesSameNavigationAsKeyboard: a simulated gamepad
// d-pad event, fed through the same channel internal/input would use,
// drives identical navigation to a keyboard arrow key. "› Diagnose" (the
// literal focus marker viewDeviceDetail only renders once the actions pane
// actually has focus) can't appear from the initial device-list-focused
// frame, so its appearance is real proof the DPad-right event moved focus.
func TestTeatest_GamepadNavDrivesSameNavigationAsKeyboard(t *testing.T) {
	tm, navCh, _ := newTeatestModel(t, filepath.Join(t.TempDir(), "config.toml"), 100, 30)
	waitForOutput(t, tm, "PID_108JP")

	navCh <- input.NavEvent{Kind: input.EventDPadChanged, DPad: input.DirDown}
	navCh <- input.NavEvent{Kind: input.EventDPadChanged, DPad: input.DirDown}
	waitForOutput(t, tm, "Not hardware-confirmed yet")

	navCh <- input.NavEvent{Kind: input.EventDPadChanged, DPad: input.DirRight}
	waitForOutput(t, tm, "› Diagnose")
}

// TestTeatest_MappingEditorPresetCycling: preset cycling in the mapping
// editor actually changes the rendered target value, using the real ported
// preset data (not the placeholder list stage 2 originally had). The
// assertion checks the exact selected-row line ("...  (←/→ to change)" is
// only ever appended to the currently-selected row), not a bare hex value —
// button B's mock-initial target is already 0x0005, so a bare-substring
// check would spuriously pass without cycling ever happening.
func TestTeatest_MappingEditorPresetCycling(t *testing.T) {
	tm, _, _ := newTeatestModel(t, filepath.Join(t.TempDir(), "config.toml"), 100, 30)
	waitForOutput(t, tm, "PID_108JP")

	tm.Send(tea.KeyMsg{Type: tea.KeyRight}) // JP108 (full support) selected by default; into actions pane
	waitForOutput(t, tm, "› Diagnose")
	tm.Send(tea.KeyMsg{Type: tea.KeyDown}) // Diagnose(0) -> Mapping Editor(1)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "0x0004  (←/→ to change)") // button A's mock-initial target, row 0 selected by default

	tm.Send(tea.KeyMsg{Type: tea.KeyRight})
	waitForOutput(t, tm, "0x0005  (←/→ to change)")
}

// TestTeatest_FirmwareFlowGatesOnRiskAckModal: triggering firmware update
// on a full-support device must show the real brick-risk acknowledgement
// modal before anything else happens; esc must cancel without starting
// anything (still on the devices screen, action cursor unmoved); and
// confirming must actually proceed through preflight to completion.
func TestTeatest_FirmwareFlowGatesOnRiskAckModal(t *testing.T) {
	tm, _, _ := newTeatestModel(t, filepath.Join(t.TempDir(), "config.toml"), 100, 30)
	waitForOutput(t, tm, "PID_108JP")

	tm.Send(tea.KeyMsg{Type: tea.KeyRight}) // JP108 selected; into actions pane at Diagnose(0)
	waitForOutput(t, tm, "› Diagnose")
	tm.Send(tea.KeyMsg{Type: tea.KeyDown}) // Diagnose(0) -> Mapping Editor(1)
	tm.Send(tea.KeyMsg{Type: tea.KeyDown}) // Mapping Editor(1) -> Firmware Update(2)
	waitForOutput(t, tm, "› Firmware Update")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "permanently brick the") // the risk-ack modal's body text (wraps before "device")

	// Cancel: no screen transition happened underneath the modal (it never
	// left screenDevices), and the action cursor is still on Firmware
	// Update(2) — pressing enter again re-opens the same modal rather than
	// needing to re-navigate, which is itself evidence nothing advanced.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForOutput(t, tm, "› Firmware Update")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "Unsafe operation acknowledgement")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // confirm the modal

	waitForOutput(t, tm, "Press enter to begin")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // begin the transfer
	waitForOutput(t, tm, "Update completed.")
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
	waitForOutput(t, tm, "PID_108JP")

	tm.Send(tea.KeyMsg{Type: tea.KeyRight}) // JP108 selected; into actions pane at Diagnose(0)
	waitForOutput(t, tm, "› Diagnose")
	tm.Send(tea.KeyMsg{Type: tea.KeyDown}) // -> Mapping Editor(1)
	tm.Send(tea.KeyMsg{Type: tea.KeyDown}) // -> Firmware Update(2)
	tm.Send(tea.KeyMsg{Type: tea.KeyDown}) // -> Settings(3)
	waitForOutput(t, tm, "› Settings")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "Advanced Mode: false")

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
