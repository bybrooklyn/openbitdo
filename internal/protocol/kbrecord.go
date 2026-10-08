package protocol

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"
)

// A Retro 87 (Xbox edition, 0x2028; its UK sibling 0x3026), a Retro 68
// (0x203a) and the Riviera keyboard (0x205a) keep their whole profile in
// one 1532-byte record and are configured with 64-byte reports on their
// vendor interface. Requests and replies carry an 8-byte header:
//
//	request  81 04 | cmd | sub | len | sum | offset u32 | data (up to 53 bytes)
//	reply    02 04 | 03  | cmd | len | five more bytes  | data from byte 10
//
// sum is the low byte of the sum of the data bytes. A reply whose third
// byte is c0 instead of 03 is the keyboard refusing the request. The record
// is read with cmd 2 and written with cmd 1, any byte range at a time; a
// write is stored as it is acknowledged, there is no commit. Recorded
// macros and the per-key light colours live outside the record, each with
// read, prepare and write commands of its own. Everything here follows
// docs/clean-room-evidence/dossiers/2028/keyboard_record.toml; none of it
// has been exchanged with a real keyboard.

const (
	kbRecordCmdWrite       byte = 0x01
	kbRecordCmdRead        byte = 0x02
	kbRecordCmdMacroErase  byte = 0x05
	kbRecordCmdMacroWrite  byte = 0x06
	kbRecordCmdMacroRead   byte = 0x07
	kbRecordCmdReportMode  byte = 0x08
	kbRecordCmdLightsBegin byte = 0x0d
	kbRecordCmdLightsWrite byte = 0x0e
	kbRecordCmdLightsRead  byte = 0x0f

	// kbRecordReplyOK is the third byte of a reply that answers a request.
	kbRecordReplyOK byte = 0x03

	// kbRecordMaxChunk is how many data bytes one report carries.
	kbRecordMaxChunk = 53
	// kbRecordDataOffset is where a request's and a reply's data start.
	kbRecordDataOffset = 10

	// KbRecordSize is the size of the configuration record, the same on
	// all four keyboards.
	KbRecordSize = 0x5fc

	// KbRecordMacroAreaMax is the largest macro storage any of the
	// keyboards has: ten slots of 4096 bytes. An offset is sent as sixteen
	// bits, which this just fits.
	KbRecordMacroAreaMax = 10 * 4096
	// KbRecordLightsMax is the largest per-key colour block: nine bytes of
	// settings, three bytes for each of 92 lights, and a closing byte.
	KbRecordLightsMax = 286

	// The vendor application waits this long before preparing a macro slot
	// or the colour block, and before each report of macro or colour data.
	kbRecordPrepareWait = 10 * time.Millisecond
	kbRecordStoreWait   = 100 * time.Millisecond
)

// kbRecordFrame fills in a request from its row template, which already
// holds the report id, the 04 marker and the command.
func kbRecordFrame(template []byte, sub byte, data []byte, length, offset int) []byte {
	frame := append([]byte(nil), template...)
	frame[3] = sub
	frame[4] = byte(length)
	var sum byte
	for _, b := range data {
		sum += b
	}
	frame[5] = sum
	binary.LittleEndian.PutUint32(frame[6:], uint32(offset))
	copy(frame[kbRecordDataOffset:], data)
	return frame
}

// kbRecordReplyFor reports whether response answers cmd, and returns its
// declared length and data. A read's reply is matched on the low four bits
// of the command, an acknowledgement on the whole byte, as the vendor
// application matches them.
func kbRecordReplyFor(response []byte, cmd byte) (length int, data []byte, ok bool) {
	if len(response) < kbRecordDataOffset || response[0] != 0x02 || response[1] != 0x04 || response[2] != kbRecordReplyOK {
		return 0, nil, false
	}
	switch cmd {
	case kbRecordCmdRead, kbRecordCmdMacroRead, kbRecordCmdLightsRead:
		if response[3]&0x0f != cmd&0x0f {
			return 0, nil, false
		}
	default:
		if response[3] != cmd {
			return 0, nil, false
		}
	}
	length = int(response[4])
	data = response[kbRecordDataOffset:]
	if length < len(data) {
		data = data[:length]
	}
	return length, data, true
}

// kbRecordCommandCode maps a command to the protocol's command number.
func kbRecordCommandCode(command CommandID) (byte, bool) {
	switch command {
	case CommandKbRecordWrite:
		return kbRecordCmdWrite, true
	case CommandKbRecordRead:
		return kbRecordCmdRead, true
	case CommandKbRecordMacroErase:
		return kbRecordCmdMacroErase, true
	case CommandKbRecordMacroWrite:
		return kbRecordCmdMacroWrite, true
	case CommandKbRecordMacroRead:
		return kbRecordCmdMacroRead, true
	case CommandKbRecordLightsBegin:
		return kbRecordCmdLightsBegin, true
	case CommandKbRecordLightsWrite:
		return kbRecordCmdLightsWrite, true
	case CommandKbRecordLightsRead:
		return kbRecordCmdLightsRead, true
	}
	return 0, false
}

// unpacedTransport is implemented by a transport that stands in for a
// keyboard and needs none of the waits a real one is given.
type unpacedTransport interface {
	Unpaced() bool
}

// kbRecordWait waits as the vendor application does before a request the
// keyboard stores to flash.
func (s *DeviceSession) kbRecordWait(ctx context.Context, d time.Duration) error {
	if t, ok := s.transport.(unpacedTransport); ok && t.Unpaced() {
		return nil
	}
	return ctxSleep(ctx, d)
}

// KbRecordSetKeyReports turns the keyboard's live key reports on its
// configuration interface on or off. They are off while configuring so
// replies are not interleaved with key presses. The keyboard sends no
// reply to this.
func (s *DeviceSession) KbRecordSetKeyReports(ctx context.Context, on bool) error {
	row, err := s.ensureCommandAllowed(CommandKbRecordSetReportMode)
	if err != nil {
		return err
	}
	var sub byte
	if on {
		sub = 1
	}
	_, err = s.sendRow(ctx, row, kbRecordFrame(row.Request, sub, nil, 0, 0))
	return err
}

// kbRecordReadChunks reads length bytes at offset with a read-class
// command, a report's worth at a time.
func (s *DeviceSession) kbRecordReadChunks(ctx context.Context, command CommandID, offset, length int) ([]byte, error) {
	row, err := s.ensureCommandAllowed(command)
	if err != nil {
		return nil, err
	}
	cmd, _ := kbRecordCommandCode(command)
	out := make([]byte, 0, length)
	for len(out) < length {
		want := min(kbRecordMaxChunk, length-len(out))
		resp, err := s.sendRow(ctx, row, kbRecordFrame(row.Request, 0, nil, want, offset+len(out)))
		if err != nil {
			return nil, err
		}
		_, data, _ := kbRecordReplyFor(resp.Raw, cmd)
		if len(data) == 0 {
			return nil, errInvalidResponse(row.ID, fmt.Sprintf("empty chunk at offset %d", offset+len(out)))
		}
		out = append(out, data[:min(len(data), length-len(out))]...)
	}
	return out, nil
}

// kbRecordWriteChunks writes data at offset with a write-class command, a
// report's worth at a time, waiting pause before each report.
func (s *DeviceSession) kbRecordWriteChunks(ctx context.Context, command CommandID, offset int, data []byte, pause time.Duration) error {
	row, err := s.ensureCommandAllowed(command)
	if err != nil {
		return err
	}
	cmd, _ := kbRecordCommandCode(command)
	for sent := 0; sent < len(data); {
		if pause > 0 {
			if err := s.kbRecordWait(ctx, pause); err != nil {
				return err
			}
		}
		chunk := data[sent:min(len(data), sent+kbRecordMaxChunk)]
		resp, err := s.sendRow(ctx, row, kbRecordFrame(row.Request, 0, chunk, len(chunk), offset+sent))
		if err != nil {
			return err
		}
		accepted, _, _ := kbRecordReplyFor(resp.Raw, cmd)
		if accepted != len(chunk) {
			// A partly accepted chunk would leave the caller's idea of
			// what is stored and the keyboard's out of step.
			return errInvalidResponse(row.ID, fmt.Sprintf("keyboard accepted %d of %d bytes at offset %d", accepted, len(chunk), offset+sent))
		}
		sent += accepted
	}
	return nil
}

// kbRecordPrepare sends a command that carries only an offset and waits for
// its acknowledgement.
func (s *DeviceSession) kbRecordPrepare(ctx context.Context, command CommandID, offset int) error {
	row, err := s.ensureCommandAllowed(command)
	if err != nil {
		return err
	}
	if err := s.kbRecordWait(ctx, kbRecordPrepareWait); err != nil {
		return err
	}
	_, err = s.sendRow(ctx, row, kbRecordFrame(row.Request, 0, nil, 0, offset))
	return err
}

// KbRecordRead reads length bytes of the configuration record at offset;
// 0 and KbRecordSize read all of it.
func (s *DeviceSession) KbRecordRead(ctx context.Context, offset, length int) ([]byte, error) {
	if offset < 0 || length < 1 || offset+length > KbRecordSize {
		return nil, errInvalidInput("record range %d+%d is outside the %d-byte record", offset, length, KbRecordSize)
	}
	return s.kbRecordReadChunks(ctx, CommandKbRecordRead, offset, length)
}

// KbRecordWriteRange writes record[offset:offset+length] to the
// configuration record. The keyboard stores each report as it acknowledges
// it.
func (s *DeviceSession) KbRecordWriteRange(ctx context.Context, record []byte, offset, length int) error {
	if len(record) != KbRecordSize {
		return errInvalidInput("%d bytes is not the size of the configuration record", len(record))
	}
	if offset < 0 || length < 1 || offset+length > len(record) {
		return errInvalidInput("range %d+%d is outside the record", offset, length)
	}
	return s.kbRecordWriteChunks(ctx, CommandKbRecordWrite, offset, record[offset:offset+length], 0)
}

func kbRecordMacroRangeOK(offset, length int) bool {
	return offset >= 0 && length >= 1 && offset+length <= KbRecordMacroAreaMax
}

// KbRecordReadMacroData reads length bytes of macro storage at offset.
func (s *DeviceSession) KbRecordReadMacroData(ctx context.Context, offset, length int) ([]byte, error) {
	if !kbRecordMacroRangeOK(offset, length) {
		return nil, errInvalidInput("macro range %d+%d is outside the keyboard's macro storage", offset, length)
	}
	return s.kbRecordReadChunks(ctx, CommandKbRecordMacroRead, offset, length)
}

// KbRecordPrepareMacro readies the macro slot that starts at offset to be
// written. The vendor application sends it before every macro it writes.
func (s *DeviceSession) KbRecordPrepareMacro(ctx context.Context, offset int) error {
	if !kbRecordMacroRangeOK(offset, 1) {
		return errInvalidInput("macro offset %d is outside the keyboard's macro storage", offset)
	}
	return s.kbRecordPrepare(ctx, CommandKbRecordMacroErase, offset)
}

// KbRecordWriteMacroData writes data into macro storage at offset. The
// slot it falls in is prepared first with KbRecordPrepareMacro.
func (s *DeviceSession) KbRecordWriteMacroData(ctx context.Context, offset int, data []byte) error {
	if !kbRecordMacroRangeOK(offset, len(data)) {
		return errInvalidInput("macro range %d+%d is outside the keyboard's macro storage", offset, len(data))
	}
	return s.kbRecordWriteChunks(ctx, CommandKbRecordMacroWrite, offset, data, kbRecordStoreWait)
}

// KbRecordReadLights reads the per-key colour block, size bytes long.
func (s *DeviceSession) KbRecordReadLights(ctx context.Context, size int) ([]byte, error) {
	if size < 1 || size > KbRecordLightsMax {
		return nil, errInvalidInput("a colour block of %d bytes is outside 1-%d", size, KbRecordLightsMax)
	}
	return s.kbRecordReadChunks(ctx, CommandKbRecordLightsRead, 0, size)
}

// KbRecordWriteLights replaces the per-key colour block: the block is
// prepared, then written whole.
func (s *DeviceSession) KbRecordWriteLights(ctx context.Context, block []byte) error {
	if len(block) < 1 || len(block) > KbRecordLightsMax {
		return errInvalidInput("a colour block of %d bytes is outside 1-%d", len(block), KbRecordLightsMax)
	}
	if err := s.kbRecordPrepare(ctx, CommandKbRecordLightsBegin, 0); err != nil {
		return err
	}
	return s.kbRecordWriteChunks(ctx, CommandKbRecordLightsWrite, 0, block, kbRecordStoreWait)
}
