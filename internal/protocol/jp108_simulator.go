package protocol

import "context"

// JP108Simulator is a Transport that behaves like a Retro 108's
// configuration interface, for tests: it keeps a profile name and a mapping
// per key, and answers each request the way the keyboard was observed to
// (see docs/clean-room-evidence/dossiers/5209/jp108_hid.toml). Unlike
// MockTransport's fixed queue of replies, its reply depends on the request,
// which the per-key protocol needs.
type JP108Simulator struct {
	// Name is the stored profile name, as the bytes written.
	Name []byte
	// MacroNames and MacroValues hold the stored macros by key id.
	MacroNames  map[byte][]byte
	MacroValues map[byte][]byte
	// Mappings holds, per key id, the type byte and four value bytes.
	Mappings map[byte][5]byte
	// Features and Volume are the stored feature flags and volume level.
	Features byte
	Volume   byte
	// IgnoreWrites makes the keyboard acknowledge a mapping write without
	// storing it, as a device that silently rejects a value would.
	IgnoreWrites bool
	// ShortReads cuts every mapping read reply to this many bytes when > 0.
	ShortReads int
	// SlowReplies makes that many reads time out before a reply is
	// delivered, as a keyboard that answers late would.
	SlowReplies int
	// Frames records every frame written, for assertions.
	Frames [][]byte

	opened  bool
	pending [][]byte
	// staging gathers a macro value that arrives over several reports.
	staging []byte
}

func (k *JP108Simulator) Open(context.Context, VidPid) error { k.opened = true; return nil }
func (k *JP108Simulator) Close() error                       { k.opened = false; return nil }

func (k *JP108Simulator) reply(bytes ...byte) {
	frame := make([]byte, 33)
	frame[0] = jp108ReplyReportID
	copy(frame[1:], bytes)
	k.pending = append(k.pending, frame)
}

func (k *JP108Simulator) Write(data []byte) (int, error) {
	if !k.opened {
		return 0, errTransport("simulator not open")
	}
	k.Frames = append(k.Frames, append([]byte(nil), data...))
	// The keyboard only has an output report 0x52, 32 data bytes long.
	if len(data) != 33 || data[0] != 0x52 {
		return 0, errTransport("the keyboard has no such report")
	}
	switch data[1] {
	case 0x80: // read profile name
		k.reply(append([]byte{0x80, byte(len(k.Name)), 0x00}, k.Name...)...)
	case 0x83: // read one key's mapping
		mapping := k.Mappings[data[2]]
		k.reply(append([]byte{0x83, data[2]}, mapping[:]...)...)
		if k.ShortReads > 0 {
			last := len(k.pending) - 1
			k.pending[last] = k.pending[last][:k.ShortReads]
		}
	case 0x81: // list mapped keys
		pairs := []byte{0x81}
		for id := 255; id > 0; id-- {
			if mapping, ok := k.Mappings[byte(id)]; ok && mapping != [5]byte{} && mapping != [5]byte{JP108TypeKeyboard} {
				pairs = append(pairs, byte(id), mapping[0])
			}
		}
		// Fifteen pairs fit a report; byte 31 of the data flags another.
		for first := true; first || len(pairs) > 1; first = false {
			chunk := pairs
			if len(chunk) > 31 {
				chunk = pairs[:31]
			}
			frame := append(append([]byte{}, chunk...), make([]byte, 32-len(chunk))...)
			rest := pairs[len(chunk):]
			if len(rest) > 0 {
				frame[31] = 1
			}
			k.reply(frame...)
			pairs = append([]byte{0x81}, rest...)
			if len(rest) == 0 {
				break
			}
		}
	case 0x82: // list macros: seven four-byte entries, a more-flag, then the eighth
		var keys []byte
		for id := 255; id > 0; id-- {
			if _, ok := k.MacroValues[byte(id)]; ok {
				keys = append(keys, byte(id))
			}
		}
		first := make([]byte, 30)
		first[0] = 0x82
		for i, key := range keys {
			if i < 7 {
				first[1+i*4] = key
			}
		}
		if len(keys) > 7 {
			first[29] = 1
		}
		k.reply(first...)
		if len(keys) > 7 {
			k.reply(0x82, keys[7])
		}
	case 0x84: // read a macro's name, 28 bytes per report
		name := k.MacroNames[data[2]]
		for first := true; first || len(name) > 0; first = false {
			chunk := name[:min(len(name), 28)]
			name = name[len(chunk):]
			more := byte(0)
			if len(name) > 0 {
				more = 1
			}
			k.reply(append([]byte{0x84, data[2], byte(len(chunk)), more}, chunk...)...)
		}
	case 0x86: // read a macro's value, 26 bytes per report
		value := k.MacroValues[data[2]]
		for offset, first := 0, true; first || offset < len(value); first = false {
			end := min(len(value), offset+26)
			more := byte(0)
			if end < len(value) {
				more = 1
			}
			k.reply(append([]byte{0x86, data[2], more, byte(offset), byte(offset >> 8), byte(end - offset)}, value[offset:end]...)...)
			offset = end
		}
	case 0x74: // write a macro's name
		if k.MacroNames == nil {
			k.MacroNames = map[byte][]byte{}
		}
		if !k.IgnoreWrites {
			k.MacroNames[data[2]] = append([]byte(nil), data[5:5+int(data[3])]...)
		}
		k.reply(0xe4, 0x08)
	case 0x76: // write a macro's value: key, more, offset, length, bytes
		offset := int(data[4]) | int(data[5])<<8
		if offset == 0 {
			k.staging = nil
		}
		if offset != len(k.staging) {
			break // a chunk out of place: no acknowledgement
		}
		k.staging = append(k.staging, data[7:7+min(int(data[6]), 26)]...)
		if data[3] != 0 {
			break // more to come; acknowledged after the last report
		}
		value := k.staging
		k.staging = nil
		if len(value) >= 4 {
			value = value[:min(len(value), 4+int(value[3])*3)]
		}
		if k.MacroValues == nil {
			k.MacroValues = map[byte][]byte{}
		}
		if !k.IgnoreWrites {
			k.MacroValues[data[2]] = value
		}
		k.reply(0xe4, 0x08)
	case 0x77: // clear a macro
		delete(k.MacroNames, data[2])
		delete(k.MacroValues, data[2])
		k.reply(0xe4, 0x08)
	case 0x88: // read feature flags
		k.reply(0x88, k.Features)
	case 0x78: // write feature flags
		k.Features = data[2]
		k.reply(0xe4, 0x08)
	case 0x89: // read volume
		k.reply(0x89, k.Volume)
	case 0x79: // write volume
		k.Volume = data[2]
		k.reply(0xe4, 0x08)
	case 0x70: // write profile name; length 0 clears the profile
		length := int(data[2])
		k.Name = append([]byte(nil), data[4:4+length]...)
		if length == 0 {
			k.Mappings, k.MacroNames, k.MacroValues = nil, nil, nil
		}
		k.reply(0xe4, 0x08)
	case 0xfa: // assign a key
		if !k.IgnoreWrites {
			if k.Mappings == nil {
				k.Mappings = map[byte][5]byte{}
			}
			var mapping [5]byte
			copy(mapping[:], data[9:14])
			k.Mappings[data[8]] = mapping
		}
		k.reply(0xe4, 0x08)
	}
	return len(data), nil
}

func (k *JP108Simulator) Read(context.Context, int, uint64) ([]byte, error) {
	if len(k.pending) == 0 {
		return nil, ErrTimeout
	}
	if k.SlowReplies > 0 {
		k.SlowReplies--
		return nil, ErrTimeout
	}
	frame := k.pending[0]
	k.pending = k.pending[1:]
	return frame, nil
}
