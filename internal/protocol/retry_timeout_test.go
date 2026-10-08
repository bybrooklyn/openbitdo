package protocol

import (
	"context"
	"testing"
)

// Ported from sdk/tests/retry_timeout.rs.

func fastRetryConfig() SessionConfig {
	return SessionConfig{
		RetryPolicy:    RetryPolicy{MaxAttempts: 3, BackoffMs: 0},
		TimeoutProfile: TimeoutProfile{ProbeMs: 1, IOMs: 1, FirmwareMs: 1},
		TraceEnabled:   true,
	}
}

func TestRetriesAfterTimeoutThenSucceeds(t *testing.T) {
	transport := &MockTransport{}
	transport.PushReadTimeout()
	good := make([]byte, 64)
	good[0], good[1], good[4], good[22], good[23] = 0x02, 0x05, 0xC1, 0x09, 0x60
	transport.PushReadData(good)

	session := openSession(t, transport, 24585, fastRetryConfig())
	resp, err := session.SendCommand(context.Background(), CommandGetPid, nil)
	if err != nil {
		t.Fatalf("expected response: %v", err)
	}
	if resp.ParsedFields["detected_pid"] != 24585 {
		t.Fatalf("expected detected_pid=24585, got %v", resp.ParsedFields["detected_pid"])
	}
}

func TestRetriesAfterMalformedThenSucceeds(t *testing.T) {
	transport := &MockTransport{}
	malformed := make([]byte, 64)
	malformed[0], malformed[1], malformed[4] = 0x00, 0x05, 0xC1
	transport.PushReadData(malformed)
	good := make([]byte, 64)
	good[0], good[1], good[4], good[22], good[23] = 0x02, 0x05, 0xC1, 0x09, 0x60
	transport.PushReadData(good)

	session := openSession(t, transport, 24585, fastRetryConfig())
	resp, err := session.SendCommand(context.Background(), CommandGetPid, nil)
	if err != nil {
		t.Fatalf("expected response: %v", err)
	}
	if resp.ParsedFields["detected_pid"] != 24585 {
		t.Fatalf("expected detected_pid=24585, got %v", resp.ParsedFields["detected_pid"])
	}
}

func TestReadRetryResendsTheRequest(t *testing.T) {
	transport := &MockTransport{}
	transport.PushReadTimeout()
	transport.PushReadData(pidResponse(0x6012))

	session := openSession(t, transport, 0x6012, fastRetryConfig())
	if _, err := session.SendCommand(context.Background(), CommandGetPid, nil); err != nil {
		t.Fatalf("expected the retried read to succeed: %v", err)
	}
	if got := len(transport.Writes()); got != 2 {
		t.Fatalf("a read the device did not answer must be sent again; got %d writes", got)
	}
}

func TestWriteRetryNeverResendsTheRequest(t *testing.T) {
	// The acknowledgement arrives one read late.
	keyboard := &JP108Simulator{Name: []byte("P\x00")}
	session := openSession(t, keyboard, 0x5209, fastRetryConfig())
	keyboard.SlowReplies = 1 // the name read is retried (a read may be), then the write
	if err := session.JP108WriteDedicatedMapping(context.Background(), 0, 0x04); err != nil {
		t.Fatalf("expected the write to be acknowledged: %v", err)
	}
	keyboard.Frames = nil
	keyboard.SlowReplies = 1
	row, _ := FindCommand(CommandJp108WriteDedicatedMapping)
	if _, err := session.sendRow(context.Background(), row, row.Request); err != nil {
		t.Fatalf("expected the write to be acknowledged on the second read: %v", err)
	}
	if got := len(keyboard.Frames); got != 1 {
		t.Fatalf("a write must be sent exactly once however many reads it takes; got %d writes", got)
	}
}

func TestPartialReplyIsJudgedNotDiscarded(t *testing.T) {
	transport := &MockTransport{}
	// GetPid needs 24 bytes. Ten arrive, then the device goes quiet.
	short := pidResponse(0x6012)[:10]
	for range 3 {
		transport.PushReadData(short)
		transport.PushReadTimeout()
	}

	session := openSession(t, transport, 0x6012, fastRetryConfig())
	_, err := session.SendCommand(context.Background(), CommandGetPid, nil)
	if err == nil {
		t.Fatal("a 10-byte GetPid reply must not validate")
	}
	report := session.LastExecutionReport()
	if report == nil || report.BytesRead != 10 {
		t.Fatalf("expected the 10 bytes that did arrive to be reported, got %+v", report)
	}
}
