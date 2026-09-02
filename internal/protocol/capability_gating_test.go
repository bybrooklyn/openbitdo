package protocol

import (
	"context"
	"testing"
)

// Ported from sdk/tests/capability_gating.rs.
func TestDetectOnlyPidBlocksUnsafeOperations(t *testing.T) {
	config := cfgWith(func(c *SessionConfig) { c.AllowUnsafe = true; c.BrickRiskAck = true; c.Experimental = true })
	session := openSession(t, &MockTransport{}, 8448, config)
	err := session.EnterBootloader(context.Background())
	mustErrCode(t, err, CodeUnsupportedForPid)
}
