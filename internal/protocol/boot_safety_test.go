package protocol

import (
	"context"
	"testing"
)

// Ported from sdk/tests/boot_safety.rs.

func TestUnsafeBootRequiresDualAck(t *testing.T) {
	config := cfgWith(func(c *SessionConfig) { c.AllowUnsafe = true; c.BrickRiskAck = false; c.Experimental = true })
	session := openSession(t, &MockTransport{}, 24585, config)
	err := session.EnterBootloader(context.Background())
	mustErrCode(t, err, CodeUnsafeCommandDenied)
}

func TestUnsafeBootSucceedsWithDualAck(t *testing.T) {
	config := cfgWith(func(c *SessionConfig) { c.AllowUnsafe = true; c.BrickRiskAck = true; c.Experimental = true })
	session := openSession(t, &MockTransport{}, 24585, config)
	if err := session.EnterBootloader(context.Background()); err != nil {
		t.Fatalf("boot sequence: %v", err)
	}
}
