package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

var padTarget = protocol.VidPid{VID: 0x2dc8, PID: 0x6012}

func padCore(pad *protocol.U2Simulator) *OpenBitdoCore {
	c := New(Config{})
	c.transportOverride = pad
	return c
}

func TestPadProfileOfAnUnconfiguredControllerIsItsDefaults(t *testing.T) {
	pad := &protocol.U2Simulator{Physical: protocol.U2PlatformXInput}
	profile, err := padCore(pad).PadReadProfile(context.Background(), padTarget)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Platform != protocol.U2PlatformXInput || profile.ActiveSlot != 0 {
		t.Fatalf("platform=%d slot=%d", profile.Platform, profile.ActiveSlot)
	}
	slot := profile.Slots[0]
	// On XInput the face buttons follow the Xbox layout.
	if slot.InUse || slot.Buttons[0] != PadB || slot.Buttons[1] != PadA || slot.Buttons[18] != PadNone {
		t.Fatalf("unexpected defaults: inUse=%v buttons=%v", slot.InUse, slot.Buttons)
	}
	if slot.LeftStick != (PadRange{0, 128}) || slot.RightTrigger != (PadRange{0, 255}) || slot.VibrationLeft != 5 {
		t.Fatalf("unexpected default ranges: %+v", slot)
	}
	// Input reports were paused for the exchange and are back on.
	if !pad.InputReports {
		t.Fatal("input reports must be resumed after a read")
	}
}

func TestPadApplyWritesOnlyWhatChangedAndReadsItBack(t *testing.T) {
	pad := &protocol.U2Simulator{Physical: protocol.U2PlatformDInput}
	// Bytes this program does not model must survive an apply.
	pad.Record(protocol.U2PlatformDInput)[0x500] = 0xab
	c := padCore(pad)
	ctx := context.Background()

	profile, err := c.PadReadProfile(ctx, padTarget)
	if err != nil {
		t.Fatal(err)
	}
	slot := &profile.Slots[1]
	slot.Name = "Shooter"
	slot.Buttons[18] = PadA    // back paddle P1 -> A
	slot.Buttons[19] = PadLSUp // back paddle P2 -> left stick up
	slot.LeftStick = PadRange{12, 120}
	slot.RightTrigger = PadRange{0, 128}
	slot.VibrationLeft, slot.VibrationRight = 2, 0
	slot.Options = PadInvertRightY | PadSwapTriggers

	before := len(pad.Frames)
	report, err := c.PadApply(ctx, padTarget, profile)
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	if pad.Commits != 1 || pad.BadCRCs != 0 {
		t.Fatalf("commits=%d badCRCs=%d", pad.Commits, pad.BadCRCs)
	}
	writes := 0
	for _, frame := range pad.Frames[before:] {
		if binary.LittleEndian.Uint16(frame[2:]) == 1 {
			writes++
		}
	}
	// Name (1), buttons (92 bytes = 3), sticks, triggers, vibration,
	// options, slot flag: nine reports, not the 36 a whole record takes.
	if writes != 9 {
		t.Fatalf("expected 9 write reports for the changed sections, got %d", writes)
	}

	record := pad.Record(protocol.U2PlatformDInput)
	if record[0x500] != 0xab {
		t.Fatal("a byte outside the edited sections changed")
	}
	// Slot 2's sections, as the controller stores them.
	if got := record[0x14+32 : 0x14+32+14]; !bytes.Equal(got, []byte("\x00S\x00h\x00o\x00o\x00t\x00e\x00r")) {
		t.Fatalf("name stored as % x", got)
	}
	buttons := record[0xe0+92:]
	if binary.LittleEndian.Uint32(buttons) != 0x20200911 ||
		binary.LittleEndian.Uint32(buttons[4+18*4:]) != 0x2000 || binary.LittleEndian.Uint32(buttons[4+19*4:]) != 0x08000010 {
		t.Fatalf("button map stored wrongly: % x", buttons[:92])
	}
	if got := record[0x98+8 : 0x98+16]; !bytes.Equal(got, []byte{0x11, 0x09, 0x20, 0x20, 12, 120, 0, 128}) {
		t.Fatalf("sticks stored as % x", got)
	}
	if got := record[0x74+12+4 : 0x74+24]; !bytes.Equal(got, []byte{0xcd, 0xcc, 0xcc, 0x3e, 0, 0, 0, 0}) {
		t.Fatalf("vibration stored as % x (want 0.4 and 0.0)", got)
	}
	if binary.LittleEndian.Uint32(record[0xc8+8+4:]) != 0x88 || binary.LittleEndian.Uint32(record[4:]) != 0x20200911 {
		t.Fatal("options or the slot's in-use flag stored wrongly")
	}

	again, err := c.PadReadProfile(ctx, padTarget)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Slots[1]; !got.InUse || got.Name != "Shooter" || got.Buttons != slot.Buttons ||
		got.LeftStick != slot.LeftStick || got.VibrationLeft != 2 || got.VibrationRight != 0 || got.Options != slot.Options {
		t.Fatalf("read back %+v", got)
	}
	if again.Slots[0].InUse || again.Slots[2].InUse {
		t.Fatal("the other slots must be untouched")
	}

	// Applying the profile as read writes nothing.
	commits := pad.Commits
	if report, err := c.PadApply(ctx, padTarget, again); err != nil || !report.WriteApplied || pad.Commits != commits {
		t.Fatalf("an unedited apply should be a no-op: %+v err=%v", report, err)
	}

	// The backup from the first apply puts the controller back.
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatal(err)
	}
	restored, _ := c.PadReadProfile(ctx, padTarget)
	if restored.Slots[1].InUse || restored.Slots[1].Buttons[18] != PadNone {
		t.Fatalf("restore left slot 2 as %+v", restored.Slots[1])
	}
}

func TestPadApplyRollsBackWhenTheControllerDoesNotKeepTheWrite(t *testing.T) {
	pad := &protocol.U2Simulator{Physical: protocol.U2PlatformDInput, ShortAccept: true}
	c := padCore(pad)
	profile, err := c.PadReadProfile(context.Background(), padTarget)
	if err != nil {
		t.Fatal(err)
	}
	profile.Slots[0].Buttons[18] = PadB
	report, err := c.PadApply(context.Background(), padTarget, profile)
	if err != nil {
		t.Fatal(err)
	}
	if report.WriteApplied || !report.RollbackAttempted {
		t.Fatalf("a refused write must not be reported as applied: %+v", report)
	}
}

func TestPadRefusesWhatTheControllerCannotHold(t *testing.T) {
	pad := &protocol.U2Simulator{Physical: protocol.U2PlatformDInput}
	c := padCore(pad)
	ctx := context.Background()
	profile, _ := c.PadReadProfile(ctx, padTarget)

	for name, edit := range map[string]func(*PadSlot){
		"a stick range that is backwards":  func(s *PadSlot) { s.LeftStick = PadRange{100, 20} },
		"a stick range past its maximum":   func(s *PadSlot) { s.RightStick = PadRange{0, 200} },
		"a vibration strength over 5":      func(s *PadSlot) { s.VibrationLeft = 6 },
		"a function the pad does not have": func(s *PadSlot) { s.Buttons[0] = 0x30 },
		"a name longer than 16 characters": func(s *PadSlot) { s.Name = strings.Repeat("n", 17) },
	} {
		edited := profile
		edit(&edited.Slots[0])
		if _, err := c.PadApply(ctx, padTarget, edited); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if pad.Commits != 0 {
		t.Fatal("nothing may be written for a refused profile")
	}

	// A profile read on one mode-switch position is not written to the other.
	pad.Physical = protocol.U2PlatformXInput
	if _, err := c.PadApply(ctx, padTarget, profile); err == nil {
		t.Fatal("a profile for the other platform must be refused")
	}
}

func TestPadIsUnreachableWhileTheControllerIsOff(t *testing.T) {
	pad := &protocol.U2Simulator{Off: true}
	_, err := padCore(pad).PadReadProfile(context.Background(), padTarget)
	if err == nil || !strings.Contains(err.Error(), "off or not connected") {
		t.Fatalf("expected a plain controller-is-off error, got %v", err)
	}
}

func TestPadTargetsAreNamedUniquely(t *testing.T) {
	names := map[string]bool{}
	for _, target := range PadTargets() {
		name := target.String()
		if names[name] || strings.HasPrefix(name, "Function ") {
			t.Errorf("target %#08x has a missing or duplicate name %q", uint32(target), name)
		}
		names[name] = true
	}
	if len(PadTargets()) != 33 {
		t.Fatalf("expected 33 assignable functions, got %d", len(PadTargets()))
	}
}

func TestMockModeHasAWorkingController(t *testing.T) {
	c := New(Config{MockMode: true})
	ctx := context.Background()
	profile, err := c.PadReadProfile(ctx, padTarget)
	if err != nil {
		t.Fatal(err)
	}
	profile.Slots[0].Buttons[18] = PadA
	if report, err := c.PadApply(ctx, padTarget, profile); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	again, _ := c.PadReadProfile(ctx, padTarget)
	if again.Slots[0].Buttons[18] != PadA {
		t.Fatal("the mock controller did not keep the edit")
	}
}

func TestPadMotionAndLightsRoundTrip(t *testing.T) {
	pad := &protocol.U2Simulator{Physical: protocol.U2PlatformXInput}
	c := padCore(pad)
	ctx := context.Background()
	profile, err := c.PadReadProfile(ctx, padTarget)
	if err != nil {
		t.Fatal(err)
	}
	if profile.LightEffect != protocol.U2LightOff || profile.Slots[0].Motion.Target != PadMotionOff {
		t.Fatalf("an unconfigured controller has lights and motion off: effect=%d motion=%+v", profile.LightEffect, profile.Slots[0].Motion)
	}

	slot := &profile.Slots[0]
	slot.Motion = PadMotion{Target: PadMotionRightStick, Button: PadR2, Toggle: true, Sensitivity: 8, DeadZone: 25}
	slot.Lights.TracingColor, slot.Lights.TracingBackground = 0xff0000, 0x00007f
	slot.Lights.FireColor, slot.Lights.FireSpeed = 0xffa500, 12
	slot.Lights.Custom[0], slot.Lights.Custom[23] = 0x00ff00, 0x8b00ff
	profile.LightEffect = protocol.U2LightCustom

	report, err := c.PadApply(ctx, padTarget, profile)
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	record := pad.Record(protocol.U2PlatformXInput)
	// Motion: flag, enabling button, mode 2 (toggle), sensitivity, dead zone, target.
	if got := record[0x494 : 0x494+12]; !bytes.Equal(got, []byte{0x11, 0x09, 0x20, 0x20, 0x00, 0x80, 0, 0, 2, 8, 25, 1}) {
		t.Fatalf("motion stored as % x", got)
	}
	if got := record[0x4b8+4 : 0x4b8+12]; !bytes.Equal(got, []byte{0, 0, 0xff, 0, 0x7f, 0, 0, 0}) {
		t.Fatalf("tracing colours stored as % x", got)
	}
	if record[0x4dc+12] != 12 || binary.LittleEndian.Uint32(record[0x50c+4:]) != 0x00ff00 || binary.LittleEndian.Uint32(record[0x50c+4+23*4:]) != 0x8b00ff {
		t.Fatal("fire speed or per-LED colours stored wrongly")
	}
	if pad.Light != protocol.U2LightCustom {
		t.Fatalf("light effect = %d, want per-LED", pad.Light)
	}

	again, err := c.PadReadProfile(ctx, padTarget)
	if err != nil || again.LightEffect != protocol.U2LightCustom || again.Slots[0].Motion != slot.Motion || again.Slots[0].Lights != slot.Lights {
		t.Fatalf("read back effect=%d motion=%+v err=%v", again.LightEffect, again.Slots[0].Motion, err)
	}

	// Turning motion off stores the controller's own "off" values.
	again.Slots[0].Motion = PadMotion{Sensitivity: 5, DeadZone: 40}
	again.LightEffect = protocol.U2LightOff
	if report, err := c.PadApply(ctx, padTarget, again); err != nil || !report.WriteApplied {
		t.Fatalf("apply off: %+v err=%v", report, err)
	}
	if got := pad.Record(protocol.U2PlatformXInput)[0x494 : 0x494+12]; !bytes.Equal(got, []byte{0, 0, 0x19, 0x20, 0, 0, 0, 0, 0, 5, 40, 0}) {
		t.Fatalf("motion off stored as % x", got)
	}
	if pad.Light != protocol.U2LightOff {
		t.Fatal("the light effect should be off")
	}

	// Motion needs a button that can enable it.
	again.Slots[0].Motion = PadMotion{Target: PadMotionLeftStick, Button: PadHome, Sensitivity: 5, DeadZone: 40}
	if _, err := c.PadApply(ctx, padTarget, again); err == nil {
		t.Fatal("Home cannot enable motion and must be refused")
	}
}

func TestPadMacrosApplyReadBackAndRestore(t *testing.T) {
	pad := &protocol.U2Simulator{Physical: protocol.U2PlatformDInput}
	c := padCore(pad)
	ctx := context.Background()
	profile, err := c.PadReadProfile(ctx, padTarget)
	if err != nil {
		t.Fatal(err)
	}

	combo := PadMacro{Name: "Combo", Trigger: PadPaddle1, Repeat: 1, Steps: []PadMacroStep{
		{Millis: 50, Buttons: uint16(PadA | PadR1), Left: PadStickCentre, Right: PadStickCentre},
		{Millis: 120, Left: PadStickDownRight, Right: PadStickCentre},
		{Millis: 30, Left: PadStickCentre, Right: PadStickCentre},
	}}
	long := PadMacro{Name: "Long", Trigger: PadPaddle2, Repeat: 3, IntervalMillis: 500}
	for i := 0; i < 99; i++ {
		long.Steps = append(long.Steps,
			PadMacroStep{Millis: 20, Buttons: uint16(PadB), Left: PadStickCentre, Right: PadStickCentre},
			PadMacroStep{Millis: 20, Left: PadStickCentre, Right: PadStickCentre})
	}
	profile.Macros[1][0], profile.Macros[1][3] = combo, long
	report, err := c.PadApply(ctx, padTarget, profile)
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}

	// The first step as stored: 50 ms, A+R1, no analog triggers, sticks centred.
	area := pad.MacroArea(uint16(protocol.U2PlatformDInput)<<8 | 1)
	if got := area[:10]; !bytes.Equal(got, []byte{50, 0, 0x00, 0x28, 0, 0, 0x7f, 0x7f, 0x7f, 0x7f}) {
		t.Fatalf("first step stored as % x", got)
	}
	// The header: name, platform, three steps at offset 0, trigger P1.
	header := pad.Record(protocol.U2PlatformDInput)[0x1f4+216+8:]
	if !bytes.Equal(header[:10], []byte("\x00C\x00o\x00m\x00b\x00o")) || header[32] != 1 || header[34] != 3 ||
		binary.LittleEndian.Uint32(header[40:]) != 0x02000000 {
		t.Fatalf("macro header stored as % x", header[:52])
	}
	if pad.Record(protocol.U2PlatformDInput)[0x1f4+216+4] != 2 {
		t.Fatal("the slot should count two macros")
	}

	again, err := c.PadReadProfile(ctx, padTarget)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Macros[1][0], combo) || !reflect.DeepEqual(again.Macros[1][3], long) {
		t.Fatalf("macros read back differently:\n%+v", again.Macros[1][0])
	}
	if !again.Macros[0][0].Empty() || !again.Macros[1][1].Empty() {
		t.Fatal("untouched macro slots must stay empty")
	}
	if got := combo.Steps[0].String(); got != "A+R1 for 50 ms" {
		t.Fatalf("step described as %q", got)
	}

	// Removing a macro drops its header; restoring the backup brings it back.
	again.Macros[1][0] = PadMacro{}
	second, err := c.PadApply(ctx, padTarget, again)
	if err != nil || !second.WriteApplied {
		t.Fatalf("remove: %+v err=%v", second, err)
	}
	if after, _ := c.PadReadProfile(ctx, padTarget); !after.Macros[1][0].Empty() || after.Macros[1][3].Empty() {
		t.Fatal("removing one macro should leave the other")
	}
	if err := c.RestoreBackup(ctx, second.BackupID); err != nil {
		t.Fatal(err)
	}
	if after, _ := c.PadReadProfile(ctx, padTarget); !reflect.DeepEqual(after.Macros[1][0], combo) {
		t.Fatalf("restore should bring the macro back, got %+v", after.Macros[1][0])
	}
}

func TestPadMacroRefusals(t *testing.T) {
	pad := &protocol.U2Simulator{Physical: protocol.U2PlatformDInput}
	c := padCore(pad)
	ctx := context.Background()
	profile, _ := c.PadReadProfile(ctx, padTarget)
	rest := PadMacroStep{Millis: 20, Left: PadStickCentre, Right: PadStickCentre}
	held := PadMacroStep{Millis: 20, Buttons: uint16(PadA), Left: PadStickCentre, Right: PadStickCentre}
	for name, macro := range map[string]PadMacro{
		"a macro that ends holding a button": {Name: "x", Trigger: PadPaddle1, Steps: []PadMacroStep{held}},
		"a macro with no name":               {Trigger: PadPaddle1, Steps: []PadMacroStep{held, rest}},
		"a trigger that cannot play macros":  {Name: "x", Trigger: PadHome, Steps: []PadMacroStep{held, rest}},
		"a step with no duration":            {Name: "x", Trigger: PadPaddle1, Steps: []PadMacroStep{{Left: PadStickCentre, Right: PadStickCentre}, rest}},
	} {
		edited := profile
		edited.Macros[0][0] = macro
		before := len(pad.Frames)
		if _, err := c.PadApply(ctx, padTarget, edited); err == nil {
			t.Errorf("%s must be refused", name)
		}
		for _, frame := range pad.Frames[before:] {
			if cmd := binary.LittleEndian.Uint16(frame[2:]); cmd == 1 || cmd == 0x103 || cmd == 0x104 {
				t.Errorf("%s: nothing may be written or erased, saw cmd %#x", name, cmd)
			}
		}
	}
}

func TestSiblingControllersUseTheirOwnRecordLayout(t *testing.T) {
	ctx := context.Background()
	rest := PadMacroStep{Millis: 20, Left: PadStickCentre, Right: PadStickCentre}
	tap := PadMacroStep{Millis: 40, Buttons: uint16(PadX), Left: PadStickCentre, Right: PadStickCentre}

	// Ultimate 2 Bluetooth: a larger record, with the macro, motion and
	// light sections further along, and P1/P2 numbered the other way round
	// as macro triggers.
	bt := protocol.VidPid{VID: 0x2dc8, PID: 0x600f}
	pad := &protocol.U2Simulator{Physical: protocol.U2PlatformSwitch, RecordSize: protocol.U2BTRecordSize}
	c := padCore(pad)
	profile, err := c.PadReadProfile(ctx, bt)
	if err != nil || !profile.HasMotion || !profile.HasLights {
		t.Fatalf("Ultimate 2 Bluetooth: %+v err=%v", profile.HasMotion, err)
	}
	// On Switch the Star button takes screenshots by default.
	if profile.Slots[0].Buttons[12] != PadScreenshot || profile.Slots[0].Buttons[0] != PadA {
		t.Fatalf("Switch defaults: %v", profile.Slots[0].Buttons[:13])
	}
	profile.Slots[2].Buttons[18] = PadB
	profile.Slots[2].Motion = PadMotion{Target: PadMotionRightStick, Button: PadR2, Sensitivity: 5, DeadZone: 40}
	profile.Slots[2].Lights.Custom[5] = 0x123456
	profile.Macros[2][1] = PadMacro{Name: "bt", Trigger: PadPaddle1, Repeat: 1, Steps: []PadMacroStep{tap, rest}}
	if report, err := c.PadApply(ctx, bt, profile); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	record := pad.Record(protocol.U2PlatformSwitch)
	if len(record) != 0xad0 {
		t.Fatalf("record is %d bytes", len(record))
	}
	if record[0x92c+2*12+11] != PadMotionRightStick || binary.LittleEndian.Uint32(record[0x9a4+2*100+4+5*4:]) != 0x123456 {
		t.Fatal("motion or lights were not written where an Ultimate 2 Bluetooth keeps them")
	}
	header := record[0x68c+2*216+8+52:]
	if header[34] != 2 || binary.LittleEndian.Uint32(header[40:]) != 0x04000000 {
		t.Fatalf("macro header at the wrong place or with the wrong trigger: % x", header[32:44])
	}
	// The Ultimate 2's own offsets must be untouched on this model.
	if !bytes.Equal(record[0x494:0x494+36], make([]byte, 36)) {
		t.Fatal("something was written at the Ultimate 2's motion offset")
	}
	again, err := c.PadReadProfile(ctx, bt)
	if err != nil || again.Slots[2].Buttons[18] != PadB || again.Macros[2][1].Trigger != PadPaddle1 || again.Slots[2].Lights.Custom[5] != 0x123456 {
		t.Fatalf("read back: %+v err=%v", again.Macros[2][1], err)
	}

	// Pro 3: no motion or lights; macros where its record keeps them.
	pro3 := protocol.VidPid{VID: 0x2dc8, PID: 0x6009}
	pad = &protocol.U2Simulator{Physical: protocol.U2PlatformXInput, RecordSize: protocol.Pro3RecordSize}
	c = padCore(pad)
	profile, err = c.PadReadProfile(ctx, pro3)
	if err != nil || profile.HasMotion || profile.HasLights {
		t.Fatalf("Pro 3: motion=%v lights=%v err=%v", profile.HasMotion, profile.HasLights, err)
	}
	profile.Slots[0].LeftTrigger = PadRange{20, 200}
	profile.Macros[0][0] = PadMacro{Name: "p3", Trigger: PadPaddle1, Repeat: 1, Steps: []PadMacroStep{tap, rest}}
	if report, err := c.PadApply(ctx, pro3, profile); err != nil || !report.WriteApplied {
		t.Fatalf("Pro 3 apply: %+v err=%v", report, err)
	}
	record = pad.Record(protocol.U2PlatformDInput) // a Pro 3's first switch position
	if len(record) != 0x92c || record[0xb0+4] != 20 || binary.LittleEndian.Uint32(record[0x68c+8+40:]) != 0x02000000 {
		t.Fatalf("Pro 3 record: %d bytes, trigger % x", len(record), record[0x68c+8+40:0x68c+8+44])
	}
	profile, _ = c.PadReadProfile(ctx, pro3)
	profile.Slots[0].Motion.Target = PadMotionLeftStick
	profile.Slots[0].Motion.Button = PadL2
	if _, err := c.PadApply(ctx, pro3, profile); err == nil {
		t.Fatal("a Pro 3 has no motion setting; writing one must be refused")
	}
}

func TestControllerUnderTheSharedIDIsRoutedToItsProduct(t *testing.T) {
	shared := protocol.VidPid{VID: 0x2dc8, PID: protocol.SharedControllerPID}
	enumerated := func() []protocol.EnumeratedDevice {
		return []protocol.EnumeratedDevice{{VidPid: shared, Product: "8BitDo Controller", Serial: "S1", Path: "/dev/hidraw9", UsagePage: 0xffa0, Usage: 0x01}}
	}
	ctx := context.Background()

	// It says it is an Ultimate 2 receiver: listed as an Ultimate 2 with
	// the profile capability and nothing else.
	pad := &protocol.U2Simulator{ReportsPID: 0x6013, Physical: protocol.U2PlatformXInput}
	c := padCore(pad)
	c.enumerateDevices = enumerated
	listDevices := c.ListDevices
	devices, err := listDevices(ctx)
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices=%+v err=%v", devices, err)
	}
	device := devices[0]
	if device.VidPid != shared || device.Product.PID != 0x6012 || !device.Capability.SupportsU2SlotConfig ||
		device.Capability.SupportsFirmware || device.SupportTier != protocol.TierFull {
		t.Fatalf("unexpected listing: %+v", device)
	}
	if !strings.Contains(device.DisplayName, "Ultimate 2") {
		t.Fatalf("display name = %q", device.DisplayName)
	}
	// The answer is remembered: a second listing does not ask again.
	asked := len(pad.Frames)
	if _, err := listDevices(ctx); err != nil || len(pad.Frames) != asked {
		t.Fatalf("a second listing sent %d more frames", len(pad.Frames)-asked)
	}

	// Its profile is read and written with the Ultimate 2's layout.
	addr := PadAddressOf(device)
	profile, err := c.PadReadProfileAt(ctx, addr)
	if err != nil || profile.Platform != protocol.U2PlatformXInput || !profile.HasLights {
		t.Fatalf("profile: platform=%d err=%v", profile.Platform, err)
	}
	profile.Slots[0].Buttons[18] = PadA
	report, err := c.PadApplyAt(ctx, addr, profile)
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatalf("restore through the shared id: %v", err)
	}

	// A product with no profile support here stays as it was listed.
	pad.ReportsPID = 0x600b // Arcade Controller
	c.ForgetSharedProducts()
	devices, _ = listDevices(ctx)
	if devices[0].Product.PID != 0 || devices[0].Capability.SupportsU2SlotConfig {
		t.Fatalf("an unsupported product must not gain an editor: %+v", devices[0])
	}
	if _, err := c.PadReadProfileAt(ctx, PadAddressOf(devices[0])); err == nil {
		t.Fatal("reading a profile from an unresolved shared id must be refused")
	}
}
