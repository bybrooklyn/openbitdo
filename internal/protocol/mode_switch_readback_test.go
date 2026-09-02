package protocol

import (
	"context"
	"testing"
)

// Ported from sdk/tests/mode_switch_readback.rs.
func TestSetModeReadsBackLatestMode(t *testing.T) {
	transport := &MockTransport{}
	transport.PushReadData([]byte{0x02, 0x01, 0x00, 0x00})
	mode := make([]byte, 64)
	mode[0], mode[1], mode[5] = 0x02, 0x05, 3
	transport.PushReadData(mode)

	config := SessionConfig{
		RetryPolicy:    RetryPolicy{MaxAttempts: 2, BackoffMs: 0},
		TimeoutProfile: TimeoutProfile{ProbeMs: 10, IOMs: 10, FirmwareMs: 10},
		TraceEnabled:   true,
	}
	session := openSession(t, transport, 24585, config)

	modeState, err := session.SetMode(context.Background(), 3)
	if err != nil {
		t.Fatalf("set mode: %v", err)
	}
	if modeState.Mode != 3 {
		t.Fatalf("expected mode 3, got %d", modeState.Mode)
	}
}
