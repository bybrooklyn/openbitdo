package protocol

import (
	"context"
	"testing"
)

// panicTransport opens successfully but panics on any Write or Read,
// simulating an actual HID handle -- used to prove a code path performs
// zero HID I/O, since any attempted transport call would crash the test
// immediately rather than silently succeed. Mirrors the identical pattern
// in internal/core/safety_paths_test.go, used there for the same reason
// (proving a write never reaches a real device).
type panicTransport struct{}

func (panicTransport) Open(context.Context, VidPid) error { return nil }
func (panicTransport) Close() error                       { return nil }
func (panicTransport) Write([]byte) (int, error)          { panic("panicTransport: unexpected Write") }
func (panicTransport) Read(context.Context, int, uint64) ([]byte, error) {
	panic("panicTransport: unexpected Read")
}

// TestU2ReadButtonMapPerformsZeroHIDIO proves U2ReadButtonMap is hard-
// blocked against real hardware and never touches the transport at all --
// the confirmed 22 x uint32 wire shape needs multi-report chunking whose
// paging scheme isn't yet confirmed (see
// docs/clean-room-evidence/dossiers/6012/u2_core.toml), so this call must
// stay a no-op against real devices until that's resolved. panicTransport
// would crash this test immediately if U2ReadButtonMap ever attempted a
// Write or Read, which is exactly the point.
func TestU2ReadButtonMapPerformsZeroHIDIO(t *testing.T) {
	session := openSession(t, panicTransport{}, 0x6012, cfgWith(func(c *SessionConfig) { c.Experimental = true }))

	wireMap, err := session.U2ReadButtonMap(context.Background(), 1)
	if wireMap != nil {
		t.Fatalf("expected nil wireMap, got %v", wireMap)
	}
	mustErrCode(t, err, CodeU2ButtonMapUnavailable)
}

// TestU2WriteButtonMapPerformsZeroHIDIO is U2ReadButtonMap's write-side
// counterpart -- see its doc comment. A write using unconfirmed chunking
// assumptions could corrupt a real device's persistent button-map
// configuration, which is exactly what this hard block exists to prevent.
func TestU2WriteButtonMapPerformsZeroHIDIO(t *testing.T) {
	session := openSession(t, panicTransport{}, 0x6012, cfgWith(func(c *SessionConfig) { c.Experimental = true }))

	err := session.U2WriteButtonMap(context.Background(), 1, []IndexedFunction{{Index: 0, Function: 1}})
	mustErrCode(t, err, CodeU2ButtonMapUnavailable)
}
