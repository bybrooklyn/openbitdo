package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

var arcadeProTarget = protocol.VidPid{VID: 0x2dc8, PID: 0x2062}

// arcadeProSim is a simulated Arcade Controller Pro: its record size, and
// the platform where a two-slot record keeps it.
func arcadeProSim(platform byte) *protocol.U2Simulator {
	return &protocol.U2Simulator{Physical: platform, RecordSize: protocol.ArcadeProRecordSize, PlatformOffset: 0x0c}
}

// arcadeProCore is a core over a simulated Arcade Controller Pro, with the
// product raised to a tier that grants its profile for the length of the
// test. docs/spec/pid_matrix.csv has no row for it, which is what keeps the
// real one unreachable; the registry is put back afterwards.
func arcadeProCore(t *testing.T, pad *protocol.U2Simulator) *OpenBitdoCore {
	t.Helper()
	shipped := protocol.PIDRegistry
	t.Cleanup(func() { protocol.PIDRegistry = shipped })
	rows := append([]protocol.PidRow(nil), shipped...)
	found := false
	for i := range rows {
		if rows[i].Pid == arcadeProTarget.PID {
			rows[i].SupportTier, found = protocol.TierFull, true
		}
	}
	if !found {
		rows = append(rows, protocol.PidRow{
			Name: "PID_HitBox2", Pid: arcadeProTarget.PID, SupportLevel: protocol.SupportFull,
			SupportTier: protocol.TierFull, ProtocolFamily: protocol.DInput,
		})
	}
	protocol.PIDRegistry = rows
	return padCore(pad)
}

// framesOf counts the frames since from that carry cmd.
func framesOf(pad *protocol.U2Simulator, from int, cmd uint16) int {
	n := 0
	for _, frame := range pad.Frames[from:] {
		if frame[1] == 0x04 && binary.LittleEndian.Uint16(frame[2:]) == cmd {
			n++
		}
	}
	return n
}

func TestArcadeProIsRefusedAsShipped(t *testing.T) {
	// With no tier of its own the product is granted nothing, under
	// either of its ids, and nothing is sent to it.
	pad := arcadeProSim(protocol.U2PlatformXInput)
	c := padCore(pad)
	for _, pid := range []uint16{0x2062, 0x20aa} {
		if _, err := c.PadReadProfile(context.Background(), protocol.VidPid{VID: 0x2dc8, PID: pid}); err == nil {
			t.Fatalf("%#04x: a profile read must be refused while the product has no tier", pid)
		}
	}
	if len(pad.Frames) != 0 {
		t.Fatalf("%d frames were sent to a product with no tier", len(pad.Frames))
	}
	if protocol.DeviceProfileFor(arcadeProTarget).Capability.SupportsU2SlotConfig {
		t.Fatal("the shipped registry must not grant the Arcade Controller Pro a profile")
	}
}

func TestArcadeProProfileIsStoredWhereItsRecordKeepsIt(t *testing.T) {
	pad := arcadeProSim(protocol.U2PlatformXInput)
	stored := pad.Record(protocol.U2PlatformXInput)
	// Bytes this program does not model: the hot-key macro section, the
	// xinput rumble section and the last byte of a light section.
	stored[0x160], stored[0x620], stored[0x630+376+375] = 0xab, 0xcd, 0xee
	c := arcadeProCore(t, pad)
	ctx := context.Background()

	profile, err := c.PadReadProfile(ctx, arcadeProTarget)
	if err != nil {
		t.Fatal(err)
	}
	if profile.SlotCount != 2 || !profile.ArcadePro || !profile.Arcade || profile.HasLights || profile.HasMotion ||
		profile.Platform != protocol.U2PlatformXInput {
		t.Fatalf("profile: slots=%d pro=%v arcade=%v platform=%d", profile.SlotCount, profile.ArcadePro, profile.Arcade, profile.Platform)
	}
	defaults := profile.Slots[1]
	if defaults.InUse || defaults.Buttons[0] != PadB || defaults.ExtraButtons != [PadExtraButtons]PadTarget{PadNone, PadScreenshot} ||
		defaults.ButtonLights.Brightness[6][12] != 100 || defaults.ButtonLights.Colors[0][0] != 0xff0000 ||
		defaults.ButtonLights.Speeds != [4]byte{5, 5, 5, 5} || !defaults.Combos[0].Empty() {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}
	// Reading marks no session and writes nothing.
	if framesOf(pad, 0, 0x16) != 0 || framesOf(pad, 0, 1) != 0 {
		t.Fatal("a read must not start a configuration session or write")
	}

	slot := &profile.Slots[1]
	slot.Name = "Pro"
	slot.Buttons[18] = PadL3       // P1 -> L3
	slot.ExtraButtons[0] = PadA    // P5 -> A
	slot.ExtraButtons[1] = PadStar // Screenshot button -> Star
	slot.Options |= PadSOCDLastWins
	slot.Combos[0] = PadCombo{Input: 19, Buttons: [PadComboButtons]PadTarget{PadA, PadR1}} // on P2
	slot.Combos[2] = PadCombo{Input: 23, Buttons: [PadComboButtons]PadTarget{PadStart}}    // on the Screenshot button
	slot.ButtonLights.Effect = PadButtonLightFixed
	slot.ButtonLights.Brightness[3][0] = 40
	slot.ButtonLights.Colors[3][1] = 0x123456
	slot.ButtonLights.Speeds[2] = 9
	slot.ButtonLights.EffectBrightness[0] = 55
	// In the first slot only the effect changes: one byte, as the vendor
	// software writes it.
	profile.Slots[0].ButtonLights.Effect = PadButtonLightBreathing

	before := len(pad.Frames)
	report, err := c.PadApply(ctx, arcadeProTarget, profile)
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	if pad.Commits != 1 || pad.BadCRCs != 0 {
		t.Fatalf("commits=%d badCRCs=%d", pad.Commits, pad.BadCRCs)
	}
	// The session is marked started before the first write and over at
	// the end; every write names the record's own size.
	sync, firstWrite := -1, -1
	for i, frame := range pad.Frames[before:] {
		switch cmd := binary.LittleEndian.Uint16(frame[2:]); {
		case cmd == 0x16 && sync < 0:
			sync = i
			if binary.LittleEndian.Uint16(frame[4:]) != 1 {
				t.Fatalf("the session was not marked started: % x", frame[:8])
			}
		case cmd == 1:
			if firstWrite < 0 {
				firstWrite = i
			}
			if binary.LittleEndian.Uint32(frame[10:]) != 0xa68 {
				t.Fatalf("write names a record of %#x bytes", binary.LittleEndian.Uint32(frame[10:]))
			}
		}
	}
	if sync < 0 || firstWrite < sync || framesOf(pad, before, 0x16) != 2 || pad.Sync {
		t.Fatalf("session marking: started at %d, first write at %d, still on=%v", sync, firstWrite, pad.Sync)
	}
	if !pad.InputReports {
		t.Fatal("input reports must be resumed")
	}

	record := pad.Record(protocol.U2PlatformXInput)
	u32 := func(at int) uint32 { return binary.LittleEndian.Uint32(record[at:]) }
	if len(record) != 0xa68 || u32(0) != 0x20200911 || u32(4) != 0x20200911 {
		t.Fatalf("slot flags: %#x %#x", u32(0), u32(4))
	}
	if got := record[0x10+32 : 0x10+32+8]; !bytes.Equal(got, []byte("\x00P\x00r\x00o\x00\x00")) {
		t.Fatalf("name stored as % x", got)
	}
	buttons := 0x98 + 100
	if u32(buttons) != 0x20200911 || u32(buttons+4+18*4) != 0x2 || u32(buttons+4+22*4) != 0x2000 || u32(buttons+4+23*4) != 0x10000 {
		t.Fatalf("button map stored wrongly: % x", record[buttons:buttons+100])
	}
	if u32(0x88+8) != 0x20200911 || u32(0x88+8+4) != 0x40000 {
		t.Fatalf("opposite-directions choice stored as %#x", u32(0x88+8+4))
	}
	combos := 0x920 + 164
	if u32(combos) != 0x20200911 ||
		u32(combos+4) != 0x20200911 || u32(combos+8) != 0x04000000 || u32(combos+12) != 0x2000 || u32(combos+16) != 0x800 || u32(combos+20) != 0 ||
		u32(combos+36) != 0x20200911 || u32(combos+40) != 0x00400000 || u32(combos+44) != 0x1 ||
		u32(combos+68) != 0 || u32(combos+72) != 0 {
		t.Fatalf("combos stored wrongly: % x", record[combos:combos+164])
	}
	lights := 0x630 + 376
	if u32(lights) != 0x20200911 || record[lights+4] != 3 || record[lights+5+3*13] != 40 || record[lights+5+3*13+1] != 100 ||
		!bytes.Equal(record[lights+96+(3*13+1)*3:][:3], []byte{0x12, 0x34, 0x56}) || !bytes.Equal(record[lights+96:][:3], []byte{0xff, 0, 0}) ||
		!bytes.Equal(record[lights+369:lights+375], []byte{5, 5, 9, 5, 55, 100}) {
		t.Fatalf("lights stored wrongly: % x ... % x", record[lights:lights+8], record[lights+369:lights+376])
	}
	// The first slot: the effect byte alone, with its section still unset
	// and the slot in use for it.
	if record[0x634] != 2 || u32(0x630) != 0 || !bytes.Equal(record[0x635:0x630+376], make([]byte, 371)) {
		t.Fatalf("first slot's light section: % x", record[0x630:0x640])
	}
	if record[0x160] != 0xab || record[0x620] != 0xcd || record[lights+375] != 0xee {
		t.Fatal("a byte this program does not model changed")
	}
	// Nothing where a three-slot record keeps the same settings: its
	// second slot's option word and button map, and its macro sections.
	if u32(0xc8+8) == 0x20200911 || u32(0xe0+92) == 0x20200911 ||
		!bytes.Equal(record[0x1f4:0x1f4+3*216], make([]byte, 3*216)) || !bytes.Equal(record[0x68c:0x68c+216], make([]byte, 216)) {
		t.Fatal("something was written at another model's offsets")
	}
	// The first slot's own sections were not touched either.
	if !bytes.Equal(record[0x98:0x98+100], make([]byte, 100)) || !bytes.Equal(record[0x920:0x920+164], make([]byte, 164)) {
		t.Fatal("the first slot's button map or combos changed")
	}

	again, err := c.PadReadProfile(ctx, arcadeProTarget)
	if err != nil {
		t.Fatal(err)
	}
	// Combos come back from the front, in the order they were given.
	want := *slot
	want.Combos = [PadCombos]PadCombo{slot.Combos[0], slot.Combos[2]}
	want.InUse = true
	if again.Slots[1] != want {
		t.Fatalf("read back\n%+v\nwant\n%+v", again.Slots[1], want)
	}
	if again.Slots[0].ButtonLights.Effect != PadButtonLightBreathing || again.Slots[0].ButtonLights.Brightness[3][0] != 100 {
		t.Fatalf("first slot's lights read back as %+v", again.Slots[0].ButtonLights.Effect)
	}

	// Applying the profile as read writes nothing and marks no session.
	commits, frames := pad.Commits, len(pad.Frames)
	if report, err := c.PadApply(ctx, arcadeProTarget, again); err != nil || !report.WriteApplied || pad.Commits != commits ||
		framesOf(pad, frames, 0x16) != 0 {
		t.Fatalf("an unedited apply should be a no-op: %+v err=%v", report, err)
	}

	// The backup from the first apply puts the controller back, leaving
	// what this program does not model alone.
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatal(err)
	}
	record = pad.Record(protocol.U2PlatformXInput)
	if binary.LittleEndian.Uint32(record[4:]) != 0 || record[0x634] != 0 || record[lights+4] != 0 ||
		binary.LittleEndian.Uint32(record[combos:]) != 0 || record[0x160] != 0xab || pad.Sync {
		t.Fatalf("restore left flags % x, effect %d", record[:8], record[0x634])
	}
}

func TestArcadeProApplyRollsBackWhenTheControllerDoesNotKeepTheWrite(t *testing.T) {
	pad := arcadeProSim(protocol.U2PlatformSwitch)
	pad.ShortAccept = true
	c := arcadeProCore(t, pad)
	profile, err := c.PadReadProfile(context.Background(), arcadeProTarget)
	if err != nil {
		t.Fatal(err)
	}
	profile.Slots[0].ExtraButtons[0] = PadB
	report, err := c.PadApply(context.Background(), arcadeProTarget, profile)
	if err != nil {
		t.Fatal(err)
	}
	if report.WriteApplied || !report.RollbackAttempted || report.WriteError == "" {
		t.Fatalf("a partly accepted write must be reported and rolled back: %+v", report)
	}
	if pad.Sync || !pad.InputReports {
		t.Fatal("the session must be marked over and input reports resumed after a failed apply")
	}

	// A controller that keeps something other than what it was sent.
	pad = arcadeProSim(protocol.U2PlatformSwitch)
	c = arcadeProCore(t, pad)
	profile, _ = c.PadReadProfile(context.Background(), arcadeProTarget)
	profile.Slots[0].Combos[0] = PadCombo{Input: 0, Buttons: [PadComboButtons]PadTarget{PadX, PadY}}
	pad.DropCommit = true
	report, err = c.PadApply(context.Background(), arcadeProTarget, profile)
	if err != nil || report.WriteApplied || !report.RollbackAttempted {
		t.Fatalf("an unanswered commit must be reported: %+v err=%v", report, err)
	}
	if record := pad.Record(protocol.U2PlatformSwitch); binary.LittleEndian.Uint32(record[0x920:]) != 0 {
		t.Fatal("an uncommitted combo reached the stored record")
	}
}

func TestArcadeProRefusesWhatItCannotHoldBeforeSendingAnything(t *testing.T) {
	pad := arcadeProSim(protocol.U2PlatformXInput)
	c := arcadeProCore(t, pad)
	ctx := context.Background()
	combo := func(input int, buttons ...PadTarget) PadCombo {
		c := PadCombo{Input: input}
		copy(c.Buttons[:], buttons)
		return c
	}
	tap := PadMacro{Name: "m", Trigger: PadPaddle1, Repeat: 1, Steps: []PadMacroStep{
		{Millis: 40, Buttons: uint16(PadX), Left: PadStickCentre, Right: PadStickCentre},
		{Millis: 20, Left: PadStickCentre, Right: PadStickCentre},
	}}
	cases := map[string]func(*PadProfile){
		"Home as a target":              func(p *PadProfile) { p.Slots[0].Buttons[0] = PadHome },
		"turbo on an ordinary button":   func(p *PadProfile) { p.Slots[0].Buttons[4] = PadTurbo },
		"screenshot on a face button":   func(p *PadProfile) { p.Slots[0].Buttons[1] = PadScreenshot },
		"reassigning Home":              func(p *PadProfile) { p.Slots[0].Buttons[13] = PadA },
		"reassigning Star":              func(p *PadProfile) { p.Slots[0].Buttons[12] = PadA },
		"auto turbo on Screenshot":      func(p *PadProfile) { p.Slots[0].ExtraButtons[1] = PadAutoTurbo },
		"an unknown target on P5":       func(p *PadProfile) { p.Slots[0].ExtraButtons[0] = 0x12345 },
		"an effect that does not exist": func(p *PadProfile) { p.Slots[0].ButtonLights.Effect = 7 },
		"an effect past the last":       func(p *PadProfile) { p.Slots[1].ButtonLights.Effect = 9 },
		"brightness over 100":           func(p *PadProfile) { p.Slots[0].ButtonLights.Brightness[1][2] = 101 },
		"a colour wider than 24 bits":   func(p *PadProfile) { p.Slots[0].ButtonLights.Colors[1][2] = 0x1000000 },
		"speed 0":                       func(p *PadProfile) { p.Slots[0].ButtonLights.Speeds[1] = 0 },
		"speed 11":                      func(p *PadProfile) { p.Slots[0].ButtonLights.Speeds[3] = 11 },
		"effect brightness over 100":    func(p *PadProfile) { p.Slots[0].ButtonLights.EffectBrightness[1] = 200 },
		"a combo pressing a direction":  func(p *PadProfile) { p.Slots[0].Combos[0] = combo(0, PadA, PadUp) },
		"a combo pressing twice":        func(p *PadProfile) { p.Slots[0].Combos[0] = combo(0, PadA, PadA) },
		"a combo with a gap":            func(p *PadProfile) { p.Slots[0].Combos[0] = combo(0, PadA, PadNone, PadB) },
		"a combo on Home":               func(p *PadProfile) { p.Slots[0].Combos[0] = combo(13, PadA) },
		"a combo on no input":           func(p *PadProfile) { p.Slots[0].Combos[0] = combo(24, PadA) },
		"two combos on one button":      func(p *PadProfile) { p.Slots[0].Combos[0], p.Slots[0].Combos[1] = combo(3, PadA), combo(3, PadB) },
		"a third slot":                  func(p *PadProfile) { p.Slots[2].Name = "third" },
		"a macro in a third slot":       func(p *PadProfile) { p.Macros[2][0] = tap },
		"a macro on a combo's button": func(p *PadProfile) {
			p.Slots[0].Combos[0], p.Macros[0][0] = combo(18, PadA, PadB), tap
		},
		"two opposite-directions choices": func(p *PadProfile) { p.Slots[0].Options = PadSOCDUpWins | PadSOCDFirstWins },
		"a motion setting": func(p *PadProfile) {
			p.Slots[0].Motion = PadMotion{Target: PadMotionLeftStick, Button: PadL2, Sensitivity: 5, DeadZone: 40}
		},
		"stick-ring lights": func(p *PadProfile) { p.Slots[0].Lights.FireSpeed = 3 },
	}
	for name, edit := range cases {
		profile, err := c.PadReadProfile(ctx, arcadeProTarget)
		if err != nil {
			t.Fatal(err)
		}
		edit(&profile)
		before := len(pad.Frames)
		if _, err := c.PadApply(ctx, arcadeProTarget, profile); err == nil {
			t.Errorf("%s must be refused", name)
		}
		if n := framesOf(pad, before, 1) + framesOf(pad, before, 0x16) + framesOf(pad, before, 0x103) + framesOf(pad, before, 0x104) + framesOf(pad, before, 6); n != 0 {
			t.Errorf("%s: %d write, erase, commit or session frames were sent before the refusal", name, n)
		}
	}
	if !bytes.Equal(pad.Record(protocol.U2PlatformXInput)[:0x0c], make([]byte, 0x0c)) || pad.Commits != 0 {
		t.Fatal("a refused apply changed the controller")
	}

	// What the other models cannot hold stays refused on them, and an
	// Arcade Controller Pro's settings are refused there.
	gamepad := &protocol.U2Simulator{Physical: protocol.U2PlatformDInput}
	g := padCore(gamepad)
	for name, edit := range map[string]func(*PadProfile){
		"an extra input": func(p *PadProfile) { p.Slots[0].ExtraButtons[0] = PadA },
		"a combo":        func(p *PadProfile) { p.Slots[0].Combos[0] = combo(0, PadA, PadB) },
		"button lights":  func(p *PadProfile) { p.Slots[0].ButtonLights.Effect = PadButtonLightFixed },
		"P5 playing a macro": func(p *PadProfile) {
			p.Macros[0][0] = tap
			p.Macros[0][0].Trigger = padArcadeP5
		},
	} {
		plain, _ := g.PadReadProfile(ctx, padTarget)
		if plain.SlotCount != PadSlots || plain.ArcadePro {
			t.Fatalf("an Ultimate 2 read as %d slots, pro=%v", plain.SlotCount, plain.ArcadePro)
		}
		edit(&plain)
		if _, err := g.PadApply(ctx, padTarget, plain); err == nil {
			t.Errorf("an Ultimate 2 must refuse %s", name)
		}
	}
	if gamepad.Commits != 0 {
		t.Fatal("a refused apply changed the gamepad")
	}
}

func TestArcadeProValuesItAlreadyHoldsDoNotBlockOtherEdits(t *testing.T) {
	// A controller holding a light table this program would not write
	// (speed 0) and a target the vendor software does not offer (Home on
	// A) can still have something else changed, and keeps both.
	pad := arcadeProSim(protocol.U2PlatformSwitch)
	record := pad.Record(protocol.U2PlatformSwitch)
	binary.LittleEndian.PutUint32(record[0x630:], 0x20200911)
	binary.LittleEndian.PutUint32(record[0x98:], 0x20200911)
	binary.LittleEndian.PutUint32(record[0x98+4:], uint32(PadHome))
	c := arcadeProCore(t, pad)
	ctx := context.Background()
	profile, err := c.PadReadProfile(ctx, arcadeProTarget)
	if err != nil || profile.Slots[0].ButtonLights.Speeds[1] != 0 || profile.Slots[0].Buttons[0] != PadHome {
		t.Fatalf("read: %+v err=%v", profile.Slots[0].ButtonLights.Speeds, err)
	}
	profile.Slots[0].ButtonLights.Brightness[1][0] = 10
	profile.Slots[0].Buttons[1] = PadLSUp
	if report, err := c.PadApply(ctx, arcadeProTarget, profile); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	record = pad.Record(protocol.U2PlatformSwitch)
	if record[0x630+5+13] != 10 || record[0x630+369+1] != 0 || binary.LittleEndian.Uint32(record[0x98+4:]) != uint32(PadHome) ||
		binary.LittleEndian.Uint32(record[0x98+8:]) != 0x08000010 {
		t.Fatalf("stored: brightness %d speed %d", record[0x630+5+13], record[0x630+369+1])
	}
}

func TestArcadeProPlatformIsTheOneItSaysItIsIn(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		physical byte
		mode     byte
		want     byte
		refused  bool
	}{
		{"XInput (2)", protocol.U2PlatformXInput, 0, protocol.U2PlatformXInput, false},
		{"Switch (0)", protocol.U2PlatformSwitch, 0, protocol.U2PlatformSwitch, false},
		{"Switch (4)", protocol.U2PlatformSwitch, 4, protocol.U2PlatformSwitch, false},
		{"an answer it should not give", protocol.U2PlatformSwitch, 9, 0, true},
	} {
		pad := arcadeProSim(tc.physical)
		pad.ArcadeMode = tc.mode
		// The other platform's record must not be the one read.
		other := protocol.U2PlatformXInput + protocol.U2PlatformSwitch - tc.want
		binary.LittleEndian.PutUint32(pad.Record(other)[0:], 0x20200911)
		c := arcadeProCore(t, pad)
		profile, err := c.PadReadProfile(ctx, arcadeProTarget)
		if tc.refused {
			if err == nil {
				t.Errorf("%s: the read must be refused, not taken for Switch", tc.name)
			}
			if framesOf(pad, 0, 0x14) != 0 || framesOf(pad, 0, 2) != 0 {
				t.Errorf("%s: a record was selected or read without knowing the platform", tc.name)
			}
			if !pad.InputReports {
				t.Errorf("%s: input reports must be resumed", tc.name)
			}
			continue
		}
		if err != nil || profile.Platform != tc.want || profile.Slots[0].InUse {
			t.Errorf("%s: platform=%d inUse=%v err=%v", tc.name, profile.Platform, profile.Slots[0].InUse, err)
		}
		// The mode is asked with the arcade query, then that platform's
		// record is selected.
		asked, selected := false, false
		for _, frame := range pad.Frames {
			asked = asked || (frame[1] == 0x00 && frame[2] == 0x52)
			selected = selected || (frame[1] == 0x04 && binary.LittleEndian.Uint16(frame[2:]) == 0x14 && frame[4] == tc.want)
		}
		if !asked || !selected || framesOf(pad, 0, 0x105) != 0 {
			t.Errorf("%s: asked=%v selected=%v", tc.name, asked, selected)
		}
	}

	// On Switch the Star button's default is a screenshot and the
	// Screenshot button may be assigned one; an apply for the platform the
	// controller has left is refused.
	pad := arcadeProSim(protocol.U2PlatformSwitch)
	c := arcadeProCore(t, pad)
	profile, _ := c.PadReadProfile(ctx, arcadeProTarget)
	if profile.Slots[0].Buttons[12] != PadScreenshot || profile.Slots[0].Buttons[0] != PadA {
		t.Fatalf("Switch defaults: %v", profile.Slots[0].Buttons[:13])
	}
	pad.Physical = protocol.U2PlatformXInput
	profile.Slots[0].Buttons[0] = PadB
	if _, err := c.PadApply(ctx, arcadeProTarget, profile); err == nil {
		t.Fatal("an apply after the platform changed must be refused")
	}
}

func TestArcadeProSecondIDIsTheSameProduct(t *testing.T) {
	pad := arcadeProSim(protocol.U2PlatformXInput)
	c := arcadeProCore(t, pad)
	ctx := context.Background()
	alt := protocol.VidPid{VID: 0x2dc8, PID: 0x20aa}
	profile, err := c.PadReadProfile(ctx, alt)
	if err != nil || !profile.ArcadePro || profile.SlotCount != 2 {
		t.Fatalf("read under the second id: pro=%v err=%v", profile.ArcadePro, err)
	}
	profile.Slots[0].ExtraButtons[0] = PadX
	report, err := c.PadApply(ctx, alt, profile)
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	if got := binary.LittleEndian.Uint32(pad.Record(protocol.U2PlatformXInput)[0x98+4+22*4:]); got != 0x10 {
		t.Fatalf("P5 stored as %#x", got)
	}
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatalf("restore under the second id: %v", err)
	}
}

func TestArcadeProMacros(t *testing.T) {
	pad := arcadeProSim(protocol.U2PlatformXInput)
	c := arcadeProCore(t, pad)
	ctx := context.Background()
	profile, err := c.PadReadProfile(ctx, arcadeProTarget)
	if err != nil {
		t.Fatal(err)
	}
	macro := PadMacro{Name: "P5", Trigger: padArcadeP5, Repeat: 2, IntervalMillis: 300, Steps: []PadMacroStep{
		{Millis: 40, Buttons: uint16(PadX | PadA), Left: PadStickCentre, Right: PadStickCentre},
		{Millis: 20, Left: PadStickCentre, Right: PadStickCentre},
	}}
	profile.Macros[1][2] = macro
	report, err := c.PadApply(ctx, arcadeProTarget, profile)
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	record := pad.Record(protocol.U2PlatformXInput)
	section := record[0x470+216:][:216]
	header := section[8+2*52:]
	if binary.LittleEndian.Uint32(section) != 0x20200911 || section[4] != 1 || !bytes.Equal(header[:4], []byte("\x00P\x005")) ||
		header[32] != protocol.U2PlatformXInput || binary.LittleEndian.Uint16(header[34:]) != 2 ||
		binary.LittleEndian.Uint16(header[36:]) != 2*4096 || binary.LittleEndian.Uint32(header[40:]) != 0x00100000 ||
		binary.LittleEndian.Uint32(header[44:]) != 2 || binary.LittleEndian.Uint32(header[48:]) != 300 {
		t.Fatalf("macro header stored wrongly: % x", header[:52])
	}
	// The steps are in the storage of that platform and slot, at the
	// third macro's region.
	area := pad.MacroArea(uint16(protocol.U2PlatformXInput)<<8 | 1)
	if want := []byte{40, 0, 0x10, 0x20, 0, 0, 0x7f, 0x7f, 0x7f, 0x7f, 20, 0, 0, 0, 0, 0, 0x7f, 0x7f, 0x7f, 0x7f}; !bytes.Equal(area[2*4096:2*4096+20], want) {
		t.Fatalf("steps stored as % x", area[2*4096:2*4096+20])
	}
	// Nothing at the other models' macro sections.
	if !bytes.Equal(record[0x1f4:0x1f4+216], make([]byte, 216)) || !bytes.Equal(record[0x68c:0x68c+3*216], make([]byte, 3*216)) {
		t.Fatal("a macro header was written at another model's offset")
	}
	again, err := c.PadReadProfile(ctx, arcadeProTarget)
	if err != nil || again.Macros[1][2].Trigger != padArcadeP5 || len(again.Macros[1][2].Steps) != 2 || again.Macros[1][2].Name != "P5" {
		t.Fatalf("read back %+v err=%v", again.Macros[1][2], err)
	}
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatal(err)
	}
	if restored, _ := c.PadReadProfile(ctx, arcadeProTarget); !restored.Macros[1][2].Empty() {
		t.Fatal("restore left the macro in place")
	}
}
