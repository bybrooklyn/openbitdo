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
	// RecordSize is the size of the configuration record; zero means an
	// Ultimate 2's.
	RecordSize int
	// ReportsPID is the product id the device gives when asked what it is
	// (the report-revision query); zero leaves that query unanswered.
	ReportsPID uint16
	// Physical is the platform the mode switch selects.
	Physical byte
	// PlatformOffset is where a record keeps its platform; zero means
	// 0x10, where a three-slot record has it.
	PlatformOffset int
	// ArcadeMode, when not zero, is the byte the arcade mode query answers
	// with in place of the one Physical implies.
	ArcadeMode byte
	// Sync is whether an Arcade Controller Pro's configuration session is
	// marked as started; SwitchReport is the last state its report switch
	// was sent.
	Sync         bool
	SwitchReport uint16
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
	// Light is the stick-ring light effect in use.
	Light byte
	// Macros holds macro storage by platform<<8 | slot, erased to 0xff.
	Macros map[uint16][]byte
	// macroStaged holds macro writes until the next commit.
	macroStaged map[uint16][]byte

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
		size := u.RecordSize
		if size == 0 {
			size = U2RecordSize
		}
		record := make([]byte, size)
		at := u.PlatformOffset
		if at == 0 {
			at = 0x10
		}
		binary.LittleEndian.PutUint16(record[at:], uint16(platform))
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
	// Every request starts 81 04, except the arcade mode query: 81 00 52.
	arcadeQuery := len(data) == 64 && data[1] == 0 && data[2] == 0x52
	if len(data) != 64 || data[0] != 0x81 || (data[1] != 0x04 && !arcadeQuery) {
		return len(data), nil // not this protocol: no answer
	}
	cmd := binary.LittleEndian.Uint16(data[2:])
	arg := binary.LittleEndian.Uint16(data[4:])
	length := int(binary.LittleEndian.Uint16(data[6:]))
	crc := binary.LittleEndian.Uint16(data[8:])
	offset := int(binary.LittleEndian.Uint32(data[14:]))

	if cmd == 0x0100 && u.ReportsPID != 0 { // report revision: the product id rides in the header
		frame := make([]byte, 64)
		copy(frame, []byte{0x02, 0x04, 0x04, 0x00, 0x00, 0x01})
		frame[22], frame[23] = byte(u.ReportsPID), byte(u.ReportsPID>>8)
		u.pending = append(u.pending, frame)
		return len(data), nil
	}
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
	case u2CmdMacroRead:
		area := u.MacroArea(arg)
		if offset >= len(area) || length < 1 {
			u.reply(cmd, 0, nil)
			break
		}
		end := min(len(area), offset+min(length, u2MacroChunk))
		u.reply(cmd, end-offset, area[offset:end])
	case u2CmdMacroErase:
		area := u.MacroArea(arg)
		for i := offset; i < min(len(area), offset+length); i++ {
			area[i] = 0xff
		}
		u.reply(cmd, 0, nil)
	case u2CmdMacroWrite:
		if length < 1 || length > u2MacroChunk || offset+length > U2MacroRegion*U2MacrosPerSlot {
			break
		}
		chunk := data[u2DataOffset : u2DataOffset+length]
		if u2CRC(chunk) != crc {
			u.BadCRCs++
			break
		}
		if u.macroStaged == nil {
			u.macroStaged = map[uint16][]byte{}
		}
		if u.macroStaged[arg] == nil {
			u.macroStaged[arg] = append([]byte(nil), u.MacroArea(arg)...)
		}
		if u.ShortAccept {
			length--
		}
		// Flash can only clear bits: writing over a region that was not
		// erased first leaves garbage, as on the real thing.
		for i, b := range chunk[:length] {
			u.macroStaged[arg][offset+i] &= b
		}
		u.reply(cmd, length, nil)
	case u2CmdArcadeMode:
		mode := byte(0)
		if u.Physical == U2PlatformXInput {
			mode = 2
		}
		if u.ArcadeMode != 0 {
			mode = u.ArcadeMode
		}
		u.reply(cmd, 1, []byte{mode})
	case u2CmdArcadeProSync:
		u.Sync = arg != 0
		u.reply(cmd, 0, nil)
	case u2CmdArcadeProInput:
		u.SwitchReport = arg
		u.reply(cmd, 0, nil)
	case u2CmdGetLight:
		u.reply(cmd, 1, []byte{u.Light})
	case u2CmdSetLight:
		u.Light = byte(arg)
		u.reply(cmd, 0, nil)
	case u2CmdReportState:
		u.InputReports = arg != 0
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
		if length < 1 || length > u2MaxChunk || offset+length > len(u.Record(u.platform)) {
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
		for key, area := range u.macroStaged {
			u.Macros[key] = area
		}
		u.macroStaged = nil
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

// MacroArea returns the stored macro storage for platform<<8 | slot.
func (u *U2Simulator) MacroArea(key uint16) []byte {
	if u.Macros == nil {
		u.Macros = map[uint16][]byte{}
	}
	if u.Macros[key] == nil {
		area := make([]byte, U2MacroRegion*U2MacrosPerSlot)
		for i := range area {
			area[i] = 0xff
		}
		u.Macros[key] = area
	}
	return u.Macros[key]
}
