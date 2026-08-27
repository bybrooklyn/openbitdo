//go:build manual

package input

import (
	"context"
	"testing"
	"time"
)

// TestManualNavCaptureRealButtonPress is a live capture window, not a
// pass/fail assertion: it starts a real Start(ctx) against whatever 8BitDo
// device is physically connected and logs every NavEvent for a generous
// window, so a human can press physical buttons/d-pad directions on the
// device during the run and see whether real gamepad navigation input
// (EventDPadChanged/EventButtonDown/EventButtonUp) actually arrives.
//
// This is a genuinely different question from whether the device responds
// to internal/protocol's vendor-specific command channel (see
// internal/machid/machid_darwin.go's package doc for that separate, still-
// unresolved mystery): a HID gamepad's standard button-state input reports
// are a different mechanism from a vendor's custom request/response
// protocol on the same interface, so this may well work even though the
// vendor protocol currently doesn't -- that's exactly what this capture is
// for finding out, not assuming either way.
//
// Gated behind -tags manual, same convention as every other real-hardware
// test in this project: never runs in `go test ./...`/CI. Run explicitly:
// go test ./internal/input/... -run TestManualNavCapture -v -tags manual
func TestManualNavCaptureRealButtonPress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := Start(ctx)
	for _, note := range result.Notes {
		t.Log("Start note: " + note)
	}

	t.Log("capturing NavEvents for 25s -- press buttons/d-pad on the physical controller now")
	deadline := time.After(25 * time.Second)
	count := 0
	for {
		select {
		case ev := <-result.Events:
			switch ev.Kind {
			case EventDPadChanged:
				count++
				t.Logf("[%s] EventDPadChanged pid=%#04x dpad=%v", ev.Timestamp.Format(time.RFC3339Nano), ev.SourcePID, ev.DPad)
			case EventButtonDown:
				count++
				t.Logf("[%s] EventButtonDown pid=%#04x button=%#04x", ev.Timestamp.Format(time.RFC3339Nano), ev.SourcePID, ev.Button)
			case EventButtonUp:
				count++
				t.Logf("[%s] EventButtonUp pid=%#04x button=%#04x", ev.Timestamp.Format(time.RFC3339Nano), ev.SourcePID, ev.Button)
			case EventDeviceConnected:
				t.Logf("[%s] EventDeviceConnected pid=%#04x note=%q", ev.Timestamp.Format(time.RFC3339Nano), ev.SourcePID, ev.Note)
			case EventDeviceDisconnected:
				t.Logf("[%s] EventDeviceDisconnected pid=%#04x note=%q", ev.Timestamp.Format(time.RFC3339Nano), ev.SourcePID, ev.Note)
			}
		case <-deadline:
			t.Logf("capture window closed: %d real DPad/Button nav events received", count)
			return
		}
	}
}
