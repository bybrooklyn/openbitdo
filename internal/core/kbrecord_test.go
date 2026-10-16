package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

var (
	retro87Target   = protocol.VidPid{VID: 0x2dc8, PID: 0x2028}
	retro87UKTarget = protocol.VidPid{VID: 0x2dc8, PID: 0x3026}
	retro68Target   = protocol.VidPid{VID: 0x2dc8, PID: 0x203a}
	rivieraTarget   = protocol.VidPid{VID: 0x2dc8, PID: 0x205a}
)

// kbRecordSim is a simulated keyboard shaped like the model at target.
func kbRecordSim(target protocol.VidPid) *protocol.KbRecordSimulator {
	layout, _ := kbRecordLayoutFor(target)
	return &protocol.KbRecordSimulator{
		MacroSlotSize: layout.macroSlotSize, MacroSlots: layout.macroSlots, LightsSize: layout.lightsSize(),
	}
}

// kbRecordSimWithProfile is a simulated keyboard that holds a profile named
// "Desk" with everything at its defaults.
func kbRecordSimWithProfile(target protocol.VidPid) *protocol.KbRecordSimulator {
	layout, _ := kbRecordLayoutFor(target)
	keyboard := kbRecordSim(target)
	record := defaultKbRecord(layout, keyboard.Record())
	binary.LittleEndian.PutUint32(record, kbRecInUse)
	copy(record[kbRecOffName:], "\x00D\x00e\x00s\x00k")
	copy(keyboard.Record(), record)
	return keyboard
}

func kbRecordCore(keyboard *protocol.KbRecordSimulator) *OpenBitdoCore {
	c := New(Config{})
	c.transportOverride = keyboard
	return c
}

// kbRecordWrites counts the frames since from that store something: record
// writes (1), macro prepares and writes (5, 6), colour block prepares and
// writes (0x0d, 0x0e).
func kbRecordWrites(keyboard *protocol.KbRecordSimulator, from int) map[byte]int {
	counts := map[byte]int{}
	for _, frame := range keyboard.Frames[from:] {
		switch frame[2] {
		case 0x01, 0x05, 0x06, 0x0d, 0x0e:
			counts[frame[2]]++
		}
	}
	return counts
}

func keyEntry(keyboard *protocol.KbRecordSimulator, index int) string {
	return hex.EncodeToString(keyboard.Record()[kbRecOffKeys+index*kbRecKeyEntry : kbRecOffKeys+(index+1)*kbRecKeyEntry])
}

func TestRecordKeyboardWithoutAProfileReadsAsItsDefaults(t *testing.T) {
	keyboard := kbRecordSim(retro87Target)
	profile, err := kbRecordCore(keyboard).RecordKeyboardReadProfile(context.Background(), retro87Target)
	if err != nil {
		t.Fatal(err)
	}
	if profile.InUse || profile.ProfileOn || profile.Name != "" {
		t.Fatalf("inUse=%v on=%v name=%q", profile.InUse, profile.ProfileOn, profile.Name)
	}
	for i, target := range profile.Targets {
		if target != (RecordKeyTarget{}) {
			t.Fatalf("table entry %d reads as %+v, want its default", i, target)
		}
	}
	if profile.Volume != 2 || profile.Lighting.Theme != RecordThemeKeyResonance || profile.Lighting.PerKeyOn {
		t.Fatalf("volume=%d theme=%d", profile.Volume, profile.Lighting.Theme)
	}
	if got := profile.Lighting.Themes[RecordThemeStarlight]; got != (RecordTheme{Brightness: 255, Speed: 8, Count: 5, Color: 0xffa500}) {
		t.Fatalf("starlight defaults: %+v", got)
	}
	if got := profile.Lighting.Themes[RecordThemeRipple]; got != (RecordTheme{Brightness: 255, Speed: 8, Background: 0xffa500}) {
		t.Fatalf("ripple defaults: %+v", got)
	}
	if profile.HasTimers || profile.MacroSlots != 10 || profile.MacroStepLimit != 1008 || profile.LightCount != 91 || len(profile.Lighting.PerKey.Colors) != 91 {
		t.Fatalf("model facts: %+v", profile)
	}
	// Key reports were turned off for the exchange, as the vendor
	// application does, and are left off.
	if keyboard.KeyReports {
		t.Fatal("key reports should be off after a read")
	}

	// The key table: eight modifiers, the A and B buttons, 95 keys, K1-K8.
	keys := profile.Keys()
	byIndex := map[int]RecordKey{}
	for _, key := range keys {
		byIndex[key.Index] = key
	}
	if len(keys) != 113 || byIndex[3].Name != "Left Win" || byIndex[12].Name != "A" || byIndex[48].Name != "Enter" ||
		byIndex[107].Usage != 0x63 || !byIndex[9].Port || byIndex[108].Name != "K1" || byIndex[115].Name != "K8" {
		t.Fatalf("unexpected key table (%d keys): %+v", len(keys), keys)
	}
	for _, missing := range []int{8, 11, 58, 116} {
		if _, ok := byIndex[missing]; ok {
			t.Fatalf("table entry %d is not a key", missing)
		}
	}
	// The UK model has the ISO key at 108 and its port buttons after it.
	uk := kbRecordLayoutRetro87UK.keys()
	if len(uk) != 114 || uk[105].Index != 108 || uk[105].Usage != 0x64 || uk[106].Name != "K1" || uk[106].Index != 109 {
		t.Fatalf("unexpected UK key table: %+v", uk[100:])
	}
}

func TestRecordKeyboardApplyWritesOnlyWhatChangedAndReadsItBack(t *testing.T) {
	keyboard := kbRecordSimWithProfile(retro87Target)
	// Bytes this program does not model must survive an apply: the byte
	// after the profile switch, the light idle time, the record's last
	// byte, a table entry that is not a key, and a key assigned something
	// of a kind this program does not understand.
	stored := keyboard.Record()
	stored[0x25], stored[0x5fb], stored[kbRecOffLocks] = 0x5a, 0xee, 0x80
	binary.LittleEndian.PutUint32(stored[kbRecOffLightIdle:], 777)
	copy(stored[kbRecOffKeys+8*kbRecKeyEntry:], bytes.Repeat([]byte{0xc3}, kbRecKeyEntry))
	odd, _ := hex.DecodeString("0c0000003412000006000000")
	copy(stored[kbRecOffKeys+20*kbRecKeyEntry:], odd)
	original := append([]byte(nil), stored...)
	c := kbRecordCore(keyboard)
	ctx := context.Background()

	profile, err := c.RecordKeyboardReadProfile(ctx, retro87Target)
	if err != nil {
		t.Fatal(err)
	}
	if !profile.InUse || profile.Name != "Desk" || profile.Targets[20].Kind != RecordTargetUnknown {
		t.Fatalf("read %+v", profile)
	}
	profile.Name = "Work"
	profile.Targets[12] = RecordKeyTarget{Kind: RecordTargetKey, Modifier: 0xe1, Key: 0x1e} // A -> Shift+1
	profile.Targets[65] = RecordKeyTarget{Kind: RecordTargetOff}                            // Caps Lock off
	profile.Targets[9] = RecordKeyTarget{Kind: RecordTargetMedia, Media: 226}               // A button -> mute
	profile.Targets[87] = RecordKeyTarget{Kind: RecordTargetMouse, Mouse: RecordMouseLeft}  // Right -> left click
	profile.Locks.WinKey = true
	profile.Volume = 4
	profile.Lighting.Theme = RecordThemeBreathing
	profile.Lighting.Themes[RecordThemeBreathing] = RecordTheme{Brightness: 128, Speed: 10, Color: 0x00ff80}

	before := len(keyboard.Frames)
	report, err := c.RecordKeyboardApply(ctx, retro87Target, profile, RuntimeUnlockPolicy{})
	if err != nil || !report.WriteApplied || !report.HasBackupID {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	// Name, four keys, locks, volume, theme and the theme's settings: nine
	// reports, not the 29 a whole record takes, and nothing else stored.
	if writes := kbRecordWrites(keyboard, before); writes[0x01] != 9 || len(writes) != 1 {
		t.Fatalf("expected 9 record writes and nothing else, got %v", writes)
	}
	if keyboard.BadSums != 0 {
		t.Fatalf("%d writes carried a wrong checksum", keyboard.BadSums)
	}

	record := keyboard.Record()
	if got := record[kbRecOffName : kbRecOffName+10]; !bytes.Equal(got, []byte("\x00W\x00o\x00r\x00k\x00\x00")) {
		t.Fatalf("name stored as % x", got)
	}
	for index, want := range map[int]string{
		12: "04000000e11e000001000000", // key code, (key << 8 | modifier), kind 1
		65: "39000000f300000001000000", // 243: does nothing
		9:  "f3000000e200000002000000", // consumer usage 226, kind 2
		87: "4f000000e800000003000000", // mouse action 232, kind 3
		20: "0c0000003412000006000000", // not understood: as it was
		13: "050000000500000000000000", // untouched: itself
	} {
		if got := keyEntry(keyboard, index); got != want {
			t.Fatalf("table entry %d stored as %s, want %s", index, got, want)
		}
	}
	// The lock bits are set beside the bit this program does not name.
	if record[kbRecOffLocks] != 0x81 || record[kbRecOffVolume] != 4 || record[kbRecOffTheme] != 6 {
		t.Fatalf("locks=%#02x volume=%d theme=%d", record[kbRecOffLocks], record[kbRecOffVolume], record[kbRecOffTheme])
	}
	// Breathing: brightness, speed counted down from 10, a 1, then R G B.
	if got := record[0x5da:0x5e0]; !bytes.Equal(got, []byte{128, 1, 1, 0x00, 0xff, 0x80}) {
		t.Fatalf("breathing theme stored as % x", got)
	}
	if record[0x25] != 0x5a || record[0x5fb] != 0xee || binary.LittleEndian.Uint32(record[kbRecOffLightIdle:]) != 777 ||
		!bytes.Equal(record[kbRecOffKeys+8*kbRecKeyEntry:kbRecOffKeys+9*kbRecKeyEntry], bytes.Repeat([]byte{0xc3}, kbRecKeyEntry)) {
		t.Fatal("a byte outside the edited settings changed")
	}

	again, err := c.RecordKeyboardReadProfile(ctx, retro87Target)
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != "Work" || again.Targets != profile.Targets || again.Locks != profile.Locks || again.Volume != 4 ||
		!reflect.DeepEqual(again.Lighting, profile.Lighting) {
		t.Fatalf("read back %+v", again)
	}

	// Applying the profile as read writes nothing.
	before = len(keyboard.Frames)
	if report, err := c.RecordKeyboardApply(ctx, retro87Target, again, RuntimeUnlockPolicy{}); err != nil || !report.WriteApplied || len(kbRecordWrites(keyboard, before)) != 0 {
		t.Fatalf("an unedited apply should write nothing: %+v err=%v writes=%v", report, err, kbRecordWrites(keyboard, before))
	}

	// The backup from the first apply puts the keyboard back.
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(keyboard.Record(), original) {
		t.Fatal("restoring the backup should put the record back as it was")
	}
}

func TestRecordKeyboardKeepsAKeysOwnCode(t *testing.T) {
	// On a French keyboard the key in the A position is stored with Q's
	// code, and is "itself" when it sends Q.
	keyboard := kbRecordSimWithProfile(retro87UKTarget)
	french, _ := hex.DecodeString("140000001400000000000000")
	copy(keyboard.Record()[kbRecOffKeys+12*kbRecKeyEntry:], french)
	c := kbRecordCore(keyboard)
	ctx := context.Background()

	profile, err := c.RecordKeyboardReadProfile(ctx, retro87UKTarget)
	if err != nil || profile.Targets[12].Kind != RecordTargetDefault {
		t.Fatalf("target=%+v err=%v", profile.Targets[12], err)
	}
	profile.Targets[12] = RecordKeyTarget{Kind: RecordTargetKey, Key: 0x04}
	if report, err := c.RecordKeyboardApply(ctx, retro87UKTarget, profile, RuntimeUnlockPolicy{}); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	if got := keyEntry(keyboard, 12); got != "140000000400000001000000" {
		t.Fatalf("stored as %s", got)
	}
	// Back to itself: its own code again, as kind 0. So is naming the key
	// it already is.
	for _, target := range []RecordKeyTarget{{}, {Kind: RecordTargetKey, Key: 0x14}} {
		profile.Targets[12] = RecordKeyTarget{Kind: RecordTargetKey, Key: 0x04}
		if _, err := c.RecordKeyboardApply(ctx, retro87UKTarget, profile, RuntimeUnlockPolicy{}); err != nil {
			t.Fatal(err)
		}
		profile.Targets[12] = target
		if report, err := c.RecordKeyboardApply(ctx, retro87UKTarget, profile, RuntimeUnlockPolicy{}); err != nil || !report.WriteApplied {
			t.Fatalf("apply: %+v err=%v", report, err)
		}
		if got := keyEntry(keyboard, 12); got != "140000001400000000000000" {
			t.Fatalf("%+v stored as %s", target, got)
		}
	}
	// A modifier alone is stored as its usage; a port button that does
	// nothing is at its default.
	profile.Targets[13] = RecordKeyTarget{Kind: RecordTargetKey, Modifier: 0xe0}
	profile.Targets[109] = RecordKeyTarget{Kind: RecordTargetOff}
	profile.Targets[14] = RecordKeyTarget{Kind: RecordTargetFn}
	if report, err := c.RecordKeyboardApply(ctx, retro87UKTarget, profile, RuntimeUnlockPolicy{}); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	if keyEntry(keyboard, 13) != "05000000e000000001000000" || keyEntry(keyboard, 109) != "f3000000f300000000000000" ||
		keyEntry(keyboard, 14) != "06000000f200000001000000" {
		t.Fatalf("stored as %s, %s, %s", keyEntry(keyboard, 13), keyEntry(keyboard, 109), keyEntry(keyboard, 14))
	}
}

func TestRecordKeyboardApplyRollsBackWhenTheKeyboardIgnoresAWrite(t *testing.T) {
	keyboard := kbRecordSimWithProfile(retro87Target)
	original := append([]byte(nil), keyboard.Record()...)
	c := kbRecordCore(keyboard)
	ctx := context.Background()
	profile, err := c.RecordKeyboardReadProfile(ctx, retro87Target)
	if err != nil {
		t.Fatal(err)
	}
	profile.Volume = 5
	profile.Targets[12] = RecordKeyTarget{Kind: RecordTargetKey, Key: 0x05}

	// Every write is acknowledged and none is kept: only the readback
	// can tell.
	keyboard.IgnoreWrites = true
	report, err := c.RecordKeyboardApply(ctx, retro87Target, profile, RuntimeUnlockPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if report.WriteApplied || !report.RollbackAttempted || !strings.Contains(report.WriteError, "readback mismatch") {
		t.Fatalf("an ignored write must be reported and rolled back: %+v", report)
	}
	if !report.RollbackSucceeded || !bytes.Equal(keyboard.Record(), original) {
		t.Fatalf("the keyboard should be as it was: %+v", report)
	}

	// A keyboard that takes only part of each write fails the apply, and
	// then the rollback too; that is reported, not hidden.
	keyboard.IgnoreWrites, keyboard.ShortAccept = false, true
	report, err = c.RecordKeyboardApply(ctx, retro87Target, profile, RuntimeUnlockPolicy{})
	if err != nil || report.WriteApplied || !report.RollbackFailed() || report.RollbackError == "" {
		t.Fatalf("report=%+v err=%v", report, err)
	}

	// A keyboard that holds what it is given is rolled back for real when
	// a later step fails: here the macro, which has nowhere to go.
	small := &protocol.KbRecordSimulator{MacroSlotSize: 4096, MacroSlots: 1, LightsSize: kbRecordLayoutRetro87.lightsSize()}
	copy(small.Record(), original)
	c = kbRecordCore(small)
	profile, _ = c.RecordKeyboardReadProfile(ctx, retro87Target)
	profile.Volume = 5
	profile.Targets[66] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 3}
	profile.Macros[3] = RecordMacro{Name: "Lost", Repeat: 1, Steps: []RecordMacroStep{{Kind: RecordStepPress, Key: 4}}}
	report, err = c.RecordKeyboardApply(ctx, retro87Target, profile, RuntimeUnlockPolicy{})
	if err != nil || report.WriteApplied || !report.RollbackSucceeded {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if !bytes.Equal(small.Record(), original) {
		t.Fatal("the record should have been written back after the macro failed")
	}
}

func TestRecordKeyboardRefusesOutOfRangeValuesBeforeAnythingIsSent(t *testing.T) {
	ctx := context.Background()
	long := RecordMacro{Repeat: 1, Steps: make([]RecordMacroStep, 1009)}
	for i := range long.Steps {
		long.Steps[i] = RecordMacroStep{Kind: RecordStepWait, Millis: 1}
	}
	press := []RecordMacroStep{{Kind: RecordStepPress, Key: 4}}
	for name, edit := range map[string]func(*RecordKeyboardProfile){
		"volume":         func(p *RecordKeyboardProfile) { p.Volume = 6 },
		"volume zero":    func(p *RecordKeyboardProfile) { p.Volume = 0 },
		"empty name":     func(p *RecordKeyboardProfile) { p.Name = "" },
		"long name":      func(p *RecordKeyboardProfile) { p.Name = "seventeen letters" },
		"unknown target": func(p *RecordKeyboardProfile) { p.Targets[12] = RecordKeyTarget{Kind: RecordTargetUnknown} },
		"no key named":   func(p *RecordKeyboardProfile) { p.Targets[12] = RecordKeyTarget{Kind: RecordTargetKey} },
		"not a key":      func(p *RecordKeyboardProfile) { p.Targets[12] = RecordKeyTarget{Kind: RecordTargetKey, Key: 0xf0} },
		"not a modifier": func(p *RecordKeyboardProfile) {
			p.Targets[12] = RecordKeyTarget{Kind: RecordTargetKey, Modifier: 4, Key: 5}
		},
		"modifier as key":    func(p *RecordKeyboardProfile) { p.Targets[12] = RecordKeyTarget{Kind: RecordTargetKey, Key: 0xe1} },
		"media":              func(p *RecordKeyboardProfile) { p.Targets[12] = RecordKeyTarget{Kind: RecordTargetMedia, Media: 999} },
		"mouse":              func(p *RecordKeyboardProfile) { p.Targets[12] = RecordKeyTarget{Kind: RecordTargetMouse, Mouse: 242} },
		"macro slot":         func(p *RecordKeyboardProfile) { p.Targets[12] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 10} },
		"not a key entry":    func(p *RecordKeyboardProfile) { p.Targets[8] = RecordKeyTarget{Kind: RecordTargetOff} },
		"timers":             func(p *RecordKeyboardProfile) { p.SleepMinutes, p.LightsOffMinutes = 10, 5 },
		"theme":              func(p *RecordKeyboardProfile) { p.Lighting.Theme = 8 },
		"brightness":         func(p *RecordKeyboardProfile) { p.Lighting.Themes[RecordThemeSingle].Brightness = 256 },
		"speed":              func(p *RecordKeyboardProfile) { p.Lighting.Themes[RecordThemeLoop].Speed = 11 },
		"speed it lacks":     func(p *RecordKeyboardProfile) { p.Lighting.Themes[RecordThemeSingle].Speed = 5 },
		"count":              func(p *RecordKeyboardProfile) { p.Lighting.Themes[RecordThemeStarlight].Count = 4 },
		"direction":          func(p *RecordKeyboardProfile) { p.Lighting.Themes[RecordThemeColorRipple].Direction = 6 },
		"colour":             func(p *RecordKeyboardProfile) { p.Lighting.Themes[RecordThemeSingle].Color = 0x1000000 },
		"colour it lacks":    func(p *RecordKeyboardProfile) { p.Lighting.Themes[RecordThemeLoop].Color = 0xff0000 },
		"off theme settings": func(p *RecordKeyboardProfile) { p.Lighting.Themes[RecordThemeOff].Brightness = 1 },
		"per-key effect":     func(p *RecordKeyboardProfile) { p.Lighting.PerKey.Effect = 4 },
		"per-key colour":     func(p *RecordKeyboardProfile) { p.Lighting.PerKey.Colors[0] = 0x1000000 },
		"per-key count":      func(p *RecordKeyboardProfile) { p.Lighting.PerKey.Colors = p.Lighting.PerKey.Colors[:90] },
		"macro without steps": func(p *RecordKeyboardProfile) {
			p.Targets[12] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 0}
		},
		"macro too long": func(p *RecordKeyboardProfile) {
			p.Targets[12], p.Macros[0] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 0}, long
		},
		"macro step kind": func(p *RecordKeyboardProfile) {
			p.Targets[12] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 0}
			p.Macros[0] = RecordMacro{Repeat: 1, Steps: []RecordMacroStep{{Kind: 4}}}
		},
		"macro step key": func(p *RecordKeyboardProfile) {
			p.Targets[12] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 0}
			p.Macros[0] = RecordMacro{Repeat: 1, Steps: []RecordMacroStep{{Kind: RecordStepPress, Key: 0xf0}}}
		},
		"macro wait": func(p *RecordKeyboardProfile) {
			p.Targets[12] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 0}
			p.Macros[0] = RecordMacro{Repeat: 1, Steps: []RecordMacroStep{{Kind: RecordStepWait, Millis: 60001}}}
		},
		"macro repeat": func(p *RecordKeyboardProfile) {
			p.Targets[12], p.Macros[0] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 0}, RecordMacro{Repeat: 100, Steps: press}
		},
		"macro play mode": func(p *RecordKeyboardProfile) {
			p.Targets[12], p.Macros[0] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 0}, RecordMacro{Repeat: 1, Mode: RecordMacroHold, Steps: press}
		},
		"macro on two keys": func(p *RecordKeyboardProfile) {
			p.Targets[12] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 0}
			p.Targets[13], p.Macros[0] = p.Targets[12], RecordMacro{Repeat: 1, Steps: press}
		},
	} {
		keyboard := kbRecordSimWithProfile(retro87Target)
		original := append([]byte(nil), keyboard.Record()...)
		c := kbRecordCore(keyboard)
		profile, err := c.RecordKeyboardReadProfile(ctx, retro87Target)
		if err != nil {
			t.Fatal(err)
		}
		edit(&profile)
		before := len(keyboard.Frames)
		report, err := c.RecordKeyboardApply(ctx, retro87Target, profile, RuntimeUnlockPolicy{})
		var coreErr *Error
		if !errors.As(err, &coreErr) || coreErr.Kind != KindInvalidState || report.HasBackupID {
			t.Fatalf("%s: expected the apply to be refused, got report=%+v err=%v", name, report, err)
		}
		if writes := kbRecordWrites(keyboard, before); len(writes) != 0 || !bytes.Equal(keyboard.Record(), original) {
			t.Fatalf("%s: something was sent before the value was refused: %v", name, writes)
		}
	}
}

func TestRecordKeyboardMacroIsStoredTheWayTheVendorStoresIt(t *testing.T) {
	keyboard := kbRecordSimWithProfile(retro87Target)
	c := kbRecordCore(keyboard)
	ctx := context.Background()
	profile, err := c.RecordKeyboardReadProfile(ctx, retro87Target)
	if err != nil {
		t.Fatal(err)
	}
	// F1 plays the macro in slot 3: A down, 100 ms, A up, three times
	// with 250 ms between.
	profile.Targets[66] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 2}
	profile.Macros[2] = RecordMacro{
		Name: "Tap", Repeat: 3, IntervalMs: 250,
		Steps: []RecordMacroStep{{Kind: RecordStepPress, Key: 4}, {Kind: RecordStepWait, Millis: 100}, {Kind: RecordStepRelease, Key: 4}},
	}
	before := len(keyboard.Frames)
	report, err := c.RecordKeyboardApply(ctx, retro87Target, profile, RuntimeUnlockPolicy{})
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	// One key entry, one prepare, the header in one report and the steps
	// in another; the key is written before its macro.
	if writes := kbRecordWrites(keyboard, before); writes[0x01] != 1 || writes[0x05] != 1 || writes[0x06] != 2 || len(writes) != 3 {
		t.Fatalf("writes = %v", writes)
	}
	var order []byte
	for _, frame := range keyboard.Frames[before:] {
		if frame[2] == 0x01 || frame[2] == 0x05 || frame[2] == 0x06 {
			order = append(order, frame[2])
		}
	}
	if !bytes.Equal(order, []byte{1, 5, 6, 6}) || keyboard.Unprepared != 0 {
		t.Fatalf("order=%v unprepared=%d", order, keyboard.Unprepared)
	}
	// The key's target is the slot's offset, 2 x 4096, as kind 4.
	if got := keyEntry(keyboard, 66); got != "3a0000000020000004000000" {
		t.Fatalf("key stored as %s", got)
	}
	slot := keyboard.Macros()[8192:]
	// Mark, the playing key's table entry, the name from byte 5, then at
	// 38 the step count, the interval and the repeat count.
	wantHeader, _ := hex.DecodeString("02092020" + "42" + "005400610070" + strings.Repeat("00", 26) + "00" + "0300" + "fa00" + "03" + "00" + "00000000")
	if !bytes.Equal(slot[:48], wantHeader) {
		t.Fatalf("header stored as % x", slot[:48])
	}
	if want, _ := hex.DecodeString("010400000300640002040000"); !bytes.Equal(slot[64:76], want) {
		t.Fatalf("steps stored as % x", slot[64:76])
	}
	// Between header and steps, and after them, the slot is as prepared.
	if slot[48] != 0xff || slot[63] != 0xff || slot[76] != 0xff {
		t.Fatal("bytes outside the header and steps were written")
	}

	again, err := c.RecordKeyboardReadProfile(ctx, retro87Target)
	if err != nil || !reflect.DeepEqual(again.Macros, profile.Macros) || again.Targets[66] != profile.Targets[66] {
		t.Fatalf("read back %+v err=%v", again.Macros[2], err)
	}

	// Changing only the macro rewrites only the macro.
	again.Macros[2].Repeat = 0
	before = len(keyboard.Frames)
	if report, err := c.RecordKeyboardApply(ctx, retro87Target, again, RuntimeUnlockPolicy{}); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	if writes := kbRecordWrites(keyboard, before); writes[0x01] != 0 || writes[0x05] != 1 || writes[0x06] != 2 {
		t.Fatalf("writes = %v", writes)
	}
	if keyboard.Macros()[8192+42] != 0 {
		t.Fatal("the repeat count should now be 0")
	}

	// Taking the macro off its key rewrites the key and leaves the slot.
	again.Targets[66] = RecordKeyTarget{}
	stored := append([]byte(nil), keyboard.Macros()...)
	before = len(keyboard.Frames)
	if report, err := c.RecordKeyboardApply(ctx, retro87Target, again, RuntimeUnlockPolicy{}); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	if writes := kbRecordWrites(keyboard, before); writes[0x01] != 1 || len(writes) != 1 || !bytes.Equal(keyboard.Macros(), stored) {
		t.Fatalf("writes = %v", writes)
	}
	if got := keyEntry(keyboard, 66); got != "3a0000003a00000000000000" {
		t.Fatalf("key stored as %s", got)
	}
	freed, _ := c.RecordKeyboardReadProfile(ctx, retro87Target)
	if len(freed.Macros[2].Steps) != 0 {
		t.Fatal("a slot no key plays reads as empty")
	}
}

func TestRecordKeyboardRetro68HasSmallerSlotsTimersAndPlayModes(t *testing.T) {
	keyboard := kbRecordSimWithProfile(retro68Target)
	keyboard.Record()[kbRecOffSleep], keyboard.Record()[kbRecOffLightsOff] = 30, 30
	c := kbRecordCore(keyboard)
	ctx := context.Background()
	profile, err := c.RecordKeyboardReadProfile(ctx, retro68Target)
	if err != nil {
		t.Fatal(err)
	}
	if !profile.HasTimers || profile.MacroSlots != 8 || profile.MacroStepLimit != 200 || profile.LightCount != 72 ||
		profile.SleepMinutes != 30 || profile.LightsOffMinutes != 30 {
		t.Fatalf("model facts: %+v", profile)
	}
	steps := make([]RecordMacroStep, 200)
	for i := range steps {
		steps[i] = RecordMacroStep{Kind: RecordStepPress + byte(i%2), Key: 0x2c}
	}
	profile.SleepMinutes, profile.LightsOffMinutes = 15, 10
	profile.Targets[52] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 2} // Space
	profile.Macros[2] = RecordMacro{Name: "Hop", Repeat: 1, Mode: RecordMacroHold, Steps: steps}

	before := len(keyboard.Frames)
	report, err := c.RecordKeyboardApply(ctx, retro68Target, profile, RuntimeUnlockPolicy{})
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	// The key and the two timers, each a write of its own; then the
	// macro: a prepare, the header, and 800 bytes of steps in 16 reports.
	if writes := kbRecordWrites(keyboard, before); writes[0x01] != 3 || writes[0x05] != 1 || writes[0x06] != 17 || keyboard.Unprepared != 0 {
		t.Fatalf("writes = %v unprepared=%d", writes, keyboard.Unprepared)
	}
	record := keyboard.Record()
	if record[kbRecOffSleep] != 15 || record[kbRecOffLightsOff] != 10 {
		t.Fatalf("timers stored as %d and %d", record[kbRecOffSleep], record[kbRecOffLightsOff])
	}
	// Slots are 1024 bytes here: slot 3 starts at 2048.
	if got := keyEntry(keyboard, 52); got != "2c0000000008000004000000" {
		t.Fatalf("key stored as %s", got)
	}
	slot := keyboard.Macros()[2048:3072]
	if slot[4] != 52 || binary.LittleEndian.Uint16(slot[38:]) != 200 || slot[43] != RecordMacroHold || slot[64+799] != 0 || slot[64+800] != 0xff {
		t.Fatalf("macro stored wrongly: % x", slot[:64])
	}
	if keyboard.Macros()[1024] != 0xff || keyboard.Macros()[3072] != 0xff {
		t.Fatal("a neighbouring slot was written")
	}

	for name, edit := range map[string]func(*RecordKeyboardProfile){
		"lights after sleep": func(p *RecordKeyboardProfile) { p.SleepMinutes, p.LightsOffMinutes = 10, 15 },
		"odd minutes":        func(p *RecordKeyboardProfile) { p.SleepMinutes = 7 },
		"too long":           func(p *RecordKeyboardProfile) { p.SleepMinutes = 35 },
		"201 steps": func(p *RecordKeyboardProfile) {
			p.Macros[2].Steps = append(p.Macros[2].Steps, RecordMacroStep{Kind: RecordStepWait})
		},
		"ninth slot": func(p *RecordKeyboardProfile) { p.Targets[52] = RecordKeyTarget{Kind: RecordTargetMacro, Macro: 8} },
		"fn":         func(p *RecordKeyboardProfile) { p.Targets[12] = RecordKeyTarget{Kind: RecordTargetFn} },
	} {
		edited, _ := c.RecordKeyboardReadProfile(ctx, retro68Target)
		edit(&edited)
		before := len(keyboard.Frames)
		if _, err := c.RecordKeyboardApply(ctx, retro68Target, edited, RuntimeUnlockPolicy{}); err == nil || len(kbRecordWrites(keyboard, before)) != 0 {
			t.Fatalf("%s: expected a refusal with nothing sent, got err=%v", name, err)
		}
	}
}

func TestRecordKeyboardPerKeyLightsAreWrittenWhole(t *testing.T) {
	keyboard := kbRecordSimWithProfile(retro87Target)
	// As the vendor application leaves a new block, with one of the bytes
	// this program does not model set.
	block := keyboard.Lights()
	block[0], block[3], block[4], block[6] = 255, 3, 5, 0x77
	c := kbRecordCore(keyboard)
	ctx := context.Background()
	profile, err := c.RecordKeyboardReadProfile(ctx, retro87Target)
	if err != nil {
		t.Fatal(err)
	}
	if got := profile.Lighting.PerKey; got.Brightness != 255 || got.Speed != 8 || got.Count != 5 || got.Effect != RecordPerKeyOff {
		t.Fatalf("read %+v", got)
	}
	profile.Lighting.PerKeyOn = true
	profile.Lighting.PerKey.Effect = RecordPerKeySteady
	profile.Lighting.PerKey.Brightness = 200
	profile.Lighting.PerKey.Colors[3] = 0x102030
	profile.Lighting.PerKey.Colors[90] = 0xffffff

	before := len(keyboard.Frames)
	report, err := c.RecordKeyboardApply(ctx, retro87Target, profile, RuntimeUnlockPolicy{})
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	// The switch is one byte of the record; the block is prepared once
	// and its 283 bytes written in six reports.
	if writes := kbRecordWrites(keyboard, before); writes[0x01] != 1 || writes[0x0d] != 1 || writes[0x0e] != 6 || len(writes) != 3 || keyboard.Unprepared != 0 {
		t.Fatalf("writes = %v unprepared=%d", writes, keyboard.Unprepared)
	}
	if keyboard.Record()[kbRecOffPerKey] != 1 {
		t.Fatal("the per-key switch should be on")
	}
	block = keyboard.Lights()
	// Brightness, a 1, the effect, speed, count, four kept bytes.
	if want := []byte{200, 1, 1, 3, 5, 0, 0x77, 0, 0}; !bytes.Equal(block[:9], want) {
		t.Fatalf("settings stored as % x", block[:9])
	}
	if !bytes.Equal(block[9+9:9+12], []byte{0x10, 0x20, 0x30}) || !bytes.Equal(block[9+270:9+273], []byte{0xff, 0xff, 0xff}) || block[282] != 1 {
		t.Fatalf("colours stored wrongly: % x ... % x", block[9:24], block[270:])
	}
	again, _ := c.RecordKeyboardReadProfile(ctx, retro87Target)
	if !reflect.DeepEqual(again.Lighting, profile.Lighting) {
		t.Fatalf("read back %+v", again.Lighting.PerKey)
	}

	// The backup puts both the switch and the block back.
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatal(err)
	}
	if keyboard.Record()[kbRecOffPerKey] != 0 || keyboard.Lights()[0] != 255 || keyboard.Lights()[2] != 0 || keyboard.Lights()[9+9] != 0 {
		t.Fatalf("after restore: % x", keyboard.Lights()[:24])
	}
}

func TestRecordKeyboardFirstChangeCreatesAWholeProfile(t *testing.T) {
	keyboard := kbRecordSim(retro87Target)
	// With no profile these bytes still belong to the keyboard.
	keyboard.Record()[kbRecOffProfileOn], keyboard.Record()[0x25] = 1, 7
	c := kbRecordCore(keyboard)
	ctx := context.Background()
	profile, err := c.RecordKeyboardReadProfile(ctx, retro87Target)
	if err != nil || profile.InUse {
		t.Fatalf("inUse=%v err=%v", profile.InUse, err)
	}

	// Applying it untouched creates nothing.
	if report, err := c.RecordKeyboardApply(ctx, retro87Target, profile, RuntimeUnlockPolicy{}); err != nil || !report.WriteApplied || len(kbRecordWrites(keyboard, 0)) != 0 {
		t.Fatalf("an unedited apply should write nothing: %+v err=%v", report, err)
	}
	profile.Volume = 3
	profile.Targets[12] = RecordKeyTarget{Kind: RecordTargetKey, Key: 0x05}
	if _, err := c.RecordKeyboardApply(ctx, retro87Target, profile, RuntimeUnlockPolicy{}); err == nil || len(kbRecordWrites(keyboard, 0)) != 0 {
		t.Fatalf("a profile without a name must be refused: %v", err)
	}
	profile.Name = "First"
	report, err := c.RecordKeyboardApply(ctx, retro87Target, profile, RuntimeUnlockPolicy{})
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	// The whole record in one go: 1532 bytes at 53 a report.
	if writes := kbRecordWrites(keyboard, 0); writes[0x01] != 29 || len(writes) != 1 {
		t.Fatalf("writes = %v", writes)
	}
	record := keyboard.Record()
	if binary.LittleEndian.Uint32(record) != 0x20200902 || record[kbRecOffProfileOn] != 1 || record[0x25] != 7 {
		t.Fatalf("header stored as % x", record[:0x28])
	}
	for index, want := range map[int]string{
		0:   "e0000000e000000000000000",
		8:   "000000000000000000000000", // not a key
		9:   "f3000000f300000000000000", // a port button: does nothing
		12:  "040000000500000001000000", // the edit
		13:  "050000000500000000000000",
		107: "630000006300000000000000",
		115: "f3000000f300000000000000",
		116: "000000000000000000000000",
	} {
		if got := keyEntry(keyboard, index); got != want {
			t.Fatalf("table entry %d stored as %s, want %s", index, got, want)
		}
	}
	// Locks, volume, theme, a pad byte, the light idle time, then the
	// seven themes' settings and the two switches.
	want, _ := hex.DecodeString("00" + "03" + "01" + "00" + "2c010000" +
		"ff01ffa500" + "ff03" + "ff0300" + "ff0301ffa500" + "ff0301ffa500" + "ff0301000000ffa500" + "ff030501000000ffa500" +
		"00" + "00" + "00")
	if !bytes.Equal(record[kbRecOffLocks:], want) {
		t.Fatalf("tail stored as % x", record[kbRecOffLocks:])
	}
	again, _ := c.RecordKeyboardReadProfile(ctx, retro87Target)
	if !again.InUse || again.Name != "First" || again.Volume != 3 || again.Targets != profile.Targets || !again.ProfileOn {
		t.Fatalf("read back %+v", again)
	}

	// The UK model's defaults depend on its national layout, which is not
	// known here: no profile is created for it.
	uk := kbRecordSim(retro87UKTarget)
	c = kbRecordCore(uk)
	profile, _ = c.RecordKeyboardReadProfile(ctx, retro87UKTarget)
	profile.Name, profile.Volume = "First", 3
	if _, err := c.RecordKeyboardApply(ctx, retro87UKTarget, profile, RuntimeUnlockPolicy{}); err == nil || len(kbRecordWrites(uk, 0)) != 0 {
		t.Fatalf("creating a profile on the UK model must be refused: %v", err)
	}

	// A Retro 68's volume is not part of a new profile: it is kept.
	small := kbRecordSim(retro68Target)
	small.Record()[kbRecOffVolume], small.Record()[kbRecOffSleep], small.Record()[kbRecOffLightsOff] = 4, 20, 10
	c = kbRecordCore(small)
	profile, _ = c.RecordKeyboardReadProfile(ctx, retro68Target)
	if profile.Volume != 4 || profile.SleepMinutes != 20 {
		t.Fatalf("volume=%d sleep=%d", profile.Volume, profile.SleepMinutes)
	}
	profile.Name = "Small"
	if report, err := c.RecordKeyboardApply(ctx, retro68Target, profile, RuntimeUnlockPolicy{}); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	if got := small.Record(); got[kbRecOffVolume] != 4 || got[kbRecOffSleep] != 20 || got[kbRecOffLightsOff] != 10 || binary.LittleEndian.Uint32(got) != kbRecInUse {
		t.Fatalf("stored volume=%d sleep=%d lights=%d", got[kbRecOffVolume], got[kbRecOffSleep], got[kbRecOffLightsOff])
	}
}

func TestRecordKeyboardRivieraNumbersItsThemesWithoutLoopAndColourRipple(t *testing.T) {
	layout, ok := kbRecordLayoutFor(rivieraTarget)
	if !ok || !layout.timers || layout.macroSlotSize != 1024 {
		t.Fatalf("layout = %+v", layout)
	}
	record := defaultKbRecord(layout, make([]byte, protocol.KbRecordSize))
	binary.LittleEndian.PutUint32(record, kbRecInUse)
	record[kbRecOffTheme] = 4
	was, err := decodeKbRecordProfile(record, make([]byte, layout.lightsSize()), layout)
	if err != nil || was.Lighting.Theme != RecordThemeBreathing {
		t.Fatalf("theme=%d err=%v", was.Lighting.Theme, err)
	}
	edited := was
	edited.Lighting.Theme = RecordThemeRipple
	spans, err := encodeKbRecord(record, layout, edited, was)
	if err != nil || len(spans) != 1 || spans[0] != (kbSpan{kbRecOffTheme, 1}) || record[kbRecOffTheme] != 5 {
		t.Fatalf("spans=%v stored=%d err=%v", spans, record[kbRecOffTheme], err)
	}
	for _, theme := range []int{RecordThemeLoop, RecordThemeColorRipple} {
		edited.Lighting.Theme = theme
		if _, err := encodeKbRecord(record, layout, edited, was); err == nil {
			t.Fatalf("theme %d must be refused on this keyboard", theme)
		}
	}
	edited = was
	edited.Lighting.Themes[RecordThemeLoop].Speed = 2
	if _, err := encodeKbRecord(record, layout, edited, was); err == nil {
		t.Fatal("a theme this keyboard lacks must not be given settings")
	}

	// Its tier is still detect-only: nothing is exchanged with one.
	keyboard := kbRecordSim(rivieraTarget)
	_, err = kbRecordCore(keyboard).RecordKeyboardReadProfile(context.Background(), rivieraTarget)
	var coreErr *Error
	if !errors.As(err, &coreErr) || coreErr.Kind != KindPolicyDenied || len(keyboard.Frames) != 0 {
		t.Fatalf("err=%v frames=%d", err, len(keyboard.Frames))
	}
}

func TestRecordKeyboardIsNotTouchedWithoutTheUnlock(t *testing.T) {
	ctx := context.Background()
	// With no simulator in place a real keyboard would be opened: reading
	// needs advanced mode, writing the whole unlock ceremony.
	c := New(Config{})
	var coreErr *Error
	if _, err := c.RecordKeyboardReadProfile(ctx, retro87Target); !errors.As(err, &coreErr) || coreErr.Reason != ReasonExperimentalRequired {
		t.Fatalf("read without advanced mode: %v", err)
	}
	unlocked := RuntimeUnlockPolicy{AdvancedMode: true, AcknowledgedRisk: true, UnlockFilePresent: true}
	if _, err := c.RecordKeyboardApply(ctx, retro87Target, RecordKeyboardProfile{}, unlocked); !errors.As(err, &coreErr) || coreErr.Reason != ReasonNotHardwareConfirmed {
		t.Fatalf("apply without advanced mode: %v", err)
	}
	c.SetAdvancedMode(true)
	for _, policy := range []RuntimeUnlockPolicy{
		{}, {AdvancedMode: true, AcknowledgedRisk: true}, {AdvancedMode: true, UnlockFilePresent: true}, {AcknowledgedRisk: true, UnlockFilePresent: true},
	} {
		if _, err := c.RecordKeyboardApply(ctx, retro87Target, RecordKeyboardProfile{}, policy); !errors.As(err, &coreErr) || coreErr.Reason != ReasonNotHardwareConfirmed {
			t.Fatalf("apply with %+v: %v", policy, err)
		}
	}
	// Mock mode's simulated devices are not keyboards of this kind.
	mock := New(Config{MockMode: true})
	if _, err := mock.RecordKeyboardReadProfile(ctx, retro87Target); !errors.As(err, &coreErr) || coreErr.Reason != ReasonFeatureUnavailable {
		t.Fatalf("read in mock mode: %v", err)
	}
	// And a device of another kind is refused outright.
	if _, err := kbRecordCore(kbRecordSim(retro87Target)).RecordKeyboardReadProfile(ctx, padTarget); !errors.As(err, &coreErr) || coreErr.Reason != ReasonUnsupportedPid {
		t.Fatalf("read of a controller: %v", err)
	}
}
