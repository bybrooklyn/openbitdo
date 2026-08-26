package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
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

	resultMsg := cmd()
	result, ok := resultMsg.(candidateProbeResultMsg)
	if !ok {
		t.Fatalf("expected candidateProbeResultMsg, got %T", resultMsg)
	}
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
	savedMsg := saveCmd().(reportSavedMsg)
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
	resultMsg := cmd()
	result := resultMsg.(candidateProbeResultMsg)
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
	m.devices.pane = paneActions
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
	if !strings.Contains(view, "Diagnostics failed") {
		t.Fatalf("expected the failure message to still render, got:\n%s", view)
	}
	if !strings.Contains(view, "press r on the dashboard to rescan") {
		t.Fatalf("expected the rescan hint for a disconnected device, got:\n%s", view)
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
