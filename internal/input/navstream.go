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

// hotplugPollInterval is how often Start's background poller re-enumerates
// vid==0x2dc8 devices to detect connects/disconnects after startup.
// karalabe/hid has no cross-platform device-added/removed event, so this is
// poll-based; 1.5s is frequent enough to feel responsive in a TUI without
// meaningfully loading the system.
const hotplugPollInterval = 1500 * time.Millisecond

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

// NavEventKind distinguishes the navigation-relevant transitions a device
// stream (or the hotplug poller) reports.
type NavEventKind int

const (
	EventDPadChanged NavEventKind = iota
	EventButtonDown
	EventButtonUp
	// EventDeviceConnected and EventDeviceDisconnected are emitted by
	// Start's background hotplug poller (see pollHotplug) when a vid==0x2dc8
	// device appears or disappears after startup. A newly-connected device
	// also gets its nav stream started automatically, the same as one found
	// during Start's initial enumeration.
	EventDeviceConnected
	EventDeviceDisconnected
)

// NavEvent is one menu-navigation-relevant input transition from a
// connected 8BitDo device — a d-pad direction change, a button
// press/release, or a hotplug connect/disconnect. This is decoupled from
// internal/protocol's command session: it is read-only and never competes
// with a diagnostic/mapping session for the same device handle.
type NavEvent struct {
	Kind      NavEventKind
	SourcePID uint16
	Serial    string    // meaningful for EventDeviceConnected/EventDeviceDisconnected
	DPad      Direction // meaningful for EventDPadChanged
	Button    uint16    // meaningful for EventButtonDown/EventButtonUp
	// Note is a human-readable outcome for EventDeviceConnected/
	// EventDeviceDisconnected, in the same phrasing as StartResult.Notes
	// (e.g. "pid=0x6012: gamepad nav active" or "...: open failed (...)"),
	// so a consumer doesn't need to reimplement that formatting to show
	// hotplug changes live.
	Note      string
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
// stop when ctx is cancelled. A background poller (see pollHotplug) keeps
// watching for devices connected or disconnected after this call returns,
// emitting EventDeviceConnected/EventDeviceDisconnected on the same channel
// and starting nav streams for newly-connected devices automatically.
func Start(ctx context.Context) StartResult {
	events := make(chan NavEvent, 64)
	infos := hid.Enumerate(bitdoVID, 0)
	notes := make([]string, 0, len(infos))
	known := make(map[deviceKey]struct{}, len(infos))

	for _, info := range infos {
		notes = append(notes, startDeviceStream(ctx, info, events))
		known[deviceKeyOf(info)] = struct{}{}
	}

	go pollHotplug(ctx, hotplugPollInterval, known, events)

	return StartResult{Events: events, Notes: notes}
}

// startDeviceStream attempts to bring up a nav-only stream for one
// enumerated device: fetch + parse its report descriptor, open it, and (on
// success) start its streamDevice goroutine. Returns a human-readable note
// describing the outcome either way. Shared by Start's initial enumeration
// and pollHotplug's handling of newly-connected devices, so both paths
// report identically-phrased outcomes.
func startDeviceStream(ctx context.Context, info hid.DeviceInfo, out chan<- NavEvent) string {
	descriptor, err := fetchReportDescriptor(info)
	if err != nil {
		return fmt.Sprintf("pid=%#04x: gamepad nav unavailable (%v)", info.ProductID, err)
	}
	fields, err := ParseReportDescriptor(descriptor)
	if err != nil {
		return fmt.Sprintf("pid=%#04x: gamepad nav unavailable (bad report descriptor: %v)", info.ProductID, err)
	}
	device, err := openNavDevice(info)
	if err != nil {
		return fmt.Sprintf("pid=%#04x: gamepad nav unavailable (open failed: %v)%s",
			info.ProductID, err, linuxOpenHintSuffix())
	}
	go streamDevice(ctx, device, info.ProductID, fields, out)
	return fmt.Sprintf("pid=%#04x: gamepad nav active", info.ProductID)
}

// deviceKey uniquely identifies a physical 8BitDo device for hotplug
// diffing across poll cycles. PID alone isn't enough (multiple identical
// controllers can be connected at once); Serial disambiguates them the same
// way internal/core's AppDevice identity does.
type deviceKey struct {
	pid    uint16
	serial string
}

func deviceKeyOf(info hid.DeviceInfo) deviceKey {
	return deviceKey{pid: info.ProductID, serial: info.Serial}
}

// diffDeviceSets compares two enumeration snapshots (by deviceKey) and
// reports which devices appeared and which disappeared between them. Pure
// and side-effect-free so it's directly testable with synthetic device
// sets, independent of real HID enumeration.
func diffDeviceSets(prev, next map[deviceKey]struct{}) (added, removed []deviceKey) {
	for k := range next {
		if _, ok := prev[k]; !ok {
			added = append(added, k)
		}
	}
	for k := range prev {
		if _, ok := next[k]; !ok {
			removed = append(removed, k)
		}
	}
	return added, removed
}

// hotplugTick runs one poll cycle given a freshly enumerated device list:
// it diffs against known, starts nav streams for newly-connected devices,
// emits EventDeviceConnected/EventDeviceDisconnected for every change, and
// returns the updated known set for the next cycle. Split out from
// pollHotplug so the diffing/event-emission behavior is testable with a
// synthetic infos slice, without a real ticker or real HID hardware.
func hotplugTick(ctx context.Context, infos []hid.DeviceInfo, known map[deviceKey]struct{}, out chan<- NavEvent) map[deviceKey]struct{} {
	next := make(map[deviceKey]struct{}, len(infos))
	byKey := make(map[deviceKey]hid.DeviceInfo, len(infos))
	for _, info := range infos {
		k := deviceKeyOf(info)
		next[k] = struct{}{}
		byKey[k] = info
	}

	added, removed := diffDeviceSets(known, next)
	for _, k := range added {
		note := startDeviceStream(ctx, byKey[k], out)
		sendNavEvent(out, NavEvent{Kind: EventDeviceConnected, SourcePID: k.pid, Serial: k.serial, Note: note, Timestamp: time.Now()})
	}
	for _, k := range removed {
		sendNavEvent(out, NavEvent{
			Kind: EventDeviceDisconnected, SourcePID: k.pid, Serial: k.serial,
			Note: fmt.Sprintf("pid=%#04x: disconnected", k.pid), Timestamp: time.Now(),
		})
	}
	return next
}

// pollHotplug re-enumerates vid==0x2dc8 devices every interval, diffing
// against known (seeded by Start from its initial enumeration) until ctx is
// cancelled. Runs detached from Bubbletea's Cmd system, same as
// streamDevice — see its panic-recovery comment for why that's safe here:
// hotplugTick's own work (report-descriptor parsing via startDeviceStream)
// is exactly the same not-yet-hardware-verified path streamDevice guards
// against, so the same recover-and-keep-going stance applies.
func pollHotplug(ctx context.Context, interval time.Duration, known map[deviceKey]struct{}, out chan<- NavEvent) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "gamepad nav: recovered from panic in hotplug poll: %v\n%s\n", r, debug.Stack())
		}
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			known = hotplugTick(ctx, hid.Enumerate(bitdoVID, 0), known, out)
		}
	}
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
