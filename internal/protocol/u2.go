package protocol

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"
)

// An Ultimate 2 (0x6012, and its receiver 0x6013) is configured with 64-byte
// reports on its vendor interface. Every request and reply carries the same
// 16-byte header:
//
//	request  81 04 | cmd u16 | arg u16 | len u16 | crc u16 | total u32 | offset u32 | data (up to 45 bytes)
//	reply    02 04 | 04 00   | cmd u16 | len u16 | crc u16 | ...                    | data from byte 18
//
// crc is CRC-16/MODBUS of the data. The controller's whole configuration is
// one 1592-byte record: it is read in chunks with cmd 2, written in chunks
// with cmd 1 (any byte range), and a write takes effect when committed with
// cmd 6. Everything here follows
// docs/clean-room-evidence/dossiers/6012/u2_adv.toml.

const (
	u2CmdWrite          uint16 = 0x0001
	u2CmdRead           uint16 = 0x0002
	u2CmdCommit         uint16 = 0x0006
	u2CmdReportState    uint16 = 0x0007
	u2CmdSelectPlatform uint16 = 0x0014
	u2CmdPhysicalMode   uint16 = 0x0105
	u2CmdConnected      uint16 = 0x0120

	// u2CommitArg is the argument a commit carries.
	u2CommitArg uint16 = 0x0123

	// u2MaxChunk is how many data bytes one report carries.
	u2MaxChunk = 45
	// u2DataOffset is where a reply's data starts.
	u2DataOffset = 18

	// U2RecordSize is the size of an Ultimate 2's configuration record.
	U2RecordSize = 0x638

	// u2CommitTimeout is how long a commit may take: the controller writes
	// its flash before it answers.
	u2CommitTimeout = 4 * time.Second
)

// Platform banks. A controller keeps a separate configuration record per
// platform mode; the bank is selected before the record is read or written.
const (
	U2PlatformSwitch byte = 0
	U2PlatformDInput byte = 1
	U2PlatformMac    byte = 2
	U2PlatformXInput byte = 3
)

// u2CRC is CRC-16/MODBUS: polynomial 0xA001 (reflected), initial value 0xFFFF.
func u2CRC(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		for bit := 0; bit < 8; bit++ {
			if (crc^uint16(b))&1 != 0 {
				crc = crc>>1 ^ 0xa001
			} else {
				crc >>= 1
			}
			b >>= 1
		}
	}
	return crc
}

// u2Frame fills in a request from its row template, which already holds the
// report id, the 04 marker and the command.
func u2Frame(template []byte, arg uint16, data []byte, length int, total, offset uint32) []byte {
	frame := append([]byte(nil), template...)
	binary.LittleEndian.PutUint16(frame[4:], arg)
	binary.LittleEndian.PutUint16(frame[6:], uint16(length))
	if len(data) > 0 {
		binary.LittleEndian.PutUint16(frame[8:], u2CRC(data))
	}
	binary.LittleEndian.PutUint32(frame[10:], total)
	binary.LittleEndian.PutUint32(frame[14:], offset)
	copy(frame[u2DataOffset:], data)
	return frame
}

// u2ReplyFor reports whether response is a reply to cmd, and returns its
// declared length and data.
func u2ReplyFor(response []byte, cmd uint16) (length int, data []byte, ok bool) {
	if len(response) < u2DataOffset || response[0] != 0x02 || response[1] != 0x04 {
		return 0, nil, false
	}
	if binary.LittleEndian.Uint16(response[2:]) != 4 || binary.LittleEndian.Uint16(response[4:]) != cmd {
		return 0, nil, false
	}
	length = int(binary.LittleEndian.Uint16(response[6:]))
	data = response[u2DataOffset:]
	if length < len(data) {
		data = data[:length]
	}
	return length, data, true
}

// u2CommandCode maps a command to the protocol's command number.
func u2CommandCode(command CommandID) (uint16, bool) {
	switch command {
	case CommandU2RecordWrite:
		return u2CmdWrite, true
	case CommandU2RecordRead:
		return u2CmdRead, true
	case CommandU2Commit:
		return u2CmdCommit, true
	case CommandU2SelectPlatform:
		return u2CmdSelectPlatform, true
	case CommandU2GetPhysicalMode:
		return u2CmdPhysicalMode, true
	case CommandU2GetConnected:
		return u2CmdConnected, true
	}
	return 0, false
}

// U2Connected asks a receiver whether its controller is connected to it. A
// receiver stays plugged in and answers while the controller is off.
func (s *DeviceSession) U2Connected(ctx context.Context) (bool, error) {
	resp, err := s.SendCommand(ctx, CommandU2GetConnected, nil)
	if err != nil {
		return false, err
	}
	_, data, _ := u2ReplyFor(resp.Raw, u2CmdConnected)
	return len(data) > 0 && data[0] == 1, nil
}

// U2PhysicalPlatform reads which platform bank the controller's mode switch
// selects: XInput or DInput.
func (s *DeviceSession) U2PhysicalPlatform(ctx context.Context) (byte, error) {
	resp, err := s.SendCommand(ctx, CommandU2GetPhysicalMode, nil)
	if err != nil {
		return 0, err
	}
	if len(resp.Raw) > u2DataOffset && resp.Raw[u2DataOffset] == 1 {
		return U2PlatformXInput, nil
	}
	return U2PlatformDInput, nil
}

// U2SetInputReports pauses (false) or resumes (true) the controller's
// stream of input reports on the configuration interface. It is paused
// while configuring so replies are not interleaved with input. The
// controller sends no reply to this.
func (s *DeviceSession) U2SetInputReports(ctx context.Context, on bool) error {
	row, err := s.ensureCommandAllowed(CommandU2SetReportState)
	if err != nil {
		return err
	}
	var arg uint16
	if on {
		arg = 1
	}
	_, err = s.sendRow(ctx, row, u2Frame(row.Request, arg, nil, 0, 0, 0))
	return err
}

// U2SelectPlatform selects which platform's configuration record the read
// and write commands address.
func (s *DeviceSession) U2SelectPlatform(ctx context.Context, platform byte) error {
	row, err := s.ensureCommandAllowed(CommandU2SelectPlatform)
	if err != nil {
		return err
	}
	_, err = s.sendRow(ctx, row, u2Frame(row.Request, uint16(platform), nil, 0, 0, 0))
	return err
}

// U2ReadRecord reads the first size bytes of the selected platform's
// configuration record; U2RecordSize reads all of it.
func (s *DeviceSession) U2ReadRecord(ctx context.Context, size int) ([]byte, error) {
	row, err := s.ensureCommandAllowed(CommandU2RecordRead)
	if err != nil {
		return nil, err
	}
	if size < 1 || size > U2RecordSize {
		return nil, errInvalidInput("record read of %d bytes is outside 1-%d", size, U2RecordSize)
	}
	record := make([]byte, 0, size)
	for len(record) < size {
		want := min(u2MaxChunk, size-len(record))
		resp, err := s.sendRow(ctx, row, u2Frame(row.Request, 0, nil, want, uint32(size), uint32(len(record))))
		if err != nil {
			return nil, err
		}
		_, data, _ := u2ReplyFor(resp.Raw, u2CmdRead)
		if len(data) == 0 {
			return nil, errInvalidResponse(row.ID, fmt.Sprintf("empty chunk at offset %d of %d", len(record), size))
		}
		if len(data) > size-len(record) {
			data = data[:size-len(record)]
		}
		record = append(record, data...)
	}
	return record, nil
}

// U2WriteRecordRange writes record[offset:offset+length] to the selected
// platform's configuration record. Nothing takes effect until U2Commit.
func (s *DeviceSession) U2WriteRecordRange(ctx context.Context, record []byte, offset, length int) error {
	row, err := s.ensureCommandAllowed(CommandU2RecordWrite)
	if err != nil {
		return err
	}
	if len(record) != U2RecordSize {
		return errInvalidInput("a configuration record is %d bytes, got %d", U2RecordSize, len(record))
	}
	if offset < 0 || length < 1 || offset+length > U2RecordSize {
		return errInvalidInput("range %d+%d is outside the record", offset, length)
	}
	for end := offset + length; offset < end; {
		chunk := record[offset:min(end, offset+u2MaxChunk)]
		resp, err := s.sendRow(ctx, row, u2Frame(row.Request, 0, chunk, len(chunk), U2RecordSize, uint32(offset)))
		if err != nil {
			return err
		}
		accepted, _, _ := u2ReplyFor(resp.Raw, u2CmdWrite)
		if accepted != len(chunk) {
			// A partly accepted chunk would leave the caller's idea of
			// the record and the controller's out of step.
			return errInvalidResponse(row.ID, fmt.Sprintf("controller accepted %d of %d bytes at offset %d", accepted, len(chunk), offset))
		}
		offset += accepted
	}
	return nil
}

// U2Commit makes the writes since the last commit take effect and persist.
func (s *DeviceSession) U2Commit(ctx context.Context) error {
	row, err := s.ensureCommandAllowed(CommandU2Commit)
	if err != nil {
		return err
	}
	_, err = s.sendRow(ctx, row, u2Frame(row.Request, u2CommitArg, nil, 0, 0, 0))
	return err
}
