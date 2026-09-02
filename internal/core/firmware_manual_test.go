//go:build manual

package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// failingTransport opens successfully but returns an ordinary error from
// every subsequent call. The transfer error path then performs a real HID
// enumeration to distinguish a disconnected device from another failure.
type failingTransport struct{}

func (failingTransport) Open(context.Context, protocol.VidPid) error { return nil }
func (failingTransport) Close() error                                { return nil }
func (failingTransport) Write([]byte) (int, error)                   { return 0, protocol.ErrTimeout }
func (failingTransport) Read(context.Context, int, uint64) ([]byte, error) {
	return nil, protocol.ErrTimeout
}

func TestFirmwareTransferReportsDisconnectedWhenDeviceGenuinelyAbsent(t *testing.T) {
	if protocol.IsDevicePresent(protocol.VidPid{VID: 0x2dc8, PID: 0x6009}) {
		t.Skip("a real 8BitDo device is attached to this machine — this test needs one to be absent")
	}

	c := New(enableFirmwareForTest(Config{DefaultChunkSize: 16, ProgressIntervalMs: 1}))
	c.transportOverride = failingTransport{}
	path := filepath.Join(t.TempDir(), "openbitdo-disconnect.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte{4}, 64), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ctx := context.Background()

	preflight, err := c.PreflightFirmware(ctx, makeReq(t, path, 0x6009))
	if err != nil || !preflight.Gate.Allowed {
		t.Fatalf("preflight: gate=%+v err=%v", preflight.Gate, err)
	}
	plan := preflight.Plan
	if _, err := c.StartFirmware(ctx, FirmwareStartRequest{SessionID: plan.SessionID}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := c.ConfirmFirmware(ctx, FirmwareConfirmRequest{SessionID: plan.SessionID, AcknowledgedRisk: true}); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		report, err := c.FirmwareReport(ctx, plan.SessionID)
		if err != nil {
			t.Fatalf("report: %v", err)
		}
		if report != nil {
			if report.Status != OutcomeFailed {
				t.Fatalf("expected Failed, got %s", report.Status)
			}
			if report.ErrorCode != protocol.CodeDeviceDisconnected {
				t.Fatalf("expected ErrorCode=DeviceDisconnected, got %q (message: %q)", report.ErrorCode, report.Message)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for firmware report")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
