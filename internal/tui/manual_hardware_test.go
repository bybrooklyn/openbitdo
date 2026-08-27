//go:build manual

package tui

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// TestManualRealHardwareDeviceListAndDiagnose drives the actual running
// program (real Update/View, real tea.Cmd scheduling, teatest's virtual
// terminal for capture -- no real terminal window involved) against
// whatever 8BitDo device is physically connected right now, MockMode:false.
// Gated behind -tags manual, same as internal/machid's manual tests --
// requires real, currently-connected hardware and is never run by `go test
// ./...`/CI. Run explicitly: go test ./internal/tui/... -run
// TestManualRealHardware -v -tags manual
//
// This exists to verify the whole app (not just internal/machid in
// isolation) against real hardware for the first time: does the real
// device show up on the Devices screen, does Diagnose actually complete
// (however it completes) without crashing/hanging, and does the Mapping
// Editor handle a real (non-mock) session sensibly. It intentionally does
// NOT assert success on the diagnostic checks themselves -- see
// internal/machid/machid_darwin.go's package doc: this project's actual
// Ultimate2 (PID 0x6013) writes successfully but has never been observed
// to send a protocol response to any command, a separate, already-known,
// pre-existing protocol mystery this test is not trying to solve. What
// this test verifies is that the app's UI layer handles that honestly --
// clear failure state, no crash, no hang -- not that the device answers.
func TestManualRealHardwareDeviceListAndDiagnose(t *testing.T) {
	c := core.New(core.Config{MockMode: false, ProgressIntervalMs: 5, DefaultChunkSize: 56})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	nav := input.Start(ctx)

	model := NewModel(ctx, cancel, c, nav, Options{
		SettingsPath: filepath.Join(t.TempDir(), "config.toml"), Settings: defaultSettings(), MockMode: false,
	})
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(100, 30))
	t.Cleanup(func() { _ = tm.Quit() })
	tm.Send(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Real enumeration: wait for the device list to render with something
	// other than "No devices found" -- proves Phase 1's internal/machid fix
	// reaches all the way through the real app, not just in isolation.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("PID_")) || bytes.Contains(bts, []byte("Ultimate"))
	}, teatest.WithCheckInterval(50*time.Millisecond), teatest.WithDuration(10*time.Second))
	t.Log("real device enumerated and rendered on the Devices screen")

	tm.Send(tea.KeyMsg{Type: tea.KeyRight}) // into actions pane, Diagnose(0)
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("› Diagnose"))
	}, teatest.WithCheckInterval(50*time.Millisecond), teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // run Diagnose
	// "Last run:" (the staleness indicator) only renders once m.diag.ranAt
	// is set, i.e. strictly after the real probe completes -- unlike
	// "Diagnostics"/"passed"/"failed", which can appear in the header or
	// mid-loading frame too early. Real command round-trips (3 retries
	// each, per command) take real wall time -- generous timeout, this is
	// the whole point of a manual/hardware-gated test not run in CI.
	var diagFrame []byte
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		if bytes.Contains(bts, []byte("Last run:")) {
			diagFrame = bts
			return true
		}
		return false
	}, teatest.WithCheckInterval(200*time.Millisecond), teatest.WithDuration(60*time.Second))
	t.Logf("Diagnostics screen rendered against real hardware:\n%s", diagFrame)

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc}) // back to devices
	tm.Send(tea.KeyMsg{Type: tea.KeyRight})
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("› Diagnose"))
	}, teatest.WithCheckInterval(50*time.Millisecond), teatest.WithDuration(5*time.Second))
	tm.Send(tea.KeyMsg{Type: tea.KeyDown}) // Diagnose(0) -> Mapping Editor(1)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Mapping Editor against a real, non-mock Ultimate2 session: must
	// render *something* coherent (a real screen, an honest error) within a
	// bounded time, not hang. Not asserting exact wording since which path
	// it takes (partial profile w/ MappingsUnavailable vs. a full profile-
	// read error, if U2GetCurrentSlot/U2ReadConfigSlot themselves get no
	// response either) is itself part of what this test is checking.
	// Wait for "Loading mapping…" to resolve one way or the other: either
	// "Error:" (viewMapping's m.mapping.err branch -- e.g. if
	// U2GetCurrentSlot/U2ReadConfigSlot themselves get no real response,
	// the whole profile read fails before ever reaching the deliberately-
	// blocked button-map call) or "Apply Changes" (the draft loaded
	// successfully, always the last row per commit 2888ae5).
	var mapFrame []byte
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		if bytes.Contains(bts, []byte("Error:")) || bytes.Contains(bts, []byte("Apply Changes")) {
			mapFrame = bts
			return true
		}
		return false
	}, teatest.WithCheckInterval(200*time.Millisecond), teatest.WithDuration(30*time.Second))
	t.Logf("Mapping Editor screen rendered against real hardware:\n%s", mapFrame)
}
