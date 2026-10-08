package protocol

import (
	"strings"
	"testing"
)

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

func TestParseIndexedU16TableRejectsShortReply(t *testing.T) {
	full := make([]byte, 64)
	full[8], full[9] = 0x04, 0x00
	full[26], full[27] = 0x1d, 0x00
	table, err := parseIndexedU16Table(CommandJp108ReadDedicatedMappings, full, 10)
	if err != nil || len(table) != 10 || table[0].Usage != 0x04 || table[9].Usage != 0x1d {
		t.Fatalf("unexpected table %+v err=%v", table, err)
	}

	// 27 bytes holds nine and a half entries. Padding the rest with zeros
	// would produce a backup that unmaps a key when restored.
	if _, err := parseIndexedU16Table(CommandJp108ReadDedicatedMappings, full[:27], 10); err == nil {
		t.Fatal("expected a reply too short for the whole table to be an error")
	}
}

func TestDiagDetailOnlyClaimsAPIDThatMatchesTheDevice(t *testing.T) {
	session := &DeviceSession{target: VidPid{VID: 0x2dc8, PID: 0x6013}}

	// Captured from an Ultimate 2: GetPid's PID field held 0x32a0.
	if got := session.diagIdentityDetail(CommandGetPid, map[string]uint32{"detected_pid": 0x32a0}); !strings.Contains(got, "does not carry this device's product ID") {
		t.Fatalf("a mismatched PID must not be reported as detected: %q", got)
	}
	if got := session.diagIdentityDetail(CommandGetPid, map[string]uint32{"detected_pid": 0x6013}); got != "detected pid 0x6013" {
		t.Fatalf("unexpected detail for a matching PID: %q", got)
	}
	if got := session.diagIdentityDetail(CommandGetReportRevision, map[string]uint32{"revision": 1, "reported_pid": 0x6013}); got != "report revision 1; device reports pid 0x6013" {
		t.Fatalf("unexpected revision detail: %q", got)
	}
	if got := session.diagIdentityDetail(CommandGetReportRevision, map[string]uint32{"revision": 1, "reported_pid": 0}); got != "report revision 1" {
		t.Fatalf("a non-matching PID must be left out: %q", got)
	}
}
