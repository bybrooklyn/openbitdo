package protocol

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
)

func u2Session(t *testing.T, pad *U2Simulator) *DeviceSession {
	t.Helper()
	config := fastRetryConfig()
	config.Experimental = true
	return openSession(t, pad, 0x6012, config)
}

func TestU2CRCIsModbus(t *testing.T) {
	// The standard check value for CRC-16/MODBUS.
	if got := u2CRC([]byte("123456789")); got != 0x4b37 {
		t.Fatalf("crc = %#04x, want 0x4b37", got)
	}
}

func TestU2ReceiverReportsAControllerThatIsOff(t *testing.T) {
	// The frame and reply a real 0x6013 receiver exchanged with its
	// controller switched off.
	pad := &U2Simulator{Off: true}
	session := u2Session(t, pad)
	connected, err := session.U2Connected(context.Background())
	if err != nil || connected {
		t.Fatalf("connected=%v err=%v, want false and no error", connected, err)
	}
	last := pad.Frames[len(pad.Frames)-1]
	if want, _ := hex.DecodeString("81042001"); !bytes.Equal(last[:4], want) || len(last) != 64 {
		t.Fatalf("request = % x", last[:8])
	}
	// With the controller off nothing else is answered.
	if _, err := session.U2ReadRecord(context.Background(), 0x14); err == nil {
		t.Fatal("a read must fail while the controller is off")
	}

	pad.Off = false
	if connected, err := session.U2Connected(context.Background()); err != nil || !connected {
		t.Fatalf("connected=%v err=%v, want true", connected, err)
	}
}

func TestU2RecordReadIsChunkedAndComplete(t *testing.T) {
	pad := &U2Simulator{Physical: U2PlatformXInput}
	record := pad.Record(U2PlatformXInput)
	for i := range record {
		record[i] = byte(i*7 + i>>8)
	}
	session := u2Session(t, pad)
	ctx := context.Background()

	if platform, err := session.U2PhysicalPlatform(ctx); err != nil || platform != U2PlatformXInput {
		t.Fatalf("platform=%d err=%v", platform, err)
	}
	if err := session.U2SetInputReports(ctx, false); err != nil || pad.InputReports {
		t.Fatalf("input reports should be paused: err=%v on=%v", err, pad.InputReports)
	}
	if err := session.U2SelectPlatform(ctx, U2PlatformXInput); err != nil {
		t.Fatal(err)
	}
	before := len(pad.Frames)
	got, err := session.U2ReadRecord(ctx, U2RecordSize)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, record) {
		t.Fatal("the record read back differs from what the controller holds")
	}
	// 1592 bytes at 45 per report is 36 requests; the second asks for
	// 45 bytes at offset 45 of a total of 0x638.
	if n := len(pad.Frames) - before; n != 36 {
		t.Fatalf("expected 36 read requests, got %d", n)
	}
	second := pad.Frames[before+1]
	if want, _ := hex.DecodeString("8104020000002d000000380600002d000000"); !bytes.Equal(second[:18], want) {
		t.Fatalf("second read request = % x", second[:18])
	}
}

func TestU2WriteTakesEffectOnlyOnCommit(t *testing.T) {
	pad := &U2Simulator{Physical: U2PlatformDInput}
	session := u2Session(t, pad)
	ctx := context.Background()
	if err := session.U2SelectPlatform(ctx, U2PlatformDInput); err != nil {
		t.Fatal(err)
	}
	record, err := session.U2ReadRecord(ctx, U2RecordSize)
	if err != nil {
		t.Fatal(err)
	}
	// A 92-byte button-map slot spans three reports.
	for i := 0xe0; i < 0xe0+92; i++ {
		record[i] = byte(i)
	}
	if err := session.U2WriteRecordRange(ctx, record, 0xe0, 92); err != nil {
		t.Fatal(err)
	}
	if pad.BadCRCs != 0 {
		t.Fatalf("%d chunks carried a wrong crc", pad.BadCRCs)
	}
	if bytes.Equal(pad.Record(U2PlatformDInput), record) {
		t.Fatal("a write must not take effect before it is committed")
	}
	if err := session.U2Commit(ctx); err != nil || pad.Commits != 1 {
		t.Fatalf("commit: err=%v commits=%d", err, pad.Commits)
	}
	if !bytes.Equal(pad.Record(U2PlatformDInput), record) {
		t.Fatal("after commit the controller should hold the written record")
	}
	// The other platform's record is untouched.
	if !bytes.Equal(pad.Record(U2PlatformXInput)[0xe0:0xe0+92], make([]byte, 92)) {
		t.Fatal("a write to one platform's record changed another's")
	}
}

func TestU2WriteStopsOnAPartlyAcceptedChunk(t *testing.T) {
	pad := &U2Simulator{ShortAccept: true}
	session := u2Session(t, pad)
	record := make([]byte, U2RecordSize)
	if err := session.U2WriteRecordRange(context.Background(), record, 0x98, 8); err == nil {
		t.Fatal("a chunk the controller only partly accepted must be an error")
	}
	if err := session.U2WriteRecordRange(context.Background(), record[:10], 0, 4); err == nil {
		t.Fatal("a record of the wrong size must be refused")
	}
	if err := session.U2WriteRecordRange(context.Background(), record, U2RecordSize-2, 4); err == nil {
		t.Fatal("a range past the end of the record must be refused")
	}
}

func TestU2CommitThatIsNeverAnsweredIsAnError(t *testing.T) {
	pad := &U2Simulator{DropCommit: true}
	config := fastRetryConfig()
	config.Experimental = true
	config.TimeoutProfile.IOMs = 1
	session := openSession(t, pad, 0x6012, config)
	if err := session.U2Commit(context.Background()); err == nil {
		t.Fatal("an unanswered commit must not be reported as done")
	}
}

func TestU2ReplyToAnotherCommandIsNotAccepted(t *testing.T) {
	reply := make([]byte, 64)
	copy(reply, []byte{0x02, 0x04, 0x04, 0x00, 0x20, 0x01, 0x01, 0x00})
	if ValidateResponse(CommandU2GetConnected, reply) != StatusOk {
		t.Fatal("the real receiver's reply should validate")
	}
	if ValidateResponse(CommandU2RecordRead, reply) == StatusOk {
		t.Fatal("a reply to cmd 0x120 must not pass as a reply to a read")
	}
	if ValidateResponse(CommandU2RecordRead, reply[:10]) != StatusMalformed {
		t.Fatal("a reply too short to hold the header is malformed")
	}
}
