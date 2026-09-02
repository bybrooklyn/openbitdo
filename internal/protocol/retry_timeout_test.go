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
