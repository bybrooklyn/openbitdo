package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// These tests cover the auto-detect/auto-diagnose/diagnostics-cache wiring:
// internal/input's hotplug poller (EventDeviceConnected/EventDeviceDisconnected)
// and internal/core's diagnostic-result cache (DiagProbeCached/DiagProbeFresh)
// both landed in prior commits with their own unit tests; what's tested here
// is this package's wiring of that API surface into Update/View — live
// gamepadConnected/device-list refresh on hotplug, the auto-diagnose sweep,
// cache-hit-skips-loading in the Diagnose action, the "r"-forces-fresh
// contract, the staleness indicator, and disconnect-while-viewing.

// drainCmdTimeout bounds how long drainCmds waits for any single cmd.
// cmdListenNav's re-arm is deliberately long-blocking -- it waits for the
// *next* channel event, forever, by design -- and every hotplug-triggered
// batch in this package includes it (see handleHotplugEvent). Bubbletea's
// real runtime just leaves that one pending in the background; a plain
// synchronous drain must not wait on it, so a cmd that doesn't produce a
// message within this window is treated as "still pending" and left alone
// rather than recursed into.
const drainCmdTimeout = 500 * time.Millisecond

// drainCmds synchronously runs cmd to completion against m, recursively
// unpacking tea.BatchMsg and feeding every resulting message back through
// Update. Bubbletea's real runtime does this asynchronously; these are unit
// tests, not teatest's real-program-loop tests, so this stands in for it
// when a handler (like handleDevicesLoaded's per-device auto-diagnose
// batch) returns a tea.Cmd that must actually run for the test to observe
// its effect.
func drainCmds(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := runCmdWithTimeout(cmd, drainCmdTimeout)
	if msg == nil {
		return m
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = drainCmds(t, m, c)
		}
		return m
	}
	next, nextCmd := m.Update(msg)
	return drainCmds(t, next.(Model), nextCmd)
}

// runCmdWithTimeout runs cmd on its own goroutine and returns its message,
// or nil if it doesn't produce one within timeout. A cmd that never returns
// (like cmdListenNav waiting on a channel nothing ever sends to again in
// these tests) leaks that one goroutine for the life of the test binary --
// harmless and standard practice for draining a long-blocking listener cmd
// in a synchronous test, same as newTeatestModel's own never-closed navCh.
func runCmdWithTimeout(cmd tea.Cmd, timeout time.Duration) tea.Msg {
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

func TestReplacePIDNote(t *testing.T) {
	notes := replacePIDNote(nil, 0x6012, "pid=0x6012: gamepad nav active")
	if len(notes) != 1 || notes[0] != "pid=0x6012: gamepad nav active" {
		t.Fatalf("expected a single fresh note, got %v", notes)
	}

	notes = replacePIDNote(notes, 0x5209, "pid=0x5209: gamepad nav active")
	if len(notes) != 2 {
		t.Fatalf("expected a different PID's note to be appended, not replace, got %v", notes)
	}

	// The core case: a disconnect for 0x6012 must replace its "active" note
	// in place, not accumulate a second entry alongside it.
	notes = replacePIDNote(notes, 0x6012, "pid=0x6012: disconnected")
	if len(notes) != 2 {
		t.Fatalf("expected replacement in place (still 2 notes), got %v", notes)
	}
	joined := strings.Join(notes, "|")
	if strings.Contains(joined, "gamepad nav active") && strings.Contains(joined, "0x6012") {
		// only fails if the stale 0x6012-active note is still present
		found := false
		for _, n := range notes {
			if n == "pid=0x6012: gamepad nav active" {
				found = true
			}
		}
		if found {
			t.Fatalf("expected the stale 0x6012 active note to be gone, got %v", notes)
		}
	}
	if !strings.Contains(joined, "0x6012: disconnected") {
		t.Fatalf("expected the new disconnected note for 0x6012, got %v", notes)
	}
	if !strings.Contains(joined, "0x5209: gamepad nav active") {
		t.Fatalf("expected the unrelated 0x5209 note untouched, got %v", notes)
	}
}

func TestHandleHotplugEvent_ConnectAndDisconnectFlipGamepadConnectedLive(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	if m.gamepadConnected() {
		t.Fatal("expected no gamepad connected at startup with no nav notes")
	}

	next, _ := m.Update(navEventMsg{event: input.NavEvent{
		Kind: input.EventDeviceConnected, SourcePID: 0x6012, Note: "pid=0x6012: gamepad nav active",
	}})
	m = next.(Model)
	if !m.gamepadConnected() {
		t.Fatal("expected EventDeviceConnected to flip gamepadConnected() live, without a restart")
	}

	next, _ = m.Update(navEventMsg{event: input.NavEvent{
		Kind: input.EventDeviceDisconnected, SourcePID: 0x6012, Note: "pid=0x6012: disconnected",
	}})
	m = next.(Model)
	if m.gamepadConnected() {
		t.Fatal("expected EventDeviceDisconnected to flip gamepadConnected() back off live")
	}
}

func TestHandleHotplugEvent_TriggersDeviceListReload(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	if len(m.devices.devices) != 0 {
		t.Fatal("expected no devices loaded yet in a fresh model")
	}

	next, cmd := m.Update(navEventMsg{event: input.NavEvent{
		Kind: input.EventDeviceConnected, SourcePID: 0x6012, Note: "pid=0x6012: gamepad nav active",
	}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected a hotplug connect event to return a non-nil cmd (nav re-arm + device reload)")
	}
	m = drainCmds(t, m, cmd)

	if len(m.devices.devices) == 0 {
		t.Fatal("expected the device list to be populated after a hotplug-triggered reload, same as a manual rescan")
	}
	_ = c
}

// loadDevicesAndDrain is like app_test.go's loadDevices, but also drains the
// auto-diagnose batch handleDevicesLoaded now returns -- deliberately a
// separate helper: loadDevices is relied on by other tests (e.g.
// TestDiagnostics_RunThenBack) to leave devices undiagnosed so their own
// manual Diagnose trigger still produces a real probe command.
func loadDevicesAndDrain(t *testing.T, m Model, c *core.OpenBitdoCore) Model {
	t.Helper()
	msg := cmdLoadDevices(m.ctx, c)()
	next, cmd := m.Update(msg)
	return drainCmds(t, next.(Model), cmd)
}

func TestHandleDevicesLoaded_AutoDiagnosesEveryUndiagnosedDevice(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)

	if len(m.devices.devices) == 0 {
		t.Fatal("expected devices to be loaded")
	}
	for _, d := range m.devices.devices {
		if !c.HasDiagnosed(d) {
			t.Fatalf("expected %s (%s) to be auto-diagnosed after devices loaded, HasDiagnosed is false", d.Name, d.VidPid)
		}
	}
}

func TestHandleDevicesLoaded_DoesNotRerunForAlreadyDiagnosedDevice(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)

	device := m.devices.devices[0]
	firstEntry, ok := c.CachedDiag(device)
	if !ok {
		t.Fatal("expected a cached entry after the first auto-diagnose sweep")
	}

	time.Sleep(2 * time.Millisecond)

	// Simulate a second load (e.g. a manual "r" rescan, or another hotplug
	// event for an unrelated device) -- the already-diagnosed device must
	// not be re-probed.
	m = loadDevicesAndDrain(t, m, c)
	secondEntry, ok := c.CachedDiag(device)
	if !ok {
		t.Fatal("expected the cache entry to still exist")
	}
	if !secondEntry.RanAt.Equal(firstEntry.RanAt) {
		t.Fatalf("expected the second devices-loaded sweep to skip an already-diagnosed device, but RanAt advanced from %v to %v",
			firstEntry.RanAt, secondEntry.RanAt)
	}
}

func TestActionDiagnose_CacheHitRendersInstantlyWithoutLoading(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevices(t, m, c) // note: the *undraining* helper -- devices start undiagnosed
	device, ok := m.devices.selected()
	if !ok {
		t.Fatal("expected a selected device")
	}
	m.devices.pane = paneActions
	m.devices.actionIdx = 0 // Diagnose

	// First trigger: cache miss, must return a real probe command.
	next, cmd := m.triggerDevicesEnter()
	m = next.(Model)
	if !m.diag.loading {
		t.Fatal("expected loading=true on a cache miss")
	}
	if cmd == nil {
		t.Fatal("expected a non-nil probe command on a cache miss")
	}
	m = drainCmds(t, m, cmd)
	if m.diag.loading {
		t.Fatal("expected loading to clear once the probe result lands")
	}
	firstRanAt := m.diag.ranAt
	if firstRanAt.IsZero() {
		t.Fatal("expected ranAt to be set after a probe completes")
	}

	// Re-enter Diagnose for the same device: must now be a cache hit --
	// instant, no loading flash, nil cmd.
	m.screen = screenDevices
	m.devices.pane = paneActions
	m.devices.actionIdx = 0
	next, cmd = m.triggerDevicesEnter()
	m = next.(Model)
	if m.diag.loading {
		t.Fatal("expected a cache hit to render without a loading state")
	}
	if cmd != nil {
		t.Fatal("expected a cache hit to need no probe command")
	}
	if !m.diag.ranAt.Equal(firstRanAt) {
		t.Fatalf("expected the cache hit to reuse the first probe's RanAt %v, got %v", firstRanAt, m.diag.ranAt)
	}
	_ = device
}

func TestScreenDiagnostics_RerunKeyForcesFreshBypassingCache(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevices(t, m, c)
	device, _ := m.devices.selected()
	m.devices.pane = paneActions
	m.devices.actionIdx = 0

	next, cmd := m.triggerDevicesEnter()
	m = next.(Model)
	m = drainCmds(t, m, cmd)
	firstRanAt := m.diag.ranAt

	time.Sleep(2 * time.Millisecond)

	next, cmd = m.updateDiagnostics(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(Model)
	if !m.diag.loading {
		t.Fatal("expected 'r' to set loading while the fresh probe runs")
	}
	if cmd == nil {
		t.Fatal("expected 'r' to return a probe command")
	}
	m = drainCmds(t, m, cmd)

	if !m.diag.ranAt.After(firstRanAt) {
		t.Fatalf("expected 'r' to force a fresh probe with a later RanAt, first=%v second=%v", firstRanAt, m.diag.ranAt)
	}
	entry, ok := c.CachedDiag(device)
	if !ok || !entry.RanAt.Equal(m.diag.ranAt) {
		t.Fatal("expected the fresh result to also replace the session cache entry")
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{200 * time.Millisecond, "just now"},
		{5 * time.Second, "5s ago"},
		{90 * time.Second, "1m ago"},
		{2 * time.Hour, "2h ago"},
	}
	for _, tc := range cases {
		if got := formatAge(tc.d); got != tc.want {
			t.Errorf("formatAge(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestViewDiagnostics_StalenessIndicatorOnlyShownOnceRanAtIsSet(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.screen = screenDiagnostics
	m.diag.device = core.AppDevice{VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, Name: "Test Device"}
	m.diag.loading = false

	view := m.viewDiagnostics(m.height)
	if strings.Contains(view, "Last run:") {
		t.Fatalf("expected no staleness indicator with a zero ranAt, got:\n%s", view)
	}

	m.diag.ranAt = time.Now().Add(-90 * time.Second)
	view = m.viewDiagnostics(m.height)
	if !strings.Contains(view, "Last run: 1m ago") {
		t.Fatalf("expected the staleness indicator once ranAt is set, got:\n%s", view)
	}
	if !strings.Contains(view, "r to rerun") {
		t.Fatalf("expected the staleness indicator to mention the force-refresh key, got:\n%s", view)
	}
}

func TestHandleHotplugEvent_DisconnectWhileViewingSameDeviceShowsRescanHint(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	device := core.AppDevice{VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, Serial: "MOCK-FULL-6012", Name: "Ultimate2"}
	m.screen = screenDiagnostics
	m.diag.device = device
	m.diag.loading = false

	next, _ := m.Update(navEventMsg{event: input.NavEvent{
		Kind: input.EventDeviceDisconnected, SourcePID: 0x6012, Serial: "MOCK-FULL-6012", Note: "pid=0x6012: disconnected",
	}})
	m = next.(Model)

	coreErr, ok := m.diag.err.(*core.Error)
	if !ok || coreErr.Kind != core.KindDeviceDisconnected {
		t.Fatalf("expected a KindDeviceDisconnected error on the currently-viewed device, got %v", m.diag.err)
	}
	view := m.viewDiagnostics(m.height)
	if !strings.Contains(view, "press r on the dashboard to rescan") {
		t.Fatalf("expected the same rescan hint an operation-level disconnect already shows, got:\n%s", view)
	}
}

func TestHandleHotplugEvent_DisconnectOfDifferentDeviceDoesNotDisturbCurrentView(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	device := core.AppDevice{VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, Serial: "MOCK-FULL-6012", Name: "Ultimate2"}
	m.screen = screenDiagnostics
	m.diag.device = device
	m.diag.loading = false
	m.diag.ranAt = time.Now()

	// A different PID disconnecting must not touch this device's view.
	next, _ := m.Update(navEventMsg{event: input.NavEvent{
		Kind: input.EventDeviceDisconnected, SourcePID: 0x5209, Serial: "MOCK-FULL-5209", Note: "pid=0x5209: disconnected",
	}})
	m = next.(Model)
	if m.diag.err != nil {
		t.Fatalf("expected an unrelated device's disconnect not to set diag.err, got %v", m.diag.err)
	}
}

func TestAutoDiagResultMsg_OnlyUpdatesLiveViewWhenLoadingSameDevice(t *testing.T) {
	device := core.AppDevice{VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, Serial: "MOCK-FULL-6012", Name: "Ultimate2"}
	otherDevice := core.AppDevice{VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x5209}, Serial: "MOCK-FULL-5209", Name: "JP108"}

	// Case 1: loading this exact device -- must update.
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.screen = screenDiagnostics
	m.diag.device = device
	m.diag.loading = true
	sentinelRanAt := time.Now()
	next, _ := m.Update(autoDiagResultMsg{device: device, ranAt: sentinelRanAt})
	m = next.(Model)
	if m.diag.loading || !m.diag.ranAt.Equal(sentinelRanAt) {
		t.Fatal("expected a background result for the device currently loading to update the live view")
	}

	// Case 2: not loading (already showing a manual/cached result) -- must not clobber it.
	m2, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m2.screen = screenDiagnostics
	m2.diag.device = device
	m2.diag.loading = false
	existingRanAt := time.Now().Add(-time.Hour)
	m2.diag.ranAt = existingRanAt
	next, _ = m2.Update(autoDiagResultMsg{device: device, ranAt: time.Now()})
	m2 = next.(Model)
	if !m2.diag.ranAt.Equal(existingRanAt) {
		t.Fatal("expected a background result to not overwrite an already-displayed result")
	}

	// Case 3: background result for a different device -- must not touch this view at all.
	m3, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m3.screen = screenDiagnostics
	m3.diag.device = device
	m3.diag.loading = true
	next, _ = m3.Update(autoDiagResultMsg{device: otherDevice, ranAt: time.Now()})
	m3 = next.(Model)
	if !m3.diag.loading {
		t.Fatal("expected a different device's background result to leave this device's loading state untouched")
	}
}
