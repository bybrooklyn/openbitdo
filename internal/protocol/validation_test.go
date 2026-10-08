package protocol

import (
	"context"
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
	if len(seen) != 41 {
		t.Fatalf("expected 41 distinct command IDs, got %d", len(seen))
	}
}

func jp108Session(t *testing.T, keyboard *JP108Simulator) *DeviceSession {
	t.Helper()
	return openSession(t, keyboard, 0x5209, fastRetryConfig())
}

// Frames and replies here are the ones a real Retro 108 (0x5209) exchanged.
func TestJP108ReadsEachButtonByItsKeyID(t *testing.T) {
	keyboard := &JP108Simulator{Mappings: map[byte][5]byte{
		233: {0x07, 0x00, 0x76}, // A button -> a key
		240: {0x07, 0xe1},       // K1 -> a modifier, stored in the first value byte
	}}
	table, err := jp108Session(t, keyboard).JP108ReadDedicatedMappings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(table) != 10 || table[0].Usage != 0x76 || table[1].Usage != 0 || table[2].Usage != 0xe1 {
		t.Fatalf("unexpected table: %+v", table)
	}
	// One 33-byte request per button, on report 0x52, naming the key id.
	wantKeys := []byte{233, 232, 240, 241, 238, 239, 236, 237, 234, 235}
	if len(keyboard.Frames) != len(wantKeys) {
		t.Fatalf("expected %d requests, got %d", len(wantKeys), len(keyboard.Frames))
	}
	for i, frame := range keyboard.Frames {
		if len(frame) != 33 || frame[0] != 0x52 || frame[1] != 0x83 || frame[2] != wantKeys[i] {
			t.Fatalf("request %d = % x", i, frame[:4])
		}
	}
}

func TestJP108WriteNamesAProfileFirstOnlyWhenThereIsNone(t *testing.T) {
	keyboard := &JP108Simulator{}
	session := jp108Session(t, keyboard)
	if err := session.JP108WriteDedicatedMapping(context.Background(), 0, 0x76); err != nil {
		t.Fatal(err)
	}
	if got := string(keyboard.Name); got != "O\x00p\x00e\x00n\x00B\x00i\x00t\x00d\x00o\x00" {
		t.Fatalf("expected a UTF-16LE profile name, got %q", got)
	}
	if got := keyboard.Mappings[233]; got != [5]byte{0x07, 0x00, 0x76} {
		t.Fatalf("unexpected stored mapping for the A button: % x", got)
	}
	last := keyboard.Frames[len(keyboard.Frames)-1]
	if want := []byte{0x52, 0xfa, 0x03, 0x0c, 0x00, 0xaa, 0x09, 0x71, 233, 0x07, 0x00, 0x76, 0x00, 0x00}; string(last[:14]) != string(want) {
		t.Fatalf("mapping frame = % x, want % x", last[:14], want)
	}

	// With a profile in place the name is left alone.
	keyboard.Name = []byte("M\x00i\x00n\x00e\x00")
	before := len(keyboard.Frames)
	if err := session.JP108WriteDedicatedMapping(context.Background(), 1, 0xe0); err != nil {
		t.Fatal(err)
	}
	if string(keyboard.Name) != "M\x00i\x00n\x00e\x00" {
		t.Fatal("an existing profile name must not be overwritten")
	}
	for _, frame := range keyboard.Frames[before:] {
		if frame[1] == 0x70 {
			t.Fatal("no name write expected when a profile exists")
		}
	}
	if got := keyboard.Mappings[232]; got != [5]byte{0x07, 0xe0, 0x00} {
		t.Fatalf("a modifier goes in the first value byte, got % x", got)
	}
	if name, err := session.JP108ReadProfileName(context.Background()); err != nil || name != "Mine" {
		t.Fatalf("read back name %q err=%v", name, err)
	}
}

func TestJP108ReadRefusesWhatItCannotRepresent(t *testing.T) {
	// A media-key assignment (type 0x0c). Reading it as "unassigned" would
	// make a later restore erase it.
	keyboard := &JP108Simulator{Mappings: map[byte][5]byte{232: {0x0c, 0xe9}}}
	if _, err := jp108Session(t, keyboard).JP108ReadDedicatedMappings(context.Background()); err == nil {
		t.Fatal("expected an error for a mapping type that is not understood")
	}

	// A reply too short to hold the mapping is an error, not a zero.
	short := &JP108Simulator{ShortReads: 5}
	if _, err := jp108Session(t, short).JP108ReadDedicatedMappings(context.Background()); err == nil {
		t.Fatal("expected a short reply to be rejected")
	}
}

// A JP108 has one 32-byte output report. The 64-byte commands every other
// family shares must never be sent to it.
func TestJP108IsNeverSentGenericCommands(t *testing.T) {
	keyboard := &JP108Simulator{}
	session := jp108Session(t, keyboard)
	if _, err := session.SendCommand(context.Background(), CommandGetPid, nil); err == nil {
		t.Fatal("GetPid must be refused for a JP108")
	}
	diag := session.DiagProbe(context.Background())
	if len(diag.CommandChecks) == 0 || !diag.TransportReady {
		t.Fatalf("expected the keyboard's own checks to run and pass: %+v", diag.CommandChecks)
	}
	for _, check := range diag.CommandChecks {
		if !check.OK {
			t.Errorf("%s failed against the simulator: %s", check.Command, check.Detail)
		}
	}
	for _, frame := range keyboard.Frames {
		if len(frame) != 33 || frame[0] != 0x52 {
			t.Fatalf("a %d-byte frame starting %#02x was sent to the keyboard", len(frame), frame[0])
		}
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
