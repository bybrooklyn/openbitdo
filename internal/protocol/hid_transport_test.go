package protocol

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/karalabe/hid"
)

func TestWithOpenHintForGOOSAddsHintOnlyOnLinux(t *testing.T) {
	base := errors.New("open failed")

	got := withOpenHintForGOOS(base, "linux")
	if !strings.Contains(got.Error(), "udev rule") {
		t.Fatalf("expected the linux udev hint in the error, got %q", got.Error())
	}
	if !strings.Contains(got.Error(), base.Error()) {
		t.Fatalf("expected the original error message to be preserved, got %q", got.Error())
	}

	for _, goos := range []string{"darwin", "windows", "freebsd"} {
		got := withOpenHintForGOOS(base, goos)
		if got.Error() != base.Error() {
			t.Fatalf("goos=%s: expected the error to pass through unchanged, got %q", goos, got.Error())
		}
	}
}

func TestWithOpenHintForGOOSPassesThroughNil(t *testing.T) {
	if withOpenHintForGOOS(nil, "linux") != nil {
		t.Fatal("expected a nil error to stay nil regardless of GOOS")
	}
}

type syntheticHIDDevice struct{}

func (*syntheticHIDDevice) Read([]byte) (int, error)    { return 0, nil }
func (*syntheticHIDDevice) Write(p []byte) (int, error) { return len(p), nil }
func (*syntheticHIDDevice) Close() error                { return nil }

func syntheticInfo(path, serial string, usagePage, usage uint16) hid.DeviceInfo {
	return hid.DeviceInfo{
		Path: path, VendorID: 0x2dc8, ProductID: 0x6012, Serial: serial,
		Product: "Ultimate 2", Manufacturer: "8BitDo", UsagePage: usagePage, Usage: usage,
	}
}

func TestEnumeratedDevicesCarryStableIdentityAndUsage(t *testing.T) {
	info := syntheticInfo("stable-path", "stable-serial", 0xffa0, 0x0001)
	info.Interface = 3
	devices := enumeratedDevicesFromHIDInfos([]hid.DeviceInfo{info})
	if len(devices) != 1 {
		t.Fatalf("expected one converted device, got %d", len(devices))
	}
	got := devices[0]
	if got.Path != info.Path || got.Serial != info.Serial || got.UsagePage != info.UsagePage || got.Usage != info.Usage || got.Interface != info.Interface {
		t.Fatalf("identity/usage fields were not preserved: got=%+v info=%+v", got, info)
	}
	if !got.IsVendorConfigInterface() {
		t.Fatalf("expected converted interface to be recognized as vendor config: %+v", got)
	}
}

func TestSelectVendorConfigInterfaceAllowsSoleUnknownLinuxInterface(t *testing.T) {
	target := VidPid{VID: 0x2dc8, PID: 0x6012}
	unknown := syntheticInfo("linux-only-interface", "controller-1", 0, 0)
	unknown.Interface = 2
	selected, err := selectVendorConfigInterfaceForGOOS(target, []hid.DeviceInfo{unknown}, "linux")
	if err != nil {
		t.Fatalf("select sole unknown interface: %v", err)
	}
	if selected.Path != unknown.Path || selected.Interface != unknown.Interface {
		t.Fatalf("expected sole unknown interface, got %+v", selected)
	}
}

func TestSelectVendorConfigInterfaceRejectsSoleUnknownOutsideLinux(t *testing.T) {
	target := VidPid{VID: 0x2dc8, PID: 0x6012}
	unknown := syntheticInfo("unknown-interface", "controller-1", 0, 0)
	for _, goos := range []string{"darwin", "windows"} {
		_, err := selectVendorConfigInterfaceForGOOS(target, []hid.DeviceInfo{unknown}, goos)
		if err == nil || !strings.Contains(err.Error(), "no vendor configuration HID interface") {
			t.Fatalf("goos=%s: expected sole unknown interface rejection, got %v", goos, err)
		}
	}
}

func TestSelectVendorConfigInterfaceRejectsMultipleUnknownLinuxInterfaces(t *testing.T) {
	target := VidPid{VID: 0x2dc8, PID: 0x6012}
	first := syntheticInfo("linux-interface-0", "controller-1", 0, 0)
	second := syntheticInfo("linux-interface-1", "controller-1", 0, 0)
	first.Interface, second.Interface = 0, 1
	_, err := selectVendorConfigInterfaceForGOOS(target, []hid.DeviceInfo{second, first}, "linux")
	if err == nil || !strings.Contains(err.Error(), "ambiguous HID interfaces") || !strings.Contains(err.Error(), "cannot be selected safely") {
		t.Fatalf("expected clear ambiguous-interface error, got %v", err)
	}
}

func TestSelectVendorConfigInterfaceRejectsAmbiguousDarwinPhysicalDevices(t *testing.T) {
	target := VidPid{VID: 0x2dc8, PID: 0x6012}
	first := syntheticInfo("vendor-a", "serial-a", 0xffa0, 0x0001)
	second := syntheticInfo("vendor-b", "serial-b", 0xffa0, 0x0001)
	_, err := selectVendorConfigInterfaceForGOOS(target, []hid.DeviceInfo{second, first}, "darwin")
	if err == nil || !strings.Contains(err.Error(), "ambiguous macOS vendor configuration interfaces") ||
		!strings.Contains(err.Error(), "physical identity cannot be honored") {
		t.Fatalf("expected fail-closed Darwin identity error, got %v", err)
	}

	// Non-Darwin openers consume the selected DeviceInfo path and can honor
	// the deterministic physical choice.
	selected, err := selectVendorConfigInterfaceForGOOS(target, []hid.DeviceInfo{second, first}, "linux")
	if err != nil || selected.Serial != "serial-a" {
		t.Fatalf("Linux should select the deterministic addressable interface: selected=%+v err=%v", selected, err)
	}
}

func TestSelectVendorConfigInterfaceIsOrderIndependent(t *testing.T) {
	target := VidPid{VID: 0x2dc8, PID: 0x6012}
	gamepad := syntheticInfo("gamepad", "controller-1", 0x0001, 0x0005)
	vendorZ := syntheticInfo("vendor-z", "controller-1", 0xffa0, 0x0001)
	vendorA := syntheticInfo("vendor-a", "controller-1", 0xffa0, 0x0001)

	orders := [][]hid.DeviceInfo{
		{gamepad, vendorZ, vendorA},
		{vendorA, gamepad, vendorZ},
		{vendorZ, vendorA, gamepad},
	}
	for i, infos := range orders {
		selected, err := selectVendorConfigInterface(target, infos)
		if err != nil {
			t.Fatalf("order %d: select: %v", i, err)
		}
		if selected.Path != "vendor-a" || !isVendorConfigInterface(selected) {
			t.Fatalf("order %d: expected deterministic vendor-a selection, got %+v", i, selected)
		}
	}
}

func TestHidTransportOpenPassesSelectedVendorInfoToOpener(t *testing.T) {
	target := VidPid{VID: 0x2dc8, PID: 0x6012}
	gamepad := syntheticInfo("gamepad", "controller-1", 0x0001, 0x0005)
	vendor := syntheticInfo("vendor", "controller-1", 0xffa0, 0x0001)
	var opened hid.DeviceInfo
	openCalls := 0
	transport := newHidTransport(
		func(vid, pid uint16) []hid.DeviceInfo {
			if vid != target.VID || pid != target.PID {
				t.Fatalf("unexpected enumeration target %04x:%04x", vid, pid)
			}
			return []hid.DeviceInfo{gamepad, vendor}
		},
		func(info hid.DeviceInfo) (hidDevice, error) {
			openCalls++
			opened = info
			return &syntheticHIDDevice{}, nil
		},
	)

	if err := transport.Open(context.Background(), target); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = transport.Close() }()
	if openCalls != 1 || opened.Path != vendor.Path || opened.UsagePage != 0xffa0 || opened.Usage != 0x0001 {
		t.Fatalf("opener did not receive selected vendor interface: calls=%d info=%+v", openCalls, opened)
	}
}

func TestHidTransportOpenFailsWithoutVendorInterfaceBeforeOpen(t *testing.T) {
	target := VidPid{VID: 0x2dc8, PID: 0x6012}
	openCalls := 0
	transport := newHidTransport(
		func(uint16, uint16) []hid.DeviceInfo {
			return []hid.DeviceInfo{syntheticInfo("gamepad", "controller-1", 0x0001, 0x0005)}
		},
		func(hid.DeviceInfo) (hidDevice, error) {
			openCalls++
			return &syntheticHIDDevice{}, nil
		},
	)

	err := transport.Open(context.Background(), target)
	if err == nil || !strings.Contains(err.Error(), "no vendor configuration HID interface") ||
		!strings.Contains(err.Error(), "0xffa0") || !strings.Contains(err.Error(), "0x0001") {
		t.Fatalf("expected clear missing-vendor error, got %v", err)
	}
	if openCalls != 0 {
		t.Fatalf("opener was called %d times without a vendor interface", openCalls)
	}
}
