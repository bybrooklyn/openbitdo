package protocol

import (
	"bytes"
	"context"
	"encoding/hex"
	"reflect"
	"testing"
)

const (
	retroMousePID   uint16 = 0x5205
	rivieraMousePID uint16 = 0x205d
)

// raiseMouseTier makes the mice candidate-readonly for one test. In
// docs/spec/pid_matrix.csv the Retro R8 and its receiver are detect-only
// and the Riviera mouse has no row, so as shipped nothing is exchanged with
// any of them; these tests show what happens the day that changes.
func raiseMouseTier(t *testing.T) {
	t.Helper()
	shipped := PIDRegistry
	t.Cleanup(func() { PIDRegistry = shipped })
	rows := append([]PidRow(nil), shipped...)
	riviera := false
	for i := range rows {
		if mousePIDs[rows[i].Pid] {
			rows[i].SupportTier = TierCandidateReadOnly
			riviera = riviera || rows[i].Pid == rivieraMousePID
		}
	}
	if !riviera {
		rows = append(rows, PidRow{Name: "PID_RivieraMouse", Pid: rivieraMousePID, SupportTier: TierCandidateReadOnly, ProtocolFamily: Standard64})
	}
	PIDRegistry = rows
}

// mouseSession opens a session on a mouse as someone who has been through
// the write-unlock ceremony.
func mouseSession(t *testing.T, mouse Transport, pid uint16) *DeviceSession {
	t.Helper()
	raiseMouseTier(t)
	config := fastRetryConfig()
	config.Experimental, config.CandidateWriteUnlock = true, true
	return openSession(t, mouse, pid, config)
}

// wantMouseFrames checks the frames written since from: each is 65 bytes
// and starts with the given hex, the rest being zero.
func wantMouseFrames(t *testing.T, frames [][]byte, from int, want ...string) {
	t.Helper()
	got := frames[from:]
	if len(got) != len(want) {
		t.Fatalf("%d frames were sent, expected %d", len(got), len(want))
	}
	for i, text := range want {
		head, err := hex.DecodeString(text)
		if err != nil {
			t.Fatal(err)
		}
		full := append(head, make([]byte, 65-len(head))...)
		if !bytes.Equal(got[i], full) {
			t.Fatalf("frame %d = % x\nwant      % x", i, got[i][:max(len(head)+2, 12)], head)
		}
	}
}

func TestMouseSettingsTravelInTheVendorsFrames(t *testing.T) {
	mouse := NewMouseSimulator(false)
	session := mouseSession(t, mouse, retroMousePID)
	ctx := context.Background()

	// The name: ten bytes a report, the block number above the length.
	name := make([]byte, MouseNameLen)
	copy(name, "\x00D\x00e\x00s\x00k\x00 \x00t\x00w\x00o")
	if err := session.MouseWriteProfileName(ctx, name); err != nil {
		t.Fatal(err)
	}
	wantMouseFrames(t, mouse.Frames, 0,
		"0001061c04"+"00000a00"+"004400650073006b0020",
		"0001061c04"+"00002a00"+"00740077006f")
	if got, err := session.MouseReadProfileName(ctx); err != nil || !bytes.Equal(got, name) {
		t.Fatalf("name read back as % x err=%v", got, err)
	}
	wantMouseFrames(t, mouse.Frames, 2, "0001061c0100000a00", "0001061c0100002a00")
	if err := session.MouseWriteProfileName(ctx, name[:10]); err == nil {
		t.Fatal("a name of the wrong size must be refused")
	}

	// A button: the macro switch, its number, the function.
	from := len(mouse.Frames)
	if err := session.MouseWriteButton(ctx, 4, MouseButtonEntry{Function: 128}); err != nil {
		t.Fatal(err)
	}
	if err := session.MouseWriteButton(ctx, 6, MouseButtonEntry{Macro: 1}); err != nil {
		t.Fatal(err)
	}
	pair, err := session.MouseReadButtons(ctx, 3)
	if err != nil || pair != [2]MouseButtonEntry{{Function: 5}, {Function: 128}} {
		t.Fatalf("buttons 3 and 4 = %+v err=%v", pair, err)
	}
	wantMouseFrames(t, mouse.Frames, from, "000106100400048000000000", "00010610040106", "0001061401090302")
	if _, err := session.MouseReadButtons(ctx, 2); err == nil {
		t.Fatal("buttons are read in pairs from an odd number")
	}
	if err := session.MouseWriteButton(ctx, 7, MouseButtonEntry{}); err == nil {
		t.Fatal("there is no button 7")
	}

	// The one-byte settings, each with its own command byte.
	from = len(mouse.Frames)
	for setting, value := range map[MouseSetting]byte{MouseLeftHanded: 1, MouseLiftOff: 2, MouseWheelSpeed: 14, MouseWheelDirection: 1} {
		if err := session.MouseWriteSetting(ctx, setting, value); err != nil {
			t.Fatal(err)
		}
		if got, err := session.MouseReadSetting(ctx, setting); err != nil || got != value {
			t.Fatalf("setting %d read back as %d err=%v", setting, got, err)
		}
	}
	if mouse.LeftHanded != 1 || mouse.LiftOff != 2 || mouse.WheelSpeed != 14 || mouse.WheelDirection != 1 {
		t.Fatalf("stored settings: %d %d %d %d", mouse.LeftHanded, mouse.LiftOff, mouse.WheelSpeed, mouse.WheelDirection)
	}
	sent := map[string]bool{}
	for _, frame := range mouse.Frames[from:] {
		sent[hex.EncodeToString(frame[:6])] = true
	}
	for _, want := range []string{
		"000106270401", "000106270100", "000106260402", "000106260100",
		"00010624040e", "000106240100", "000106230401", "000106230100",
	} {
		if !sent[want] {
			t.Fatalf("no frame %s among %v", want, sent)
		}
	}
	if err := session.MouseWriteSetting(ctx, MouseSetting(9), 1); err == nil {
		t.Fatal("an unknown setting must be refused")
	}
}

func TestMouseDpiAndPollingRateFrames(t *testing.T) {
	mouse := NewMouseSimulator(false)
	session := mouseSession(t, mouse, retroMousePID)
	ctx := context.Background()

	// An axis is written whole: the axis, the mask, six values.
	y := MouseDpi{Mask: 0x05, Values: [6]uint16{800, 1200, 26000, 2400, 3200, 50}}
	if err := session.MouseWriteDpi(ctx, MouseAxisY, y); err != nil {
		t.Fatal(err)
	}
	wantMouseFrames(t, mouse.Frames, 0, "00010628040105"+"2003"+"b004"+"9065"+"6009"+"800c"+"3200")
	if got, err := session.MouseReadDpi(ctx, MouseAxisY); err != nil || got != y {
		t.Fatalf("Y stages read back as %+v err=%v", got, err)
	}
	if got, err := session.MouseReadDpi(ctx, MouseAxisX); err != nil || got.Mask != 0x3f || got.Values[5] != 6400 {
		t.Fatalf("X stages = %+v err=%v", got, err)
	}
	wantMouseFrames(t, mouse.Frames, 1, "000106280101", "000106280100")
	if _, err := session.MouseReadDpi(ctx, 2); err == nil {
		t.Fatal("there is no third axis")
	}

	// The stage in use goes with a 6 and the stage's X value.
	from := len(mouse.Frames)
	if err := session.MouseWriteDpiStage(ctx, 2, 1600); err != nil {
		t.Fatal(err)
	}
	if stage, err := session.MouseReadDpiStage(ctx); err != nil || stage != 2 || mouse.StageDpi != 1600 {
		t.Fatalf("stage in use = %d (%d DPI) err=%v", stage, mouse.StageDpi, err)
	}
	wantMouseFrames(t, mouse.Frames, from, "000106120406024006", "0001061201")
	if err := session.MouseWriteDpiStage(ctx, 6, 800); err == nil {
		t.Fatal("there is no seventh stage")
	}

	// The polling rate goes with its whole table.
	from = len(mouse.Frames)
	rate, err := session.MouseReadPollingRate(ctx)
	if err != nil || rate != (MousePollingRate{Count: 6, Current: 1, Rates: [6]uint16{250, 500, 1000, 2000, 4000, 8000}}) {
		t.Fatalf("rate = %+v err=%v", rate, err)
	}
	rate.Current = 5
	if err := session.MouseWritePollingRate(ctx, rate); err != nil {
		t.Fatal(err)
	}
	// It is written with four zero bytes before the table, where the reply
	// has two.
	wantMouseFrames(t, mouse.Frames, from, "0001060d01", "0001060d04"+"0605"+"00000000"+"fa00"+"f401"+"e803"+"d007"+"a00f"+"401f")
	if got, err := session.MouseReadPollingRate(ctx); err != nil || got != rate {
		t.Fatalf("rate read back as %+v err=%v", got, err)
	}
	// A mouse with five rates is not sent a sixth.
	from = len(mouse.Frames)
	rate.Count, rate.Current = 5, 0
	if err := session.MouseWritePollingRate(ctx, rate); err != nil {
		t.Fatal(err)
	}
	wantMouseFrames(t, mouse.Frames, from, "0001060d04"+"0500"+"00000000"+"fa00"+"f401"+"e803"+"d007"+"a00f")
	if mouse.Rate.Rates[5] != 8000 {
		t.Fatal("the simulated mouse should have kept its sixth rate")
	}

	from = len(mouse.Frames)
	if err := session.MouseClearProfile(ctx); err != nil || mouse.Cleared != 1 {
		t.Fatalf("clear: err=%v cleared=%d", err, mouse.Cleared)
	}
	wantMouseFrames(t, mouse.Frames, from, "0001013304")
}

func TestMouseMacroIsWrittenInPartsWithItsSum(t *testing.T) {
	mouse := NewMouseSimulator(false)
	session := mouseSession(t, mouse, retroMousePID)
	ctx := context.Background()

	macro := MouseMacro{
		Cycles: 2, IntervalMs: 500, Name: []byte("\x00G\x00o"),
		Records: []MouseMacroRecord{{State: 10, Code: 4, Timer: 10}, {State: 2, Code: 4, Timer: 300}, {State: 15, Code: 1, Timer: 0}},
	}
	if err := session.MouseWriteMacro(ctx, 4, macro); err != nil {
		t.Fatal(err)
	}
	// Header: record count, button, cycles, the macro's number, 0x20, the
	// interval. Name: three reports of ten bytes. Records: two a report,
	// the last one marked 3 and carrying the sum of all fifteen bytes
	// (10+4+10 + 2+4+0x2c+1 + 15+1 = 91).
	wantMouseFrames(t, mouse.Frames, 0,
		"0001060f04"+"0103040200020000"+"20f401",
		"0001060f04"+"040400"+"0047006f",
		"0001060f04"+"040408",
		"0001060f04"+"040410",
		"0001060f04"+"0200000002"+"0a04000a00"+"0204002c01",
		"0001060f04"+"035b000201"+"0f01000000",
	)
	stored := mouse.Macros[1]
	if stored.Cycles != 2 || stored.IntervalMs != 500 || !reflect.DeepEqual(stored.Records, macro.Records) || len(stored.Name) != MouseMacroNameLen {
		t.Fatalf("stored macro: %+v", stored)
	}

	// Reading asks for the header, two blocks of the name, then the
	// records two at a time without naming the button again.
	from := len(mouse.Frames)
	got, err := session.MouseReadMacro(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	wantMouseFrames(t, mouse.Frames, from,
		"0001060f01"+"010004", "0001060f01"+"040400", "0001060f01"+"040408",
		"0001060f01"+"0200000002", "0001060f01"+"0200000201")
	wantName := make([]byte, MouseMacroNameRead)
	copy(wantName, macro.Name)
	if got.Cycles != 2 || got.IntervalMs != 500 || !bytes.Equal(got.Name, wantName) || !reflect.DeepEqual(got.Records, macro.Records) {
		t.Fatalf("macro read back as %+v", got)
	}

	// Another button's macro is its own; one with nothing stored is empty.
	if empty, err := session.MouseReadMacro(ctx, 5); err != nil || len(empty.Records) != 0 {
		t.Fatalf("button 5's macro: %+v err=%v", empty, err)
	}
	for _, bad := range []struct {
		button int
		macro  MouseMacro
	}{
		{2, macro}, {7, macro}, {4, MouseMacro{}},
		{4, MouseMacro{Records: make([]MouseMacroRecord, MouseMacroMaxRecords+1)}},
		{4, MouseMacro{Records: macro.Records, Name: make([]byte, MouseMacroNameLen+2)}},
	} {
		before := len(mouse.Frames)
		if err := session.MouseWriteMacro(ctx, bad.button, bad.macro); err == nil || len(mouse.Frames) != before {
			t.Fatalf("button %d with %d records must be refused before anything is sent", bad.button, len(bad.macro.Records))
		}
	}
	// The longest macro: eighty records in forty reports.
	long := MouseMacro{Cycles: 1, Records: make([]MouseMacroRecord, MouseMacroMaxRecords)}
	for i := range long.Records {
		long.Records[i] = MouseMacroRecord{State: 10, Code: uint16(4 + i%20), Timer: uint16(i)}
	}
	from = len(mouse.Frames)
	if err := session.MouseWriteMacro(ctx, 6, long); err != nil {
		t.Fatal(err)
	}
	if sent := len(mouse.Frames) - from; sent != 1+3+40 || mouse.BadSums != 0 {
		t.Fatalf("%d frames, %d bad sums", sent, mouse.BadSums)
	}
	if got, err := session.MouseReadMacro(ctx, 6); err != nil || !reflect.DeepEqual(got.Records, long.Records) {
		t.Fatalf("long macro read back wrong: err=%v", err)
	}
}

func TestMouseReceiverSaysWhetherAMouseIsLinked(t *testing.T) {
	receiver := NewMouseSimulator(true)
	session := mouseSession(t, receiver, mouseReceiverPID)
	ctx := context.Background()

	if linked, err := session.MouseReceiverLinked(ctx); err != nil || !linked {
		t.Fatalf("linked=%v err=%v", linked, err)
	}
	wantMouseFrames(t, receiver.Frames, 0, "0006030a01")
	// Through the receiver the mouse's replies are marked 01.
	if got, err := session.MouseReadSetting(ctx, MouseLiftOff); err != nil || got != 1 {
		t.Fatalf("lift-off through the receiver = %d err=%v", got, err)
	}

	// With the mouse off the receiver still answers, and nothing else does.
	receiver.Off = true
	if linked, err := session.MouseReceiverLinked(ctx); err != nil || linked {
		t.Fatalf("linked=%v err=%v with the mouse off", linked, err)
	}
	if _, err := session.MouseReadSetting(ctx, MouseLiftOff); err == nil {
		t.Fatal("a mouse that is off answers nothing")
	}

	// A reply marked for the other connection is not taken, as the vendor
	// application does not take it.
	wired := NewMouseSimulator(false)
	_, err := mouseSession(t, wired, mouseReceiverPID).MouseReadSetting(ctx, MouseLiftOff)
	mustErrCode(t, err, CodeInvalidResponse)
	_, err = mouseSession(t, NewMouseSimulator(true), retroMousePID).MouseReadSetting(ctx, MouseLiftOff)
	mustErrCode(t, err, CodeInvalidResponse)

	// The mouse by cable is not asked the receiver's question.
	_, err = mouseSession(t, wired, retroMousePID).MouseReceiverLinked(ctx)
	mustErrCode(t, err, CodeUnsupportedForPid)
}

func TestMouseReplyHasToAnswerTheRequest(t *testing.T) {
	reply := func(head ...byte) []byte { return append(head, make([]byte, 64-len(head))...) }
	for _, c := range []struct {
		command CommandID
		reply   []byte
		want    ResponseStatus
	}{
		{CommandMouseReadLiftOff, reply(0x21, 0x06, 0x26, 0x11, 1), StatusOk},
		{CommandMouseReadLiftOff, reply(0x01, 0x06, 0x26, 0x01, 1), StatusOk},
		{CommandMouseReadLiftOff, reply(0x21, 0x06, 0x26, 0x14, 1), StatusInvalid}, // a write's acknowledgement
		{CommandMouseReadLiftOff, reply(0x21, 0x06, 0x24, 0x11, 1), StatusInvalid}, // another setting
		{CommandMouseReadLiftOff, reply(0x02, 0x06, 0x26, 0x11, 1), StatusInvalid}, // not a mouse's reply
		{CommandMouseWriteLiftOff, reply(0x21, 0x06, 0x26, 0x14), StatusOk},
		{CommandMouseWriteLiftOff, reply(0x21, 0x06, 0x26, 0x11), StatusInvalid},
		{CommandMouseClearProfile, reply(0x21, 0x01, 0x33, 0x04), StatusOk},
		{CommandMouseReadLiftOff, []byte{0x21, 0x06, 0x26}, StatusMalformed},
		{CommandMouseReceiverLinked, reply(0x06, 0x03, 0x0a, 0x11, 1), StatusOk},
		{CommandMouseReceiverLinked, reply(0x06, 0x03, 0x0a, 0x01, 1), StatusInvalid},
		{CommandMouseReceiverLinked, reply(0x01, 0x03, 0x0a, 0x11, 1), StatusInvalid},
		{CommandMouseRecordRead, reply(0x02, 0x04, 0x03, 0x02, 4), StatusOk},
		{CommandMouseRecordRead, reply(0x02, 0x04, 0xc0, 0x02, 4), StatusInvalid},
		{CommandMouseRecordWrite, reply(0x02, 0x04, 0x03, 0x02, 4), StatusInvalid},
	} {
		if got := ValidateResponse(c.command, c.reply); got != c.want {
			t.Errorf("%s answered % x: %v, want %v", c.command, c.reply[:5], got, c.want)
		}
	}
}

func TestMouseWriteIsNotRepeatedWhenUnanswered(t *testing.T) {
	mouse := NewMouseSimulator(false)
	mouse.Mute = 0x26
	session := mouseSession(t, mouse, retroMousePID)
	if err := session.MouseWriteSetting(context.Background(), MouseLiftOff, 2); err == nil {
		t.Fatal("an unanswered write must be an error")
	}
	if len(mouse.Frames) != 1 || mouse.LiftOff != 1 {
		t.Fatalf("%d frames sent, lift-off %d", len(mouse.Frames), mouse.LiftOff)
	}
}

func TestRivieraMouseRecordIsReadAndWrittenByRange(t *testing.T) {
	mouse := NewRivieraMouseSimulator()
	record := mouse.Record()
	if len(record) != MouseRecordSize {
		t.Fatalf("record is %d bytes", len(record))
	}
	for i := range record {
		record[i] = byte(i*3 + 1)
	}
	session := mouseSession(t, mouse, rivieraMousePID)
	ctx := context.Background()

	got, err := session.MouseRecordRead(ctx, 0, MouseRecordSize)
	if err != nil || !bytes.Equal(got, record) {
		t.Fatalf("record read: err=%v", err)
	}
	// 140 bytes at 53 a report: 53, 53 and the 34 that remain at 106.
	if len(mouse.Frames) != 3 {
		t.Fatalf("expected 3 read requests, got %d", len(mouse.Frames))
	}
	for i, want := range []string{"81040200350000000000", "81040200350035000000", "8104020022006a000000"} {
		if head, _ := hex.DecodeString(want); !bytes.Equal(mouse.Frames[i][:10], head) || len(mouse.Frames[i]) != 64 {
			t.Fatalf("read request %d = % x", i, mouse.Frames[i][:10])
		}
	}
	// The stage in use is one byte asked for on its own.
	if one, err := session.MouseRecordRead(ctx, 0x25, 1); err != nil || !bytes.Equal(one, record[0x25:0x26]) {
		t.Fatalf("single byte read: % x err=%v", one, err)
	}

	// A button's entry: twelve bytes at 0x5c with their sum (1+2+...+12).
	edited := append([]byte(nil), record...)
	for i := 0; i < 12; i++ {
		edited[0x5c+i] = byte(i + 1)
	}
	from := len(mouse.Frames)
	if err := session.MouseRecordWriteRange(ctx, edited, 0x5c, 12); err != nil {
		t.Fatal(err)
	}
	if want, _ := hex.DecodeString("810401000c4e5c0000000102030405060708090a0b0c"); len(mouse.Frames) != from+1 || !bytes.Equal(mouse.Frames[from][:22], want) {
		t.Fatalf("write request = % x", mouse.Frames[from][:22])
	}
	if !bytes.Equal(mouse.Record(), edited) || mouse.BadSums != 0 {
		t.Fatal("the entry was not stored where it was sent")
	}

	for _, bad := range [][2]int{{-1, 4}, {0, 0}, {MouseRecordSize - 2, 4}, {MouseRecordSize, 1}} {
		if _, err := session.MouseRecordRead(ctx, bad[0], bad[1]); err == nil {
			t.Fatalf("read of %d+%d must be refused", bad[0], bad[1])
		}
		if err := session.MouseRecordWriteRange(ctx, edited, bad[0], bad[1]); err == nil {
			t.Fatalf("write of %d+%d must be refused", bad[0], bad[1])
		}
	}
	if err := session.MouseRecordWriteRange(ctx, edited[:100], 0, 4); err == nil {
		t.Fatal("a record of the wrong size must be refused")
	}
	// A report the mouse only partly took is an error, not silently short.
	mouse.ShortAccept = true
	if err := session.MouseRecordWriteRange(ctx, edited, 0x44, 2); err == nil {
		t.Fatal("a chunk the mouse only partly accepted must be an error")
	}
}

func TestMouseCommandsNeedTheTierAndTheUnlock(t *testing.T) {
	ctx := context.Background()
	unlocked := fastRetryConfig()
	unlocked.Experimental, unlocked.CandidateWriteUnlock = true, true

	// As shipped the mice are detect-only (the Riviera has no row at all):
	// nothing is sent to one, whatever the caller has unlocked.
	for _, pid := range []uint16{retroMousePID, mouseReceiverPID, rivieraMousePID} {
		mouse := NewMouseSimulator(pid == mouseReceiverPID)
		session := openSession(t, mouse, pid, unlocked)
		_, err := session.MouseReadSetting(ctx, MouseLiftOff)
		mustErrCode(t, err, CodeUnsupportedForPid)
		_, err = session.MouseRecordRead(ctx, 0, 4)
		mustErrCode(t, err, CodeUnsupportedForPid)
		mustErrCode(t, session.MouseWriteSetting(ctx, MouseLiftOff, 2), CodeUnsupportedForPid)
		_, err = session.MouseReceiverLinked(ctx)
		mustErrCode(t, err, CodeUnsupportedForPid)
		if len(mouse.Frames) != 0 {
			t.Fatalf("pid=%#04x: a mouse command was sent to a detect-only device", pid)
		}
		if session.Profile().Capability != (PidCapability{}) {
			t.Fatalf("pid=%#04x: a detect-only device has capabilities: %+v", pid, session.Profile().Capability)
		}
	}

	raiseMouseTier(t)
	if got := DeviceProfileFor(VidPid{VID: 0x2dc8, PID: retroMousePID}).Capability; got != (PidCapability{SupportsMouse: true}) {
		t.Fatalf("a mouse's capabilities: %+v", got)
	}
	// Reads need experimental mode; writes the write-unlock as well.
	mouse := NewMouseSimulator(false)
	session := openSession(t, mouse, retroMousePID, fastRetryConfig())
	_, err := session.MouseReadSetting(ctx, MouseLiftOff)
	mustErrCode(t, err, CodeExperimentalRequired)
	readOnly := fastRetryConfig()
	readOnly.Experimental = true
	session = openSession(t, mouse, retroMousePID, readOnly)
	mustErrCode(t, session.MouseWriteSetting(ctx, MouseLiftOff, 2), CodeUnsupportedForPid)
	mustErrCode(t, session.MouseClearProfile(ctx), CodeUnsupportedForPid)
	mustErrCode(t, session.MouseWriteMacro(ctx, 3, MouseMacro{Records: []MouseMacroRecord{{State: 10, Code: 4}}}), CodeUnsupportedForPid)
	if len(mouse.Frames) != 0 || mouse.LiftOff != 1 {
		t.Fatalf("%d frames were sent without the gates open", len(mouse.Frames))
	}
	if got, err := session.MouseReadSetting(ctx, MouseLiftOff); err != nil || got != 1 {
		t.Fatalf("a read with experimental mode: %d err=%v", got, err)
	}
	riviera := NewRivieraMouseSimulator()
	session = openSession(t, riviera, rivieraMousePID, readOnly)
	mustErrCode(t, session.MouseRecordWriteRange(ctx, make([]byte, MouseRecordSize), 0, 4), CodeUnsupportedForPid)
	if len(riviera.Frames) != 0 {
		t.Fatal("a record write was sent without the write-unlock")
	}
}

func TestMouseTakesOnlyItsOwnCommands(t *testing.T) {
	raiseMouseTier(t)
	ctx := context.Background()
	config := fastRetryConfig()
	config.Experimental, config.CandidateWriteUnlock = true, true

	// A mouse is sent nothing of another family's, the identity queries
	// included, and each mouse only its own kind of command.
	for _, pid := range []uint16{retroMousePID, mouseReceiverPID, rivieraMousePID} {
		mouse := NewMouseSimulator(pid == mouseReceiverPID)
		session := openSession(t, mouse, pid, config)
		for _, command := range []CommandID{CommandGetPid, CommandGetReportRevision, CommandVersion, CommandIdle, CommandGetMode, CommandReadProfile} {
			_, err := session.SendCommand(ctx, command, nil)
			mustErrCode(t, err, CodeUnsupportedForPid)
		}
		_, err := session.KbRecordRead(ctx, 0, 4)
		mustErrCode(t, err, CodeUnsupportedForPid)
		mustErrCode(t, session.KbRecordSetKeyReports(ctx, false), CodeUnsupportedForPid)
		_, err = session.U2ReadRecord(ctx, 0x14)
		mustErrCode(t, err, CodeUnsupportedForPid)
		if pid == rivieraMousePID {
			_, err = session.MouseReadSetting(ctx, MouseLiftOff)
			mustErrCode(t, err, CodeUnsupportedForPid)
			mustErrCode(t, session.MouseClearProfile(ctx), CodeUnsupportedForPid)
		} else {
			_, err = session.MouseRecordRead(ctx, 0, 4)
			mustErrCode(t, err, CodeUnsupportedForPid)
		}
		if len(mouse.Frames) != 0 {
			t.Fatalf("pid=%#04x: something not meant for this mouse was sent: % x", pid, mouse.Frames[0][:8])
		}
	}
	// And nothing else is sent a mouse command: an Ultimate 2, a Retro 108,
	// a record keyboard, a candidate controller.
	for _, pid := range []uint16{0x6012, 0x5209, 0x2028, 0x6002} {
		mouse := NewMouseSimulator(false)
		session := openSession(t, mouse, pid, config)
		_, err := session.MouseReadSetting(ctx, MouseLiftOff)
		mustErrCode(t, err, CodeUnsupportedForPid)
		_, err = session.MouseRecordRead(ctx, 0, 4)
		mustErrCode(t, err, CodeUnsupportedForPid)
		mustErrCode(t, session.MouseWriteDpiStage(ctx, 0, 800), CodeUnsupportedForPid)
		if len(mouse.Frames) != 0 {
			t.Fatalf("pid=%#04x: a mouse command was sent", pid)
		}
	}
}

func TestMouseDiagnosticProbeAsksOnlyWholeQuestions(t *testing.T) {
	raiseMouseTier(t)
	config := fastRetryConfig()
	config.Experimental = true

	receiver := NewMouseSimulator(true)
	result := openSession(t, receiver, mouseReceiverPID, config).DiagProbe(context.Background())
	// The four one-byte settings, the X stages, the stage in use, the
	// polling rate, and the receiver's link.
	if len(receiver.Frames) != 8 || len(result.CommandChecks) != 8 || !result.TransportReady {
		t.Fatalf("%d frames, %d checks, ready=%v", len(receiver.Frames), len(result.CommandChecks), result.TransportReady)
	}
	for _, check := range result.CommandChecks {
		if !check.OK || !check.IsExperimental {
			t.Fatalf("%s: ok=%v experimental=%v %s", check.Command, check.OK, check.IsExperimental, check.Detail)
		}
	}
	for _, frame := range receiver.Frames {
		if frame[4] != mouseOpRead {
			t.Fatalf("the probe sent something other than a read: % x", frame[:8])
		}
		switch frame[3] {
		case 0x1c, 0x14, 0x0f:
			t.Fatalf("the probe sent a bare part request: % x", frame[:8])
		}
	}

	// A Riviera mouse's only read names a range; the probe has nothing to
	// ask it.
	riviera := NewRivieraMouseSimulator()
	openSession(t, riviera, rivieraMousePID, config).DiagProbe(context.Background())
	if len(riviera.Frames) != 0 {
		t.Fatalf("the probe sent a Riviera mouse %d frames", len(riviera.Frames))
	}
}
