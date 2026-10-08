package core

import (
	"bytes"
	"context"
	"encoding/binary"
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
