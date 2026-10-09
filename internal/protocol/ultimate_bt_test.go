package protocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// ultimateBTSession is a session with a first-generation Ultimate Bluetooth
// (or, under pid UltimateBTAdapterPID, its adapter) at the tier
// docs/spec/pid_matrix.csv gives it.
func ultimateBTSession(t *testing.T, pad *U2Simulator, pid uint16) *DeviceSession {
	t.Helper()
	config := fastRetryConfig()
	config.Experimental = true
	return openSession(t, pad, pid, config)
}

// wantFrame is a 64-byte request: head, then tail repeated to the end of
// what the count covers, then zeros.
func wantFrame(t *testing.T, head string, fill byte, filled int) []byte {
	t.Helper()
	raw, err := hex.DecodeString(strings.ReplaceAll(head, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 64)
	copy(frame, raw)
	for i := 0; i < filled; i++ {
		frame[len(raw)+i] = fill
	}
	return frame
}

func TestUltimateBTRequestsAreCountedFrames(t *testing.T) {
	// Every frame below is what the vendor's library builds for product
	// 0x6007: a count after the report id, the 04 marker and the header one
	// byte later than on the other controllers.
	pad := &U2Simulator{Counted: true, RecordSize: UltimateBTRecordSize, Physical: U2PlatformXInput}
	session := ultimateBTSession(t, pad, UltimateBTPID)
	ctx := context.Background()
	last := func() []byte { return pad.Frames[len(pad.Frames)-1] }
	check := func(what string, got, want []byte) {
		t.Helper()
		if !bytes.Equal(got, want) {
			t.Fatalf("%s\n got % x\nwant % x", what, got, want)
		}
	}

	// Pausing and resuming input reports: the header alone, no crc.
	if err := session.U2SetInputReports(ctx, false); err != nil || pad.InputReports {
		t.Fatalf("pause: err=%v on=%v", err, pad.InputReports)
	}
	check("pause", last(), wantFrame(t, "81 11 04 0700 0000", 0, 0))
	if err := session.U2SetInputReports(ctx, true); err != nil || !pad.InputReports {
		t.Fatalf("resume: err=%v on=%v", err, pad.InputReports)
	}
	// Resumed with a plain 1, not the keyed value a Pro 3 takes.
	check("resume", last(), wantFrame(t, "81 11 04 0700 0100", 0, 0))

	// Selecting a platform carries nothing, and the crc of nothing.
	if err := session.U2SelectPlatform(ctx, U2PlatformXInput); err != nil {
		t.Fatal(err)
	}
	check("select platform", last(), wantFrame(t, "81 11 04 1400 0300 0000 ffff", 0, 0))

	// A read asks for up to 45 bytes and carries that many bytes of filler
	// with their crc; the record is 0x914 bytes, so the last request asks
	// for the 29 that remain.
	stored := pad.Record(U2PlatformXInput)
	for i := range stored {
		stored[i] = byte(i*5 + i>>8)
	}
	before := len(pad.Frames)
	record, err := session.U2ReadRecord(ctx, UltimateBTRecordSize)
	if err != nil || !bytes.Equal(record, stored) {
		t.Fatalf("read: err=%v, %d bytes", err, len(record))
	}
	if n := len(pad.Frames) - before; n != 52 {
		t.Fatalf("a 0x914-byte record read in %d requests, want 52", n)
	}
	check("first read", pad.Frames[before], wantFrame(t, "81 3e 04 0200 0000 2d00 5b4f 14090000 00000000", 0xcc, 45))
	check("second read", pad.Frames[before+1], wantFrame(t, "81 3e 04 0200 0000 2d00 5b4f 14090000 2d000000", 0xcc, 45))
	check("last read", last(), wantFrame(t, "81 2e 04 0200 0000 1d00 8c01 14090000 f7080000", 0xcc, 29))

	// A write carries its data and the data's crc, and names the record's
	// whole size; a commit is the header alone.
	edited := append([]byte(nil), record...)
	copy(edited[0x98:], []byte{1, 2, 3, 4})
	if err := session.U2WriteRecordRange(ctx, edited, 0x98, 4); err != nil {
		t.Fatal(err)
	}
	check("write", last(), wantFrame(t, "81 15 04 0100 0000 0400 a12b 14090000 98000000 01020304", 0, 0))
	if err := session.U2Commit(ctx); err != nil || pad.Commits != 1 || pad.BadCRCs != 0 {
		t.Fatalf("commit: err=%v commits=%d badCRCs=%d", err, pad.Commits, pad.BadCRCs)
	}
	check("commit", last(), wantFrame(t, "81 11 04 0600 2301", 0, 0))
	if got := pad.Record(U2PlatformXInput)[0x98:0x9c]; !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Fatalf("stored % x", got)
	}

	// Macro storage: slot 1 of the XInput record, the second macro's region.
	if err := session.U2EraseMacroData(ctx, U2PlatformXInput, 1, 4096, 4096); err != nil {
		t.Fatal(err)
	}
	check("erase", last(), wantFrame(t, "81 11 04 0401 0103 0010 0000 00000000 00100000", 0, 0))
	steps := make([]byte, 32)
	for i := range steps {
		steps[i] = byte(i)
	}
	if err := session.U2WriteMacroData(ctx, U2PlatformXInput, 1, 4096, steps); err != nil {
		t.Fatal(err)
	}
	check("macro write", last(), wantFrame(t,
		"81 31 04 0301 0103 2000 6b57 20100000 00100000 000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", 0, 0))
	if err := session.U2Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := session.U2ReadMacroData(ctx, U2PlatformXInput, 1, 4096, 20)
	if err != nil || !bytes.Equal(got, steps[:20]) {
		t.Fatalf("macro read: % x err=%v", got, err)
	}
	check("macro read", last(), wantFrame(t, "81 25 04 0201 0103 1400 9e3d 14000000 00100000", 0xcc, 20))
}

func TestUltimateBTAdapterIsSentTheSameFramesWithoutACRC(t *testing.T) {
	// The vendor's library computes a crc for 0x6007 and not for 0x3106,
	// the id its software switches to once it has seen the adapter.
	pad := &U2Simulator{Counted: true, NoCRC: true, RecordSize: UltimateBTRecordSize}
	session := ultimateBTSession(t, pad, UltimateBTAdapterPID)
	ctx := context.Background()
	last := func() []byte { return pad.Frames[len(pad.Frames)-1] }

	if err := session.U2SelectPlatform(ctx, U2PlatformSwitch); err != nil {
		t.Fatal(err)
	}
	if want := wantFrame(t, "81 11 04 1400 0000 0000 0000", 0, 0); !bytes.Equal(last(), want) {
		t.Fatalf("select platform % x", last()[:12])
	}
	if _, err := session.U2ReadRecord(ctx, 0x14); err != nil {
		t.Fatal(err)
	}
	if want := wantFrame(t, "81 25 04 0200 0000 1400 0000 14000000 00000000", 0xcc, 20); !bytes.Equal(last(), want) {
		t.Fatalf("read % x", last())
	}
	record := make([]byte, UltimateBTRecordSize)
	copy(record[0x14:], []byte{1, 2, 3, 4})
	if err := session.U2WriteRecordRange(ctx, record, 0x14, 4); err != nil {
		t.Fatal(err)
	}
	if want := wantFrame(t, "81 15 04 0100 0000 0400 0000 14090000 14000000 01020304", 0, 0); !bytes.Equal(last(), want) {
		t.Fatalf("write % x", last()[:24])
	}
	if err := session.U2Commit(ctx); err != nil || pad.BadCRCs != 0 {
		t.Fatalf("commit: err=%v badCRCs=%d", err, pad.BadCRCs)
	}

	// The controller reached directly does check the crc: a frame without
	// one is not taken.
	strict := &U2Simulator{Counted: true, RecordSize: UltimateBTRecordSize}
	viaAdapter := ultimateBTSession(t, strict, UltimateBTAdapterPID)
	if err := viaAdapter.U2WriteRecordRange(ctx, record, 0x14, 4); err == nil || strict.BadCRCs != 1 {
		t.Fatalf("a write without a crc was taken: err=%v badCRCs=%d", err, strict.BadCRCs)
	}
}

func TestCountedFramesAreForTheUltimateBTAlone(t *testing.T) {
	ctx := context.Background()
	// A controller that takes the direct wrapper does not answer a counted
	// request, nor the other way round.
	direct := &U2Simulator{RecordSize: UltimateBTRecordSize}
	if _, err := ultimateBTSession(t, direct, UltimateBTPID).U2ReadRecord(ctx, 0x14); err == nil {
		t.Fatal("a direct-framed controller answered a counted request")
	}
	counted := &U2Simulator{Counted: true}
	if _, err := u2Session(t, counted).U2ReadRecord(ctx, 0x14); err == nil {
		t.Fatal("a counted-framed controller answered a direct request")
	}

	// Every other product keeps the direct wrapper, and carries the same
	// filler and crc inside it.
	for _, pid := range []uint16{0x6012, 0x6013, 0x600f, 0x6011, 0x6009, 0x600b, 0x600c, ArcadeProPID} {
		if u2FramingFor(pid) != u2FramingDirect {
			t.Errorf("%#04x must keep the direct wrapper", pid)
		}
	}
	pad := &U2Simulator{}
	session := u2Session(t, pad)
	if _, err := session.U2ReadRecord(ctx, 0x14); err != nil {
		t.Fatal(err)
	}
	filler := bytes.Repeat([]byte{0xcc}, 0x14)
	read := pad.Frames[len(pad.Frames)-1]
	if want, _ := hex.DecodeString("810402000000140000001400000000000000"); !bytes.Equal(append(append([]byte{}, read[:8]...), read[10:18]...), append(append([]byte{}, want[:8]...), want[10:18]...)) ||
		binary.LittleEndian.Uint16(read[8:]) != u2CRC(filler) || !bytes.Equal(read[18:18+0x14], filler) || read[18+0x14] != 0 {
		t.Fatalf("an Ultimate 2's read request = % x", read[:40])
	}
	if err := session.U2SelectPlatform(ctx, U2PlatformXInput); err != nil {
		t.Fatal(err)
	}
	// Nothing carried: the crc of no bytes.
	if want, _ := hex.DecodeString("8104140003000000ffff"); !bytes.Equal(pad.Frames[len(pad.Frames)-1][:10], want) {
		t.Fatalf("an Ultimate 2's platform select = % x", pad.Frames[len(pad.Frames)-1][:20])
	}
}

func TestUltimateBTIsGrantedItsRecordAndNothingElseNew(t *testing.T) {
	ctx := context.Background()
	for _, pid := range []uint16{UltimateBTPID, UltimateBTAdapterPID} {
		row, ok := FindPID(pid)
		if !ok || row.SupportTier != TierFull {
			t.Fatalf("%#04x: tier %q in the shipped registry", pid, row.SupportTier)
		}
		// What it was granted before, plus the profile: the standard mode,
		// profile, boot and firmware paths are as they were.
		want := FullCapability()
		want.SupportsJP108DedicatedMap, want.SupportsU2ButtonMap = false, false
		want.SupportsRecordKeyboard, want.SupportsMouse = false, false
		want.SupportsU2SlotConfig = true
		if got := DeviceProfileFor(VidPid{VID: 0x2dc8, PID: pid}).Capability; got != want {
			t.Fatalf("%#04x capability %+v", pid, got)
		}

		pad := &U2Simulator{Counted: true, RecordSize: UltimateBTRecordSize}
		session := ultimateBTSession(t, pad, pid)
		if session.usesU2FirmwarePath() || session.firmwareChunkCommand() != CommandFirmwareChunk ||
			session.firmwareCommitCommand() != CommandFirmwareCommit {
			t.Fatalf("%#04x: firmware path changed to %s", pid, session.firmwareChunkCommand())
		}

		// The profile capability adds no diagnostic check: the plan is what
		// it is without it.
		with := session.diagCommandsToRun()
		session.profile.Capability.SupportsU2SlotConfig = false
		without := session.diagCommandsToRun()
		session.profile.Capability.SupportsU2SlotConfig = true
		if !reflect.DeepEqual(with, without) || len(with) == 0 {
			t.Fatalf("%#04x: diagnostic plan changed\n with %v\n without %v", pid, with, without)
		}
		for _, plan := range with {
			if _, record := u2CommandCode(plan.command); record || plan.command == CommandU2SetReportState {
				t.Fatalf("%#04x: %s is run as a diagnostic check", pid, plan.command)
			}
		}

		// Record commands the vendor's software never sends this
		// controller are refused, and nothing is written.
		if _, err := session.U2Connected(ctx); err == nil {
			t.Errorf("%#04x: the connection query must be refused", pid)
		}
		if _, err := session.U2PhysicalPlatform(ctx); err == nil {
			t.Errorf("%#04x: the mode switch query must be refused", pid)
		}
		if _, err := session.U2LightEffect(ctx); err == nil {
			t.Errorf("%#04x: the light query must be refused", pid)
		}
		if err := session.U2SetLightEffect(ctx, U2LightFire); err == nil {
			t.Errorf("%#04x: setting a light effect must be refused", pid)
		}
		if _, err := session.ArcadePlatform(ctx); err == nil {
			t.Errorf("%#04x: the arcade mode query must be refused", pid)
		}
		if err := session.ArcadeProSetSync(ctx, true); err == nil {
			t.Errorf("%#04x: the arcade session mark must be refused", pid)
		}
		if len(pad.Frames) != 0 {
			t.Fatalf("%#04x: %d frames were sent for refused commands", pid, len(pad.Frames))
		}
		// The ones it does take need no experimental switch: it is listed
		// on their rows and its tier is full.
		plain := openSession(t, pad, pid, fastRetryConfig())
		if err := plain.U2SelectPlatform(ctx, U2PlatformSwitch); err != nil {
			t.Fatalf("%#04x: %v", pid, err)
		}
	}
}
