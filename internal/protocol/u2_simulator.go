package protocol

import (
	"context"
	"encoding/binary"
)

// U2Simulator is a Transport that behaves like an Ultimate 2 behind its
// receiver, for tests and mock mode: it keeps one configuration record per
// platform and answers each request the way
// docs/clean-room-evidence/dossiers/6012/u2_adv.toml describes. Writes land
// in a staging copy and only reach the stored record on commit, so a test
// can tell a committed write from an abandoned one.
type U2Simulator struct {
	// Off makes the receiver answer that no controller is connected, and
	// everything else go unanswered, as with the controller switched off.
	Off bool
	// Physical is the platform the mode switch selects.
	Physical byte
	// Records holds the stored record per platform, created zeroed on
	// first use.
	Records map[byte][]byte
	// RejectCRC makes the controller ignore a write whose crc is wrong
	// (always true of a real one; exposed so tests can assert the crc).
	BadCRCs int
	// ShortAccept makes every write chunk be acknowledged as one byte
	// short, as a controller refusing part of a write would.
	ShortAccept bool
	// DropCommit makes commits go unanswered.
	DropCommit bool
	// Frames records every frame written, for assertions.
	Frames [][]byte
	// InputReports reports whether the input stream is on.
	InputReports bool
	// Commits counts commits answered.
	Commits int

	platform byte
	staged   []byte
	opened   bool
	pending  [][]byte
}

func (u *U2Simulator) Open(context.Context, VidPid) error {
	u.opened, u.InputReports, u.platform = true, true, u.Physical
	return nil
}
func (u *U2Simulator) Close() error { u.opened = false; return nil }

// Record returns the stored record for a platform.
func (u *U2Simulator) Record(platform byte) []byte {
	if u.Records == nil {
		u.Records = map[byte][]byte{}
	}
	if u.Records[platform] == nil {
		record := make([]byte, U2RecordSize)
		binary.LittleEndian.PutUint16(record[0x10:], uint16(platform))
		u.Records[platform] = record
	}
	return u.Records[platform]
}

func (u *U2Simulator) reply(cmd uint16, length int, data []byte) {
	frame := make([]byte, 64)
	frame[0], frame[1] = 0x02, 0x04
	binary.LittleEndian.PutUint16(frame[2:], 4)
	binary.LittleEndian.PutUint16(frame[4:], cmd)
	binary.LittleEndian.PutUint16(frame[6:], uint16(length))
	copy(frame[u2DataOffset:], data)
	u.pending = append(u.pending, frame)
}

func (u *U2Simulator) Write(data []byte) (int, error) {
	if !u.opened {
		return 0, errTransport("simulator not open")
	}
	u.Frames = append(u.Frames, append([]byte(nil), data...))
	if len(data) != 64 || data[0] != 0x81 || data[1] != 0x04 {
		return len(data), nil // not this protocol: no answer
	}
	cmd := binary.LittleEndian.Uint16(data[2:])
	arg := binary.LittleEndian.Uint16(data[4:])
	length := int(binary.LittleEndian.Uint16(data[6:]))
	crc := binary.LittleEndian.Uint16(data[8:])
	offset := int(binary.LittleEndian.Uint32(data[14:]))

	if cmd == u2CmdConnected {
		connected := byte(1)
		if u.Off {
			connected = 0
		}
		u.reply(cmd, 1, []byte{connected})
		return len(data), nil
	}
	if u.Off {
		return len(data), nil
	}
	switch cmd {
	case u2CmdPhysicalMode:
		mode := byte(0)
		if u.Physical == U2PlatformXInput {
			mode = 1
		}
		u.reply(cmd, 1, []byte{mode})
	case u2CmdReportState:
		u.InputReports = arg == 1
	case u2CmdSelectPlatform:
		u.platform, u.staged = byte(arg), nil
		u.reply(cmd, 0, nil)
	case u2CmdRead:
		record := u.Record(u.platform)
		if offset >= len(record) || length < 1 {
			u.reply(cmd, 0, nil)
			break
		}
		end := min(len(record), offset+min(length, u2MaxChunk))
		u.reply(cmd, end-offset, record[offset:end])
	case u2CmdWrite:
		if length < 1 || length > u2MaxChunk || offset+length > U2RecordSize {
			break
		}
		chunk := data[u2DataOffset : u2DataOffset+length]
		if u2CRC(chunk) != crc {
			u.BadCRCs++
			break
		}
		if u.staged == nil {
			u.staged = append([]byte(nil), u.Record(u.platform)...)
		}
		if u.ShortAccept {
			length--
		}
		copy(u.staged[offset:], chunk[:length])
		u.reply(cmd, length, nil)
	case u2CmdCommit:
		if arg != u2CommitArg || u.DropCommit {
			break
		}
		if u.staged != nil {
			u.Records[u.platform], u.staged = u.staged, nil
		}
		u.Commits++
		u.reply(cmd, 0, nil)
	}
	return len(data), nil
}

func (u *U2Simulator) Read(context.Context, int, uint64) ([]byte, error) {
	if len(u.pending) == 0 {
		return nil, ErrTimeout
	}
	frame := u.pending[0]
	u.pending = u.pending[1:]
	return frame, nil
}
