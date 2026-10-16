package protocol

import (
	"context"
	"encoding/binary"
	"fmt"
	"unicode/utf16"
)

// Macros on a JP108 keyboard. Each of up to eight macros belongs to one key
// and has a name and a value (a four-byte header, then three bytes per
// step). Names and values longer than a report travel in chunks. Layouts
// are in docs/clean-room-evidence/dossiers/5209/jp108_hid.toml.

const (
	jp108MacroSlots       = 8
	jp108MacroNameChunk   = 28 // name bytes per report
	jp108MacroValueFirst  = 25 // value bytes in the first report of several
	jp108MacroValueChunk  = 24 // value bytes in each later report
	jp108MacroValueReply  = 26 // value bytes per reply report
	jp108MacroValueSingle = 26 // the length a single-report value declares
	// JP108MacroValueMax is the longest macro value: the header and 200
	// steps.
	JP108MacroValueMax = 4 + 200*3
)

// JP108ReadMacroKeys lists the ids of the keys that play a macro.
func (s *DeviceSession) JP108ReadMacroKeys(ctx context.Context) ([]byte, error) {
	row, err := s.ensureCommandAllowed(CommandJp108ReadMacroList)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendRow(ctx, row, row.Request)
	if err != nil {
		return nil, err
	}
	// Report: id, 0x82, seven four-byte entries (key id first), then a
	// byte that is non-zero when a second report carries the eighth.
	var keys []byte
	raw := resp.Raw
	for i := 2; i+4 <= 30 && i < len(raw) && raw[i] != 0; i += 4 {
		keys = append(keys, raw[i])
	}
	if len(keys) == 7 && len(raw) > 30 && raw[30] != 0 {
		next, err := s.transport.Read(ctx, 64, s.timeoutForCommand(row))
		if err != nil {
			return nil, err
		}
		if ValidateResponse(row.ID, next) != StatusOk {
			return nil, errInvalidResponse(row.ID, "unexpected report while reading the macro list")
		}
		if next[2] != 0 {
			keys = append(keys, next[2])
		}
	}
	return keys, nil
}

// jp108ReadChunked sends a one-key read and gathers the chunks of its
// reply. at gives, for one report, the data it carries and whether another
// report follows.
func (s *DeviceSession) jp108ReadChunked(ctx context.Context, command CommandID, key byte, limit int,
	at func(raw []byte) (data []byte, more bool)) ([]byte, error) {
	row, err := s.ensureCommandAllowed(command)
	if err != nil {
		return nil, err
	}
	payload := append([]byte(nil), row.Request...)
	payload[2] = key
	resp, err := s.sendRow(ctx, row, payload)
	if err != nil {
		return nil, err
	}
	var out []byte
	raw := resp.Raw
	for {
		data, more := at(raw)
		out = append(out, data...)
		if !more {
			return out, nil
		}
		if len(out) >= limit {
			return nil, errInvalidResponse(row.ID, "the reply did not end")
		}
		if raw, err = s.transport.Read(ctx, 64, s.timeoutForCommand(row)); err != nil {
			return nil, err
		}
		if ValidateResponse(row.ID, raw) != StatusOk {
			return nil, errInvalidResponse(row.ID, "unexpected report in the middle of a reply")
		}
	}
}

// JP108ReadMacroName reads the name of the macro on key.
func (s *DeviceSession) JP108ReadMacroName(ctx context.Context, key byte) (string, error) {
	// Report: id, 0x84, key, length, more, then up to 28 name bytes.
	raw, err := s.jp108ReadChunked(ctx, CommandJp108ReadMacroName, key, 61, func(raw []byte) ([]byte, bool) {
		if len(raw) < 6 {
			return nil, false
		}
		return raw[5:min(len(raw), 5+jp108MacroNameChunk)], raw[4] != 0
	})
	if err != nil {
		return "", err
	}
	return jp108DecodeName(raw), nil
}

// JP108ReadMacroValue reads the stored value of the macro on key: a
// four-byte header whose last byte is the step count, then three bytes per
// step.
func (s *DeviceSession) JP108ReadMacroValue(ctx context.Context, key byte) ([]byte, error) {
	// Report: id, 0x86, key, more, offset (two bytes), length, then up to
	// 26 value bytes.
	value, err := s.jp108ReadChunked(ctx, CommandJp108ReadMacroValue, key, JP108MacroValueMax+jp108MacroValueReply,
		func(raw []byte) ([]byte, bool) {
			if len(raw) < 8 {
				return nil, false
			}
			return raw[7:min(len(raw), 7+jp108MacroValueReply)], raw[3] != 0
		})
	if err != nil {
		return nil, err
	}
	if len(value) < 4 {
		return nil, errMalformedResponse(CommandJp108ReadMacroValue, len(value))
	}
	want := 4 + int(value[3])*3
	if len(value) < want {
		return nil, errInvalidResponse(CommandJp108ReadMacroValue,
			fmt.Sprintf("macro declares %d steps but only %d value bytes arrived", value[3], len(value)))
	}
	return value[:want], nil
}

// JP108WriteMacro stores a macro on key: its name, then its value. A macro
// the key already plays is cleared first.
func (s *DeviceSession) JP108WriteMacro(ctx context.Context, key byte, name string, value []byte) error {
	if len(value) < 4+3 || len(value) > JP108MacroValueMax || (len(value)-4)%3 != 0 || int(value[3])*3 != len(value)-4 {
		return errInvalidInput("a macro value is a 4-byte header and 1-200 three-byte steps; got %d bytes declaring %d steps", len(value), valueSteps(value))
	}
	encodedName := jp108EncodeName(name)
	if len(encodedName) == 0 || len(encodedName) > jp108MacroNameChunk {
		return errInvalidInput("a macro name is 1-%d characters", jp108MacroNameChunk/2)
	}
	nameRow, err := s.ensureCommandAllowed(CommandJp108WriteMacroName)
	if err != nil {
		return err
	}
	valueRow, err := s.ensureCommandAllowed(CommandJp108WriteMacroValue)
	if err != nil {
		return err
	}
	keys, err := s.JP108ReadMacroKeys(ctx)
	if err != nil {
		return err
	}
	exists := false
	for _, k := range keys {
		exists = exists || k == key
	}
	if !exists && len(keys) >= jp108MacroSlots {
		return errInvalidInput("the keyboard already holds %d macros", jp108MacroSlots)
	}
	if exists {
		if err := s.JP108ClearMacro(ctx, key); err != nil {
			return err
		}
	}
	if err := s.jp108EnsureProfile(ctx); err != nil {
		return err
	}

	// Name: 0x74, key, length, more, name bytes.
	payload := append([]byte(nil), nameRow.Request...)
	payload[2], payload[3] = key, byte(len(encodedName))
	copy(payload[5:], encodedName)
	if _, err := s.sendRow(ctx, nameRow, payload); err != nil {
		return err
	}

	// Value: 0x76, key, more, offset (two bytes), length, value bytes. The
	// keyboard acknowledges once, after the last report.
	frame := func(offset int, chunk []byte, declared int, more bool) []byte {
		payload := append([]byte(nil), valueRow.Request...)
		payload[2] = key
		if more {
			payload[3] = 1
		}
		binary.LittleEndian.PutUint16(payload[4:], uint16(offset))
		payload[6] = byte(declared)
		copy(payload[7:], chunk)
		return payload
	}
	if len(value) <= jp108MacroValueFirst {
		_, err := s.sendRow(ctx, valueRow, frame(0, value, jp108MacroValueSingle, false))
		return err
	}
	if _, err := s.transport.Write(frame(0, value[:jp108MacroValueFirst], jp108MacroValueFirst, true)); err != nil {
		return err
	}
	for offset := jp108MacroValueFirst; offset < len(value); {
		end := min(len(value), offset+jp108MacroValueChunk)
		last := end == len(value)
		payload := frame(offset, value[offset:end], end-offset, !last)
		if last {
			_, err := s.sendRow(ctx, valueRow, payload)
			return err
		}
		if _, err := s.transport.Write(payload); err != nil {
			return err
		}
		offset = end
	}
	return nil
}

func valueSteps(value []byte) int {
	if len(value) < 4 {
		return 0
	}
	return int(value[3])
}

// JP108ClearMacro removes the macro on key.
func (s *DeviceSession) JP108ClearMacro(ctx context.Context, key byte) error {
	row, err := s.ensureCommandAllowed(CommandJp108ClearMacro)
	if err != nil {
		return err
	}
	// The request carries the macro's step count; read it rather than
	// trust a caller's idea of it.
	steps := byte(0)
	if value, err := s.JP108ReadMacroValue(ctx, key); err == nil {
		steps = value[3]
	}
	payload := append([]byte(nil), row.Request...)
	payload[2], payload[3] = key, steps
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// jp108EncodeName encodes a name the way the vendor's application does:
// UTF-16, high byte first.
func jp108EncodeName(name string) []byte {
	units := utf16.Encode([]rune(name))
	raw := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		raw = append(raw, byte(unit>>8), byte(unit))
	}
	return raw
}

// jp108DecodeName decodes a stored name. The vendor's application writes
// UTF-16 high byte first; OpenBitdo before 0.0.4 wrote low byte first, so a
// name whose units all have a zero second byte is read that way.
func jp108DecodeName(raw []byte) string {
	raw = raw[:len(raw)&^1]
	for i := 0; i+1 < len(raw); i += 2 {
		if raw[i] == 0 && raw[i+1] == 0 {
			raw = raw[:i]
			break
		}
	}
	if len(raw) == 0 {
		return ""
	}
	lowFirst := true
	for i := 0; i+1 < len(raw); i += 2 {
		if raw[i+1] != 0 || raw[i] == 0 {
			lowFirst = false
			break
		}
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		if lowFirst {
			units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
		} else {
			units = append(units, uint16(raw[i])<<8|uint16(raw[i+1]))
		}
	}
	return string(utf16.Decode(units))
}
