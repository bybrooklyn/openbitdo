//go:build manual

package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

func manualReleaseGatePID(t *testing.T) uint16 {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("OPENBITDO_MANUAL_PID"))
	if raw == "" {
		t.Skip("set OPENBITDO_MANUAL_PID to the physically connected Ultimate2 PID, for example 0x6013")
	}
	value, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(raw), "0x"), 16, 16)
	if err != nil {
		t.Fatalf("parse OPENBITDO_MANUAL_PID=%q as hex PID: %v", raw, err)
	}
	return uint16(value)
}

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
// (however it completes) without crashing/hanging, and is the deferred real
// Ultimate2 mapping action blocked before a session begins. It intentionally does
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

	// Real Ultimate2 mapping is deliberately deferred for v0.0.3.
	// Activating the disabled row must stay on the dashboard and explain the
	// hardware-evidence gap; it must not open a session or attempt any write.
	var mapFrame []byte
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		if bytes.Contains(bts, []byte("button-map framing not hardware-confirmed")) {
			mapFrame = bts
			return true
		}
		return false
	}, teatest.WithCheckInterval(50*time.Millisecond), teatest.WithDuration(5*time.Second))
	t.Logf("Deferred real-mapping reason rendered without leaving the dashboard:\n%s", mapFrame)
}

func TestManualUltimate2ReleaseGateDiagnostics(t *testing.T) {
	pid := manualReleaseGatePID(t)
	target := protocol.VidPid{VID: 0x2dc8, PID: pid}
	c := core.New(core.Config{MockMode: false, ProgressIntervalMs: 5, DefaultChunkSize: 56})
	ctx := context.Background()

	devices, err := c.ListDevices(ctx)
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	found := false
	for _, device := range devices {
		if device.VidPid == target {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("required Ultimate2 pid %s is not currently enumerated", target)
	}

	diag, err := c.DiagProbe(ctx, target)
	if err != nil {
		t.Fatalf("diag probe: %v", err)
	}
	if !diag.TransportReady {
		t.Fatalf("transport_ready=false for %s", target)
	}
	var checked int
	for _, check := range diag.CommandChecks {
		if check.Confidence != protocol.EvidenceConfirmed || check.IsExperimental {
			continue
		}
		checked++
		if !check.OK {
			t.Fatalf("confirmed safe diagnostic %s failed: bytes_written=%d bytes_read=%d error=%s detail=%s", check.Command, check.BytesWritten, check.BytesRead, check.ErrorCode, check.Detail)
		}
		if check.BytesRead == 0 {
			t.Fatalf("confirmed safe diagnostic %s returned no response bytes", check.Command)
		}
	}
	if checked == 0 {
		t.Fatal("no confirmed non-experimental diagnostics were applicable; release gate cannot pass")
	}
}
