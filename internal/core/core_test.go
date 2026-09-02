package core

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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

func enableFirmwareForTest(config Config) Config {
	config.FirmwareUpdatesEnabled = true
	return config
}

func enableFirmwareWithEphemeralTrustForTest(t *testing.T, config Config) Config {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate firmware trust key: %v", err)
	}
	config.FirmwareTrustedKeys = []ed25519.PublicKey{publicKey}
	return enableFirmwareForTest(config)
}

func TestPreflightBlocksCandidatePidWithoutHardwareConfirmation(t *testing.T) {
	c := New(enableFirmwareForTest(DefaultConfig()))
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

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestSupportScorecardReflectsRuntimeReleaseScope(t *testing.T) {
	u2 := appDeviceFromProfile(protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, "", true)
	jp108 := appDeviceFromProfile(protocol.VidPid{VID: 0x2dc8, PID: 0x5209}, "", true)

	realU2 := New(DefaultConfig()).SupportScorecardForDevice(u2)
	if realU2.FirmwareStatus != EvidenceMissing || realU2.BackupReadbackReadiness != EvidenceMissing || realU2.PromotionReady {
		t.Fatalf("real U2 scorecard overclaims release readiness: %+v", realU2)
	}
	for _, blocker := range []string{ReleaseBlockerFirmwareDisabled, ReleaseBlockerU2ButtonMapFraming} {
		if !stringSliceContains(realU2.ReleaseBlockers, blocker) {
			t.Fatalf("real U2 scorecard missing release blocker %q: %+v", blocker, realU2)
		}
	}
	for _, missing := range []string{"firmware updates deferred in 0.1.0", u2MappingDeferredReason} {
		if !stringSliceContains(realU2.MissingEvidence, missing) {
			t.Fatalf("real U2 scorecard missing machine-readable gap %q: %+v", missing, realU2)
		}
	}

	staticU2 := u2.Scorecard()
	if staticU2.PromotionReady || !stringSliceContains(staticU2.ReleaseBlockers, ReleaseBlockerU2ButtonMapFraming) {
		t.Fatalf("static scorecard must use conservative production scope: %+v", staticU2)
	}

	realJP108 := New(DefaultConfig()).SupportScorecardForDevice(jp108)
	if realJP108.BackupReadbackReadiness != EvidencePresent || realJP108.PromotionReady ||
		!stringSliceContains(realJP108.ReleaseBlockers, ReleaseBlockerFirmwareDisabled) ||
		stringSliceContains(realJP108.ReleaseBlockers, ReleaseBlockerU2ButtonMapFraming) {
		t.Fatalf("JP108 mapping support or firmware deferral is misreported: %+v", realJP108)
	}

	mockU2 := New(Config{MockMode: true}).SupportScorecardForDevice(u2)
	if mockU2.BackupReadbackReadiness != EvidencePresent ||
		stringSliceContains(mockU2.ReleaseBlockers, ReleaseBlockerU2ButtonMapFraming) || mockU2.PromotionReady {
		t.Fatalf("mock U2 preview scope is misreported: %+v", mockU2)
	}

	fullyEnabledMockU2 := New(enableFirmwareForTest(Config{MockMode: true})).SupportScorecardForDevice(u2)
	if !fullyEnabledMockU2.PromotionReady || len(fullyEnabledMockU2.ReleaseBlockers) != 0 || fullyEnabledMockU2.FirmwareStatus != EvidencePresent {
		t.Fatalf("fully enabled mock test scope should satisfy scorecard: %+v", fullyEnabledMockU2)
	}

	firmwareEnabledRealU2 := New(enableFirmwareForTest(Config{})).SupportScorecardForDevice(u2)
	if firmwareEnabledRealU2.PromotionReady || stringSliceContains(firmwareEnabledRealU2.ReleaseBlockers, ReleaseBlockerFirmwareDisabled) ||
		!stringSliceContains(firmwareEnabledRealU2.ReleaseBlockers, ReleaseBlockerU2ButtonMapFraming) {
		t.Fatalf("real U2 mapping deferral must independently block promotion: %+v", firmwareEnabledRealU2)
	}
}

func enumeratedU2(path, serial string, usagePage, usage uint16) protocol.EnumeratedDevice {
	return protocol.EnumeratedDevice{
		VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, Product: "Ultimate 2",
		Manufacturer: "8BitDo", Serial: serial, Path: path, UsagePage: usagePage, Usage: usage,
	}
}

func TestListDevicesShowsOneDashboardDeviceForMultiInterfaceController(t *testing.T) {
	gamepad := enumeratedU2("gamepad-path", "physical-1", 0x0001, 0x0005)
	vendor := enumeratedU2("vendor-path", "physical-1", 0xffa0, 0x0001)
	orders := [][]protocol.EnumeratedDevice{{gamepad, vendor}, {vendor, gamepad}}
	var first []AppDevice
	for i, order := range orders {
		c := New(Config{})
		input := append([]protocol.EnumeratedDevice(nil), order...)
		c.enumerateDevices = func() []protocol.EnumeratedDevice {
			return append([]protocol.EnumeratedDevice(nil), input...)
		}
		// Use a method value after injecting enumeration so the repository's
		// conservative live-hardware matcher does not misclassify this
		// hermetic synthetic-enumeration test.
		listDevices := c.ListDevices
		devices, err := listDevices(context.Background())
		if err != nil {
			t.Fatalf("order %d: list devices: %v", i, err)
		}
		if len(devices) != 1 || devices[0].VidPid != vendor.VidPid || devices[0].Serial != vendor.Serial {
			t.Fatalf("order %d: expected one physical dashboard device, got %+v", i, devices)
		}
		if i == 0 {
			first = devices
		} else if !reflect.DeepEqual(devices, first) {
			t.Fatalf("dashboard discovery depends on interface order: first=%+v second=%+v", first, devices)
		}
	}

	deduplicated := deduplicateEnumeratedDevices([]protocol.EnumeratedDevice{gamepad, vendor})
	if len(deduplicated) != 1 || !deduplicated[0].IsVendorConfigInterface() {
		t.Fatalf("vendor interface was not preferred as physical representative: %+v", deduplicated)
	}
}

func TestDeviceDedupUsesExactPathFallbackWithoutUnsafeMerging(t *testing.T) {
	sharedPathGamepad := enumeratedU2("shared-path", "", 0x0001, 0x0005)
	sharedPathVendor := enumeratedU2("shared-path", "", 0xffa0, 0x0001)
	if got := deduplicateEnumeratedDevices([]protocol.EnumeratedDevice{sharedPathGamepad, sharedPathVendor}); len(got) != 1 || !got[0].IsVendorConfigInterface() {
		t.Fatalf("exact path fallback did not safely deduplicate/prefer vendor: %+v", got)
	}

	differentPath := enumeratedU2("another-path", "", 0xffa0, 0x0001)
	if got := deduplicateEnumeratedDevices([]protocol.EnumeratedDevice{sharedPathVendor, differentPath}); len(got) != 2 {
		t.Fatalf("different paths were unsafely merged as one physical device: %+v", got)
	}
}

func TestListDevicesCollapsesSamePidControllersToAddressableRepresentative(t *testing.T) {
	controllerB := enumeratedU2("vendor-b", "serial-b", 0xffa0, 0x0001)
	controllerA := enumeratedU2("vendor-a", "serial-a", 0xffa0, 0x0001)
	c := New(Config{})
	c.enumerateDevices = func() []protocol.EnumeratedDevice {
		return []protocol.EnumeratedDevice{controllerB, controllerA}
	}

	// Enumeration is injected above; no host HID registry is touched.
	listDevices := c.ListDevices
	devices, err := listDevices(context.Background())
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 1 || devices[0].Serial != "" {
		t.Fatalf("same-PID controllers must collapse to deterministic addressable representative, got %+v", devices)
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
	c := New(enableFirmwareForTest(Config{MockMode: true, DefaultChunkSize: 16, ProgressIntervalMs: 1}))
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

func TestDownloadWithInjectedEphemeralKeyAndLocalServer(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	artifact := bytes.Repeat([]byte{0xab}, 4096)
	signature := ed25519.Sign(privateKey, artifact)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/artifact.bin", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(artifact)
	})
	mux.HandleFunc("/artifact.bin.sig", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(signature)
	})
	mux.HandleFunc("/manifest.toml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `version = 1

[[artifacts]]
vid = 11720
pid = 24585
protocol_family = "Standard64"
version = "test-1.0.0"
channel = "stable"
url = %q
sha256 = %q

[artifacts.signature]
algorithm = "ed25519"
url = %q
`, srv.URL+"/artifact.bin", sha256Hex(artifact), srv.URL+"/artifact.bin.sig")
	})

	c := New(enableFirmwareForTest(Config{
		FirmwareManifestURL: srv.URL + "/manifest.toml",
		FirmwareTrustedKeys: []ed25519.PublicKey{publicKey},
	}))
	result, err := c.DownloadRecommendedFirmware(context.Background(), protocol.VidPid{VID: 0x2dc8, PID: 0x6009})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer func() { _ = os.Remove(result.FirmwarePath) }()

	data, err := os.ReadFile(result.FirmwarePath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(data, artifact) {
		t.Fatal("downloaded firmware does not match the signed local artifact")
	}
	if result.Version != "test-1.0.0" {
		t.Fatalf("expected version test-1.0.0, got %s", result.Version)
	}
	if !result.VerifiedSignature {
		t.Fatal("expected an ephemeral-key verified download")
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
	if len(profile.PaddleMappings) != len(AllU2Paddles) {
		t.Fatalf("expected %d paddle mappings, got %d", len(AllU2Paddles), len(profile.PaddleMappings))
	}

	backupID, hasBackup, err := c.U2ApplyCoreProfile(ctx, target, U2Slot1, 1, []U2ButtonMapping{{Button: U2A, Target: U2FuncStart}}, 0.5, 0.5, true)
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
	c := New(enableFirmwareForTest(Config{MockMode: true, DefaultChunkSize: 16, ProgressIntervalMs: 20}))
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
