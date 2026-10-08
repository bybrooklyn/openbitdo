package protocol

import (
	"context"
	"encoding/binary"
)

// MouseSimulator is a Transport that behaves like a Retro R8 mouse, for
// tests: it keeps every setting and answers each request the way
// docs/clean-room-evidence/dossiers/5205/mouse.toml describes. A write is
// stored as it is acknowledged; there is nothing to commit.
//
// Several things a real mouse does are not known and are chosen here: a
// polling rate's table is answered where the vendor application reads it
// and taken from where it writes it, two bytes apart; a reply's flags are
// the request's op with 0x10 added (the receiver's own reply is known to
// be 0x11 for a read; the vendor application only tests the op's bit); the
// bytes of a reply the vendor application does not read repeat the
// request's; a mouse without a profile still answers every read; clearing
// the profile puts every setting back to its default; and a macro's last
// report is refused when its sum is wrong.
type MouseSimulator struct {
	// Receiver makes this the 2.4G receiver (0x5206) with the mouse behind
	// it, rather than the mouse by cable (0x5205): replies are marked 01,
	// and the link question is answered.
	Receiver bool
	// Off is the mouse switched off behind its receiver: the receiver says
	// no mouse is linked and everything else goes unanswered. By cable
	// nothing answers at all.
	Off bool
	// IgnoreWrites makes every write be acknowledged and not stored, as a
	// mouse that silently dropped it would.
	IgnoreWrites bool
	// Mute is the command byte of writes that go unanswered and unstored;
	// zero mutes nothing. With MuteOnce only the next such write does.
	Mute     byte
	MuteOnce bool
	// BadSums counts macros whose last report carried the wrong sum.
	BadSums int
	// Cleared counts profile clears.
	Cleared int
	// Frames records every frame written, for assertions.
	Frames [][]byte

	// What the mouse holds.
	Name           [MouseNameLen]byte
	Buttons        [MouseButtons]MouseButtonEntry
	LeftHanded     byte
	LiftOff        byte
	WheelSpeed     byte
	WheelDirection byte
	Dpi            [2]MouseDpi
	Stage          byte
	StageDpi       uint16
	Rate           MousePollingRate
	// RateSpare is the two bytes of the polling rate reply the vendor
	// application does not use. Nothing written changes them.
	RateSpare [2]byte
	// Macros holds the macro stored for each button that can play one,
	// with its name at full length.
	Macros [MouseMacroLastButton - MouseMacroFirstButton + 1]MouseMacro

	// macroAt is the macro a records request is about: the one whose
	// header was last read or written.
	macroAt int
	// incoming collects the records of a macro being written.
	incoming []byte
	opened   bool
	pending  [][]byte
}

// NewMouseSimulator is a Retro R8 with no profile and every setting at the
// default the vendor application assumes.
func NewMouseSimulator(receiver bool) *MouseSimulator {
	m := &MouseSimulator{Receiver: receiver}
	m.reset()
	return m
}

func (m *MouseSimulator) reset() {
	m.Name = [MouseNameLen]byte{}
	for i, function := range []uint32{2, 3, 5, 4, 0, 0} {
		m.Buttons[i] = MouseButtonEntry{Function: function}
	}
	m.LeftHanded, m.LiftOff, m.WheelSpeed, m.WheelDirection = 0, 1, 0, 0
	stages := [MouseDpiStages]uint16{800, 1200, 1600, 2400, 3200, 6400}
	m.Dpi = [2]MouseDpi{{Mask: 0x3f, Values: stages}, {Values: stages}}
	m.Stage, m.StageDpi = 0, stages[0]
	m.Rate = MousePollingRate{Count: 6, Current: 1, Rates: [6]uint16{250, 500, 1000, 2000, 4000, 8000}}
	for i := range m.Macros {
		m.Macros[i] = MouseMacro{}
	}
}

func (m *MouseSimulator) Open(context.Context, VidPid) error { m.opened = true; return nil }
func (m *MouseSimulator) Close() error                       { m.opened = false; return nil }

// Unpaced tells a session it need not wait between requests.
func (m *MouseSimulator) Unpaced() bool { return true }

func (m *MouseSimulator) reply(mark, group, cmd, flags byte, data ...byte) {
	frame := make([]byte, 64)
	frame[0], frame[1], frame[2], frame[3] = mark, group, cmd, flags
	copy(frame[mouseReplyData:], data)
	m.pending = append(m.pending, frame)
}

func (m *MouseSimulator) Write(data []byte) (int, error) {
	if !m.opened {
		return 0, errTransport("simulator not open")
	}
	m.Frames = append(m.Frames, append([]byte(nil), data...))
	// 65 bytes: no report id, so a zero, then the 64-byte report.
	if len(data) != 65 || data[0] != 0 {
		return len(data), nil
	}
	if m.Receiver && data[1] == 0x06 && data[2] == 0x03 && data[3] == 0x0a && data[4] == mouseOpRead {
		linked := byte(1)
		if m.Off {
			linked = 0
		}
		m.reply(0x06, 0x03, 0x0a, 0x11, linked)
		return len(data), nil
	}
	if data[1] != 0x01 || m.Off {
		return len(data), nil // not this protocol, or nobody there: no answer
	}
	group, cmd, op, payload := data[2], data[3], data[4], data[mouseRequestData:]
	mark := mouseMarkWired
	if m.Receiver {
		mark = mouseMarkReceiver
	}
	answer := func(out ...byte) { m.reply(mark, group, cmd, 0x10|op, out...) }

	switch {
	case op == mouseOpWrite && cmd == m.Mute:
		if m.MuteOnce {
			m.Mute = 0
		}
	case group == 0x01 && cmd == 0x33 && op == mouseOpWrite:
		if !m.IgnoreWrites {
			m.reset()
		}
		m.Cleared++
		answer()
	case group != 0x06:
	case op == mouseOpRead:
		if out, ok := m.read(cmd, payload); ok {
			answer(out...)
		}
	case op == mouseOpWrite:
		if m.write(cmd, payload) {
			answer()
		}
	}
	return len(data), nil
}

// setting is the one-byte setting a command byte reads and writes.
func (m *MouseSimulator) setting(cmd byte) *byte {
	switch cmd {
	case 0x27:
		return &m.LeftHanded
	case 0x26:
		return &m.LiftOff
	case 0x24:
		return &m.WheelSpeed
	case 0x23:
		return &m.WheelDirection
	}
	return nil
}

func (m *MouseSimulator) read(cmd byte, payload []byte) ([]byte, bool) {
	if value := m.setting(cmd); value != nil {
		return []byte{*value}, true
	}
	switch cmd {
	case 0x1c: // name: two zero bytes, the block word, ten bytes
		block := int(binary.LittleEndian.Uint16(payload[2:]) >> 5)
		if block >= MouseNameLen/mouseNameChunk {
			return nil, false
		}
		out := append([]byte{0, 0}, mouseNameWord(block, mouseNameChunk)[2:]...)
		return append(out, m.Name[block*mouseNameChunk:(block+1)*mouseNameChunk]...), true
	case 0x14: // buttons: the request's three bytes, then six bytes a button
		first, count := int(payload[1]), int(payload[2])
		if first < 1 || count < 1 || first+count-1 > MouseButtons {
			return nil, false
		}
		out := append([]byte(nil), payload[:3]...)
		for button := first; button < first+count; button++ {
			entry := m.Buttons[button-1]
			out = append(out, entry.Macro, byte(button), 0, 0, 0, 0)
			binary.LittleEndian.PutUint32(out[len(out)-4:], entry.Function)
		}
		return out, true
	case 0x28: // DPI: the axis, the mask, six values
		axis := int(payload[0])
		if axis > MouseAxisY {
			return nil, false
		}
		out := []byte{byte(axis), m.Dpi[axis].Mask}
		for _, value := range m.Dpi[axis].Values {
			out = append(out, byte(value), byte(value>>8))
		}
		return out, true
	case 0x12: // stage in use, as it is written
		return []byte{MouseDpiStages, m.Stage, byte(m.StageDpi), byte(m.StageDpi >> 8)}, true
	case 0x0d:
		out := []byte{m.Rate.Count, m.Rate.Current, m.RateSpare[0], m.RateSpare[1]}
		for _, hz := range m.Rate.Rates {
			out = append(out, byte(hz), byte(hz>>8))
		}
		return out, true
	case 0x0f:
		return m.readMacro(payload)
	}
	return nil, false
}

func (m *MouseSimulator) macroFor(button byte) (int, bool) {
	if !mouseMacroButtonOK(int(button)) {
		return 0, false
	}
	return int(button) - MouseMacroFirstButton, true
}

func mouseMacroFullName(macro MouseMacro) []byte {
	name := make([]byte, MouseMacroNameLen)
	copy(name, macro.Name)
	return name
}

func (m *MouseSimulator) readMacro(payload []byte) ([]byte, bool) {
	switch payload[0] {
	case mouseMacroHeader:
		at, ok := m.macroFor(payload[2])
		if !ok {
			return nil, false
		}
		m.macroAt = at
		macro := m.Macros[at]
		return []byte{
			mouseMacroHeader, byte(len(macro.Records)), payload[2], byte(macro.Cycles), byte(macro.Cycles >> 8),
			byte(at + 1), 0, 0, 0x20, byte(macro.IntervalMs), byte(macro.IntervalMs >> 8),
		}, true
	case mouseMacroName:
		at, ok := m.macroFor(payload[1])
		block := int(payload[2] / 8)
		if !ok || payload[2]%8 != 0 || block >= MouseMacroNameLen/mouseNameChunk {
			return nil, false
		}
		name := mouseMacroFullName(m.Macros[at])
		return append(append([]byte(nil), payload[:3]...), name[block*mouseNameChunk:(block+1)*mouseNameChunk]...), true
	case mouseMacroRecords:
		records := m.Macros[m.macroAt].Records
		first, count := int(payload[3]), int(payload[4])
		if count > mouseMacroPerFrame || first+count > len(records) {
			return nil, false
		}
		out := append([]byte(nil), payload[:5]...)
		for _, record := range records[first : first+count] {
			out = append(out, record.State, byte(record.Code), byte(record.Code>>8), byte(record.Timer), byte(record.Timer>>8))
		}
		return out, true
	}
	return nil, false
}

// write stores what a write request carries and reports whether it is
// acknowledged.
func (m *MouseSimulator) write(cmd byte, payload []byte) bool {
	keep := !m.IgnoreWrites
	if value := m.setting(cmd); value != nil {
		if keep {
			*value = payload[0]
		}
		return true
	}
	switch cmd {
	case 0x1c:
		word := binary.LittleEndian.Uint16(payload[2:])
		block, length := int(word>>5), int(word&0x1f)
		if length > mouseNameChunk || block*mouseNameChunk+length > MouseNameLen {
			return false
		}
		if keep {
			copy(m.Name[block*mouseNameChunk:], payload[4:4+length])
		}
	case 0x10: // macro switch, button, function
		button := int(payload[1])
		if button < 1 || button > MouseButtons {
			return false
		}
		if keep {
			m.Buttons[button-1] = MouseButtonEntry{Macro: payload[0], Function: binary.LittleEndian.Uint32(payload[2:])}
		}
	case 0x28:
		axis := int(payload[0])
		if axis > MouseAxisY {
			return false
		}
		if keep {
			m.Dpi[axis].Mask = payload[1]
			for i := range m.Dpi[axis].Values {
				m.Dpi[axis].Values[i] = binary.LittleEndian.Uint16(payload[2+2*i:])
			}
		}
	case 0x12:
		if payload[1] >= MouseDpiStages {
			return false
		}
		if keep {
			m.Stage, m.StageDpi = payload[1], binary.LittleEndian.Uint16(payload[2:])
		}
	case 0x0d:
		if keep {
			m.Rate.Count, m.Rate.Current = payload[0], payload[1]
			// A sixth rate is only sent when there are six.
			for i := range m.Rate.Rates {
				if i < 5 || payload[0] == 6 {
					m.Rate.Rates[i] = binary.LittleEndian.Uint16(payload[mouseRateWriteTable+2*i:])
				}
			}
		}
	case 0x0f:
		return m.writeMacro(payload, keep)
	default:
		return false
	}
	return true
}

func (m *MouseSimulator) writeMacro(payload []byte, keep bool) bool {
	switch payload[0] {
	case mouseMacroHeader: // count, button, cycles, number, two bytes, 0x20, interval
		at, ok := m.macroFor(payload[2])
		if !ok || int(payload[1]) > MouseMacroMaxRecords || int(payload[5]) != at+1 {
			return false
		}
		m.macroAt, m.incoming = at, nil
		if keep {
			// A new header leaves the name and empties the records until
			// they arrive.
			m.Macros[at].Cycles = binary.LittleEndian.Uint16(payload[3:])
			m.Macros[at].IntervalMs = binary.LittleEndian.Uint16(payload[9:])
			m.Macros[at].Records = nil
		}
	case mouseMacroName:
		at, ok := m.macroFor(payload[1])
		block := int(payload[2] / 8)
		if !ok || payload[2]%8 != 0 || block >= MouseMacroNameLen/mouseNameChunk {
			return false
		}
		if keep {
			name := mouseMacroFullName(m.Macros[at])
			copy(name[block*mouseNameChunk:], payload[3:3+mouseNameChunk])
			m.Macros[at].Name = name
		}
	case mouseMacroRecords, mouseMacroLastPart:
		first, count := int(payload[3]), int(payload[4])
		if count < 1 || count > mouseMacroPerFrame || first*mouseMacroRecord != len(m.incoming) {
			return false
		}
		m.incoming = append(m.incoming, payload[5:5+count*mouseMacroRecord]...)
		if payload[0] != mouseMacroLastPart {
			break
		}
		var sum uint16
		for _, b := range m.incoming {
			sum += uint16(b)
		}
		raw := m.incoming
		m.incoming = nil
		if sum != binary.LittleEndian.Uint16(payload[1:]) {
			m.BadSums++
			return false
		}
		if keep {
			records := make([]MouseMacroRecord, 0, len(raw)/mouseMacroRecord)
			for i := 0; i+mouseMacroRecord <= len(raw); i += mouseMacroRecord {
				records = append(records, MouseMacroRecord{
					State: raw[i], Code: binary.LittleEndian.Uint16(raw[i+1:]), Timer: binary.LittleEndian.Uint16(raw[i+3:]),
				})
			}
			m.Macros[m.macroAt].Records = records
		}
	default:
		return false
	}
	return true
}

func (m *MouseSimulator) Read(context.Context, int, uint64) ([]byte, error) {
	if len(m.pending) == 0 {
		return nil, ErrTimeout
	}
	frame := m.pending[0]
	m.pending = m.pending[1:]
	return frame, nil
}

// NewRivieraMouseSimulator is a Transport that behaves like a Riviera
// mouse: a record keyboard's simulator holding the mouse's 140-byte record
// and nothing else.
func NewRivieraMouseSimulator() *KbRecordSimulator {
	return &KbRecordSimulator{RecordSize: MouseRecordSize}
}
