package core

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// --- rollbackAfterWriteFailure: the safety-critical backup/rollback outcome
// logic behind every mapping/profile write. openSessionForOps hardcodes real
// HID access in non-mock mode, so these exercise the outcome-handling logic
// directly (same technique core_test.go's
// TestCancelRunningTransferExitsBootloaderBeforeReportingCancelled already
// uses for a different reason) rather than driving it via a full
// JP108ApplyDedicatedMapping(WithRecovery) call.

func TestRollbackAfterWriteFailureNoBackup(t *testing.T) {
	c := New(Config{MockMode: true})
	report, err := c.rollbackAfterWriteFailure(context.Background(), "", false, errWriteBoom)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.WriteApplied {
		t.Fatal("expected WriteApplied=false")
	}
	if report.RollbackAttempted {
		t.Fatal("expected no rollback attempted when there's no backup")
	}
	if report.WriteError != errWriteBoom.Error() {
		t.Fatalf("expected WriteError=%q, got %q", errWriteBoom.Error(), report.WriteError)
	}
	if report.RollbackFailed() {
		t.Fatal("RollbackFailed() should be false when no rollback was attempted")
	}
}

func TestRollbackAfterWriteFailureSucceeds(t *testing.T) {
	c := New(Config{MockMode: true})
	target := protocol.VidPid{VID: 0x2dc8, PID: 0x5209}
	backupID := c.storeBackup(target, configBackupPayload{kind: backupJP108, jp108Mappings: defaultJP108Mappings()})

	report, err := c.rollbackAfterWriteFailure(context.Background(), backupID, true, errWriteBoom)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.WriteApplied {
		t.Fatal("expected WriteApplied=false")
	}
	if !report.RollbackAttempted || !report.RollbackSucceeded {
		t.Fatalf("expected a successful rollback, got %+v", report)
	}
	if report.RollbackFailed() {
		t.Fatal("RollbackFailed() should be false when rollback succeeded")
	}
	if report.WriteError != errWriteBoom.Error() {
		t.Fatalf("expected WriteError=%q, got %q", errWriteBoom.Error(), report.WriteError)
	}
}

// TestRollbackAfterWriteFailureFails exercises the "rollback also fails"
// branch via an unknown backup ID (RestoreBackup's errNotFound path, which
// fires before the mock-mode check). This is an honest stand-in for a real
// device-write failure during restore, not a literal reproduction of one —
// openSessionForOps's hardcoded real-HID transport means a genuine
// restore-time device failure isn't reachable from this package's tests
// without hardware. What's verified here is the outcome-handling logic once
// RestoreBackup returns any error, which is the same code regardless of why
// RestoreBackup failed.
func TestRollbackAfterWriteFailureFails(t *testing.T) {
	c := New(Config{MockMode: true})
	report, err := c.rollbackAfterWriteFailure(context.Background(), ConfigBackupID("never-stored"), true, errWriteBoom)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !report.RollbackAttempted || report.RollbackSucceeded {
		t.Fatalf("expected a failed rollback, got %+v", report)
	}
	if !report.RollbackFailed() {
		t.Fatal("RollbackFailed() should be true when rollback was attempted but did not succeed")
	}
	if report.RollbackError == "" {
		t.Fatal("expected a non-empty RollbackError")
	}
}

func TestWriteRecoveryReportRollbackFailed(t *testing.T) {
	cases := []struct {
		name              string
		rollbackAttempted bool
		rollbackSucceeded bool
		want              bool
	}{
		{"never attempted", false, false, false},
		{"attempted and succeeded", true, true, false},
		{"attempted and failed", true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := WriteRecoveryReport{RollbackAttempted: tc.rollbackAttempted, RollbackSucceeded: tc.rollbackSucceeded}
			if got := r.RollbackFailed(); got != tc.want {
				t.Fatalf("RollbackFailed()=%v, want %v", got, tc.want)
			}
		})
	}
}

var errWriteBoom = errInvalidState("simulated device write failure")

// --- CandidateWriteProbe: the three independent gates (support tier,
// advanced-mode+risk-ack, unlock file) plus the capability check, each
// exercised as its own distinct denial rather than one combined test.
// UnlockFilePresent's own denial is already covered by
// TestCandidateWriteProbeRequiresExplicitUnlockFile in core_test.go.

func TestCandidateWriteProbeDeniedForNonCandidateTier(t *testing.T) {
	c := New(Config{MockMode: true})
	// 0x5209 (JP108) is full-tier, not candidate-readonly.
	report, err := c.CandidateWriteProbe(context.Background(), protocol.VidPid{VID: 0x2dc8, PID: 0x5209}, RuntimeUnlockPolicy{
		AdvancedMode: true, AcknowledgedRisk: true, UnlockFilePresent: true,
	})
	if err != nil {
		t.Fatalf("probe report: %v", err)
	}
	if report.Allowed {
		t.Fatal("expected denial for a non-candidate-readonly device")
	}
	if report.Message == "" {
		t.Fatal("expected an explanatory denial message")
	}
}

func TestCandidateWriteProbeDeniedWithoutAdvancedModeOrRiskAck(t *testing.T) {
	c := New(Config{MockMode: true})
	target := protocol.VidPid{VID: 0x2dc8, PID: 0x6002}
	cases := []RuntimeUnlockPolicy{
		{AdvancedMode: false, AcknowledgedRisk: true, UnlockFilePresent: true},
		{AdvancedMode: true, AcknowledgedRisk: false, UnlockFilePresent: true},
		{AdvancedMode: false, AcknowledgedRisk: false, UnlockFilePresent: true},
	}
	for i, policy := range cases {
		report, err := c.CandidateWriteProbe(context.Background(), target, policy)
		if err != nil {
			t.Fatalf("case %d: probe report: %v", i, err)
		}
		if report.Allowed {
			t.Fatalf("case %d: expected denial with policy=%+v", i, policy)
		}
		if report.WriteApplied {
			t.Fatalf("case %d: expected no write applied", i)
		}
	}
}

func TestCandidateWriteProbeDeniedWhenNoSafeWriteCapability(t *testing.T) {
	c := New(Config{MockMode: true})
	// 0x5200 is candidate-readonly but has neither SupportsMode nor
	// SupportsProfileRW -- identify-only, nothing safe to probe-write.
	report, err := c.CandidateWriteProbe(context.Background(), protocol.VidPid{VID: 0x2dc8, PID: 0x5200}, RuntimeUnlockPolicy{
		AdvancedMode: true, AcknowledgedRisk: true, UnlockFilePresent: true,
	})
	if err != nil {
		t.Fatalf("probe report: %v", err)
	}
	if report.Allowed {
		t.Fatal("expected denial when the device has no non-firmware safe-write capability")
	}
}

// --- Firmware session guard clauses not yet covered.

func TestConfirmFirmwareRequiresAcknowledgedRisk(t *testing.T) {
	c := New(Config{MockMode: true})
	_, err := c.ConfirmFirmware(context.Background(), FirmwareConfirmRequest{SessionID: "does-not-matter", AcknowledgedRisk: false})
	if err == nil {
		t.Fatal("expected an error when risk is not acknowledged")
	}
	coreErr, ok := err.(*Error)
	if !ok || coreErr.Kind != KindPolicyDenied || coreErr.Reason != ReasonUnsafeFlagsMissing {
		t.Fatalf("expected a PolicyDenied/UnsafeFlagsMissing error, got %v", err)
	}
}

func TestStartFirmwareRejectsUnknownSession(t *testing.T) {
	c := New(Config{MockMode: true})
	_, err := c.StartFirmware(context.Background(), FirmwareStartRequest{SessionID: "unknown"})
	if err == nil {
		t.Fatal("expected an error for an unknown session id")
	}
	if coreErr, ok := err.(*Error); !ok || coreErr.Kind != KindNotFound {
		t.Fatalf("expected a NotFound error, got %v", err)
	}
}

func TestCancelFirmwareRejectsUnknownSession(t *testing.T) {
	c := New(Config{MockMode: true})
	_, err := c.CancelFirmware(context.Background(), FirmwareCancelRequest{SessionID: "unknown"})
	if err == nil {
		t.Fatal("expected an error for an unknown session id")
	}
	if coreErr, ok := err.(*Error); !ok || coreErr.Kind != KindNotFound {
		t.Fatalf("expected a NotFound error, got %v", err)
	}
}

func TestStartFirmwareRejectsWrongState(t *testing.T) {
	c := New(Config{MockMode: true, DefaultChunkSize: 16})
	path := filepath.Join(t.TempDir(), "openbitdo-wrong-state.bin")
	if err := os.WriteFile(path, make([]byte, 64), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	preflight, err := c.PreflightFirmware(context.Background(), makeReq(t, path, 0x6009))
	if err != nil || !preflight.Gate.Allowed {
		t.Fatalf("preflight: gate=%+v err=%v", preflight.Gate, err)
	}
	sessionID := preflight.Plan.SessionID

	// Move it out of preflight once, legitimately.
	if _, err := c.StartFirmware(context.Background(), FirmwareStartRequest{SessionID: sessionID}); err != nil {
		t.Fatalf("first start: %v", err)
	}
	// A second Start against an already-started session must be rejected.
	_, err = c.StartFirmware(context.Background(), FirmwareStartRequest{SessionID: sessionID})
	if err == nil {
		t.Fatal("expected an error starting a session that's no longer in preflight")
	}
	if coreErr, ok := err.(*Error); !ok || coreErr.Kind != KindInvalidState {
		t.Fatalf("expected an InvalidState error, got %v", err)
	}
}

// --- validateFirmwareImage edge cases.

func TestValidateFirmwareImageRejectsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := validateFirmwareImage(path)
	if err == nil {
		t.Fatal("expected an error for an empty firmware image")
	}
	coreErr, ok := err.(*Error)
	if !ok || coreErr.Kind != KindPolicyDenied || coreErr.Reason != ReasonImageValidationFailed {
		t.Fatalf("expected PolicyDenied/ImageValidationFailed, got %v", err)
	}
}

func TestValidateFirmwareImageRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Sparse-write past the 64MB limit without actually allocating 64MB+1.
	if _, err := f.WriteAt([]byte{0}, 64*1024*1024+1); err != nil {
		t.Fatalf("writeat: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, err = validateFirmwareImage(path)
	if err == nil {
		t.Fatal("expected an error for an oversized firmware image")
	}
	coreErr, ok := err.(*Error)
	if !ok || coreErr.Kind != KindPolicyDenied || coreErr.Reason != ReasonImageValidationFailed {
		t.Fatalf("expected PolicyDenied/ImageValidationFailed, got %v", err)
	}
}

func TestValidateFirmwareImageRejectsMissingFile(t *testing.T) {
	_, err := validateFirmwareImage(filepath.Join(t.TempDir(), "does-not-exist.bin"))
	if err == nil {
		t.Fatal("expected an error for a missing firmware image")
	}
	if coreErr, ok := err.(*Error); !ok || coreErr.Kind != KindIO {
		t.Fatalf("expected an Io error, got %v", err)
	}
}

// --- fetchBytes / verifyArtifactSignature: these hit real network code in
// non-mock mode, so a local httptest.Server stands in for the manifest/
// artifact/signature endpoints. The pinned Ed25519 keys are public-only by
// design (the private key isn't in this repo), so only the *rejection*
// paths are testable here -- a genuinely valid signature can't be
// constructed without it. That's fine: the rejection paths are exactly the
// safety-relevant ones (does the app correctly refuse a bad/tampered
// artifact), and they were at 0% coverage before this.

func TestFetchBytesRejectsNon2xxStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(Config{})
	_, err := c.fetchBytes(context.Background(), srv.URL, "test")
	if err == nil {
		t.Fatal("expected an error for a 404 response")
	}
	if coreErr, ok := err.(*Error); !ok || coreErr.Kind != KindDownload {
		t.Fatalf("expected a Download error, got %v", err)
	}
}

func TestFetchBytesRejectsUnreachableHost(t *testing.T) {
	c := New(Config{})
	_, err := c.fetchBytes(context.Background(), "http://127.0.0.1:1/unreachable", "test")
	if err == nil {
		t.Fatal("expected an error for an unreachable host")
	}
	if coreErr, ok := err.(*Error); !ok || coreErr.Kind != KindDownload {
		t.Fatalf("expected a Download error, got %v", err)
	}
}

func TestVerifyArtifactSignatureRejectsUnsupportedAlgorithm(t *testing.T) {
	c := New(Config{})
	err := c.verifyArtifactSignature(context.Background(), FirmwareArtifact{
		Signature: ManifestSignature{Algorithm: "rsa", URL: "http://unused.invalid/sig"},
	}, []byte("payload"))
	if err == nil {
		t.Fatal("expected an error for an unsupported signature algorithm")
	}
	if coreErr, ok := err.(*Error); !ok || coreErr.Kind != KindManifest {
		t.Fatalf("expected a Manifest error, got %v", err)
	}
}

func TestVerifyArtifactSignatureRejectsBadSignature(t *testing.T) {
	// A well-formed but wrong-key Ed25519 signature: verifiable-shaped, but
	// it won't match either pinned public key, so this exercises the real
	// "signature present, verification fails against both pinned keys" path.
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	payload := []byte("firmware bytes")
	sig := ed25519.Sign(priv, payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(sig)
	}))
	defer srv.Close()

	c := New(Config{})
	err = c.verifyArtifactSignature(context.Background(), FirmwareArtifact{
		Signature: ManifestSignature{Algorithm: "ed25519", URL: srv.URL},
	}, payload)
	if err == nil {
		t.Fatal("expected signature verification to fail against an untrusted key")
	}
	coreErr, ok := err.(*Error)
	if !ok || coreErr.Kind != KindPolicyDenied || coreErr.Reason != ReasonImageValidationFailed {
		t.Fatalf("expected PolicyDenied/ImageValidationFailed, got %v", err)
	}
}

func TestVerifyArtifactSignatureRejectsMalformedBase64(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not valid base64!!"))
	}))
	defer srv.Close()

	c := New(Config{})
	err := c.verifyArtifactSignature(context.Background(), FirmwareArtifact{
		Signature: ManifestSignature{Algorithm: "ed25519", URL: srv.URL},
	}, []byte("payload"))
	if err == nil {
		t.Fatal("expected an error for a malformed signature body")
	}
	if coreErr, ok := err.(*Error); !ok || coreErr.Kind != KindManifest {
		t.Fatalf("expected a Manifest error, got %v", err)
	}
}

func TestDownloadRecommendedFirmwareRejectsHashMismatch(t *testing.T) {
	artifactBody := []byte("actual firmware bytes")
	wrongHash := hex.EncodeToString(sha256.New().Sum([]byte("not the real hash input")))

	mux := http.NewServeMux()
	mux.HandleFunc("/artifact.bin", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(artifactBody)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	manifestTOML := `
version = 1

[[artifacts]]
vid = 11720
pid = 21001
protocol_family = "Standard64"
version = "9.9.9"
channel = "stable"
url = "` + srv.URL + `/artifact.bin"
sha256 = "` + wrongHash + `"

[artifacts.signature]
algorithm = "ed25519"
url = "` + srv.URL + `/artifact.bin.sig"
`
	mux.HandleFunc("/manifest.toml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(manifestTOML))
	})

	c := New(Config{FirmwareManifestURL: srv.URL + "/manifest.toml"})
	_, err := c.DownloadRecommendedFirmware(context.Background(), protocol.VidPid{VID: 0x2dc8, PID: 0x5209})
	if err == nil {
		t.Fatal("expected a hash-mismatch error")
	}
	coreErr, ok := err.(*Error)
	if !ok || coreErr.Kind != KindPolicyDenied || coreErr.Reason != ReasonImageValidationFailed {
		t.Fatalf("expected PolicyDenied/ImageValidationFailed, got %v", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("hash mismatch")) {
		t.Fatalf("expected a hash-mismatch message, got %q", err.Error())
	}
}

func TestDownloadRecommendedFirmwareRejectsNoMatchingArtifact(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("version = 1\n"))
	}))
	defer srv.Close()

	c := New(Config{FirmwareManifestURL: srv.URL})
	_, err := c.DownloadRecommendedFirmware(context.Background(), protocol.VidPid{VID: 0x2dc8, PID: 0x5209})
	if err == nil {
		t.Fatal("expected an error when the manifest has no matching artifact")
	}
	if coreErr, ok := err.(*Error); !ok || coreErr.Kind != KindDownload {
		t.Fatalf("expected a Download error, got %v", err)
	}
}

// panicTransport opens successfully but panics on every Write, simulating an
// unexpected internal error partway through a firmware transfer (as opposed
// to an ordinary transport error, which the non-panic error paths already
// cover) — this is what runTransferTask's recover() exists to catch.
type panicTransport struct{}

func (panicTransport) Open(context.Context, protocol.VidPid) error { return nil }
func (panicTransport) Close() error                                { return nil }
func (panicTransport) Write([]byte) (int, error)                   { panic("simulated transport panic") }
func (panicTransport) Read(context.Context, int, uint64) ([]byte, error) {
	panic("simulated transport panic")
}

// TestFirmwareTransferPanicIsRecoveredAsFailure verifies runTransferTask's
// recover() actually works end to end: a panicking transport must not crash
// the test process (it would, uncaught, since this runs in its own
// goroutine outside Bubbletea's own panic recovery) and the session must
// reach a Failed report instead of hanging forever.
func TestFirmwareTransferPanicIsRecoveredAsFailure(t *testing.T) {
	c := New(Config{DefaultChunkSize: 16, ProgressIntervalMs: 1})
	c.transportOverride = panicTransport{}
	path := filepath.Join(t.TempDir(), "openbitdo-panic.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte{3}, 128), 0o644); err != nil {
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
			if report.Status != OutcomeFailed {
				t.Fatalf("expected Failed, got %s", report.Status)
			}
			if !bytes.Contains([]byte(report.Message), []byte("internal error")) {
				t.Fatalf("expected the failure message to mention the panic, got %q", report.Message)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for firmware report — the panic likely wasn't recovered, so the goroutine died silently")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestTransferFailureCodeClassifiesByDevicePresence(t *testing.T) {
	target := protocol.VidPid{VID: 0x2dc8, PID: 0x6009}
	underlying := &protocol.Error{}

	got := transferFailureCodeWithPresence(target, underlying, func(protocol.VidPid) bool { return false })
	if got != protocol.CodeDeviceDisconnected {
		t.Fatalf("device absent: expected CodeDeviceDisconnected, got %q", got)
	}

	transportErr := protocol.ErrTimeout // a real *protocol.Error with a distinct, checkable code
	got = transferFailureCodeWithPresence(target, transportErr, func(protocol.VidPid) bool { return true })
	if got != protocol.CodeTimeout {
		t.Fatalf("device still present: expected the underlying error's own code (CodeTimeout), got %q", got)
	}
}

// failingTransport opens successfully but returns an ordinary (non-panic)
// error from every subsequent call — simulating a ordinary transport
// failure, as opposed to panicTransport's simulated internal bug.
type failingTransport struct{}

func (failingTransport) Open(context.Context, protocol.VidPid) error { return nil }
func (failingTransport) Close() error                                { return nil }
func (failingTransport) Write([]byte) (int, error)                   { return 0, protocol.ErrTimeout }
func (failingTransport) Read(context.Context, int, uint64) ([]byte, error) {
	return nil, protocol.ErrTimeout
}

// TestFirmwareTransferReportsDisconnectedWhenDeviceGenuinelyAbsent exercises
// transferFailureCode's real (non-injected) path end to end. No physical
// 8BitDo device (vid 0x2dc8) is attached in this environment, so
// protocol.IsDevicePresent genuinely returns false here — this isn't a
// simulated disconnect, it's the actual real-world condition of this test
// machine, which happens to be exactly the scenario this feature exists for.
func TestFirmwareTransferReportsDisconnectedWhenDeviceGenuinelyAbsent(t *testing.T) {
	if protocol.IsDevicePresent(protocol.VidPid{VID: 0x2dc8, PID: 0x6009}) {
		t.Skip("a real 8BitDo device is attached to this machine — this test needs one to be absent")
	}

	c := New(Config{DefaultChunkSize: 16, ProgressIntervalMs: 1})
	c.transportOverride = failingTransport{}
	path := filepath.Join(t.TempDir(), "openbitdo-disconnect.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte{4}, 64), 0o644); err != nil {
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
			if report.Status != OutcomeFailed {
				t.Fatalf("expected Failed, got %s", report.Status)
			}
			if report.ErrorCode != protocol.CodeDeviceDisconnected {
				t.Fatalf("expected ErrorCode=DeviceDisconnected, got %q (message: %q)", report.ErrorCode, report.Message)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for firmware report")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// --- verifyPostFlash: the post-firmware-update verification step. Tested
// directly against the package-private function (same technique as
// TestCancelRunningTransferExitsBootloaderBeforeReportingCancelled) since
// scripting a full bootloader-enter/chunk/commit/exit-bootloader sequence
// through the public API just to reach this one step would obscure what's
// actually being tested.

func newVersionResponse(versionX100 uint16, beta byte) []byte {
	resp := make([]byte, 64)
	resp[0], resp[1] = 0x02, 0x22
	resp[2] = byte(versionX100)
	resp[3] = byte(versionX100 >> 8)
	resp[4] = beta
	return resp
}

func TestVerifyPostFlashCompletesWhenDeviceRespondsToVersionRead(t *testing.T) {
	handle := &firmwareSessionHandle{
		plan: FirmwareUpdatePlan{
			SessionID: "verify-ok", ChunkSize: 32, BytesTotal: 64, ChunksTotal: 2, TargetVersion: "unspecified",
		},
		events: newBroadcaster(),
		state:  stageRunning,
	}
	transport := &protocol.MockTransport{}
	transport.PushReadData(newVersionResponse(123, 0))

	verifyPostFlash(context.Background(), handle, 2, transport)

	handle.mu.Lock()
	report := handle.report
	handle.mu.Unlock()
	if report == nil {
		t.Fatal("expected a report")
	}
	if report.Status != OutcomeCompleted {
		t.Fatalf("expected Completed, got %s (message: %q)", report.Status, report.Message)
	}
	// beta is always present in a well-formed response (the parser sets it
	// unconditionally whenever the response is long enough), so
	// formatFirmwareVersion always includes "beta=N" even when N is 0 --
	// matches existing behavior elsewhere (e.g. diagSuccessDetail).
	if report.ObservedVersion != "firmware 1.23 beta=0" {
		t.Fatalf("expected ObservedVersion %q, got %q", "firmware 1.23 beta=0", report.ObservedVersion)
	}
}

func TestVerifyPostFlashReportsUnverifiedWhenDeviceDoesNotRespond(t *testing.T) {
	handle := &firmwareSessionHandle{
		plan: FirmwareUpdatePlan{
			SessionID: "verify-silent", ChunkSize: 32, BytesTotal: 64, ChunksTotal: 2, TargetVersion: "unspecified",
		},
		events: newBroadcaster(),
		state:  stageRunning,
	}
	// No PushReadData calls at all -- every read on this transport times out,
	// simulating a device that went unresponsive after a flash that itself
	// reported no transfer error. This is exactly the dangerous case
	// verifyPostFlash exists to catch.
	transport := &protocol.MockTransport{}

	verifyPostFlash(context.Background(), handle, 2, transport)

	handle.mu.Lock()
	report := handle.report
	handle.mu.Unlock()
	if report == nil {
		t.Fatal("expected a report")
	}
	if report.Status != OutcomeCompletedUnverified {
		t.Fatalf("expected CompletedUnverified, got %s", report.Status)
	}
	if report.ObservedVersion != "" {
		t.Fatalf("expected empty ObservedVersion for an unreachable device, got %q", report.ObservedVersion)
	}
	if report.Message == "" {
		t.Fatal("expected a non-empty explanatory message")
	}
}

// --- U2PreviewSlot: must read exactly the requested slot, never overriding
// it with the device's currently-active slot the way U2ReadCoreProfile
// deliberately does for its own use case.

func minimalOkResponse() []byte {
	resp := make([]byte, 64)
	resp[0], resp[1] = 0x02, 0x05
	return resp
}

func TestU2PreviewSlotReadsRequestedSlotNotActiveSlot(t *testing.T) {
	// PID 0x6012 is an Ultimate2 full-tier device (SupportsU2SlotConfig +
	// SupportsU2ButtonMap) — see protocol.DeviceProfileFor's registry.
	target := protocol.VidPid{VID: 0x2dc8, PID: 0x6012}
	transport := &protocol.MockTransport{}
	transport.PushReadData(minimalOkResponse()) // U2ReadConfigSlot response
	transport.PushReadData(minimalOkResponse()) // U2ReadButtonMap response

	c := New(Config{})
	c.transportOverride = transport

	profile, err := c.U2PreviewSlot(context.Background(), target, U2Slot3)
	if err != nil {
		t.Fatalf("U2PreviewSlot: %v", err)
	}
	if profile.Slot != U2Slot3 {
		t.Fatalf("expected profile.Slot=%v, got %v", U2Slot3, profile.Slot)
	}

	writes := transport.Writes()
	if len(writes) != 2 {
		t.Fatalf("expected exactly 2 writes (ReadConfigSlot, ReadButtonMap) — critically, no U2GetCurrentSlot call — got %d: %v", len(writes), writes)
	}
	for i, w := range writes {
		if len(w) < 5 || w[4] != U2Slot3.WireValue() {
			t.Fatalf("write %d: expected slot byte (index 4) = %#02x, got %v", i, U2Slot3.WireValue(), w)
		}
	}
}
