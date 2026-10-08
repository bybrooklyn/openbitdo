package protocol

import "context"

// JP108Simulator is a Transport that behaves like a Retro 108's
// configuration interface, for tests: it keeps a profile name and a mapping
// per key, and answers each request the way the keyboard was observed to
// (see docs/clean-room-evidence/dossiers/5209/jp108_hid.toml). Unlike
// MockTransport's fixed queue of replies, its reply depends on the request,
// which the per-key protocol needs.
type JP108Simulator struct {
	// Name is the stored profile name, as UTF-16LE bytes.
	Name []byte
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
	case 0x82: // list macros
		k.reply(0x82)
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
			k.Mappings = nil
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
