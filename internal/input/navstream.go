package input

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/karalabe/hid"
)

const bitdoVID = 0x2dc8

// linuxUdevHint mirrors internal/protocol's hint of the same name (not
// shared across packages — these two packages are deliberately decoupled,
// per navstream's own design, and this is a few lines, not worth a shared
// dependency for). karalabe/hid's Open() returns a hardcoded generic error
// on any failure, with no OS errno passthrough on any platform, so this
// can't reliably distinguish "permission denied" from any other cause —
// phrased as a likely cause and a thing to try, not a certainty.
const linuxUdevHint = ` (if this is a permission error: openbitdo talks to devices via libusb, ` +
	`so a udev rule targeting the "usb" subsystem is usually the fix -- see ` +
	`packaging/linux/99-openbitdo.rules, or add ` +
	`'SUBSYSTEM=="usb", ATTR{idVendor}=="2dc8", TAG+="uaccess"' ` +
	`to /etc/udev/rules.d/ and reconnect the device)`

func linuxOpenHintSuffix() string {
	return openHintSuffixForGOOS(runtime.GOOS)
}

// openHintSuffixForGOOS is linuxOpenHintSuffix's testable core — goos is
// passed in explicitly so tests can exercise the linux/non-linux branches
// without needing to actually run on Linux.
func openHintSuffixForGOOS(goos string) string {
	if goos != "linux" {
		return ""
	}
	return linuxUdevHint
}

// NavEventKind distinguishes the three navigation-relevant transitions a
// device stream reports.
type NavEventKind int

const (
	EventDPadChanged NavEventKind = iota
	EventButtonDown
	EventButtonUp
)

// NavEvent is one menu-navigation-relevant input transition from a
// connected 8BitDo device — a d-pad direction change, or a button
// press/release. This is decoupled from internal/protocol's command
// session: it is read-only and never competes with a diagnostic/mapping
// session for the same device handle.
type NavEvent struct {
	Kind      NavEventKind
	SourcePID uint16
	DPad      Direction // meaningful for EventDPadChanged
	Button    uint16    // meaningful for EventButtonDown/EventButtonUp
	Timestamp time.Time
}

// StartResult is what Start reports back: the merged event channel, plus a
// human-readable note per enumerated device explaining whether nav input
// was wired up for it and, if not, why — surfaced so the UI can be honest
// about the gap rather than silently not responding to a controller.
type StartResult struct {
	Events <-chan NavEvent
	Notes  []string
}

// navDevice is the read/close surface streamDevice needs from an opened
// device handle — satisfied by both *hid.Device (openNavDevice on
// non-darwin platforms) and *machid.Device (openNavDevice on darwin; see
// navdevice_darwin.go for why darwin needs a different Open path).
type navDevice interface {
	Read([]byte) (int, error)
	Close() error
}

// Start opens a read-only, nav-only input stream on every enumerated
// vid==0x2dc8 HID device and returns a single merged event channel. Streams
// stop when ctx is cancelled.
func Start(ctx context.Context) StartResult {
	events := make(chan NavEvent, 64)
	infos := hid.Enumerate(bitdoVID, 0)
	notes := make([]string, 0, len(infos))

	for _, info := range infos {
		descriptor, err := fetchReportDescriptor(info)
		if err != nil {
			notes = append(notes, fmt.Sprintf("pid=%#04x: gamepad nav unavailable (%v)", info.ProductID, err))
			continue
		}
		fields, err := ParseReportDescriptor(descriptor)
		if err != nil {
			notes = append(notes, fmt.Sprintf("pid=%#04x: gamepad nav unavailable (bad report descriptor: %v)", info.ProductID, err))
			continue
		}
		device, err := openNavDevice(info)
		if err != nil {
			notes = append(notes, fmt.Sprintf("pid=%#04x: gamepad nav unavailable (open failed: %v)%s",
				info.ProductID, err, linuxOpenHintSuffix()))
			continue
		}
		notes = append(notes, fmt.Sprintf("pid=%#04x: gamepad nav active", info.ProductID))
		go streamDevice(ctx, device, info.ProductID, fields, events)
	}

	return StartResult{Events: events, Notes: notes}
}

func streamDevice(ctx context.Context, device navDevice, pid uint16, fields []Field, out chan<- NavEvent) {
	// This goroutine runs detached from Bubbletea's Cmd system (started via
	// a bare 'go' statement in Start), so an unrecovered panic here would
	// crash the whole process -- Bubbletea's own panic recovery only wraps
	// goroutines it spawns itself. Report decoding is untested against real
	// 8BitDo hardware (only synthetic descriptors and non-8BitDo devices so
	// far), so a decode panic on an unexpected real report shape is a real
	// possibility, not a theoretical one. Nav is best-effort: recovering and
	// dropping just this device's stream (others keep working) is strictly
	// better than taking down the entire running program over an input
	// decode bug.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "gamepad nav: recovered from panic decoding pid=%#04x: %v\n%s\n", pid, r, debug.Stack())
		}
	}()

	// karalabe/hid's Read has no timeout; Close() on ctx cancellation is
	// what unblocks a pending Read (hidapi's read returns an error once the
	// handle is closed), matching the same pattern internal/protocol's HID
	// transport uses for the same underlying library limitation.
	go func() {
		<-ctx.Done()
		_ = device.Close()
	}()
	defer func() { _ = device.Close() }()

	var prev GamepadState
	buf := make([]byte, 64)
	for {
		n, err := device.Read(buf)
		if err != nil {
			return // device closed (shutdown) or disconnected either way
		}
		if n == 0 {
			continue
		}

		reportID := byte(0)
		report := buf[:n]
		// If any parsed field declares a report ID, the first byte of the
		// report is that ID rather than data.
		hasReportID := false
		for _, f := range fields {
			if f.ReportID != 0 {
				hasReportID = true
				break
			}
		}
		if hasReportID && n > 0 {
			reportID = report[0]
		}

		state := DecodeReport(fields, reportID, report)
		emitTransitions(prev, state, pid, out)
		prev = state
	}
}

func emitTransitions(prev, next GamepadState, pid uint16, out chan<- NavEvent) {
	now := time.Now()
	if next.DPad != prev.DPad {
		sendNavEvent(out, NavEvent{Kind: EventDPadChanged, SourcePID: pid, DPad: next.DPad, Timestamp: now})
	}
	for usage := range next.Buttons {
		if !prev.Buttons[usage] {
			sendNavEvent(out, NavEvent{Kind: EventButtonDown, SourcePID: pid, Button: usage, Timestamp: now})
		}
	}
	for usage := range prev.Buttons {
		if !next.Buttons[usage] {
			sendNavEvent(out, NavEvent{Kind: EventButtonUp, SourcePID: pid, Button: usage, Timestamp: now})
		}
	}
}

// sendNavEvent drops the event if the channel is full rather than blocking
// the device read loop — a menu-nav stream should never let a slow consumer
// stall input decoding.
func sendNavEvent(out chan<- NavEvent, event NavEvent) {
	select {
	case out <- event:
	default:
	}
}
