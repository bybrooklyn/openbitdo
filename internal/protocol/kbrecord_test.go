package protocol

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
)

// kbRecordSession opens a session on a Retro 87 as someone who has been
// through the write-unlock ceremony: these keyboards are read-only without
// it.
func kbRecordSession(t *testing.T, keyboard *KbRecordSimulator) *DeviceSession {
	t.Helper()
	config := fastRetryConfig()
	config.Experimental, config.CandidateWriteUnlock = true, true
	return openSession(t, keyboard, 0x2028, config)
}

func kbRecordFramesOf(keyboard *KbRecordSimulator, from int, cmd byte) [][]byte {
	var frames [][]byte
	for _, frame := range keyboard.Frames[from:] {
		if frame[2] == cmd {
			frames = append(frames, frame)
		}
	}
	return frames
}

func TestKbRecordReadIsChunkedAndComplete(t *testing.T) {
	keyboard := &KbRecordSimulator{}
	record := keyboard.Record()
	for i := range record {
		record[i] = byte(i*7 + i>>8)
	}
	session := kbRecordSession(t, keyboard)
	ctx := context.Background()

	if err := session.KbRecordSetKeyReports(ctx, false); err != nil || keyboard.KeyReports {
		t.Fatalf("key reports should be off: err=%v on=%v", err, keyboard.KeyReports)
	}
	if want, _ := hex.DecodeString("81040800000000000000"); !bytes.Equal(keyboard.Frames[0][:10], want) || len(keyboard.Frames[0]) != 64 {
		t.Fatalf("report mode request = % x", keyboard.Frames[0][:10])
	}
	before := len(keyboard.Frames)
	got, err := session.KbRecordRead(ctx, 0, KbRecordSize)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, record) {
		t.Fatal("the record read back differs from what the keyboard holds")
	}
	// 1532 bytes at 53 per report is 29 requests; the second asks for 53
	// bytes at offset 53, and the last for the 48 that remain at 1484.
	reads := kbRecordFramesOf(keyboard, before, kbRecordCmdRead)
	if len(reads) != 29 || len(keyboard.Frames)-before != 29 {
		t.Fatalf("expected 29 read requests, got %d", len(reads))
	}
	if want, _ := hex.DecodeString("81040200350035000000"); !bytes.Equal(reads[1][:10], want) {
		t.Fatalf("second read request = % x", reads[1][:10])
	}
	if want, _ := hex.DecodeString("810402003000cc050000"); !bytes.Equal(reads[28][:10], want) {
		t.Fatalf("last read request = % x", reads[28][:10])
	}
	for _, frame := range reads {
		if !bytes.Equal(frame[kbRecordDataOffset:], make([]byte, 64-kbRecordDataOffset)) {
			t.Fatalf("a read request carries no data: % x", frame)
		}
	}

	// One byte anywhere can be asked for on its own: the profile switch.
	record[0x24] = 1
	if one, err := session.KbRecordRead(ctx, 0x24, 1); err != nil || !bytes.Equal(one, []byte{1}) {
		t.Fatalf("single byte read: % x err=%v", one, err)
	}
	if err := session.KbRecordSetKeyReports(ctx, true); err != nil || !keyboard.KeyReports {
		t.Fatalf("key reports should be back on: err=%v", err)
	}
	if last := keyboard.Frames[len(keyboard.Frames)-1]; last[2] != 0x08 || last[3] != 0x01 {
		t.Fatalf("report mode request = % x", last[:10])
	}
}

func TestKbRecordWriteFramesCarryLengthSumAndOffset(t *testing.T) {
	keyboard := &KbRecordSimulator{}
	session := kbRecordSession(t, keyboard)
	ctx := context.Background()
	record, err := session.KbRecordRead(ctx, 0, KbRecordSize)
	if err != nil {
		t.Fatal(err)
	}

	// One key's twelve-byte entry: key 12 (the A key) sends Shift+1.
	entry, _ := hex.DecodeString("04000000e11e000001000000")
	copy(record[0x28+12*12:], entry)
	before := len(keyboard.Frames)
	if err := session.KbRecordWriteRange(ctx, record, 0x28+12*12, 12); err != nil {
		t.Fatal(err)
	}
	// 81 04, cmd 1, sub 0, len 12, sum 04+e1+1e+01 = 0x04 (mod 256),
	// offset 0xb8, then the entry.
	want, _ := hex.DecodeString("810401000c04b8000000" + "04000000e11e000001000000")
	if frame := keyboard.Frames[before]; !bytes.Equal(frame[:22], want) || !bytes.Equal(frame[22:], make([]byte, 42)) {
		t.Fatalf("write request = % x", frame)
	}
	if len(keyboard.Frames)-before != 1 || keyboard.BadSums != 0 {
		t.Fatalf("frames=%d badSums=%d", len(keyboard.Frames)-before, keyboard.BadSums)
	}
	// Stored as acknowledged: there is no commit.
	if !bytes.Equal(keyboard.Record()[0x28+12*12:0x28+12*13], entry) {
		t.Fatal("the keyboard should hold the entry once it is acknowledged")
	}

	// A 120-byte range spans three reports: 53, 53 and 14 bytes.
	for i := 0x100; i < 0x100+120; i++ {
		record[i] = byte(i)
	}
	before = len(keyboard.Frames)
	if err := session.KbRecordWriteRange(ctx, record, 0x100, 120); err != nil {
		t.Fatal(err)
	}
	writes := kbRecordFramesOf(keyboard, before, kbRecordCmdWrite)
	if len(writes) != 3 {
		t.Fatalf("expected 3 write requests, got %d", len(writes))
	}
	for i, shape := range []struct{ length, offset byte }{{53, 0x00}, {53, 0x35}, {14, 0x6a}} {
		if writes[i][4] != shape.length || writes[i][6] != shape.offset || writes[i][7] != 0x01 {
			t.Fatalf("write %d: len=%d offset=% x", i, writes[i][4], writes[i][6:10])
		}
	}
	if keyboard.BadSums != 0 || !bytes.Equal(keyboard.Record(), record) {
		t.Fatalf("badSums=%d; the keyboard should hold the written record", keyboard.BadSums)
	}
}

func TestKbRecordWriteFailsWhenTheKeyboardDoesNotTakeIt(t *testing.T) {
	ctx := context.Background()
	record := make([]byte, KbRecordSize)

	short := &KbRecordSimulator{ShortAccept: true}
	err := kbRecordSession(t, short).KbRecordWriteRange(ctx, record, 0x5c9, 1)
	mustErrCode(t, err, CodeInvalidResponse)

	refusing := &KbRecordSimulator{Refuse: true}
	session := kbRecordSession(t, refusing)
	if err := session.KbRecordWriteRange(ctx, record, 0x5c9, 1); err == nil {
		t.Fatal("a refused write must fail")
	}
	// A write is never sent twice, however it was answered.
	if n := len(kbRecordFramesOf(refusing, 0, kbRecordCmdWrite)); n != 1 {
		t.Fatalf("a refused write was sent %d times", n)
	}
	if _, err := session.KbRecordRead(ctx, 0, 4); err == nil {
		t.Fatal("a refused read must fail")
	}

	off := &KbRecordSimulator{Off: true}
	if _, err := kbRecordSession(t, off).KbRecordRead(ctx, 0, 4); err == nil {
		t.Fatal("a read must fail while the keyboard is off")
	}
}

func TestKbRecordRangesAreCheckedBeforeAnythingIsSent(t *testing.T) {
	keyboard := &KbRecordSimulator{}
	session := kbRecordSession(t, keyboard)
	ctx := context.Background()
	record := make([]byte, KbRecordSize)

	_, err := session.KbRecordRead(ctx, KbRecordSize-1, 2)
	mustErrCode(t, err, CodeInvalidInput)
	mustErrCode(t, session.KbRecordWriteRange(ctx, record, KbRecordSize-3, 4), CodeInvalidInput)
	mustErrCode(t, session.KbRecordWriteRange(ctx, record[:100], 0, 4), CodeInvalidInput)
	mustErrCode(t, session.KbRecordWriteRange(ctx, record, 4, 0), CodeInvalidInput)
	_, err = session.KbRecordReadMacroData(ctx, KbRecordMacroAreaMax-4, 8)
	mustErrCode(t, err, CodeInvalidInput)
	mustErrCode(t, session.KbRecordPrepareMacro(ctx, KbRecordMacroAreaMax), CodeInvalidInput)
	mustErrCode(t, session.KbRecordWriteMacroData(ctx, -1, []byte{1}), CodeInvalidInput)
	mustErrCode(t, session.KbRecordWriteLights(ctx, make([]byte, KbRecordLightsMax+1)), CodeInvalidInput)
	_, err = session.KbRecordReadLights(ctx, 0)
	mustErrCode(t, err, CodeInvalidInput)
	if len(keyboard.Frames) != 0 {
		t.Fatalf("%d frames were sent for requests that should have been refused", len(keyboard.Frames))
	}
}

func TestKbRecordMacroIsPreparedThenWritten(t *testing.T) {
	keyboard := &KbRecordSimulator{}
	session := kbRecordSession(t, keyboard)
	ctx := context.Background()

	// Slot 2 starts at 8192: a 48-byte header there, steps 64 bytes in.
	header := bytes.Repeat([]byte{0x5a}, 48)
	steps := make([]byte, 120)
	for i := range steps {
		steps[i] = byte(i + 1)
	}
	if err := session.KbRecordPrepareMacro(ctx, 8192); err != nil {
		t.Fatal(err)
	}
	if err := session.KbRecordWriteMacroData(ctx, 8192, header); err != nil {
		t.Fatal(err)
	}
	if err := session.KbRecordWriteMacroData(ctx, 8192+64, steps); err != nil {
		t.Fatal(err)
	}
	if keyboard.Unprepared != 0 || keyboard.BadSums != 0 {
		t.Fatalf("unprepared=%d badSums=%d", keyboard.Unprepared, keyboard.BadSums)
	}
	// Prepare: cmd 5, no data, the slot's offset. Then cmd 6: the header
	// in one report, the steps in three (53, 53, 14).
	if want, _ := hex.DecodeString("81040500000000200000"); !bytes.Equal(keyboard.Frames[0][:10], want) {
		t.Fatalf("prepare request = % x", keyboard.Frames[0][:10])
	}
	writes := kbRecordFramesOf(keyboard, 0, kbRecordCmdMacroWrite)
	if len(writes) != 4 {
		t.Fatalf("expected 4 macro writes, got %d", len(writes))
	}
	if want, _ := hex.DecodeString("8104060030e000200000"); !bytes.Equal(writes[0][:10], want) {
		t.Fatalf("header write = % x", writes[0][:10])
	}
	if want, _ := hex.DecodeString("81040600359740200000"); !bytes.Equal(writes[1][:10], want) {
		t.Fatalf("first step write = % x", writes[1][:10])
	}

	got, err := session.KbRecordReadMacroData(ctx, 8192, 48)
	if err != nil || !bytes.Equal(got, header) {
		t.Fatalf("header read back: % x err=%v", got, err)
	}
	if want, _ := hex.DecodeString("81040700300000200000"); !bytes.Equal(keyboard.Frames[len(keyboard.Frames)-1][:10], want) {
		t.Fatalf("macro read request = % x", keyboard.Frames[len(keyboard.Frames)-1][:10])
	}
	got, err = session.KbRecordReadMacroData(ctx, 8192+64, len(steps))
	if err != nil || !bytes.Equal(got, steps) {
		t.Fatalf("steps read back: % x err=%v", got, err)
	}
	// The other slots are as they were: erased.
	if keyboard.Macros()[4096] != 0xff || keyboard.Macros()[12288] != 0xff {
		t.Fatal("a write to one slot changed another")
	}

	// The simulator notices a macro written without preparing its slot.
	if err := session.KbRecordWriteMacroData(ctx, 4096, header); err != nil || keyboard.Unprepared != 1 {
		t.Fatalf("err=%v unprepared=%d, want an unprepared write counted", err, keyboard.Unprepared)
	}
}

func TestKbRecordLightsBlockIsPreparedThenWrittenWhole(t *testing.T) {
	keyboard := &KbRecordSimulator{}
	session := kbRecordSession(t, keyboard)
	ctx := context.Background()

	block := make([]byte, 283)
	for i := range block {
		block[i] = byte(i * 3)
	}
	if err := session.KbRecordWriteLights(ctx, block); err != nil {
		t.Fatal(err)
	}
	// One prepare (cmd 0x0d), then 283 bytes at 53 per report: six writes.
	if want, _ := hex.DecodeString("81040d00000000000000"); !bytes.Equal(keyboard.Frames[0][:10], want) {
		t.Fatalf("prepare request = % x", keyboard.Frames[0][:10])
	}
	writes := kbRecordFramesOf(keyboard, 0, kbRecordCmdLightsWrite)
	if len(writes) != 6 || len(keyboard.Frames) != 7 {
		t.Fatalf("expected a prepare and 6 writes, got %d frames", len(keyboard.Frames))
	}
	if last := writes[5]; last[4] != 18 || last[6] != 0x09 || last[7] != 0x01 {
		t.Fatalf("last write: len=%d offset=% x", last[4], last[6:10])
	}
	if keyboard.Unprepared != 0 || !bytes.Equal(keyboard.Lights(), block) {
		t.Fatalf("unprepared=%d; the keyboard should hold the block", keyboard.Unprepared)
	}
	got, err := session.KbRecordReadLights(ctx, 283)
	if err != nil || !bytes.Equal(got, block) {
		t.Fatalf("block read back differs: err=%v", err)
	}
	if reads := kbRecordFramesOf(keyboard, 7, kbRecordCmdLightsRead); len(reads) != 6 {
		t.Fatalf("expected 6 read requests, got %d", len(reads))
	}
}

func TestKbRecordReplyMatching(t *testing.T) {
	reply := func(status, cmd, length byte) []byte {
		frame := make([]byte, 64)
		frame[0], frame[1], frame[2], frame[3], frame[4] = 0x02, 0x04, status, cmd, length
		frame[10], frame[11] = 0xaa, 0xbb
		return frame
	}
	if length, data, ok := kbRecordReplyFor(reply(3, 2, 2), kbRecordCmdRead); !ok || length != 2 || !bytes.Equal(data, []byte{0xaa, 0xbb}) {
		t.Fatalf("read reply: ok=%v len=%d data=% x", ok, length, data)
	}
	// A read's reply is matched on the command's low four bits only.
	if _, _, ok := kbRecordReplyFor(reply(3, 0x1f, 1), kbRecordCmdLightsRead); !ok {
		t.Fatal("a read reply with high bits set in its command byte should match")
	}
	if _, _, ok := kbRecordReplyFor(reply(3, 0x11, 1), kbRecordCmdWrite); ok {
		t.Fatal("an acknowledgement is matched on the whole command byte")
	}
	if _, _, ok := kbRecordReplyFor(reply(3, 7, 1), kbRecordCmdRead); ok {
		t.Fatal("a reply to another command must not match")
	}
	if _, _, ok := kbRecordReplyFor(reply(0xc0, 2, 0), kbRecordCmdRead); ok {
		t.Fatal("a refusal must not match")
	}
	if ValidateResponse(CommandKbRecordRead, reply(0xc0, 2, 0)) != StatusInvalid ||
		ValidateResponse(CommandKbRecordWrite, reply(3, 1, 12)) != StatusOk ||
		ValidateResponse(CommandKbRecordMacroErase, reply(3, 5, 0)[:9]) != StatusMalformed {
		t.Fatal("ValidateResponse disagrees with the reply rules")
	}
	// A declared length longer than the report is cut to what arrived.
	if _, data, _ := kbRecordReplyFor(reply(3, 2, 200), kbRecordCmdRead); len(data) != 54 {
		t.Fatalf("data is %d bytes, want the 54 a report holds", len(data))
	}
}

func TestKbRecordIsReadOnlyUntilUnlocked(t *testing.T) {
	ctx := context.Background()
	record := make([]byte, KbRecordSize)
	// Every record keyboard and receiver that is a candidate takes reads
	// in experimental mode and refuses writes without the unlock.
	for _, pid := range []uint16{0x2028, 0x202e, 0x3026, 0x3027, 0x203a, 0x2049} {
		keyboard := &KbRecordSimulator{}
		config := fastRetryConfig()
		config.Experimental = true
		session := openSession(t, keyboard, pid, config)
		if _, err := session.KbRecordRead(ctx, 0, 4); err != nil {
			t.Fatalf("pid=%#04x: read: %v", pid, err)
		}
		sent := len(keyboard.Frames)
		mustErrCode(t, session.KbRecordWriteRange(ctx, record, 0, 4), CodeUnsupportedForPid)
		mustErrCode(t, session.KbRecordPrepareMacro(ctx, 0), CodeUnsupportedForPid)
		mustErrCode(t, session.KbRecordWriteMacroData(ctx, 0, []byte{1}), CodeUnsupportedForPid)
		mustErrCode(t, session.KbRecordWriteLights(ctx, []byte{1}), CodeUnsupportedForPid)
		if len(keyboard.Frames) != sent {
			t.Fatalf("pid=%#04x: a refused write reached the keyboard", pid)
		}
	}

	// Without experimental mode not even a read is sent.
	plain := &KbRecordSimulator{}
	session := openSession(t, plain, 0x2028, fastRetryConfig())
	_, err := session.KbRecordRead(ctx, 0, 4)
	mustErrCode(t, err, CodeExperimentalRequired)
	if len(plain.Frames) != 0 {
		t.Fatal("a read was sent without experimental mode")
	}
}

func TestKbRecordCommandsAreForRecordKeyboardsOnly(t *testing.T) {
	ctx := context.Background()
	config := fastRetryConfig()
	config.Experimental, config.CandidateWriteUnlock = true, true
	// An Ultimate 2, a Retro 108, a candidate controller, and the Riviera
	// keyboard, whose tier is still detect-only.
	for _, pid := range []uint16{0x6012, 0x5209, 0x6002, 0x205a} {
		keyboard := &KbRecordSimulator{}
		session := openSession(t, keyboard, pid, config)
		_, err := session.KbRecordRead(ctx, 0, 4)
		mustErrCode(t, err, CodeUnsupportedForPid)
		mustErrCode(t, session.KbRecordSetKeyReports(ctx, false), CodeUnsupportedForPid)
		mustErrCode(t, session.KbRecordWriteRange(ctx, make([]byte, KbRecordSize), 0, 4), CodeUnsupportedForPid)
		if len(keyboard.Frames) != 0 {
			t.Fatalf("pid=%#04x: a record keyboard command was sent", pid)
		}
	}
	// And a record keyboard takes nothing meant for the others.
	session := kbRecordSession(t, &KbRecordSimulator{})
	_, err := session.U2ReadRecord(ctx, 0x14)
	mustErrCode(t, err, CodeUnsupportedForPid)
}

func TestKbRecordDiagnosticProbeSendsNoRecordCommand(t *testing.T) {
	keyboard := &KbRecordSimulator{}
	config := fastRetryConfig()
	config.Experimental = true
	session := openSession(t, keyboard, 0x2028, config)
	session.DiagProbe(context.Background())
	if len(keyboard.Frames) == 0 {
		t.Fatal("the probe should have sent its identity queries")
	}
	for _, frame := range keyboard.Frames {
		for _, row := range CommandRegistry {
			if row.OperationGroup == "RecordKeyboard" && bytes.Equal(frame, row.Request) {
				t.Fatalf("the probe sent a bare %s", row.ID)
			}
		}
	}
}
