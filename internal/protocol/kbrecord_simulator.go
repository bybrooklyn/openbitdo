package protocol

import (
	"context"
	"encoding/binary"
)

// KbRecordSimulator is a Transport that behaves like a record keyboard, for
// tests: it keeps the configuration record, the macro storage and the
// per-key colour block, and answers each request the way
// docs/clean-room-evidence/dossiers/2028/keyboard_record.toml describes.
// A write is stored as it is acknowledged; there is nothing to commit.
//
// What a real keyboard does when a macro slot or the colour block is
// prepared is not known. Here preparing a macro slot erases it, so a test
// notices anything that was not written again, and a macro or colour write
// that was not prepared for is counted.
type KbRecordSimulator struct {
	// Off makes every request go unanswered, as with the keyboard switched
	// off behind its receiver.
	Off bool
	// MacroSlotSize and MacroSlots give the macro storage's shape; zero
	// means a Retro 87's ten slots of 4096 bytes.
	MacroSlotSize, MacroSlots int
	// LightsSize is the size of the per-key colour block; zero means a
	// Retro 87's 283 bytes.
	LightsSize int
	// IgnoreWrites makes every write be acknowledged in full and not
	// stored, as a keyboard that silently dropped it would.
	IgnoreWrites bool
	// ShortAccept makes every write chunk be acknowledged as one byte
	// short.
	ShortAccept bool
	// Refuse makes every request be answered with the refusal 02 04 c0.
	Refuse bool
	// BadSums counts writes whose checksum was wrong; they go unanswered.
	BadSums int
	// Unprepared counts macro and colour writes that did not follow the
	// prepare command for the slot or block they fall in.
	Unprepared int
	// KeyReports reports whether live key reports are on.
	KeyReports bool
	// Frames records every frame written, for assertions.
	Frames [][]byte

	record, macros, lights []byte
	// prepared is the command a prepare allows next, and for a macro the
	// slot it was for.
	prepared     byte
	preparedSlot int
	opened       bool
	pending      [][]byte
}

func (k *KbRecordSimulator) Open(context.Context, VidPid) error {
	k.opened, k.KeyReports = true, true
	return nil
}
func (k *KbRecordSimulator) Close() error { k.opened = false; return nil }

// Unpaced tells a session it need not wait between writes.
func (k *KbRecordSimulator) Unpaced() bool { return true }

// Record returns the stored configuration record, created zeroed on first
// use.
func (k *KbRecordSimulator) Record() []byte {
	if k.record == nil {
		k.record = make([]byte, KbRecordSize)
	}
	return k.record
}

func (k *KbRecordSimulator) macroSlotSize() int {
	if k.MacroSlotSize == 0 {
		return 4096
	}
	return k.MacroSlotSize
}

// Macros returns the stored macro storage, created erased (0xff) on first
// use.
func (k *KbRecordSimulator) Macros() []byte {
	if k.macros == nil {
		slots := k.MacroSlots
		if slots == 0 {
			slots = 10
		}
		k.macros = make([]byte, slots*k.macroSlotSize())
		for i := range k.macros {
			k.macros[i] = 0xff
		}
	}
	return k.macros
}

// Lights returns the stored per-key colour block, created zeroed on first
// use.
func (k *KbRecordSimulator) Lights() []byte {
	if k.lights == nil {
		size := k.LightsSize
		if size == 0 {
			size = 283
		}
		k.lights = make([]byte, size)
	}
	return k.lights
}

func (k *KbRecordSimulator) reply(status, cmd byte, length int, data []byte) {
	frame := make([]byte, 64)
	frame[0], frame[1], frame[2], frame[3], frame[4] = 0x02, 0x04, status, cmd, byte(length)
	copy(frame[kbRecordDataOffset:], data)
	k.pending = append(k.pending, frame)
}

func (k *KbRecordSimulator) Write(data []byte) (int, error) {
	if !k.opened {
		return 0, errTransport("simulator not open")
	}
	k.Frames = append(k.Frames, append([]byte(nil), data...))
	if len(data) != 64 || data[0] != 0x81 || data[1] != 0x04 || k.Off {
		return len(data), nil // not this protocol, or nobody there: no answer
	}
	cmd, sub, length, sum := data[2], data[3], int(data[4]), data[5]
	offset := int(binary.LittleEndian.Uint32(data[6:]))

	// A prepare holds only until something other than the write it was
	// for arrives.
	prepared, preparedSlot := k.prepared, k.preparedSlot
	if cmd != prepared {
		k.prepared = 0
	}
	if cmd == kbRecordCmdReportMode {
		k.KeyReports = sub != 0
		return len(data), nil
	}
	if k.Refuse {
		k.reply(0xc0, cmd, 0, nil)
		return len(data), nil
	}

	var area []byte
	switch cmd {
	case kbRecordCmdRead, kbRecordCmdWrite:
		area = k.Record()
	case kbRecordCmdMacroRead, kbRecordCmdMacroWrite, kbRecordCmdMacroErase:
		area = k.Macros()
	case kbRecordCmdLightsRead, kbRecordCmdLightsWrite, kbRecordCmdLightsBegin:
		area = k.Lights()
	default:
		return len(data), nil
	}

	switch cmd {
	case kbRecordCmdRead, kbRecordCmdMacroRead, kbRecordCmdLightsRead:
		if offset >= len(area) || length < 1 {
			k.reply(kbRecordReplyOK, cmd, 0, nil)
			break
		}
		end := min(len(area), offset+min(length, kbRecordMaxChunk))
		k.reply(kbRecordReplyOK, cmd, end-offset, area[offset:end])
	case kbRecordCmdMacroErase:
		if offset >= len(area) {
			break
		}
		slot := offset / k.macroSlotSize()
		if !k.IgnoreWrites {
			for i := slot * k.macroSlotSize(); i < (slot+1)*k.macroSlotSize(); i++ {
				area[i] = 0xff
			}
		}
		k.prepared, k.preparedSlot = kbRecordCmdMacroWrite, slot
		k.reply(kbRecordReplyOK, cmd, 0, nil)
	case kbRecordCmdLightsBegin:
		k.prepared = kbRecordCmdLightsWrite
		k.reply(kbRecordReplyOK, cmd, 0, nil)
	case kbRecordCmdWrite, kbRecordCmdMacroWrite, kbRecordCmdLightsWrite:
		if length < 1 || length > kbRecordMaxChunk || offset+length > len(area) {
			break
		}
		chunk := data[kbRecordDataOffset : kbRecordDataOffset+length]
		var want byte
		for _, b := range chunk {
			want += b
		}
		if want != sum {
			k.BadSums++
			break
		}
		if cmd != kbRecordCmdWrite && (prepared != cmd ||
			(cmd == kbRecordCmdMacroWrite && offset/k.macroSlotSize() != preparedSlot)) {
			k.Unprepared++
		}
		accepted := length
		if k.ShortAccept {
			accepted--
		}
		if !k.IgnoreWrites {
			copy(area[offset:], chunk[:accepted])
		}
		k.reply(kbRecordReplyOK, cmd, accepted, nil)
	}
	return len(data), nil
}

func (k *KbRecordSimulator) Read(context.Context, int, uint64) ([]byte, error) {
	if len(k.pending) == 0 {
		return nil, ErrTimeout
	}
	frame := k.pending[0]
	k.pending = k.pending[1:]
	return frame, nil
}
