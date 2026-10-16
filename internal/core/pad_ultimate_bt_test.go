package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

var (
	ultimateBTTarget  = protocol.VidPid{VID: 0x2dc8, PID: 0x6007}
	ultimateBTAdapter = protocol.VidPid{VID: 0x2dc8, PID: 0x3106}
)

const (
	ubtXInput = protocol.U2PlatformXInput
	ubtSwitch = protocol.U2PlatformSwitch
)

// ultimateBTSim is a simulated first-generation Ultimate Bluetooth: its
// record size and the counted wrapper its requests travel in. Both of its
// ids are full-tier in docs/spec/pid_matrix.csv, so no test raises a tier.
func ultimateBTSim() *protocol.U2Simulator {
	return &protocol.U2Simulator{Counted: true, RecordSize: protocol.UltimateBTRecordSize}
}

func ultimateBTAddr() PadAddress { return PadAddress{Enumerated: ultimateBTTarget} }

// countedFramesOf counts the frames since from that carry cmd in the counted
// wrapper.
func countedFramesOf(pad *protocol.U2Simulator, from int, cmd uint16) int {
	n := 0
	for _, frame := range pad.Frames[from:] {
		if frame[2] == 0x04 && binary.LittleEndian.Uint16(frame[3:]) == cmd {
			n++
		}
	}
	return n
}

// ultimateBTWrites is how many frames since from would change the
// controller: record writes, commits, macro writes and erases.
func ultimateBTWrites(pad *protocol.U2Simulator, from int) int {
	return countedFramesOf(pad, from, 1) + countedFramesOf(pad, from, 6) +
		countedFramesOf(pad, from, 0x103) + countedFramesOf(pad, from, 0x104)
}

func TestUltimateBTPlatformIsTheOneItsUserChooses(t *testing.T) {
	pad := ultimateBTSim()
	c := padCore(pad)
	ctx := context.Background()

	if got := PadPlatformChoices(ultimateBTAddr()); !bytes.Equal(got, []byte{ubtSwitch, ubtXInput}) {
		t.Fatalf("choices %v", got)
	}
	if got := PadPlatformChoices(PadAddress{Enumerated: ultimateBTAdapter}); len(got) != 2 {
		t.Fatalf("choices through the adapter %v", got)
	}
	if PadPlatformChoices(PadAddress{Enumerated: padTarget}) != nil {
		t.Fatal("an Ultimate 2 says itself which platform it is on")
	}

	// With no platform chosen, or one it keeps no record for, nothing is
	// sent: there is no asking this controller.
	if _, err := c.PadReadProfile(ctx, ultimateBTTarget); err == nil {
		t.Fatal("a read with no platform chosen must be refused")
	}
	if _, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), protocol.U2PlatformDInput); err == nil {
		t.Fatal("the DInput record must be refused: the controller has none")
	}
	if len(pad.Frames) != 0 {
		t.Fatalf("%d frames were sent before a platform was chosen", len(pad.Frames))
	}

	for _, platform := range []byte{ubtSwitch, ubtXInput} {
		// The other platform's record must not be the one read.
		other := ubtSwitch + ubtXInput - platform
		binary.LittleEndian.PutUint32(pad.Record(other)[0:], 0x20200911)
		binary.LittleEndian.PutUint32(pad.Record(platform)[0:], 0)
		from := len(pad.Frames)
		profile, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), platform)
		if err != nil || profile.Platform != platform || profile.Slots[0].InUse {
			t.Fatalf("platform %d: read platform=%d inUse=%v err=%v", platform, profile.Platform, profile.Slots[0].InUse, err)
		}
		// Input reports are paused with 0, the platform is selected, the
		// record is read, and reports are resumed with 1: not the other way
		// round, and nothing is asked about a mode switch or a receiver.
		var states []uint16
		selected := false
		for _, frame := range pad.Frames[from:] {
			if frame[0] != 0x81 || frame[2] != 0x04 {
				t.Fatalf("a frame left in another wrapper: % x", frame[:8])
			}
			switch cmd, arg := binary.LittleEndian.Uint16(frame[3:]), binary.LittleEndian.Uint16(frame[5:]); cmd {
			case 7:
				states = append(states, arg)
			case 0x14:
				selected = arg == uint16(platform)
			case 2:
				if !selected || len(states) != 1 {
					t.Fatal("the record was read before the platform was selected")
				}
			default:
				t.Fatalf("unexpected command %#x during a read", cmd)
			}
		}
		if len(states) != 2 || states[0] != 0 || states[1] != 1 || !pad.InputReports {
			t.Fatalf("report states sent: %v", states)
		}
	}

	// A controller that answers with another platform's record is not taken
	// at its word.
	binary.LittleEndian.PutUint16(pad.Record(ubtXInput)[0x10:], uint16(ubtSwitch))
	if _, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), ubtXInput); err == nil {
		t.Fatal("a record of the wrong platform must be refused")
	}

	// Choosing is for this model alone.
	other := &protocol.U2Simulator{Physical: protocol.U2PlatformDInput}
	if _, err := padCore(other).PadReadProfileOn(ctx, PadAddress{Enumerated: padTarget}, ubtXInput); err == nil || len(other.Frames) != 0 {
		t.Fatalf("an Ultimate 2 took a chosen platform: err=%v frames=%d", err, len(other.Frames))
	}
}

func TestUltimateBTProfileIsStoredWhereItsRecordKeepsIt(t *testing.T) {
	pad := ultimateBTSim()
	stored := pad.Record(ubtXInput)
	// What this program does not model: the header's crc, the hot-key macro
	// section (3 x 392 bytes at 0x1dc) and the xinput rumble section (3 x 8
	// at 0x8fc). The controller is on its third slot.
	binary.LittleEndian.PutUint32(stored[0x0c:], 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(stored[0x12:], 2)
	for i := 0x1dc; i < 0x674; i++ {
		stored[i] = byte(i*3 + 1)
	}
	copy(stored[0x8fc:], []byte{0x11, 0x09, 0x20, 0x20, 1, 100, 1, 100})
	seeded := append([]byte(nil), stored...)
	c := padCore(pad)
	ctx := context.Background()

	profile, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), ubtXInput)
	if err != nil {
		t.Fatal(err)
	}
	if profile.SlotCount != 3 || !profile.UltimateBT || profile.ButtonCount != 20 || profile.ActiveSlot != 2 ||
		profile.HasMotion || profile.HasLights || profile.Arcade || profile.ArcadePro || profile.Platform != ubtXInput {
		t.Fatalf("profile: slots=%d buttons=%d active=%d ubt=%v", profile.SlotCount, profile.ButtonCount, profile.ActiveSlot, profile.UltimateBT)
	}
	// On XInput every face button is itself, where the other controllers
	// trade A with B and X with Y.
	defaults := profile.Slots[1]
	if defaults.InUse || defaults.Buttons[0] != PadA || defaults.Buttons[1] != PadB || defaults.Buttons[2] != PadX ||
		defaults.Buttons[3] != PadY || defaults.Buttons[12] != PadStar || defaults.Buttons[18] != PadNone ||
		defaults.Buttons[20] != PadNone || defaults.Buttons[21] != PadNone || profile.DefaultTarget(0) != PadA {
		t.Fatalf("unexpected defaults: %v", defaults.Buttons)
	}
	if countedFramesOf(pad, 0, 1) != 0 {
		t.Fatal("a read must not write")
	}

	slot := &profile.Slots[1]
	slot.Name = "Old"
	slot.Buttons[0] = PadX          // A -> X
	slot.Buttons[3] = PadL1         // Y -> L1
	slot.Buttons[12] = PadAutoTurbo // Star -> auto turbo
	slot.Buttons[18] = PadL3        // P1 -> L3
	slot.LeftStick = PadRange{10, 120}
	slot.RightTrigger = PadRange{30, 200}
	slot.Options |= PadSwapSticks | PadInvertRightY

	before := len(pad.Frames)
	report, err := c.PadApplyAt(ctx, ultimateBTAddr(), profile)
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	if pad.Commits != 1 || pad.BadCRCs != 0 || !pad.InputReports {
		t.Fatalf("commits=%d badCRCs=%d reports=%v", pad.Commits, pad.BadCRCs, pad.InputReports)
	}
	// The record goes out whole, from its first byte to its last in order,
	// each write naming the record's own size.
	next := 0
	for _, frame := range pad.Frames[before:] {
		if binary.LittleEndian.Uint16(frame[3:]) != 1 {
			continue
		}
		length, total, offset := int(binary.LittleEndian.Uint16(frame[7:])), binary.LittleEndian.Uint32(frame[11:]), int(binary.LittleEndian.Uint32(frame[15:]))
		if total != 0x914 || offset != next || int(frame[1]) != 17+length {
			t.Fatalf("write of %d at %#x of %#x (count %d), expected offset %#x", length, offset, total, frame[1], next)
		}
		next += length
	}
	if next != 0x914 {
		t.Fatalf("%#x bytes of the record were written, want all 0x914", next)
	}

	record := pad.Record(ubtXInput)
	u32 := func(at int) uint32 { return binary.LittleEndian.Uint32(record[at:]) }
	if len(record) != 0x914 || u32(0) != 0 || u32(4) != 0x20200911 || u32(8) != 0 {
		t.Fatalf("slot flags: %#x %#x %#x", u32(0), u32(4), u32(8))
	}
	if got := record[0x14+32 : 0x14+32+8]; !bytes.Equal(got, []byte("\x00O\x00l\x00d\x00\x00")) {
		t.Fatalf("name stored as % x", got)
	}
	// The second slot's button map: 84 bytes at 0xe0 + 84, the face buttons
	// in the order B, A, Y, X.
	buttons := 0xe0 + 84
	want := []uint32{0x1000, 0x10, 0x400, 0x10, 0x400, 0x800, 0x4000, 0x8000, 0x2, 0x4, 0x8, 0x1,
		0x01000000, 0x20000, 0x200, 0x100, 0x80, 0x40, 0x2, 0}
	if u32(buttons) != 0x20200911 {
		t.Fatalf("button map flag %#x", u32(buttons))
	}
	for i, target := range want {
		if got := u32(buttons + 4 + i*4); got != target {
			t.Fatalf("button map entry %d stored as %#x, want %#x", i, got, target)
		}
	}
	if !bytes.Equal(record[0x98+8:0x98+16], []byte{0x11, 0x09, 0x20, 0x20, 10, 120, 0, 128}) ||
		!bytes.Equal(record[0xb0+8:0xb0+16], []byte{0x11, 0x09, 0x20, 0x20, 0, 255, 30, 200}) ||
		u32(0xc8+8) != 0x20200911 || u32(0xc8+12) != 0x18 {
		t.Fatalf("sticks % x triggers % x options %#x", record[0x98+8:0x98+16], record[0xb0+8:0xb0+16], u32(0xc8+12))
	}
	// Everything this program does not model, and everything of the other
	// two slots, is as it was: the crc, the active slot, the first and third
	// slots' button maps, the hot-key macros, the recorded-macro section and
	// the xinput rumble.
	for _, span := range [][2]int{{0x08, 0x14}, {0x14, 0x14 + 32}, {0x14 + 64, 0x98 + 8}, {0xe0, 0xe0 + 84}, {0xe0 + 168, 0x914}} {
		if !bytes.Equal(record[span[0]:span[1]], seeded[span[0]:span[1]]) {
			t.Fatalf("bytes %#x-%#x changed", span[0], span[1])
		}
	}
	// Nothing where a 22-input record keeps the same things: its second
	// slot's button map starts 8 bytes later, its third ends where this
	// record's hot-key macros are well under way, and its macro sections
	// are at 0x1f4 or 0x68c.
	if u32(0xe0+92) == 0x20200911 || !bytes.Equal(record[0x1f4:0x1f4+3*216], seeded[0x1f4:0x1f4+3*216]) ||
		!bytes.Equal(record[0x68c:0x914], seeded[0x68c:0x914]) {
		t.Fatal("something was written at another model's offsets")
	}

	again, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), ubtXInput)
	if err != nil {
		t.Fatal(err)
	}
	expect := *slot
	expect.InUse = true
	if again.Slots[1] != expect {
		t.Fatalf("read back\n%+v\nwant\n%+v", again.Slots[1], expect)
	}

	// Applying the profile as read writes nothing.
	commits, frames := pad.Commits, len(pad.Frames)
	if report, err := c.PadApplyAt(ctx, ultimateBTAddr(), again); err != nil || !report.WriteApplied || pad.Commits != commits ||
		ultimateBTWrites(pad, frames) != 0 {
		t.Fatalf("an unedited apply should be a no-op: %+v err=%v", report, err)
	}

	// The backup from the first apply puts the controller back, leaving the
	// header and what this program does not model alone.
	binary.LittleEndian.PutUint16(pad.Record(ubtXInput)[0x12:], 1) // the user moved to the second slot since
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatal(err)
	}
	record = pad.Record(ubtXInput)
	wantRestored := append([]byte(nil), seeded...)
	binary.LittleEndian.PutUint16(wantRestored[0x12:], 1)
	if !bytes.Equal(record, wantRestored) {
		t.Fatal("restore did not put the record back, or put back a stale active slot")
	}
}

func TestUltimateBTFaceButtonsAreStoredBAYX(t *testing.T) {
	pad := ultimateBTSim()
	record := pad.Record(ubtSwitch)
	// A first slot whose map gives each face button a different shoulder.
	binary.LittleEndian.PutUint32(record[0xe0:], 0x20200911)
	for i, target := range []PadTarget{PadL1, PadR1, PadL2, PadR2} {
		binary.LittleEndian.PutUint32(record[0xe4+i*4:], uint32(target))
	}
	c := padCore(pad)
	ctx := context.Background()
	profile, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), ubtSwitch)
	if err != nil {
		t.Fatal(err)
	}
	// Stored first is B, then A, then Y, then X.
	got := profile.Slots[0].Buttons
	if got[1] != PadL1 || got[0] != PadR1 || got[3] != PadL2 || got[2] != PadR2 {
		t.Fatalf("A=%s B=%s X=%s Y=%s", got[0], got[1], got[2], got[3])
	}
	// A slot with no map: on Switch the face buttons trade places (the
	// values an unset map holds are every model's, the places are not), and
	// Star takes screenshots.
	unset := profile.Slots[1].Buttons
	if unset[0] != PadB || unset[1] != PadA || unset[2] != PadY || unset[3] != PadX || unset[12] != PadScreenshot ||
		profile.DefaultTarget(1) != PadA || profile.DefaultTarget(4) != PadL1 {
		t.Fatalf("Switch defaults: %v", unset[:13])
	}

	// Vibration strength is stored on the Switch record, as on the others.
	profile.Slots[1].VibrationLeft, profile.Slots[1].VibrationRight = 3, 1
	profile.Slots[1].Buttons[1] = PadB // B, which was A by default, back to B
	if report, err := c.PadApplyAt(ctx, ultimateBTAddr(), profile); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	record = pad.Record(ubtSwitch)
	at := 0x74 + 12
	if binary.LittleEndian.Uint32(record[at:]) != 0x20200911 ||
		binary.LittleEndian.Uint32(record[at+4:]) != math.Float32bits(float32(3)*0.2) ||
		binary.LittleEndian.Uint32(record[at+8:]) != math.Float32bits(float32(1)*0.2) {
		t.Fatalf("vibration stored as % x", record[at:at+12])
	}
	// B is the first entry of the second slot's map; A, untouched, the next.
	if binary.LittleEndian.Uint32(record[0xe0+84+4:]) != uint32(PadB) || binary.LittleEndian.Uint32(record[0xe0+84+8:]) != uint32(PadB) {
		t.Fatalf("face buttons stored as % x", record[0xe0+84+4:0xe0+84+20])
	}
	// The XInput record was never touched.
	if !bytes.Equal(pad.Record(ubtXInput)[:0x10], make([]byte, 0x10)) {
		t.Fatal("the other platform's record changed")
	}
}

func TestUltimateBTRefusesWhatItCannotHoldBeforeSendingAnything(t *testing.T) {
	pad := ultimateBTSim()
	c := padCore(pad)
	ctx := context.Background()
	tap := PadMacro{Name: "m", Trigger: PadPaddle1, Repeat: 1, Steps: []PadMacroStep{
		{Millis: 40, Buttons: uint16(PadX), Left: PadStickCentre, Right: PadStickCentre},
		{Millis: 20, Left: PadStickCentre, Right: PadStickCentre},
	}}
	cases := map[string]func(*PadProfile){
		"an L4 it does not have": func(p *PadProfile) { p.Slots[0].Buttons[20] = PadA },
		"an R4 it does not have": func(p *PadProfile) { p.Slots[2].Buttons[21] = PadB },
		"an extra input":         func(p *PadProfile) { p.Slots[0].ExtraButtons[0] = PadA },
		"a combo":                func(p *PadProfile) { p.Slots[0].Combos[0] = PadCombo{Buttons: [PadComboButtons]PadTarget{PadA, PadB}} },
		"button lights":          func(p *PadProfile) { p.Slots[0].ButtonLights.Effect = PadButtonLightFixed },
		"a motion setting": func(p *PadProfile) {
			p.Slots[0].Motion = PadMotion{Target: PadMotionLeftStick, Button: PadL2, Sensitivity: 5, DeadZone: 40}
		},
		"stick-ring lights":                func(p *PadProfile) { p.Slots[0].Lights.FireSpeed = 3 },
		"a light effect of its own":        func(p *PadProfile) { p.Slots[0].Lights.TracingColor = 0x102030 },
		"the opposite-directions choice":   func(p *PadProfile) { p.Slots[0].Options |= PadSOCDLastWins },
		"reassigning Home":                 func(p *PadProfile) { p.Slots[0].Buttons[13] = PadA },
		"turbo on an ordinary button":      func(p *PadProfile) { p.Slots[0].Buttons[4] = PadTurbo },
		"auto turbo on a face button":      func(p *PadProfile) { p.Slots[0].Buttons[0] = PadAutoTurbo },
		"a stick direction on a paddle":    func(p *PadProfile) { p.Slots[0].Buttons[18] = PadRSUp },
		"a paddle's function on A":         func(p *PadProfile) { p.Slots[0].Buttons[0] = PadPaddle1 },
		"a screenshot on XInput":           func(p *PadProfile) { p.Slots[0].Buttons[12] = PadScreenshot },
		"Left Stick Left on Star":          func(p *PadProfile) { p.Slots[0].Buttons[12] = PadLSLeft },
		"swap on Star":                     func(p *PadProfile) { p.Slots[0].Buttons[12] = PadSwap },
		"an unknown target":                func(p *PadProfile) { p.Slots[0].Buttons[5] = 0x12345 },
		"vibration strength on XInput":     func(p *PadProfile) { p.Slots[0].VibrationLeft = 2 },
		"a name too long":                  func(p *PadProfile) { p.Slots[0].Name = "seventeen letters" },
		"a stick range past its end":       func(p *PadProfile) { p.Slots[0].LeftStick = PadRange{0, 129} },
		"a macro played from L4":           func(p *PadProfile) { p.Macros[0][0] = tap; p.Macros[0][0].Trigger = padMotionP3 },
		"a macro played from R4":           func(p *PadProfile) { p.Macros[1][3] = tap; p.Macros[1][3].Trigger = padMotionP4 },
		"a macro played from a fifth":      func(p *PadProfile) { p.Macros[0][0] = tap; p.Macros[0][0].Trigger = padArcadeP5 },
		"a macro that never lets go":       func(p *PadProfile) { p.Macros[0][0] = tap; p.Macros[0][0].Steps = tap.Steps[:1] },
		"a platform it keeps no record of": func(p *PadProfile) { p.Platform = protocol.U2PlatformDInput; p.Slots[0].Name = "x" },
	}
	for name, edit := range cases {
		profile, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), ubtXInput)
		if err != nil {
			t.Fatal(err)
		}
		edit(&profile)
		before := len(pad.Frames)
		if _, err := c.PadApplyAt(ctx, ultimateBTAddr(), profile); err == nil {
			t.Errorf("%s must be refused", name)
		}
		if n := ultimateBTWrites(pad, before); n != 0 {
			t.Errorf("%s: %d write, erase or commit frames were sent before the refusal", name, n)
		}
		if name == "a platform it keeps no record of" && len(pad.Frames) != before {
			t.Errorf("%s: %d frames were sent", name, len(pad.Frames)-before)
		}
		if !pad.InputReports {
			t.Errorf("%s: input reports were left paused", name)
		}
	}
	if !bytes.Equal(pad.Record(ubtXInput)[:0x10], make([]byte, 0x10)) || pad.Commits != 0 {
		t.Fatal("a refused apply changed the controller")
	}

	// Each of its buttons takes what the vendor's software offers for it.
	if PadUltimateBTTargets(13) != nil || PadUltimateBTTargets(20) != nil || PadUltimateBTTargets(-1) != nil {
		t.Fatal("Home and the inputs it does not have take nothing")
	}
	if got := PadUltimateBTTargets(0); len(got) != 18 || !padTargetIn(got, PadHome) || padTargetIn(got, PadAutoTurbo) {
		t.Fatalf("a face button's targets: %v", got)
	}
	if got := PadUltimateBTTargets(12); len(got) != 27 || !padTargetIn(got, PadAutoTurbo) || !padTargetIn(got, PadStar) ||
		!padTargetIn(got, PadLSRight) || padTargetIn(got, PadLSLeft) || padTargetIn(got, PadScreenshot) {
		t.Fatalf("Star's targets: %v", got)
	}
	// Every one of them is accepted.
	for i := 0; i < 20; i++ {
		for _, target := range PadUltimateBTTargets(i) {
			profile, _ := c.PadReadProfileOn(ctx, ultimateBTAddr(), ubtSwitch)
			profile.Slots[2].Buttons[i] = target
			if report, err := c.PadApplyAt(ctx, ultimateBTAddr(), profile); err != nil || !report.WriteApplied {
				t.Fatalf("%s -> %s: %+v err=%v", PadInputs[i].Name, target, report, err)
			}
		}
	}
}

func TestUltimateBTValuesItAlreadyHoldsDoNotBlockOtherEdits(t *testing.T) {
	// A controller holding a target the vendor's software does not offer
	// (turbo on L1) can still have something else changed, and keeps it.
	pad := ultimateBTSim()
	record := pad.Record(ubtXInput)
	binary.LittleEndian.PutUint32(record[0xe0:], 0x20200911)
	binary.LittleEndian.PutUint32(record[0xe4+4*4:], uint32(PadTurbo))
	c := padCore(pad)
	ctx := context.Background()
	profile, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), ubtXInput)
	if err != nil || profile.Slots[0].Buttons[4] != PadTurbo {
		t.Fatalf("read: L1=%s err=%v", profile.Slots[0].Buttons[4], err)
	}
	profile.Slots[0].Buttons[19] = PadHome
	if report, err := c.PadApplyAt(ctx, ultimateBTAddr(), profile); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	record = pad.Record(ubtXInput)
	if binary.LittleEndian.Uint32(record[0xe4+4*4:]) != uint32(PadTurbo) || binary.LittleEndian.Uint32(record[0xe4+19*4:]) != uint32(PadHome) {
		t.Fatalf("stored L1 %#x P2 %#x", binary.LittleEndian.Uint32(record[0xe4+4*4:]), binary.LittleEndian.Uint32(record[0xe4+19*4:]))
	}
}

func TestUltimateBTApplyRollsBackWhenTheControllerDoesNotKeepTheWrite(t *testing.T) {
	ctx := context.Background()
	pad := ultimateBTSim()
	pad.ShortAccept = true
	c := padCore(pad)
	profile, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), ubtSwitch)
	if err != nil {
		t.Fatal(err)
	}
	profile.Slots[0].Buttons[18] = PadB
	report, err := c.PadApplyAt(ctx, ultimateBTAddr(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if report.WriteApplied || !report.RollbackAttempted || report.WriteError == "" || !report.HasBackupID {
		t.Fatalf("a partly accepted write must be reported and rolled back: %+v", report)
	}
	if !pad.InputReports {
		t.Fatal("input reports must be resumed after a failed apply")
	}

	// A commit that is never answered: nothing reaches the stored record.
	pad = ultimateBTSim()
	c = padCore(pad)
	profile, _ = c.PadReadProfileOn(ctx, ultimateBTAddr(), ubtSwitch)
	profile.Slots[0].Name = "Kept"
	pad.DropCommit = true
	report, err = c.PadApplyAt(ctx, ultimateBTAddr(), profile)
	if err != nil || report.WriteApplied || !report.RollbackAttempted {
		t.Fatalf("an unanswered commit must be reported: %+v err=%v", report, err)
	}
	if record := pad.Record(ubtSwitch); binary.LittleEndian.Uint32(record[0:]) != 0 || record[0x15] != 0 {
		t.Fatal("an uncommitted name reached the stored record")
	}

	// A controller that commits something other than what it was sent is
	// put back to what it held.
	pad = ultimateBTSim()
	held := pad.Record(ubtSwitch)
	binary.LittleEndian.PutUint32(held[0xc8:], 0x20200911)
	binary.LittleEndian.PutUint32(held[0xcc:], PadInvertLeftX)
	c = padCore(pad)
	profile, _ = c.PadReadProfileOn(ctx, ultimateBTAddr(), ubtSwitch)
	profile.Slots[0].Options = PadSwapTriggers
	fail := &failingReadback{U2Simulator: pad, at: 0xcc}
	c.transportOverride = fail
	report, err = c.PadApplyAt(ctx, ultimateBTAddr(), profile)
	if err != nil || report.WriteApplied || !report.RollbackAttempted || !report.RollbackSucceeded {
		t.Fatalf("a write the controller did not keep must be rolled back: %+v err=%v", report, err)
	}
	if got := binary.LittleEndian.Uint32(pad.Record(ubtSwitch)[0xcc:]); got != PadInvertLeftX {
		t.Fatalf("after rollback the option word is %#x", got)
	}
}

// failingReadback is a controller that stores a different byte at one place
// than the first whole-record write gave it there.
type failingReadback struct {
	*protocol.U2Simulator
	at     int
	spoilt bool
}

func (f *failingReadback) Write(data []byte) (int, error) {
	// A record write whose chunk covers the byte: flip it once, with the
	// crc made good so the controller takes it.
	if !f.spoilt && data[2] == 0x04 && binary.LittleEndian.Uint16(data[3:]) == 1 {
		length, offset := int(binary.LittleEndian.Uint16(data[7:])), int(binary.LittleEndian.Uint32(data[15:]))
		if f.at >= offset && f.at < offset+length {
			spoilt := append([]byte(nil), data...)
			spoilt[19+f.at-offset] ^= 0x40
			binary.LittleEndian.PutUint16(spoilt[9:], modbus(spoilt[19:19+length]))
			f.spoilt = true
			return f.U2Simulator.Write(spoilt)
		}
	}
	return f.U2Simulator.Write(data)
}

func modbus(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b)
		for bit := 0; bit < 8; bit++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func TestUltimateBTMacros(t *testing.T) {
	ctx := context.Background()
	macro := PadMacro{Name: "LT", Trigger: PadPaddle1, Repeat: 2, IntervalMillis: 300, Steps: []PadMacroStep{
		{Millis: 40, Buttons: uint16(PadA | PadL2), Left: PadStickUp, Right: PadStickCentre},
		{Millis: 25, Buttons: uint16(PadL2 | PadR2), Left: PadStickCentre, Right: PadStickCentre},
		{Millis: 20, Left: PadStickCentre, Right: PadStickCentre},
	}}
	for _, tc := range []struct {
		platform byte
		steps    []byte
	}{
		// On the XInput record a trigger is an analog value, L2 in the high
		// byte and R2 in the low, and not a button bit.
		{ubtXInput, []byte{
			40, 0, 0x00, 0x20, 0x00, 0xff, 0x7f, 0x00, 0x7f, 0x7f,
			25, 0, 0x00, 0x00, 0xff, 0xff, 0x7f, 0x7f, 0x7f, 0x7f,
			20, 0, 0x00, 0x00, 0x00, 0x00, 0x7f, 0x7f, 0x7f, 0x7f,
		}},
		// On the Switch record it is a button bit, as on the others.
		{ubtSwitch, []byte{
			40, 0, 0x00, 0x60, 0x00, 0x00, 0x7f, 0x00, 0x7f, 0x7f,
			25, 0, 0x00, 0xc0, 0x00, 0x00, 0x7f, 0x7f, 0x7f, 0x7f,
			20, 0, 0x00, 0x00, 0x00, 0x00, 0x7f, 0x7f, 0x7f, 0x7f,
		}},
	} {
		pad := ultimateBTSim()
		seeded := append([]byte(nil), pad.Record(tc.platform)...)
		c := padCore(pad)
		profile, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), tc.platform)
		if err != nil {
			t.Fatal(err)
		}
		profile.Macros[1][2] = macro
		before := len(pad.Frames)
		report, err := c.PadApplyAt(ctx, ultimateBTAddr(), profile)
		if err != nil || !report.WriteApplied {
			t.Fatalf("platform %d apply: %+v err=%v", tc.platform, report, err)
		}
		record := pad.Record(tc.platform)
		// The second slot's macro section is at 0x674 + 216. Its count is
		// always four; P1 is P1, not swapped as on an Ultimate 2 Bluetooth.
		section := record[0x674+216:][:216]
		header := section[8+2*52:]
		if binary.LittleEndian.Uint32(section) != 0x20200911 || section[4] != 4 || !bytes.Equal(header[:4], []byte("\x00L\x00T")) ||
			header[32] != tc.platform || binary.LittleEndian.Uint16(header[34:]) != 3 ||
			binary.LittleEndian.Uint16(header[36:]) != 2*4096 || binary.LittleEndian.Uint32(header[40:]) != 0x02000000 ||
			binary.LittleEndian.Uint32(header[44:]) != 2 || binary.LittleEndian.Uint32(header[48:]) != 300 {
			t.Fatalf("platform %d: macro header stored wrongly: % x", tc.platform, header[:52])
		}
		if !bytes.Equal(section[8:8+2*52], make([]byte, 2*52)) || !bytes.Equal(section[8+3*52:], make([]byte, 52)) {
			t.Fatalf("platform %d: the unused macro entries are not empty", tc.platform)
		}
		// The steps are in the storage of that platform and slot, at the
		// third macro's region.
		area := pad.MacroArea(uint16(tc.platform)<<8 | 1)
		if !bytes.Equal(area[2*4096:2*4096+30], tc.steps) || area[2*4096+30] != 0xff {
			t.Fatalf("platform %d: steps stored as % x", tc.platform, area[2*4096:2*4096+32])
		}
		// They were sent as one whole report of 32 bytes, filled out with
		// what erased storage holds, after the region was erased.
		erased, wrote := false, 0
		for _, frame := range pad.Frames[before:] {
			switch binary.LittleEndian.Uint16(frame[3:]) {
			case 0x104:
				erased = binary.LittleEndian.Uint16(frame[7:]) == 4096 && binary.LittleEndian.Uint32(frame[15:]) == 2*4096
			case 0x103:
				wrote++
				if !erased || frame[1] != 17+32 || binary.LittleEndian.Uint16(frame[7:]) != 32 ||
					binary.LittleEndian.Uint32(frame[11:]) != 2*4096+32 || binary.LittleEndian.Uint32(frame[15:]) != 2*4096 ||
					!bytes.Equal(frame[19:49], tc.steps) || frame[49] != 0xff || frame[50] != 0xff {
					t.Fatalf("platform %d: macro write % x", tc.platform, frame)
				}
			}
		}
		if wrote != 1 || pad.BadCRCs != 0 {
			t.Fatalf("platform %d: %d macro writes, %d bad crcs", tc.platform, wrote, pad.BadCRCs)
		}
		// Nothing but that section and the slot's flag changed; in
		// particular nothing at 0x1f4 or 0x68c, where other models keep
		// their macro sections.
		for _, span := range [][2]int{{0x08, 0x674 + 216}, {0x674 + 432, 0x914}} {
			if !bytes.Equal(record[span[0]:span[1]], seeded[span[0]:span[1]]) {
				t.Fatalf("platform %d: bytes %#x-%#x changed", tc.platform, span[0], span[1])
			}
		}
		again, err := c.PadReadProfileOn(ctx, ultimateBTAddr(), tc.platform)
		if err != nil || again.Macros[1][2].Name != "LT" || again.Macros[1][2].Trigger != PadPaddle1 ||
			len(again.Macros[1][2].Steps) != 3 || again.Macros[1][2].Steps[0] != macro.Steps[0] || again.Macros[1][2].Steps[1] != macro.Steps[1] {
			t.Fatalf("platform %d: read back %+v err=%v", tc.platform, again.Macros[1][2], err)
		}
		if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
			t.Fatal(err)
		}
		if restored, _ := c.PadReadProfileOn(ctx, ultimateBTAddr(), tc.platform); !restored.Macros[1][2].Empty() {
			t.Fatalf("platform %d: restore left the macro in place", tc.platform)
		}
	}

	// A macro on the controller played from a button this model does not
	// have is not shown.
	pad := ultimateBTSim()
	record := pad.Record(ubtSwitch)
	binary.LittleEndian.PutUint32(record[0x674:], 0x20200911)
	record[0x674+4] = 4
	binary.LittleEndian.PutUint16(record[0x674+8+34:], 1)
	binary.LittleEndian.PutUint32(record[0x674+8+40:], 0x00200000)
	profile, err := padCore(pad).PadReadProfileOn(ctx, ultimateBTAddr(), ubtSwitch)
	if err != nil || !profile.Macros[0][0].Empty() || countedFramesOf(pad, 0, 0x102) != 0 {
		t.Fatalf("a macro on L4 was read: %+v err=%v", profile.Macros[0][0], err)
	}
}

func TestUltimateBTThroughItsAdapter(t *testing.T) {
	// Through the adapter the vendor's library leaves the crc out; the
	// record, its layout and everything else are the same.
	pad := ultimateBTSim()
	pad.NoCRC = true
	c := padCore(pad)
	ctx := context.Background()
	addr := PadAddress{Enumerated: ultimateBTAdapter}
	profile, err := c.PadReadProfileOn(ctx, addr, ubtXInput)
	if err != nil || !profile.UltimateBT || profile.ButtonCount != 20 {
		t.Fatalf("read through the adapter: ubt=%v err=%v", profile.UltimateBT, err)
	}
	profile.Slots[0].Buttons[1] = PadR3 // B, stored first
	report, err := c.PadApplyAt(ctx, addr, profile)
	if err != nil || !report.WriteApplied || pad.BadCRCs != 0 {
		t.Fatalf("apply: %+v err=%v badCRCs=%d", report, err, pad.BadCRCs)
	}
	if got := binary.LittleEndian.Uint32(pad.Record(ubtXInput)[0xe4:]); got != uint32(PadR3) {
		t.Fatalf("B stored as %#x", got)
	}
	for _, frame := range pad.Frames {
		if frame[2] != 0x04 || binary.LittleEndian.Uint16(frame[9:]) != 0 {
			t.Fatalf("a frame through the adapter carries a crc or another wrapper: % x", frame[:20])
		}
	}
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatalf("restore through the adapter: %v", err)
	}
	if binary.LittleEndian.Uint32(pad.Record(ubtXInput)[0xe0:]) != 0 {
		t.Fatal("restore left the button map in place")
	}
}

func TestUltimateBTLeavesTheOtherModelsAsTheyWere(t *testing.T) {
	// An Ultimate 2 has all 22 inputs, trades its face buttons on XInput
	// and not on DInput, keeps them in the order A, B, X, Y, and is sent
	// only the spans that changed.
	pad := &protocol.U2Simulator{Physical: protocol.U2PlatformXInput}
	c := padCore(pad)
	ctx := context.Background()
	profile, err := c.PadReadProfile(ctx, padTarget)
	if err != nil || profile.UltimateBT || profile.ButtonCount != PadButtons || profile.Slots[0].Buttons[0] != PadB ||
		profile.DefaultTarget(0) != PadB {
		t.Fatalf("an Ultimate 2 read as ubt=%v buttons=%d A=%s err=%v", profile.UltimateBT, profile.ButtonCount, profile.Slots[0].Buttons[0], err)
	}
	profile.Slots[0].Buttons[0] = PadL3
	profile.Slots[0].Buttons[21] = PadR3
	before := len(pad.Frames)
	if report, err := c.PadApply(ctx, padTarget, profile); err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}
	record := pad.Record(protocol.U2PlatformXInput)
	if binary.LittleEndian.Uint32(record[0xe4:]) != uint32(PadL3) || binary.LittleEndian.Uint32(record[0xe4+21*4:]) != uint32(PadR3) {
		t.Fatalf("an Ultimate 2's map stored as % x", record[0xe4:0xe4+16])
	}
	written := 0
	for _, frame := range pad.Frames[before:] {
		if frame[1] == 0x04 && binary.LittleEndian.Uint16(frame[2:]) == 1 {
			written += int(binary.LittleEndian.Uint16(frame[6:]))
		}
	}
	if written != 92+4 {
		t.Fatalf("an Ultimate 2 was sent %d bytes for one button map and its slot flag", written)
	}
}
