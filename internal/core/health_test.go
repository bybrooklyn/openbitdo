package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

var (
	ultimate2 = protocol.VidPid{VID: 0x2dc8, PID: 0x6013}
	retro108  = protocol.VidPid{VID: 0x2dc8, PID: 0x5209}
)

// The interfaces a Retro 108 and an Ultimate 2 expose over USB on Linux.
func attachedHardware() []protocol.EnumeratedDevice {
	return []protocol.EnumeratedDevice{
		{VidPid: ultimate2, Product: "8BitDo Ultimate 2", Serial: "22EC9EA4DF", Path: "/dev/hidraw0", UsagePage: 0xffa0, Usage: 0x01},
		{VidPid: retro108, Product: "8BitDo 8BitDo Retro 108 Keyboard", Path: "/dev/hidraw5", UsagePage: 0x01, Usage: 0x02},
		{VidPid: retro108, Product: "8BitDo 8BitDo Retro 108 Keyboard", Path: "/dev/hidraw6", UsagePage: 0x01, Usage: 0x06, Interface: 1},
		{VidPid: retro108, Product: "8BitDo 8BitDo Retro 108 Keyboard", Path: "/dev/hidraw7", UsagePage: 0x8c, Usage: 0x01, Interface: 2},
	}
}

func TestListDevicesReportsNamesAndConfigChannel(t *testing.T) {
	c := New(Config{})
	c.enumerateDevices = attachedHardware
	listDevices := c.ListDevices
	devices, err := listDevices(context.Background())
	if err != nil || len(devices) != 2 {
		t.Fatalf("expected two devices, got %+v err=%v", devices, err)
	}
	byPID := map[uint16]AppDevice{}
	for _, d := range devices {
		byPID[d.VidPid.PID] = d
	}
	if got := byPID[0x6013]; got.DisplayName != "Ultimate 2" || got.ConfigChannel != ChannelPresent {
		t.Fatalf("unexpected controller: %+v", got)
	}
	// The vendor prefix the device repeats is dropped. A JP108's
	// configuration interface is the one on usage page 0x8c, not 0xffa0.
	if got := byPID[0x5209]; got.DisplayName != "Retro 108 Keyboard" || got.ConfigChannel != ChannelPresent {
		t.Fatalf("unexpected keyboard: %+v", got)
	}
	if got := byPID[0x5209].WorksAs; got != RoleKeyboard {
		t.Fatalf("expected the keyboard to be recognised as one, got %v", got)
	}
}

// An Ultimate 2 in its gamepad mode (0x6012) exposes one HID interface, a
// Generic Desktop gamepad, and no configuration interface.
func TestGamepadModeDeviceIsAWorkingGamepadWithNoConfigChannel(t *testing.T) {
	c := New(Config{})
	pad := protocol.VidPid{VID: 0x2dc8, PID: 0x6012}
	c.enumerateDevices = func() []protocol.EnumeratedDevice {
		return []protocol.EnumeratedDevice{{VidPid: pad, Product: "8BitDo 8BitDo Ultimate 2 Wireless Controller for PC", Serial: "22EC9EA4DF", Path: "/dev/hidraw10", UsagePage: 0x01, Usage: 0x05}}
	}
	listDevices := c.ListDevices
	devices, err := listDevices(context.Background())
	if err != nil || len(devices) != 1 {
		t.Fatalf("expected one device, got %+v err=%v", devices, err)
	}
	if got := devices[0]; got.WorksAs != RoleGamepad || got.ConfigChannel != ChannelAbsent || got.DisplayName != "Ultimate 2 Wireless Controller for PC" {
		t.Fatalf("unexpected device: %+v", got)
	}
}

func TestConfigChannelIsUnknownWhenAnInterfaceHasNoUsageMetadata(t *testing.T) {
	states := configChannelStates([]protocol.EnumeratedDevice{
		{VidPid: retro108, UsagePage: 0x01, Usage: 0x06},
		{VidPid: retro108}, // a platform that cannot describe this interface
	})
	if states[retro108] != ChannelUnknown {
		t.Fatalf("an undescribed interface must not rule the device out, got %v", states[retro108])
	}
}

func TestFriendlyDeviceNameFallsBackToCatalogThenRegistryID(t *testing.T) {
	if got := friendlyDeviceName("", protocol.DeviceProfileFor(retro108)); got != "Retro 108 Mechanical Keyboard" {
		t.Fatalf("expected the catalog name, got %q", got)
	}
	// 0x2100's catalog entry is an "Unconfirmed ..." placeholder.
	if got := friendlyDeviceName("", protocol.DeviceProfileFor(protocol.VidPid{VID: 0x2dc8, PID: 0x2100})); got != "Xcloud" {
		t.Fatalf("expected the registry ID without its prefix, got %q", got)
	}
}

func TestHealthReflectsTheLastProbeNotTheSupportTier(t *testing.T) {
	c := New(Config{})
	pad := AppDevice{VidPid: ultimate2, Serial: "A", SupportTier: protocol.TierFull, ConfigChannel: ChannelPresent}

	if got := c.Health(pad); got.State != HealthUnknown || !got.Reachable() {
		t.Fatalf("an unprobed device is unknown, got %+v", got)
	}

	store := func(entry DiagCacheEntry) {
		entry.RanAt = time.Now()
		c.diagCache[diagCacheKeyFor(pad)] = entry
	}
	checks := func(ok ...bool) protocol.DiagProbeResult {
		var result protocol.DiagProbeResult
		for _, passed := range ok {
			result.CommandChecks = append(result.CommandChecks, protocol.DiagCommandStatus{OK: passed})
		}
		return result
	}

	store(DiagCacheEntry{Result: checks(true, true, false)})
	if got := c.Health(pad); got.State != HealthResponding || got.Answered != 2 || got.Total != 3 {
		t.Fatalf("unexpected health for a partly answering device: %+v", got)
	}

	// A "full" tier device that answers nothing is silent, not supported.
	store(DiagCacheEntry{Result: checks(false, false)})
	if got := c.Health(pad); got.State != HealthSilent || got.Reachable() {
		t.Fatalf("expected a silent device, got %+v", got)
	}

	store(DiagCacheEntry{Err: errProtocol(protocol.ErrDeviceDisconnected(ultimate2))})
	if got := c.Health(pad); got.State != HealthError {
		t.Fatalf("an unclassified probe failure is a plain error, got %+v", got)
	}
	store(DiagCacheEntry{Err: &Error{Kind: KindPermissionDenied, Message: "denied"}})
	if got := c.Health(pad); got.State != HealthNoPermission {
		t.Fatalf("expected no-permission, got %+v", got)
	}

	keyboard := AppDevice{VidPid: retro108, SupportTier: protocol.TierFull, ConfigChannel: ChannelAbsent}
	if got := c.Health(keyboard); got.State != HealthNoChannel || got.Reachable() {
		t.Fatalf("a device with no configuration interface is unreachable, got %+v", got)
	}
}

func TestProbeFailureIsCachedSoItIsNotRetriedEveryRescan(t *testing.T) {
	c := New(Config{})
	c.transportOverride = &failingOpenTransport{}
	c.enumerateDevices = attachedHardware // still plugged in: a failure, not a disconnect
	device := AppDevice{VidPid: ultimate2, Serial: "A"}

	_, err := c.DiagProbeCached(context.Background(), device)
	var coreErr *Error
	if !errors.As(err, &coreErr) || coreErr.Kind != KindProtocol {
		t.Fatalf("expected a protocol error for a device that is still present, got %v", err)
	}
	if !c.HasDiagnosed(device) {
		t.Fatal("a failed probe must be remembered, or every device reload re-opens the device")
	}
	if _, again := c.DiagProbeCached(context.Background(), device); again == nil {
		t.Fatal("the cached entry must return the cached failure")
	}

	// Unplugged, the same failure is a disconnect.
	c.ForgetFailedDiags()
	c.enumerateDevices = func() []protocol.EnumeratedDevice { return nil }
	if _, err := c.DiagProbeCached(context.Background(), device); !errors.As(err, &coreErr) || coreErr.Kind != KindDeviceDisconnected {
		t.Fatalf("expected a disconnect once the device is no longer enumerated, got %v", err)
	}
}

type failingOpenTransport struct{ protocol.MockTransport }

func (*failingOpenTransport) Open(context.Context, protocol.VidPid) error {
	return errors.New("open failed")
}

func TestForgetFailedDiagsKeepsResultsAndDropsFailures(t *testing.T) {
	c := New(Config{})
	failed := AppDevice{VidPid: ultimate2, Serial: "A"}
	answered := AppDevice{VidPid: retro108, Serial: "B"}
	c.diagCache[diagCacheKeyFor(failed)] = DiagCacheEntry{Err: errors.New("open failed")}
	c.diagCache[diagCacheKeyFor(answered)] = DiagCacheEntry{}

	c.ForgetFailedDiags()
	if c.HasDiagnosed(failed) || !c.HasDiagnosed(answered) {
		t.Fatal("only the failed probe should be forgotten")
	}
}

func TestReceiverWhoseControllerIsOffIsNotReachable(t *testing.T) {
	// A real 0x6013 receiver answers for itself while its controller is
	// switched off; those answers must not read as a working controller.
	pad := &protocol.U2Simulator{Off: true}
	c := New(Config{})
	c.transportOverride = pad
	device := AppDevice{VidPid: ultimate2, Serial: "A", SupportTier: protocol.TierFull, ConfigChannel: ChannelPresent}

	if _, err := c.DiagProbeFresh(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	health := c.Health(device)
	if health.State != HealthControllerOff || health.Reachable() || health.Answered == 0 {
		t.Fatalf("expected a receiver that answers but an unreachable controller, got %+v", health)
	}

	pad.Off = false
	if _, err := c.DiagProbeFresh(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if health := c.Health(device); health.State != HealthResponding {
		t.Fatalf("with the controller on it should be reachable, got %+v", health)
	}
}
