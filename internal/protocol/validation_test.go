package protocol

import "testing"

// Ported from sdk/tests/parser_rejection.rs.

func TestMalformedResponseIsRejected(t *testing.T) {
	if got := ValidateResponse(CommandGetPid, []byte{0x02}); got != StatusMalformed {
		t.Fatalf("expected StatusMalformed, got %s", got)
	}
}

func TestInvalidSignatureIsRejected(t *testing.T) {
	bad := make([]byte, 64)
	bad[0], bad[1], bad[4] = 0x00, 0x05, 0xC1
	if got := ValidateResponse(CommandGetPid, bad); got != StatusInvalid {
		t.Fatalf("expected StatusInvalid, got %s", got)
	}
}

func TestValidSignatureIsAccepted(t *testing.T) {
	good := make([]byte, 64)
	good[0], good[1], good[4], good[22], good[23] = 0x02, 0x05, 0xC1, 0x09, 0x60
	if got := ValidateResponse(CommandGetPid, good); got != StatusOk {
		t.Fatalf("expected StatusOk, got %s", got)
	}
}

// Adapted from sdk/tests/frame_roundtrip.rs: Go doesn't carry a separate
// CommandFrame/Report64 encode step (Rust's CommandFrame::encode() was an
// identity passthrough of payload, so DeviceSession writes row.Request
// directly) — the invariant worth keeping is that every declared command
// has a non-empty request, and 64-byte-report commands really are 64 bytes.
func TestCommandRegistryRequestsAreWellFormed(t *testing.T) {
	seen := map[CommandID]bool{}
	for _, row := range CommandRegistry {
		seen[row.ID] = true
		if len(row.Request) == 0 {
			t.Errorf("%s: empty request", row.ID)
		}
		if row.ReportID == 0x81 && len(row.Request) != 64 && row.ExpectedResponse != "none" {
			t.Errorf("%s: report_id=0x81 but request is %d bytes, not 64", row.ID, len(row.Request))
		}
	}
	if len(seen) != 37 {
		t.Fatalf("expected 37 distinct command IDs, got %d", len(seen))
	}
}
