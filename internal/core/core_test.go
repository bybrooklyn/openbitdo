package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// Ported from sdk/crates/bitdo_app_core/src/lib.rs's #[cfg(test)] mod tests.

func makeReq(t *testing.T, path string, pid uint16) FirmwarePreflightRequest {
	t.Helper()
	return FirmwarePreflightRequest{
		VidPid: protocol.VidPid{VID: 0x2dc8, PID: pid}, FirmwarePath: path,
		AllowUnsafe: true, BrickRiskAck: true, Experimental: true, ChunkSize: 32,
	}
}

func TestPreflightBlocksCandidatePidWithoutHardwareConfirmation(t *testing.T) {
	c := New(DefaultConfig())
	path := filepath.Join(t.TempDir(), "openbitdo-candidate-no-hardware.bin")
	if err := os.WriteFile(path, make([]byte, 256), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	result, err := c.PreflightFirmware(context.Background(), makeReq(t, path, 0x2100))
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if result.Gate.Allowed {
		t.Fatal("expected preflight to be denied")
	}
	if result.Gate.Reason != ReasonNotHardwareConfirmed {
		t.Fatalf("expected ReasonNotHardwareConfirmed, got %s", result.Gate.Reason)
	}
}

func TestCandidateScorecardKeepsRuntimeAndHardwareGapsVisible(t *testing.T) {
	device := appDeviceFromProfile(protocol.VidPid{VID: 0x2dc8, PID: 0x6002}, "", true)
	scorecard := device.Scorecard()

	if scorecard.SupportTier != protocol.TierCandidateReadOnly {
		t.Fatalf("expected candidate-readonly, got %s", scorecard.SupportTier)
	}
	if scorecard.StaticEvidence != EvidencePresent {
		t.Fatalf("expected static evidence present, got %s", scorecard.StaticEvidence)
	}
	if scorecard.RuntimeEvidence != EvidenceMissing {
		t.Fatalf("expected runtime evidence missing, got %s", scorecard.RuntimeEvidence)
	}
	if scorecard.HardwareConfirmation != EvidenceMissing {
		t.Fatalf("expected hardware confirmation missing, got %s", scorecard.HardwareConfirmation)
	}
	if scorecard.SafeWriteReadiness != EvidenceMissing {
		t.Fatalf("expected safe write readiness missing, got %s", scorecard.SafeWriteReadiness)
	}
	if scorecard.PromotionReady {
		t.Fatal("expected promotion not ready")
	}
	found := false
	for _, gap := range scorecard.MissingEvidence {
		if strings.Contains(gap, "runtime") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a missing-evidence gap mentioning 'runtime', got %v", scorecard.MissingEvidence)
	}
}

func TestCandidateWriteProbeRequiresExplicitUnlockFile(t *testing.T) {
	c := New(Config{MockMode: true})
	report, err := c.CandidateWriteProbe(context.Background(), protocol.VidPid{VID: 0x2dc8, PID: 0x6002}, RuntimeUnlockPolicy{
		AdvancedMode: true, AcknowledgedRisk: true, UnlockFilePresent: false,
		UnlockFilePath: "/tmp/openbitdo-candidate-unlock.toml",
	})
	if err != nil {
		t.Fatalf("probe report: %v", err)
	}
	if report.Allowed {
		t.Fatal("expected denial")
	}
	if report.WriteApplied {
		t.Fatal("expected no write applied")
	}
	if len(report.CommandsAttempted) != 0 {
		t.Fatalf("expected no commands attempted, got %v", report.CommandsAttempted)
	}
	if !strings.Contains(report.Message, "candidate_write_unlock = true") {
		t.Fatalf("expected message to mention candidate_write_unlock, got %q", report.Message)
	}
}

func TestCandidateWriteProbeMockCompletesNonFirmwareReadback(t *testing.T) {
	c := New(Config{MockMode: true})
	report, err := c.CandidateWriteProbe(context.Background(), protocol.VidPid{VID: 0x2dc8, PID: 0x6002}, RuntimeUnlockPolicy{
		AdvancedMode: true, AcknowledgedRisk: true, UnlockFilePresent: true,
		UnlockFilePath: "/tmp/openbitdo-candidate-unlock.toml",
	})
	if err != nil {
		t.Fatalf("probe report: %v", err)
	}
	if !report.Allowed || !report.WriteApplied || !report.ReadbackVerified || report.WriteLockRequired {
		t.Fatalf("unexpected report: %+v", report)
	}
	want := []string{"SetModeDInput", "WriteProfile"}
	if len(report.CommandsAttempted) != len(want) || report.CommandsAttempted[0] != want[0] || report.CommandsAttempted[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, report.CommandsAttempted)
	}
	if report.Scorecard.SupportTier != protocol.TierCandidateReadOnly {
		t.Fatalf("expected candidate-readonly scorecard, got %s", report.Scorecard.SupportTier)
	}
}

func TestFirmwareHappyPathReachesCompletedReport(t *testing.T) {
	c := New(Config{MockMode: true, DefaultChunkSize: 16, ProgressIntervalMs: 1})
	path := filepath.Join(t.TempDir(), "openbitdo-happy.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte{2}, 128), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ctx := context.Background()

	preflight, err := c.PreflightFirmware(ctx, makeReq(t, path, 0x6009))
	if err != nil || !preflight.Gate.Allowed {
		t.Fatalf("preflight: gate=%+v err=%v", preflight.Gate, err)
	}
	plan := preflight.Plan

	if _, err := c.StartFirmware(ctx, FirmwareStartRequest{SessionID: plan.SessionID}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := c.ConfirmFirmware(ctx, FirmwareConfirmRequest{SessionID: plan.SessionID, AcknowledgedRisk: true}); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		report, err := c.FirmwareReport(ctx, plan.SessionID)
		if err != nil {
			t.Fatalf("report: %v", err)
		}
		if report != nil {
			if report.Status != OutcomeCompleted {
				t.Fatalf("expected Completed, got %s", report.Status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for firmware report")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestMockDownloadReturnsValidFile(t *testing.T) {
	c := New(Config{MockMode: true})
	result, err := c.DownloadRecommendedFirmware(context.Background(), protocol.VidPid{VID: 0x2dc8, PID: 0x6009})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer func() { _ = os.Remove(result.FirmwarePath) }()

	data, err := os.ReadFile(result.FirmwarePath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty firmware file")
	}
	if result.Version != "mock-1.0.0" {
		t.Fatalf("expected version mock-1.0.0, got %s", result.Version)
	}
}

func TestJP108MockMappingRoundtripSupportsBackupAndRestore(t *testing.T) {
	c := New(Config{MockMode: true})
	target := protocol.VidPid{VID: 0x2dc8, PID: 0x5209}
	ctx := context.Background()

	mappings, err := c.JP108ReadDedicatedMapping(ctx, target)
	if err != nil {
		t.Fatalf("read mappings: %v", err)
	}
	if len(mappings) != len(AllDedicatedButtons) {
		t.Fatalf("expected %d mappings, got %d", len(AllDedicatedButtons), len(mappings))
	}

	backupID, hasBackup, err := c.JP108ApplyDedicatedMapping(ctx, target, []DedicatedButtonMapping{{Button: ButtonA, TargetHIDUsage: 0x2c}}, true)
	if err != nil {
		t.Fatalf("apply mappings: %v", err)
	}
	if !hasBackup {
		t.Fatal("expected a backup id")
	}
	if err := c.RestoreBackup(ctx, backupID); err != nil {
		t.Fatalf("restore backup: %v", err)
	}
}

func TestU2MockProfileRoundtripSupportsBackupAndRestore(t *testing.T) {
	c := New(Config{MockMode: true})
	target := protocol.VidPid{VID: 0x2dc8, PID: 0x6012}
	ctx := context.Background()

	profile, err := c.U2ReadCoreProfile(ctx, target, U2Slot1)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if profile.Slot != U2Slot1 {
		t.Fatalf("expected slot 1, got %v", profile.Slot)
	}
	if len(profile.Mappings) == 0 {
		t.Fatal("expected non-empty mappings")
	}

	backupID, hasBackup, err := c.U2ApplyCoreProfile(ctx, target, U2Slot1, 1, []U2ButtonMapping{{Button: U2A, TargetHIDUsage: 0x0110}}, 0.5, 0.5, true)
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	if !hasBackup {
		t.Fatal("expected a backup id")
	}
	if err := c.RestoreBackup(ctx, backupID); err != nil {
		t.Fatalf("restore backup: %v", err)
	}
}

func TestGuidedButtonTestReturnsBeginnerGuidance(t *testing.T) {
	c := New(Config{MockMode: true})
	result, err := c.GuidedButtonTest(context.Background(), KindJP108, []string{"A -> Space", "K1 -> Enter"})
	if err != nil {
		t.Fatalf("guided test: %v", err)
	}
	if !result.Passed {
		t.Fatal("expected passed")
	}
	if !strings.Contains(result.Guidance, "JP108") {
		t.Fatalf("expected guidance to mention JP108, got %q", result.Guidance)
	}
}

func TestManifestRequiresExactPidMatch(t *testing.T) {
	manifest := FirmwareManifest{
		Version: 1,
		Artifacts: []FirmwareArtifact{{
			VID: 0x2dc8, PID: 0x6013, ProtocolFamily: protocol.DInput, Version: "1.2.3", Channel: "stable",
			URL: "https://example.invalid/fw.bin", SHA256: "abc",
			Signature: ManifestSignature{Algorithm: "ed25519", URL: "https://example.invalid/fw.sig"},
		}},
	}
	if _, ok := manifest.recommendedFor(protocol.VidPid{VID: 0x2dc8, PID: 0x6012}); ok {
		t.Fatal("expected no match for a different pid")
	}
}

func TestCancelRunningFirmwareKeepsFinalReportAvailable(t *testing.T) {
	c := New(Config{MockMode: true, DefaultChunkSize: 16, ProgressIntervalMs: 20})
	path := filepath.Join(t.TempDir(), "openbitdo-cancel-running.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0xCC}, 256), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ctx := context.Background()

	preflight, err := c.PreflightFirmware(ctx, makeReq(t, path, 0x6009))
	if err != nil || !preflight.Gate.Allowed {
		t.Fatalf("preflight: gate=%+v err=%v", preflight.Gate, err)
	}
	plan := preflight.Plan

	if _, err := c.StartFirmware(ctx, FirmwareStartRequest{SessionID: plan.SessionID}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := c.ConfirmFirmware(ctx, FirmwareConfirmRequest{SessionID: plan.SessionID, AcknowledgedRisk: true}); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	time.Sleep(25 * time.Millisecond)

	report, err := c.CancelFirmware(ctx, FirmwareCancelRequest{SessionID: plan.SessionID})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if report.Status != OutcomeCancelled {
		t.Fatalf("expected Cancelled, got %s", report.Status)
	}

	saved, err := c.FirmwareReport(ctx, plan.SessionID)
	if err != nil {
		t.Fatalf("report lookup: %v", err)
	}
	if saved == nil {
		t.Fatal("expected report to remain available")
	}
	if saved.Status != OutcomeCancelled {
		t.Fatalf("expected Cancelled, got %s", saved.Status)
	}
}

func TestCancelRunningTransferExitsBootloaderBeforeReportingCancelled(t *testing.T) {
	handle := &firmwareSessionHandle{
		request: makeReq(t, filepath.Join(t.TempDir(), "unused.bin"), 0x6012),
		plan: FirmwareUpdatePlan{
			SessionID: "cancel-u2", ChunkSize: 32, BytesTotal: 128, ChunksTotal: 4,
			ExpectedSeconds: 1, ImageSHA256: "abc", CurrentVersion: "1.0", TargetVersion: "1.1",
		},
		events:          newBroadcaster(),
		state:           stageRunning,
		cancelRequested: true,
	}

	transport := &protocol.MockTransport{}
	session, err := protocol.NewDeviceSession(context.Background(), transport, protocol.VidPid{VID: 0x2dc8, PID: 0x6012},
		protocol.SessionConfig{AllowUnsafe: true, BrickRiskAck: true, RetryPolicy: protocol.DefaultRetryPolicy(), TimeoutProfile: protocol.DefaultTimeoutProfile()})
	if err != nil {
		t.Fatalf("session init: %v", err)
	}

	cancelRunningTransfer(context.Background(), handle, session, 2)

	handle.mu.Lock()
	report := handle.report
	handle.mu.Unlock()
	if report == nil {
		t.Fatal("expected a cancel report")
	}
	if report.Status != OutcomeCancelled {
		t.Fatalf("expected Cancelled, got %s", report.Status)
	}

	writes := transport.Writes()
	if len(writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(writes))
	}
	if !bytes.Equal(writes[0], []byte{0x05, 0x00, 0x51, 0x01, 0x00, 0x00}) {
		t.Fatalf("unexpected exit-bootloader frame: %x", writes[0])
	}
}

func TestSupportStatusMapsFromTier(t *testing.T) {
	cases := map[protocol.SupportTier]UserSupportStatus{
		protocol.TierFull:              StatusSupported,
		protocol.TierCandidateReadOnly: StatusInProgress,
		protocol.TierDetectOnly:        StatusPlanned,
	}
	for tier, want := range cases {
		if got := SupportStatusForTier(tier); got != want {
			t.Errorf("tier=%s: expected %s, got %s", tier, want, got)
		}
	}
}
