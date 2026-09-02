package input

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/karalabe/hid"
)

func TestOpenHintSuffixForGOOSOnlyOnLinux(t *testing.T) {
	if !strings.Contains(openHintSuffixForGOOS("linux"), "udev rule") {
		t.Fatalf("expected the linux udev hint, got %q", openHintSuffixForGOOS("linux"))
	}
	for _, goos := range []string{"darwin", "windows", "freebsd"} {
		if got := openHintSuffixForGOOS(goos); got != "" {
			t.Fatalf("goos=%s: expected an empty suffix, got %q", goos, got)
		}
	}
}

func TestDiffDeviceSetsFindsAddedAndRemoved(t *testing.T) {
	prev := map[deviceKey]struct{}{
		{pid: 0x6012, serial: "AAA"}: {},
		{pid: 0x5209, serial: "BBB"}: {},
	}
	next := map[deviceKey]struct{}{
		{pid: 0x5209, serial: "BBB"}: {}, // unchanged
		{pid: 0x2100, serial: "CCC"}: {}, // added
	}

	added, removed := diffDeviceSets(prev, next)

	if len(added) != 1 || added[0] != (deviceKey{pid: 0x2100, serial: "CCC"}) {
		t.Fatalf("expected exactly one added device (pid=0x2100 serial=CCC), got %v", added)
	}
	if len(removed) != 1 || removed[0] != (deviceKey{pid: 0x6012, serial: "AAA"}) {
		t.Fatalf("expected exactly one removed device (pid=0x6012 serial=AAA), got %v", removed)
	}
}

func TestDiffDeviceSetsNoChangeReportsNothing(t *testing.T) {
	set := map[deviceKey]struct{}{
		{pid: 0x6012, serial: "AAA"}: {},
	}
	added, removed := diffDeviceSets(set, set)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("expected no changes for an identical set, got added=%v removed=%v", added, removed)
	}
}

// drainEvents reads every event currently buffered on ch without blocking.
func drainEvents(ch <-chan NavEvent) []NavEvent {
	var out []NavEvent
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestHotplugTickEmitsConnectedOnNewDevice(t *testing.T) {
	events := make(chan NavEvent, 8)
	known := map[deviceKey]struct{}{}
	infos := []hid.DeviceInfo{
		{VendorID: bitdoVID, ProductID: 0x6012, Serial: "AAA"},
	}

	next := hotplugTick(context.Background(), infos, known, events)

	if _, ok := next[deviceKey{pid: 0x6012, serial: "AAA"}]; !ok {
		t.Fatalf("expected the new device in the returned known set, got %v", next)
	}

	got := drainEvents(events)
	if len(got) != 1 {
		t.Fatalf("expected exactly one event, got %d: %v", len(got), got)
	}
	e := got[0]
	if e.Kind != EventDeviceConnected {
		t.Fatalf("expected EventDeviceConnected, got %v", e.Kind)
	}
	if e.SourcePID != 0x6012 || e.Serial != "AAA" {
		t.Fatalf("expected pid=0x6012 serial=AAA, got pid=%#04x serial=%q", e.SourcePID, e.Serial)
	}
	// The synthetic device isn't a real, openable HID handle in this test
	// environment, so streaming necessarily fails -- what's under test here
	// is that the event still fires with an honest outcome, not that the
	// open itself succeeds (that path is exercised by the darwin/other
	// openNavDevice implementations elsewhere).
	if !strings.Contains(e.Note, "gamepad nav unavailable") {
		t.Fatalf("expected an honest 'unavailable' note for an unopenable synthetic device, got %q", e.Note)
	}
}

func TestHotplugTickEmitsDisconnectedOnRemovedDevice(t *testing.T) {
	events := make(chan NavEvent, 8)
	known := map[deviceKey]struct{}{
		{pid: 0x6012, serial: "AAA"}: {},
	}

	next := hotplugTick(context.Background(), nil, known, events)

	if len(next) != 0 {
		t.Fatalf("expected an empty known set after the only device disappeared, got %v", next)
	}

	got := drainEvents(events)
	if len(got) != 1 {
		t.Fatalf("expected exactly one event, got %d: %v", len(got), got)
	}
	e := got[0]
	if e.Kind != EventDeviceDisconnected {
		t.Fatalf("expected EventDeviceDisconnected, got %v", e.Kind)
	}
	if e.SourcePID != 0x6012 || e.Serial != "AAA" {
		t.Fatalf("expected pid=0x6012 serial=AAA, got pid=%#04x serial=%q", e.SourcePID, e.Serial)
	}
}

func TestHotplugTickIsIdempotentWhenNothingChanges(t *testing.T) {
	events := make(chan NavEvent, 8)
	known := map[deviceKey]struct{}{
		{pid: 0x6012, serial: "AAA"}: {},
	}
	infos := []hid.DeviceInfo{
		{VendorID: bitdoVID, ProductID: 0x6012, Serial: "AAA"},
	}

	next := hotplugTick(context.Background(), infos, known, events)

	if len(drainEvents(events)) != 0 {
		t.Fatalf("expected no events when the device set didn't change")
	}
	if len(next) != 1 {
		t.Fatalf("expected the known set to still have exactly one device, got %v", next)
	}
}

func TestPollHotplugStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan NavEvent, 8)
	done := make(chan struct{})

	go func() {
		pollHotplug(ctx, time.Millisecond, map[deviceKey]struct{}{}, events)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollHotplug did not return after context cancellation")
	}
}
