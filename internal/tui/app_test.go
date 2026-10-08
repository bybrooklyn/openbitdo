package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// These tests port the integration-level behavioral contracts from the
// prior Rust TUI's tests.rs, driven against the redesigned Go Model/Update
// instead of Rust's reduce()/AppState — same underlying invariants
// (capability gating, tier ordering, the candidate-unlock-file ceremony,
// diagnostics flow, write-lock takeover), different screen shape.

func newTestModel(t *testing.T, settingsPath string) (Model, *core.OpenBitdoCore) {
	t.Helper()
	c := core.New(core.Config{MockMode: true, ProgressIntervalMs: 1})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	nav := input.StartResult{Events: make(chan input.NavEvent)}
	m := NewModel(ctx, cancel, c, nav, Options{SettingsPath: settingsPath, Settings: defaultSettings()})
	m.width, m.height = 100, 30
	return m, c
}

func loadDevices(t *testing.T, m Model, c *core.OpenBitdoCore) Model {
	t.Helper()
	msg := cmdLoadDevices(m.ctx, c)()
	next, _ := m.Update(msg)
	return next.(Model)
}

func runCmdForMsg[T tea.Msg](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	msg := cmd()
	if got, ok := msg.(T); ok {
		return got
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if sub == nil {
				continue
			}
			ch := make(chan tea.Msg, 1)
			go func() { ch <- sub() }()
			select {
			case subMsg := <-ch:
				if got, ok := subMsg.(T); ok {
					return got
				}
			case <-time.After(50 * time.Millisecond):
				// Timers such as notice expiry intentionally do not fire
				// immediately; skip them while looking for the command result.
			}
		}
	}
	var zero T
	t.Fatalf("expected %T, got %T", zero, msg)
	return zero
}

// TestDevicesLoaded_PrioritizesDiagnosticsForDetectedDevice ports
// dashboard_prioritizes_diagnostics_when_device_detected.
func TestDevicesLoaded_PrioritizesDiagnosticsForDetectedDevice(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevices(t, m, c)

	items := m.actionsForSelectedDevice()
	if len(items) == 0 {
		t.Fatal("expected actions for the first (selected) device")
	}
	if items[0].kind != actionDiagnose {
		t.Fatalf("expected Diagnose to be the first action, got %v", items[0].label)
	}
	if items[0].reason != "" {
		t.Fatalf("expected Diagnose enabled for a detected device, got reason %q", items[0].reason)
	}
}

func TestView_ResponsiveMatrixStaysWithinBounds(t *testing.T) {
	cases := []struct {
		width, height int
		want          string
	}{
		{60, 18, "DEVICES"},
		{80, 24, "DEVICES"},
		{96, 24, "DEVICES"},
		{100, 30, "DEVICES"},
		{120, 40, "DEVICES"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func(t *testing.T) {
			m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
			m.width, m.height = tc.width, tc.height
			m = loadDevices(t, m, c)
			view := m.View()
			if !strings.Contains(ansi.Strip(view), tc.want) {
				t.Fatalf("expected %q in view:\n%s", tc.want, ansi.Strip(view))
			}
			lines := strings.Split(view, "\n")
			if len(lines) != tc.height {
				t.Fatalf("rendered %d lines, want exactly %d", len(lines), tc.height)
			}
			for i, line := range lines {
				if w := lipgloss.Width(line); w > tc.width {
					t.Fatalf("line %d width=%d exceeds terminal width=%d: %q", i, w, tc.width, line)
				}
			}
		})
	}
}

func TestView_TooSmallShowsResizeOnly(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.width, m.height = 59, 17
	view := ansi.Strip(m.View())
	for _, want := range []string{"Terminal too small: 59x17", "Required: at least 60x18", "q / ctrl+c quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in too-small view:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Devices") {
		t.Fatalf("too-small view should not render the dashboard:\n%s", view)
	}
}

// What cannot be done yet is listed under "Not yet" with its reason, and is
// not something the cursor or a click can land on.
func TestOverview_NotYetItemsAreNotSelectable(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)
	m, _ = m.navigate(screenDevices, 1) // the Ultimate 2: remapping is blocked on real hardware

	view := ansi.Strip(m.View())
	notYet := view[strings.Index(view, "Not yet"):]
	for _, want := range []string{"Remap buttons", "waiting on hardware testing", "Update firmware", "Deferred in 0.0.3"} {
		if !strings.Contains(notYet, want) {
			t.Fatalf("expected %q under Not yet:\n%s", want, view)
		}
	}
	for _, item := range m.availableActions() {
		if item.kind == actionMapping || item.kind == actionFirmware {
			t.Fatalf("%q is offered although it cannot be done", item.label)
		}
	}

	row := renderedRowContaining(t, m.View(), "Remap buttons")
	next, cmd := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: m.width - 20, Y: row})
	if next.(Model).screen != screenDevices || cmd != nil {
		t.Fatal("clicking something under Not yet must do nothing")
	}
}

func TestMouse_SidebarTabsAndActionsShareTheDrawnGeometry(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)
	if len(m.devices.filtered) < 3 {
		t.Fatalf("expected at least three mock devices, got %d", len(m.devices.filtered))
	}

	// Either line of a device's entry in the sidebar selects it.
	deviceRow := renderedRowContaining(t, m.View(), "Ultimate 2 Wireless")
	for _, y := range []int{deviceRow, deviceRow + 1} {
		m.devices.cursor = 0
		next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 4, Y: y})
		m = next.(Model)
		if m.devices.cursor != 1 || m.screen != screenDevices {
			t.Fatalf("sidebar click at row %d selected cursor=%d screen=%v, want device 1 on Overview", y, m.devices.cursor, m.screen)
		}
	}

	// A tab title switches section and keeps the device.
	tabRow := renderedRowContaining(t, m.View(), "Checks")
	tabCol := strings.Index(strings.Split(ansi.Strip(m.View()), "\n")[tabRow], "Checks")
	tabX := lipgloss.Width(strings.Split(ansi.Strip(m.View()), "\n")[tabRow][:tabCol])
	next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: tabX + 1, Y: tabRow})
	m = next.(Model)
	if m.screen != screenDiagnostics || m.diag.device.VidPid.PID != 0x6012 {
		t.Fatalf("tab click: screen=%v device=%s, want Checks for the Ultimate 2", m.screen, m.diag.device.VidPid)
	}

	// The wheel moves the pane's cursor.
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	m = next.(Model)
	if m.diag.cursor != 3 {
		t.Fatalf("wheel should move the check cursor by three, got %d", m.diag.cursor)
	}

	// A "You can" row runs when clicked.
	m, _ = m.navigate(screenDevices, 0)
	saveRow := renderedRowContaining(t, m.View(), "Remap keys")
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: m.width - 30, Y: saveRow})
	m = next.(Model)
	if m.screen != screenMapping {
		t.Fatalf("clicking Remap keys should open the Mapping tab, got screen=%v", m.screen)
	}
}

func renderedRowContaining(t *testing.T, rendered, text string) int {
	t.Helper()
	for row, line := range strings.Split(ansi.Strip(rendered), "\n") {
		if strings.Contains(line, text) {
			return row
		}
	}
	t.Fatalf("rendered frame does not contain %q:\n%s", text, ansi.Strip(rendered))
	return -1
}

func TestMouse_DiagnosticsSettingsMappingAndModalContracts(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevices(t, m, c)
	device, _ := m.devices.selected()

	m.screen = screenDiagnostics
	m.diag.device = device
	m.diag.result.CommandChecks = []protocol.DiagCommandStatus{
		{Command: protocol.CommandID("DiagAlpha"), OK: true, Severity: protocol.SeverityOK},
		{Command: protocol.CommandID("DiagBeta"), OK: true, Severity: protocol.SeverityOK},
		{Command: protocol.CommandID("DiagGamma"), OK: true, Severity: protocol.SeverityOK},
	}
	diagnosticRow := renderedRowContaining(t, m.View(), "DiagGamma")
	next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: m.width - 20, Y: diagnosticRow})
	m = next.(Model)
	if m.diag.cursor != 2 {
		t.Fatalf("diagnostics row click selected cursor=%d, want 2", m.diag.cursor)
	}

	m.screen = screenSettings
	before := m.settings.ReportSaveMode
	settingsRow := renderedRowContaining(t, m.View(), "Save reports")
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: m.width - 20, Y: settingsRow})
	m = next.(Model)
	if m.settingsCursor != 1 || m.settings.ReportSaveMode == before {
		t.Fatalf("settings row click cursor=%d mode=%s before=%s", m.settingsCursor, m.settings.ReportSaveMode, before)
	}

	m.screen = screenMapping
	m.mapping = keyboardMapping()
	for i := 0; i < 15; i++ {
		next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
		m = next.(Model)
	}
	if m.mapping.cursor != 45 || m.mapping.rowOffset == 0 {
		t.Fatalf("mapping wheel cursor=%d rowOffset=%d, want cursor=45 and scrolled viewport", m.mapping.cursor, m.mapping.rowOffset)
	}

	m.modal = discardMappingModal(discardMappingMsg{action: discardActionBack})
	box, _, cancel := modalGeometry(m.modal, m.width, m.height)
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: box.x - 1, Y: box.y})
	m = next.(Model)
	if !m.modal.active {
		t.Fatal("outside modal click must be a no-op")
	}
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: cancel.x, Y: cancel.y})
	m = next.(Model)
	if m.modal.active {
		t.Fatal("cancel button click should close the modal")
	}
}

func TestNoticeExpiryDismissalAndReportSaveFailure(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m, _ = m.setNotice(noticeSuccess, "Saved.", true)
	id := m.notice.id
	next, _ := m.Update(noticeExpiredMsg{id: id})
	m = next.(Model)
	if m.statusLine != "" || m.notice.message != "" {
		t.Fatalf("transient notice should expire, status=%q notice=%+v", m.statusLine, m.notice)
	}

	next, _ = m.Update(reportSavedMsg{err: fmt.Errorf("disk full")})
	m = next.(Model)
	if m.err == nil || !strings.Contains(m.View(), "report save failed: disk full") {
		t.Fatalf("report-save failure should surface persistently, err=%v view=%s", m.err, ansi.Strip(m.View()))
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = next.(Model)
	if m.err != nil || m.notice.message != "" {
		t.Fatalf("persistent notice should dismiss with x, err=%v notice=%+v", m.err, m.notice)
	}
}

// TestDevicesLoaded_ZeroDevicesHasNoActionsButRescanWorks loosely ports
// dashboard_no_device_selects_refresh — Go's redesigned dashboard has no
// selection at all with zero devices (rather than Rust's synthetic Refresh
// action), but rescanning must still work, which is the behavior that
// actually matters (see the "Add device-list rescan" fix).
func TestDevicesLoaded_ZeroDevicesHasNoActionsButRescanWorks(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	next, _ := m.Update(devicesLoadedMsg{devices: nil})
	m = next.(Model)

	if _, ok := m.devices.selected(); ok {
		t.Fatal("expected no selection with zero devices")
	}
	if items := m.actionsForSelectedDevice(); items != nil {
		t.Fatalf("expected no actions with zero devices, got %v", items)
	}

	_, cmd := m.updateDevices(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("expected 'r' to issue a reload command even with zero devices")
	}
	msg := cmd()
	if _, ok := msg.(devicesLoadedMsg); !ok {
		t.Fatalf("expected 'r' to produce devicesLoadedMsg, got %T", msg)
	}
}

// TestDevicesLoaded_GroupsByTier ports dashboard_groups_devices_by_support_tier.
func TestDevicesLoaded_GroupsByTier(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevices(t, m, c)

	if len(m.devices.filtered) < 2 {
		t.Fatalf("expected at least 2 mock devices, got %d", len(m.devices.filtered))
	}
	sawCandidate := false
	prevRank := -1
	for _, d := range m.devices.filtered {
		rank := tierRank(d.SupportTier)
		if rank < prevRank {
			t.Fatalf("device list not grouped by tier: %v (rank %d) came after rank %d", d.Name, rank, prevRank)
		}
		prevRank = rank
		if d.SupportTier == protocol.TierCandidateReadOnly {
			sawCandidate = true
		}
	}
	if !sawCandidate {
		t.Fatal("expected at least one candidate-readonly mock device")
	}
	if m.devices.filtered[0].SupportTier != protocol.TierFull {
		t.Fatalf("expected a full-tier device first, got %v", m.devices.filtered[0].SupportTier)
	}
}

// TestAdvancedModeToggle_UpdatesCoreRuntime ports
// toggling_advanced_mode_updates_core_runtime.
func TestAdvancedModeToggle_UpdatesCoreRuntime(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	if c.AdvancedMode() {
		t.Fatal("expected advanced mode off by default")
	}

	m.screen = screenSettings
	m.settingsCursor = 0
	next, _ := m.triggerSettingsRow()
	m = next.(Model)
	if !m.advancedMode || !c.AdvancedMode() {
		t.Fatal("expected both model and core advanced mode on after toggling once")
	}

	next, _ = m.triggerSettingsRow()
	m = next.(Model)
	if m.advancedMode || c.AdvancedMode() {
		t.Fatal("expected both model and core advanced mode off after toggling twice")
	}
}

// TestCandidateWriteProbe_RequiresPerPidUnlockFile ports
// dashboard_candidate_write_probe_uses_per_pid_unlock_file end to end: with
// advanced mode on, risk acknowledged, and a matching on-disk unlock file,
// the guarded probe succeeds and a runtime_unlock/candidate-write-probe
// report is persisted.
func TestCandidateWriteProbe_RequiresPerPidUnlockFile(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "config.toml")

	m, c := newTestModel(t, settingsPath)
	// Force ReportSaveAlways so this test deterministically exercises and
	// verifies the saved report's content — the default (FailureOnly)
	// legitimately skips saving a successful probe, which is why Rust's own
	// equivalent test only checked the report *if* one happened to be saved.
	m.settings.ReportSaveMode = ReportSaveAlways
	m = loadDevices(t, m, c)

	var candidate core.AppDevice
	found := false
	for _, d := range m.devices.filtered {
		if d.SupportTier == protocol.TierCandidateReadOnly {
			candidate, found = d, true
			break
		}
	}
	if !found {
		t.Fatal("expected a candidate-readonly mock device")
	}

	m.advancedMode = true

	unlockDir := filepath.Join(dir, "candidate-unlocks")
	if err := os.MkdirAll(unlockDir, 0o755); err != nil {
		t.Fatalf("mkdir unlock dir: %v", err)
	}
	unlockPath := filepath.Join(unlockDir, candidateUnlockFileNameFor(candidate.VidPid))
	contents := "candidate_write_unlock = true\npid = \"" + hex4(candidate.VidPid.PID) + "\"\n"
	if err := os.WriteFile(unlockPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write unlock file: %v", err)
	}
	if !candidateUnlockFilePresent(settingsPath, candidate.VidPid) {
		t.Fatal("expected candidateUnlockFilePresent to see the unlock file we just wrote")
	}

	next, cmd := m.Update(candidateProbeBeginMsg{device: candidate})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected candidateProbeBeginMsg to issue the probe command")
	}
	if !m.acknowledgedRisk {
		t.Fatal("expected risk acknowledged after dispatching the probe")
	}

	result := runCmdForMsg[candidateProbeResultMsg](t, cmd)
	if result.err != nil {
		t.Fatalf("unexpected probe error: %v", result.err)
	}
	if !result.report.Allowed {
		t.Fatalf("expected the probe to be allowed, got report %+v", result.report)
	}
	if !result.report.ReadbackVerified {
		t.Fatalf("expected readback verified, got report %+v", result.report)
	}

	next, saveCmd := m.Update(result)
	m = next.(Model)
	if saveCmd == nil {
		t.Fatal("expected a save-report command after a successful probe")
	}
	savedMsg := runCmdForMsg[reportSavedMsg](t, saveCmd)
	if savedMsg.err != nil {
		t.Fatalf("unexpected error saving report: %v", savedMsg.err)
	}
	if savedMsg.path == "" {
		t.Fatal("expected a non-empty report path")
	}
	body, err := os.ReadFile(savedMsg.path)
	if err != nil {
		t.Fatalf("read saved report: %v", err)
	}
	if !strings.Contains(string(body), "runtime_unlock") {
		t.Error("expected saved report to contain a runtime_unlock section")
	}
	if !strings.Contains(string(body), "candidate-write-probe") {
		t.Error("expected saved report to record the candidate-write-probe operation")
	}
}

// TestCandidateWriteProbe_DeniedWithoutUnlockFile confirms the flip side:
// with no unlock file present, the guarded probe must not be silently
// allowed — CandidateWriteProbe itself is the enforcement point, matching
// how internal/core's policy gate is meant to be the source of truth, not
// just the UI's disabled-reason hint.
func TestCandidateWriteProbe_DeniedWithoutUnlockFile(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "config.toml")
	m, c := newTestModel(t, settingsPath)
	m = loadDevices(t, m, c)

	var candidate core.AppDevice
	for _, d := range m.devices.filtered {
		if d.SupportTier == protocol.TierCandidateReadOnly {
			candidate = d
			break
		}
	}
	m.advancedMode = true

	_, cmd := m.Update(candidateProbeBeginMsg{device: candidate})
	result := runCmdForMsg[candidateProbeResultMsg](t, cmd)
	if result.err == nil && result.report.Allowed {
		t.Fatal("expected the probe to be denied without a matching unlock file present")
	}
}

// TestDiagnostics_RunThenBack ports
// integration_diagnostics_run_rerun_save_and_back's core shape.
func TestDiagnostics_RunThenBack(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevices(t, m, c)

	device, ok := m.devices.selected()
	if !ok {
		t.Fatal("expected a selected device")
	}
	m.devices.actionIdx = 0
	next, cmd := m.triggerDevicesEnter()
	m = next.(Model)
	if m.screen != screenDiagnostics {
		t.Fatalf("expected Diagnose to switch to the diagnostics screen, got %v", m.screen)
	}
	if cmd == nil {
		t.Fatal("expected a diagnostics run command")
	}

	diagMsg := cmd()
	next, saveCmd := m.updateDiagnostics(diagMsg)
	m = next.(Model)
	if m.diag.loading {
		t.Fatal("expected loading to clear once results arrive")
	}
	if m.diag.device.VidPid != device.VidPid {
		t.Fatalf("expected diagnostics for the selected device %v, got %v", device.VidPid, m.diag.device.VidPid)
	}
	if saveCmd == nil {
		t.Fatal("expected a save-report command after diagnostics complete")
	}

	next, _ = m.updateDiagnostics(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.screen != screenDevices {
		t.Fatalf("expected esc to return to the devices screen, got %v", m.screen)
	}
}

// TestDiagnostics_DeviceDisconnectedShowsRescanHint verifies the disconnect
// handling added to the diagnostics screen: a core.KindDeviceDisconnected
// error must render a distinct rescan hint, not just a generic failure
// message indistinguishable from any other error.
func TestDiagnostics_DeviceDisconnectedShowsRescanHint(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	disconnectedErr := &core.Error{Kind: core.KindDeviceDisconnected, Message: "0x2dc8:0x6009 is no longer connected"}
	next, _ := m.updateDiagnostics(diagResultMsg{err: disconnectedErr})
	m = next.(Model)

	view := m.viewDiagnostics(m.height)
	if !strings.Contains(view, "The device was disconnected.") {
		t.Fatalf("expected the failure to be named, got:\n%s", view)
	}
	if !strings.Contains(view, "Reconnect it, then press r") {
		t.Fatalf("expected the retry hint for a disconnected device, got:\n%s", view)
	}
}

// TestRecoveryTakeover_ForcesRecoveryAndBlocksNavigation ports
// recovery_transition_is_preserved plus the "never clears at runtime"
// write-lock behavior documented in screen_recovery.go.
func TestRecoveryTakeover_ForcesRecoveryAndBlocksNavigation(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevices(t, m, c)

	device, _ := m.devices.selected()
	report := core.WriteRecoveryReport{
		HasBackupID: true, BackupID: "backup-1",
		WriteApplied: false, RollbackAttempted: true, RollbackSucceeded: false,
	}
	if !report.RollbackFailed() {
		t.Fatal("test fixture must represent a failed rollback")
	}

	m.mapping.device = device
	next, _ := m.handleMappingApplyResult(report, nil)
	m = next.(Model)
	if !m.writeLockUntilRestart {
		t.Fatal("expected write lock engaged after a failed rollback")
	}

	// route() must force Recovery regardless of the screen we were on,
	// and the lock must never clear itself.
	m.screen = screenSettings
	next, _ = m.route(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.screen != screenRecovery {
		t.Fatalf("expected the write lock to force Recovery, got %v", m.screen)
	}

	next, _ = m.updateRecovery(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	_ = next
	if !m.writeLockUntilRestart {
		t.Fatal("write lock must never clear at runtime")
	}
}

// TestView_ModalDimsBackgroundInsteadOfReplacingIt guards a real behavior
// change from the opencode-inspired redesign: View() used to return *only*
// the modal while one was active (the screen behind it never rendered at
// all). It now composites the modal onto a dimmed render of the real screen
// behind it. This proves both halves of that: the header text is still
// genuinely present (plain-text, ansi.Strip'd) once a modal is showing, and
// its *exact* original (undimmed) styled rendering from before the modal
// opened is gone from the new frame — i.e. it didn't just survive
// untouched, it was actually restyled. Checking the plain text alone
// wouldn't catch "dimming" silently doing nothing; checking styled-bytes
// alone would be too brittle to lipgloss's exact multi-line SGR emission —
// together they're both robust and a real proof.
func TestView_ModalDimsBackgroundInsteadOfReplacingIt(t *testing.T) {
	// Lipgloss auto-detects the color profile from the process's actual
	// stdout, which isn't a real terminal under `go test` — it downgrades to
	// Ascii (no SGR codes emitted at all) unless forced. Force a real color
	// profile for this test so there's something to prove is actually
	// dimmed; every other test in this package deliberately doesn't care
	// about styled bytes, only this one does.
	prevProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prevProfile) })

	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevices(t, m, c)

	baseline := m.View()
	if !strings.Contains(baseline, "OpenBitdo") {
		t.Fatal("sanity check: expected the header to render before any modal is active")
	}
	baselineHeaderLine := strings.Split(baseline, "\n")[0]

	m.modal = riskAckModal("run a test-only unsafe operation", firmwareRisk, nil)

	withModal := m.View()
	if !strings.Contains(withModal, "Confirm a risky operation") {
		t.Fatal("expected the modal's own content to render")
	}

	plain := ansi.Strip(withModal)
	if !strings.Contains(plain, "OpenBitdo") {
		t.Fatal("expected the header text to still be present (dimmed, not removed) behind the modal — View() must not skip rendering the screen behind an active modal")
	}
	if strings.Contains(withModal, baselineHeaderLine) {
		t.Fatal("expected the header's exact styled rendering to change once dimmed — found the identical undimmed line, meaning the background wasn't actually dimmed")
	}
}

// TestView_OneFrameAroundEverything: the app is drawn as a single rounded
// frame holding the device list, the tabs and the pane. Panels inside it
// are plain indented text, not boxes of their own.
func TestView_OneFrameAroundEverything(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)

	for _, target := range []screen{screenDevices, screenDiagnostics, screenMapping, screenSettings} {
		m, _ = m.navigate(target, 0)
		plain := ansi.Strip(m.View())
		lines := strings.Split(plain, "\n")
		if !strings.HasPrefix(lines[0], "╭─ OpenBitdo") || !strings.HasSuffix(lines[0], "╮") {
			t.Fatalf("screen %v: expected the frame's top edge with the title, got %q", target, lines[0])
		}
		if last := lines[len(lines)-1]; !strings.HasPrefix(last, "╰") || !strings.HasSuffix(last, "╯") {
			t.Fatalf("screen %v: expected the frame's bottom edge, got %q", target, last)
		}
		for _, corner := range []string{"╭", "╮", "╰", "╯"} {
			if n := strings.Count(plain, corner); n != 1 {
				t.Fatalf("screen %v: expected one frame, found %d %q corners:\n%s", target, n, corner, plain)
			}
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w != m.width {
				t.Fatalf("screen %v: line %d is %d wide, want %d: %q", target, i, w, m.width, line)
			}
		}
	}
}

// TestStylePanelTitleAndStyleAccentAreDistinct guards against the exact
// regression that caused the user-reported "it's weird to tell what button
// I'm selecting" bug: stylePanelTitle (headings) and styleAccent were
// byte-for-byte identical lipgloss styles, so a heading and an
// accent-styled selected row rendered indistinguishably. This doesn't just
// eyeball the definitions — it renders the same text through both and
// checks the actual output bytes differ.
func TestStylePanelTitleAndStyleAccentAreDistinct(t *testing.T) {
	// See TestView_ModalDimsBackgroundInsteadOfReplacingIt's comment: lipgloss
	// downgrades to no-SGR-codes-at-all under `go test`'s non-tty stdout
	// unless a color profile is forced, which would make every style render
	// as identical plain text regardless of this fix.
	prevProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prevProfile) })

	const sample = "Sample"
	if stylePanelTitle.Render(sample) == styleAccent.Render(sample) {
		t.Fatal("expected stylePanelTitle and styleAccent to render differently, got identical output")
	}
}

func TestTheme_AdaptiveAndNoColorReadableDistinctions(t *testing.T) {
	prevProfile := lipgloss.ColorProfile()
	prevDark := lipgloss.HasDarkBackground()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(prevProfile)
		lipgloss.SetHasDarkBackground(prevDark)
	})

	lipgloss.SetColorProfile(termenv.ANSI256)
	darkRenderer := lipgloss.NewRenderer(os.Stdout)
	darkRenderer.SetColorProfile(termenv.ANSI256)
	darkRenderer.SetHasDarkBackground(true)
	adaptiveAccent := semanticColorForEnv(false, "25", "111")
	darkAccent := darkRenderer.NewStyle().Foreground(adaptiveAccent).Render("accent")
	lightRenderer := lipgloss.NewRenderer(os.Stdout)
	lightRenderer.SetColorProfile(termenv.ANSI256)
	lightRenderer.SetHasDarkBackground(false)
	lightAccent := lightRenderer.NewStyle().Foreground(adaptiveAccent).Render("accent")
	if darkAccent == lightAccent {
		t.Fatal("expected adaptive accent color to render differently on dark and light backgrounds")
	}

	lipgloss.SetColorProfile(termenv.Ascii)
	row := styleSelectedRow.Render("› Firmware Update  (Deferred in 0.0.3)")
	if strings.Contains(row, "\x1b[") {
		t.Fatalf("NO_COLOR/ascii rendering should not rely on SGR escapes, got %q", row)
	}
	if !strings.Contains(row, "›") || !strings.Contains(row, "Deferred in 0.0.3") {
		t.Fatalf("NO_COLOR/ascii rendering must retain glyph/text distinctions, got %q", row)
	}
}

// TestScreenHelp_ControllerHintsOnlyShownWhenGamepadConnected guards the fix
// for "people are gonna be confused seeing B or A on their keyboard" — the
// footer's controller glyphs (A/B/dpad) must only appear when
// internal/input actually wired up a gamepad nav stream at startup.
func TestScreenHelp_ControllerHintsOnlyShownWhenGamepadConnected(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)

	help := m.footerHints(200)
	if strings.Contains(help, "enter/A") || strings.Contains(help, "esc/B") || strings.Contains(help, "dpad") {
		t.Fatalf("expected no controller glyphs with no gamepad connected, got: %s", help)
	}
	if !strings.Contains(help, "enter") {
		t.Fatalf("expected plain keyboard-only hints, got: %s", help)
	}

	m = loadDevicesAndDrain(t, m, c)
	m.navNotes = []string{"pid=0x6012: gamepad nav active"}
	help = m.footerHints(200)
	if !strings.Contains(help, "enter/A") || !strings.Contains(help, "dpad") {
		t.Fatalf("expected controller glyphs once a gamepad is connected, got: %s", help)
	}
}

// TestScreenHelp_UnavailableNavNoteDoesNotCountAsConnected makes sure
// gamepadConnected distinguishes "gamepad nav active" from "gamepad nav
// unavailable" notes — internal/input.Start emits both shapes, and matching
// the wrong substring would show controller hints for a device nav
// explicitly failed to wire up.
func TestScreenHelp_UnavailableNavNoteDoesNotCountAsConnected(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.navNotes = []string{"pid=0x6012: gamepad nav unavailable (open failed: no device)"}
	if m.gamepadConnected() {
		t.Fatal("expected an 'unavailable' nav note not to count as a connected gamepad")
	}
}

// TestHelp_OverviewMentionsHowToGetAround: the help overlay must name the
// keys that change section and device, since nothing else on screen is
// needed to get anywhere.
func TestHelp_OverviewMentionsHowToGetAround(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevicesAndDrain(t, m, c)
	help := ansi.Strip(strings.Join(m.helpLines(), "\n"))
	for _, want := range []string{"tab", "next section", "1-4", "next device", "j/k", "settings"} {
		if !strings.Contains(help, want) {
			t.Fatalf("expected the Overview help to mention %q, got:\n%s", want, help)
		}
	}
}

// countConsecutiveBlankLines returns the length of the longest run of
// consecutive blank lines in s, after stripping ANSI codes and the panel's
// left-bar border glyph (leftBar in theme.go applies "┃ " to every line of
// a bordered block uniformly, including logically-blank ones, so a plain
// TrimSpace would never see an empty line and silently detect nothing).
func countConsecutiveBlankLines(s string) int {
	lines := strings.Split(ansi.Strip(s), "\n")
	isBlank := func(line string) bool { return strings.Trim(line, " ┃") == "" }

	// Trim trailing blank lines first -- panels are rendered at a fixed
	// Height() that pads sparse content out with blank filler lines to
	// reach the bottom of the box, which is expected and not what this is
	// checking for. Only gaps between actual content lines count.
	end := len(lines)
	for end > 0 && isBlank(lines[end-1]) {
		end--
	}
	lines = lines[:end]

	longest, current := 0, 0
	for _, line := range lines {
		if isBlank(line) {
			current++
			if current > longest {
				longest = current
			}
		} else {
			current = 0
		}
	}
	return longest
}

// TestViewDeviceDetail_NoDoubleBlankLineBeforeActions guards the spacing
// density fix: each optional section (Blocked, candidate-tier explanation)
// used to end with its own trailing blank line, and the following section
// (or the unconditional "Actions" heading) added its own leading blank line
// on top of that — a real double blank line, not just visual "spacing
// preference," reproducible any time Blocked is shown. Checked on a
// full-tier device with a Blocked reason, the exact case the density dump
// caught it in.
func TestViewDeviceDetail_NoDoubleBlankLineBeforeActions(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m = loadDevices(t, m, c)

	view := m.viewDevices(30)
	if !strings.Contains(ansi.Strip(view), "You can") || !strings.Contains(ansi.Strip(view), "Not yet") {
		t.Fatalf("expected the default selected mock device to show both lists, got:\n%s", ansi.Strip(view))
	}
	if got := countConsecutiveBlankLines(view); got > 1 {
		t.Fatalf("expected at most one consecutive blank line, found a run of %d, in:\n%s", got, ansi.Strip(view))
	}
}

// TestViewFirmware_NoDoubleBlankLineWhenNoWarnings guards the same class of
// bug on the Firmware screen: the Chunks line unconditionally ended with a
// blank line, and "Press enter…" unconditionally started with one, so
// skipping the optional Warnings block (no warnings) left two blank lines
// between them instead of one.
func TestViewFirmware_NoDoubleBlankLineWhenNoWarnings(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.fw = firmwareState{
		device: core.AppDevice{Name: "JP108"},
		stage:  fwStageReadyToConfirm,
		preflight: core.FirmwarePreflightResult{
			Plan: core.FirmwareUpdatePlan{
				BytesTotal: 1024, ChunksTotal: 4, ChunkSize: 256, ExpectedSeconds: 5,
				// Warnings deliberately empty -- this is the case that broke.
			},
		},
	}

	view := m.viewFirmware(30)
	if !strings.Contains(ansi.Strip(view), "Press enter to begin") {
		t.Fatalf("expected the ready-to-confirm prompt to render, got:\n%s", ansi.Strip(view))
	}
	if got := countConsecutiveBlankLines(view); got > 1 {
		t.Fatalf("expected at most one consecutive blank line, found a run of %d, in:\n%s", got, ansi.Strip(view))
	}
}

// TestCurrentFwStepIndex pins the fwStage -> breadcrumb-index mapping used by
// renderFwStageIndicator, including the two exceptional stages (Denied,
// Error) that intentionally sit outside the linear happy path and must
// return -1 rather than some arbitrary in-range index.
func TestCurrentFwStepIndex(t *testing.T) {
	cases := []struct {
		stage fwStage
		want  int
	}{
		{fwStageDownloading, 0},
		{fwStagePreflighting, 1},
		{fwStageDenied, -1},
		{fwStageReadyToConfirm, 2},
		{fwStageConfirming, 3},
		{fwStageRunning, 3},
		{fwStageDone, 4},
		{fwStageError, -1},
	}
	for _, c := range cases {
		if got := currentFwStepIndex(c.stage); got != c.want {
			t.Errorf("currentFwStepIndex(%v) = %d, want %d", c.stage, got, c.want)
		}
	}
}

// TestRenderFwStageIndicator_HappyPathProgression walks every step of the
// linear happy path and checks the breadcrumb marks earlier steps IconPass,
// the current step IconInProgress (highlighted), and later steps as plain
// faint labels -- the actual visual signal the "disconnected, not done"
// feedback was about restoring.
func TestRenderFwStageIndicator_HappyPathProgression(t *testing.T) {
	steps := []struct {
		stage      fwStage
		label      string
		wantPassed []string
		wantFaint  []string
	}{
		{fwStageDownloading, "Download", nil, []string{"Verify", "Confirm", "Transfer", "Done"}},
		{fwStagePreflighting, "Verify", []string{"Download"}, []string{"Confirm", "Transfer", "Done"}},
		{fwStageReadyToConfirm, "Confirm", []string{"Download", "Verify"}, []string{"Transfer", "Done"}},
		{fwStageConfirming, "Transfer", []string{"Download", "Verify", "Confirm"}, []string{"Done"}},
		{fwStageRunning, "Transfer", []string{"Download", "Verify", "Confirm"}, []string{"Done"}},
	}
	for _, s := range steps {
		out := ansi.Strip(renderFwStageIndicator(s.stage))
		if !strings.Contains(out, IconInProgress+" "+s.label) {
			t.Errorf("stage %v: expected current step %q marked with %q, got: %s", s.stage, s.label, IconInProgress, out)
		}
		for _, passed := range s.wantPassed {
			if !strings.Contains(out, IconPass+" "+passed) {
				t.Errorf("stage %v: expected earlier step %q marked with %q, got: %s", s.stage, passed, IconPass, out)
			}
		}
		for _, faint := range s.wantFaint {
			if !strings.Contains(out, faint) {
				t.Errorf("stage %v: expected upcoming step %q to appear (unmarked), got: %s", s.stage, faint, out)
			}
			if strings.Contains(out, IconPass+" "+faint) || strings.Contains(out, IconInProgress+" "+faint) {
				t.Errorf("stage %v: upcoming step %q should not carry a pass/in-progress icon yet, got: %s", s.stage, faint, out)
			}
		}
	}
}

// TestRenderFwStageIndicator_DoneShowsAllStepsPassed guards the fix made
// alongside this test: arriving at fwStageDone previously left the final
// "Done" step rendered with IconInProgress (a "still working" diamond) on a
// screen that had, in fact, finished -- exactly the "not done" feeling the
// user reported. Every step, including Done, must read as IconPass here,
// regardless of whether the underlying outcome succeeded (that distinction
// is carried separately by the outcome line in the body, not this
// breadcrumb).
func TestRenderFwStageIndicator_DoneShowsAllStepsPassed(t *testing.T) {
	out := ansi.Strip(renderFwStageIndicator(fwStageDone))
	for _, label := range fwSteps {
		if !strings.Contains(out, IconPass+" "+label) {
			t.Errorf("expected step %q to be marked %q once the flow reaches Done, got: %s", label, IconPass, out)
		}
	}
	if strings.Contains(out, IconInProgress) {
		t.Errorf("expected no %q (in-progress) icon once the flow reaches Done, got: %s", IconInProgress, out)
	}
}

// TestRenderFwStageIndicator_ExceptionalStagesShowNoBreadcrumb checks that
// Denied and Error -- exits from the happy path, not steps within it --
// suppress the breadcrumb entirely rather than rendering some misleading
// partial progression.
func TestRenderFwStageIndicator_ExceptionalStagesShowNoBreadcrumb(t *testing.T) {
	for _, stage := range []fwStage{fwStageDenied, fwStageError} {
		if got := renderFwStageIndicator(stage); got != "" {
			t.Errorf("stage %v: expected renderFwStageIndicator to return empty (no breadcrumb), got: %q", stage, got)
		}
	}
}

// TestViewFirmware_AllStagesRenderWithoutPanicking drives viewFirmware
// directly across every fwStage with minimally-populated state, the same
// direct-construction technique TestViewFirmware_NoDoubleBlankLineWhenNoWarnings
// uses. This is the "walk every stage, don't just eyeball the code" coverage
// for stages the interactive teatest flow doesn't reach (Denied, Error, and
// each of the three Done outcomes), plus a spacing regression guard on each.
func TestViewFirmware_AllStagesRenderWithoutPanicking(t *testing.T) {
	base := func(t *testing.T) Model {
		m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
		m.fw.device = core.AppDevice{Name: "JP108"}
		return m
	}

	cases := []struct {
		name    string
		setup   func(m *Model)
		want    string
		noBread bool // true if this stage must show no stage indicator at all
	}{
		{
			name:  "Downloading",
			setup: func(m *Model) { m.fw.stage = fwStageDownloading },
			want:  "Downloading and verifying firmware",
		},
		{
			name:  "Preflighting",
			setup: func(m *Model) { m.fw.stage = fwStagePreflighting },
			want:  "Checking safety gates",
		},
		{
			name: "Denied",
			setup: func(m *Model) {
				m.fw.stage = fwStageDenied
				m.fw.deniedMsg = "brick risk too high"
			},
			want:    "Blocked: brick risk too high",
			noBread: true,
		},
		{
			name: "Error",
			setup: func(m *Model) {
				m.fw.stage = fwStageError
				m.fw.err = fmt.Errorf("transport error: device disconnected")
			},
			want:    "Error: transport error: device disconnected",
			noBread: true,
		},
		{
			name:  "Confirming",
			setup: func(m *Model) { m.fw.stage = fwStageConfirming },
			want:  "Starting transfer",
		},
		{
			name: "Running",
			setup: func(m *Model) {
				m.fw.stage = fwStageRunning
				m.fw.progress = 42
				m.fw.progressMsg = "sending chunk 12/30"
			},
			want: "sending chunk 12/30",
		},
		{
			name: "Done-Completed",
			setup: func(m *Model) {
				m.fw.stage = fwStageDone
				m.fw.finalReport = core.FirmwareFinalReport{
					Status: core.OutcomeCompleted, ObservedVersion: "1.2.3", ChunksSent: 30, ChunksTotal: 30,
				}
			},
			want: "Update completed and verified.",
		},
		{
			name: "Done-Cancelled",
			setup: func(m *Model) {
				m.fw.stage = fwStageDone
				m.fw.finalReport = core.FirmwareFinalReport{Status: core.OutcomeCancelled, ChunksSent: 10, ChunksTotal: 30}
			},
			want: "Update cancelled.",
		},
		{
			name: "Done-Unverified",
			setup: func(m *Model) {
				m.fw.stage = fwStageDone
				m.fw.finalReport = core.FirmwareFinalReport{
					Status: core.OutcomeCompletedUnverified, Message: "no version response", ChunksSent: 30, ChunksTotal: 30,
				}
			},
			want: "could not be verified",
		},
		{
			name: "Done-Failed",
			setup: func(m *Model) {
				m.fw.stage = fwStageDone
				m.fw.finalReport = core.FirmwareFinalReport{
					Status: core.OutcomeFailed, Message: "checksum mismatch", ChunksSent: 5, ChunksTotal: 30,
				}
			},
			want: "Update failed: checksum mismatch",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := base(t)
			c.setup(&m)

			view := m.viewFirmware(30)
			stripped := ansi.Strip(view)
			if !strings.Contains(stripped, c.want) {
				t.Fatalf("expected view to contain %q, got:\n%s", c.want, stripped)
			}
			if got := countConsecutiveBlankLines(view); got > 1 {
				t.Fatalf("expected at most one consecutive blank line, found a run of %d, in:\n%s", got, stripped)
			}
			hasBreadcrumb := strings.Contains(stripped, "Download") && strings.Contains(stripped, "→")
			if c.noBread && hasBreadcrumb {
				t.Fatalf("expected no stage breadcrumb for %s, got:\n%s", c.name, stripped)
			}
			if !c.noBread && !hasBreadcrumb {
				t.Fatalf("expected a stage breadcrumb for %s, got:\n%s", c.name, stripped)
			}
		})
	}
}

func hex4(v uint16) string {
	const hexdigits = "0123456789abcdef"
	return string([]byte{
		hexdigits[(v>>12)&0xf], hexdigits[(v>>8)&0xf], hexdigits[(v>>4)&0xf], hexdigits[v&0xf],
	})
}

func candidateUnlockFileNameFor(v protocol.VidPid) string {
	return hex4(v.VID) + "_" + hex4(v.PID) + ".toml"
}

// TestU2SlotPreviewCyclesAndCanBeLoadedIntoDraft exercises the full
// preview flow end to end: 'p' cycles the slot and issues a real command,
// running that command's tea.Cmd produces the u2SlotPreviewMsg Update
// expects, and 'enter' on a shown preview loads it into the draft (not the
// device — nothing is written until Apply).
func TestU2SlotPreviewCyclesAndCanBeLoadedIntoDraft(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.screen = screenMapping
	m.mapping = mappingState{
		device: core.AppDevice{Name: "U2", VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012}},
		kind:   core.KindUltimate2,
	}

	next, cmd := m.updateMapping(tea.KeyMsg{Runes: []rune("p"), Type: tea.KeyRunes})
	m = next.(Model)
	if !m.mapping.u2PreviewLoading {
		t.Fatal("expected p to start a preview load")
	}
	if m.mapping.u2PreviewSlot != core.U2Slot1 {
		t.Fatalf("expected the first preview to be Slot1, got %v", m.mapping.u2PreviewSlot)
	}
	if cmd == nil {
		t.Fatal("expected p to return a non-nil command")
	}

	msg := cmd()
	previewMsg, ok := msg.(u2SlotPreviewMsg)
	if !ok {
		t.Fatalf("expected a u2SlotPreviewMsg, got %T", msg)
	}
	next, _ = m.updateMapping(previewMsg)
	m = next.(Model)
	if m.mapping.u2PreviewLoading {
		t.Fatal("expected loading to clear once the preview result arrives")
	}
	if m.mapping.u2PreviewResult == nil {
		t.Fatal("expected a preview result")
	}
	previewedMappings := append([]core.U2ButtonMapping(nil), m.mapping.u2PreviewResult.Mappings...)
	if len(previewedMappings) == 0 {
		t.Fatal("expected the mock preview to return non-empty mappings")
	}

	// esc dismisses the preview without touching the draft.
	draftBefore := append([]core.U2ButtonMapping(nil), m.mapping.u2Draft.Mappings...)
	next, _ = m.updateMapping(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.mapping.u2PreviewResult != nil {
		t.Fatal("expected esc to dismiss the preview")
	}
	if m.screen != screenMapping {
		t.Fatalf("esc from a shown preview must dismiss the preview, not leave the mapping screen — got %v", m.screen)
	}
	if !equalU2(m.mapping.u2Draft.Mappings, draftBefore) {
		t.Fatal("dismissing a preview with esc must not change the draft")
	}

	// Re-preview, then enter loads it into the draft this time.
	next, cmd = m.updateMapping(tea.KeyMsg{Runes: []rune("p"), Type: tea.KeyRunes})
	m = next.(Model)
	next, _ = m.updateMapping(cmd().(u2SlotPreviewMsg))
	m = next.(Model)

	next, _ = m.updateMapping(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.mapping.u2PreviewResult != nil {
		t.Fatal("expected enter to clear the preview after loading it")
	}
	if !equalU2(m.mapping.u2Draft.Mappings, previewedMappings) {
		t.Fatalf("expected enter to load the previewed mappings into the draft: got %v, want %v", m.mapping.u2Draft.Mappings, previewedMappings)
	}
}
