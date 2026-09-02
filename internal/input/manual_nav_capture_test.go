//go:build manual

package input

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/karalabe/hid"
)

const manualPIDEnv = "OPENBITDO_MANUAL_PID"

func manualExpectedPID(t *testing.T) uint16 {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(manualPIDEnv))
	if raw == "" {
		t.Skipf("set %s to the connected controller PID (for example 0x6013) to run the release gate", manualPIDEnv)
	}
	value, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(raw), "0x"), 16, 16)
	if err != nil {
		t.Fatalf("parse %s=%q as a 16-bit hexadecimal PID: %v", manualPIDEnv, raw, err)
	}
	return uint16(value)
}

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

// TestManualUltimate2ReleaseGateNavigation is the strict counterpart to the
// exploratory capture above. It is intentionally manual-tagged and also
// requires OPENBITDO_MANUAL_PID, so neither an ordinary test run nor an
// accidental `-tags manual` invocation can claim hardware qualification.
// During the capture window, press each cardinal d-pad direction and the two
// physical buttons that should map to Confirm and Cancel.
func TestManualUltimate2ReleaseGateNavigation(t *testing.T) {
	pid := manualExpectedPID(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := Start(ctx)
	activeNote := false
	pidToken := "pid=" + strings.ToLower(strconv.FormatUint(uint64(pid), 16))
	for _, note := range result.Notes {
		t.Log("Start note: " + note)
		normalized := strings.ToLower(strings.ReplaceAll(note, "0x", ""))
		if strings.Contains(normalized, pidToken) && strings.Contains(normalized, "gamepad nav active") {
			activeNote = true
		}
	}
	if !activeNote {
		t.Fatalf("pid=%#04x did not expose an active standard gamepad navigation stream", pid)
	}

	wantDirections := map[Direction]bool{DirUp: false, DirRight: false, DirDown: false, DirLeft: false}
	wantButtons := map[uint16]bool{1: false, 2: false}
	t.Log("release gate: within 45s press d-pad up/right/down/left, Confirm, and Cancel")
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()

	for {
		complete := true
		for _, seen := range wantDirections {
			complete = complete && seen
		}
		for _, seen := range wantButtons {
			complete = complete && seen
		}
		if complete {
			t.Logf("navigation release gate passed for pid=%#04x", pid)
			return
		}

		select {
		case ev := <-result.Events:
			if ev.SourcePID != pid {
				continue
			}
			switch ev.Kind {
			case EventDPadChanged:
				if _, required := wantDirections[ev.DPad]; required {
					wantDirections[ev.DPad] = true
				}
				t.Logf("d-pad event: %v", ev.DPad)
			case EventButtonDown:
				if _, required := wantButtons[ev.Button]; required {
					wantButtons[ev.Button] = true
				}
				t.Logf("button-down event: %#04x", ev.Button)
			}
		case <-timer.C:
			t.Fatalf("navigation gate incomplete for pid=%#04x: directions=%v buttons=%v", pid, wantDirections, wantButtons)
		}
	}
}

// TestManualUltimate2ReleaseGateInterfaces records every interface exposed by
// the selected physical mode and requires both channels needed by the RC: the
// vendor configuration interface and a standard Generic Desktop gamepad.
// Run it once per physical USB/controller mode and retain the verbose output
// with the release-gate evidence.
func TestManualUltimate2ReleaseGateInterfaces(t *testing.T) {
	pid := manualExpectedPID(t)
	infos := hid.Enumerate(bitdoVID, pid)
	if len(infos) == 0 {
		t.Fatalf("no interfaces enumerated for %#04x:%#04x", bitdoVID, pid)
	}

	const (
		vendorConfigUsagePage = 0xffa0
		vendorConfigUsage     = 0x0001
		gamepadUsage          = 0x0005
	)
	var vendorConfig, gamepad bool
	for _, info := range infos {
		t.Logf("pid=%#04x usage_page=%#04x usage=%#04x serial=%q product=%q path=%q",
			info.ProductID, info.UsagePage, info.Usage, info.Serial, info.Product, info.Path)
		switch {
		case info.UsagePage == vendorConfigUsagePage && info.Usage == vendorConfigUsage:
			vendorConfig = true
		case info.UsagePage == UsagePageGenericDesktop && info.Usage == gamepadUsage:
			gamepad = true
		}
	}
	if !vendorConfig || !gamepad {
		t.Fatalf("pid=%#04x mode is not release-qualified: vendor_config=%v generic_desktop_gamepad=%v", pid, vendorConfig, gamepad)
	}
}

// TestManualUltimate2ReleaseGateHotplug requires one live disconnect and
// reconnect of the selected PID. The app-level manual test separately checks
// that the dashboard follows these events; this test establishes that the
// input poller itself observes both transitions and reopens gamepad nav.
func TestManualUltimate2ReleaseGateHotplug(t *testing.T) {
	pid := manualExpectedPID(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := Start(ctx)
	t.Logf("release gate: unplug pid=%#04x, then reconnect it as soon as the disconnect is logged", pid)

	disconnectTimer := time.NewTimer(45 * time.Second)
	defer disconnectTimer.Stop()
	var disconnectedSerial string
	disconnected := false
	for !disconnected {
		select {
		case ev := <-result.Events:
			if ev.SourcePID == pid && ev.Kind == EventDeviceDisconnected {
				disconnected = true
				disconnectedSerial = ev.Serial
				t.Logf("disconnect observed at %s serial=%q", ev.Timestamp.Format(time.RFC3339Nano), ev.Serial)
			}
		case <-disconnectTimer.C:
			t.Fatalf("no disconnect event observed for pid=%#04x", pid)
		}
	}

	reconnectTimer := time.NewTimer(45 * time.Second)
	defer reconnectTimer.Stop()
	for {
		select {
		case ev := <-result.Events:
			if ev.SourcePID != pid || ev.Kind != EventDeviceConnected {
				continue
			}
			if disconnectedSerial != "" && ev.Serial != "" && ev.Serial != disconnectedSerial {
				continue
			}
			if !strings.Contains(ev.Note, "gamepad nav active") {
				t.Fatalf("pid=%#04x reconnected without an active gamepad nav stream: %s", pid, ev.Note)
			}
			t.Logf("hotplug release gate passed at %s serial=%q", ev.Timestamp.Format(time.RFC3339Nano), ev.Serial)
			return
		case <-reconnectTimer.C:
			t.Fatalf("no matching reconnect event observed for pid=%#04x serial=%q", pid, disconnectedSerial)
		}
	}
}
