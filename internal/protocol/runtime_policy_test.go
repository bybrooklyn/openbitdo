package protocol

import (
	"context"
	"testing"
)

// Ported from sdk/tests/runtime_policy.rs.

func TestInferredSafeReadRequiresExperimentalMode(t *testing.T) {
	row, ok := FindCommand(CommandGetSuperButton)
	if !ok {
		t.Fatal("command present")
	}
	if row.RuntimePolicy() != ExperimentalGate {
		t.Fatalf("expected ExperimentalGate, got %v", row.RuntimePolicy())
	}

	session := openSession(t, &MockTransport{}, 0x6012, DefaultSessionConfig())
	_, err := session.SendCommand(context.Background(), CommandGetSuperButton, nil)
	mustErrCode(t, err, CodeExperimentalRequired)
}

func TestInferredWriteIsBlockedUntilConfirmed(t *testing.T) {
	row, ok := FindCommand(CommandWriteProfile)
	if !ok {
		t.Fatal("command present")
	}
	if row.RuntimePolicy() != BlockedUntilConfirmed {
		t.Fatalf("expected BlockedUntilConfirmed, got %v", row.RuntimePolicy())
	}

	session := openSession(t, &MockTransport{}, 0x6012, cfgWith(func(c *SessionConfig) { c.Experimental = true }))
	_, err := session.SendCommand(context.Background(), CommandWriteProfile, []byte{1, 2, 3})
	mustErrCode(t, err, CodeUnsupportedForPid)
}

func TestConfirmedReadRemainsEnabledDefault(t *testing.T) {
	row, ok := FindCommand(CommandGetPid)
	if !ok {
		t.Fatal("command present")
	}
	if row.RuntimePolicy() != EnabledDefault {
		t.Fatalf("expected EnabledDefault, got %v", row.RuntimePolicy())
	}
}

func TestDiagProbeMarksInferredReadsAsExperimental(t *testing.T) {
	session := openSession(t, &MockTransport{}, 0x6012, cfgWith(func(c *SessionConfig) { c.Experimental = true }))
	diag := session.DiagProbe(context.Background())

	var inferred *DiagCommandStatus
	for i := range diag.CommandChecks {
		if diag.CommandChecks[i].Command == CommandGetSuperButton {
			inferred = &diag.CommandChecks[i]
			break
		}
	}
	if inferred == nil {
		t.Fatal("inferred check present")
	}
	if !inferred.IsExperimental {
		t.Fatal("expected IsExperimental true")
	}
	if inferred.Confidence != EvidenceInferred {
		t.Fatalf("expected EvidenceInferred, got %s", inferred.Confidence)
	}
	if inferred.Attempts < 1 {
		t.Fatal("expected at least 1 attempt")
	}
	if inferred.ResponseStatus != StatusMalformed {
		t.Fatalf("expected StatusMalformed, got %s", inferred.ResponseStatus)
	}
	if inferred.BytesWritten <= 0 {
		t.Fatal("expected bytes written > 0")
	}
}

func TestFullSupportPidScopedCommandsWorkWithoutExperimentalMode(t *testing.T) {
	transport := &MockTransport{}
	transport.PushReadData([]byte{0x02, 0x05, 0x00, 0x00, 0x00, 0x02})
	transport.PushReadData([]byte{0x02, 0x00})

	session := openSession(t, transport, 0x6012, DefaultSessionConfig())
	ctx := context.Background()

	slot, err := session.U2GetCurrentSlot(ctx)
	if err != nil {
		t.Fatalf("pid-scoped read should be available: %v", err)
	}
	if slot != 2 {
		t.Fatalf("expected slot 2, got %d", slot)
	}

	mode, err := session.U2SetMode(ctx, 3)
	if err != nil {
		t.Fatalf("pid-scoped write should be available: %v", err)
	}
	if mode.Mode != 3 {
		t.Fatalf("expected mode 3, got %d", mode.Mode)
	}
}
