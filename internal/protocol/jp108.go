package protocol

import (
	"context"
	"fmt"
)

// A JP108 keyboard (Retro 108, 0x5209) is configured over its own HID
// interface with 33-byte reports: a request on output report 0x52, the reply
// on input report 0x54. Reads echo the command byte; writes are acknowledged
// with e4 08. Everything here follows
// docs/clean-room-evidence/dossiers/5209/jp108_hid.toml.

// JP108 mapping types: what kind of thing a key is assigned to.
const (
	JP108TypeNone     byte = 0x00
	JP108TypeMouse    byte = 0x01
	JP108TypeKeyboard byte = 0x07
	JP108TypeConsumer byte = 0x0c
)

// JP108Mapping is what one key is assigned to, exactly as the keyboard
// stores it: a type and four value bytes.
//
//   - Keyboard: Value[0] is a modifier usage (0xe0-0xe7) or 0, Value[1] a key
//     usage or 0. Both may be set: Shift+1 is e1 1e.
//   - Consumer: Value[0:2] is a little-endian media-key usage.
//   - Mouse: Value[0] is a button bitmask, Value[3] a signed wheel step.
//   - None, or Keyboard with a zero value: the key does nothing.
type JP108Mapping struct {
	Type  byte
	Value [4]byte
}

// Unassigned reports whether the mapping makes the key do nothing.
func (m JP108Mapping) Unassigned() bool {
	return m.Type == JP108TypeNone || m.Value == [4]byte{}
}

// jp108KeyIDs are the keyboard's ids for its ten dedicated buttons, in the
// order the rest of the program numbers them: A, B, K1..K8. The ids are not
// contiguous or in order.
var jp108KeyIDs = [...]byte{233, 232, 240, 241, 238, 239, 236, 237, 234, 235}

// jp108DefaultProfileName is the profile name written when the keyboard has
// none. A JP108 holds its mappings in a named profile.
const jp108DefaultProfileName = "OpenBitdo"

// jp108NameChunk is how many name bytes one report carries.
const jp108NameChunk = 29

// jp108Usage decodes the value of a keyboard-type mapping as a single usage,
// for the ten dedicated buttons' simple view: the key if there is one,
// otherwise the modifier.
func jp108Usage(value []byte) uint16 {
	if value[1] != 0 {
		return uint16(value[1])
	}
	return uint16(value[0])
}

func jp108Value(usage uint16) (first, second byte) {
	if usage >= 0xe0 && usage <= 0xe7 {
		return byte(usage), 0
	}
	return 0, byte(usage)
}

// JP108ReadKey reads what one key is assigned to.
func (s *DeviceSession) JP108ReadKey(ctx context.Context, key byte) (JP108Mapping, error) {
	row, err := s.ensureCommandAllowed(CommandJp108ReadDedicatedMappings)
	if err != nil {
		return JP108Mapping{}, err
	}
	payload := append([]byte(nil), row.Request...)
	payload[2] = key
	resp, err := s.sendRow(ctx, row, payload)
	if err != nil {
		return JP108Mapping{}, err
	}
	// Reply: report id, 0x83, key id, type, four value bytes.
	raw := resp.Raw
	if raw[2] != key {
		return JP108Mapping{}, errInvalidResponse(row.ID, fmt.Sprintf("asked for key %d, reply is for key %d", key, raw[2]))
	}
	mapping := JP108Mapping{Type: raw[3]}
	copy(mapping.Value[:], raw[4:8])
	return mapping, nil
}

// JP108WriteKey assigns a key. It names a profile first if the keyboard has
// none, as the vendor's own flow does: mappings live in a named profile.
func (s *DeviceSession) JP108WriteKey(ctx context.Context, key byte, mapping JP108Mapping) error {
	row, err := s.ensureCommandAllowed(CommandJp108WriteDedicatedMapping)
	if err != nil {
		return err
	}
	switch mapping.Type {
	case JP108TypeKeyboard, JP108TypeConsumer, JP108TypeMouse:
	case JP108TypeNone:
		// "No assignment" is written as a keyboard mapping to nothing.
		mapping = JP108Mapping{Type: JP108TypeKeyboard}
	default:
		return errInvalidInput("JP108 mapping type %#02x is not known", mapping.Type)
	}
	if err := s.jp108EnsureProfile(ctx); err != nil {
		return err
	}
	payload := append([]byte(nil), row.Request...)
	payload[8] = key
	payload[9] = mapping.Type
	copy(payload[10:14], mapping.Value[:])
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// JP108ReadMappedKeys lists the ids of the keys that hold a mapping.
func (s *DeviceSession) JP108ReadMappedKeys(ctx context.Context) ([]byte, error) {
	row, err := s.ensureCommandAllowed(CommandJp108ReadMappedKeys)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendRow(ctx, row, row.Request)
	if err != nil {
		return nil, err
	}
	var keys []byte
	raw := resp.Raw
	// Each report: id, 0x81, then (key id, type) pairs, zero-terminated.
	// A non-zero last byte says another report follows.
	for reports := 0; reports < 8; reports++ {
		for i := 2; i+1 < len(raw)-1 && raw[i] != 0; i += 2 {
			keys = append(keys, raw[i])
		}
		if len(raw) < 33 || raw[32] == 0 {
			return keys, nil
		}
		next, err := s.transport.Read(ctx, 64, s.timeoutForCommand(row))
		if err != nil {
			return nil, err
		}
		if ValidateResponse(row.ID, next) != StatusOk {
			return nil, errInvalidResponse(row.ID, "unexpected report while reading the mapped key list")
		}
		raw = next
	}
	return nil, errInvalidResponse(row.ID, "mapped key list did not end")
}

// JP108ReadDedicatedMappings reads what each of the JP108's ten dedicated
// buttons is assigned to, one request per button. Usage 0 means unassigned.
func (s *DeviceSession) JP108ReadDedicatedMappings(ctx context.Context) ([]IndexedUsage, error) {
	out := make([]IndexedUsage, 0, len(jp108KeyIDs))
	for index, key := range jp108KeyIDs {
		mapping, err := s.JP108ReadKey(ctx, key)
		if err != nil {
			return nil, err
		}
		if mapping.Type != JP108TypeKeyboard && !mapping.Unassigned() {
			// A mouse or media assignment cannot be shown as a key usage,
			// and a backup that dropped it would erase it on restore.
			return nil, errInvalidResponse(CommandJp108ReadDedicatedMappings,
				fmt.Sprintf("key %d holds a mapping of type %#02x, which this view cannot represent", key, mapping.Type))
		}
		out = append(out, IndexedUsage{Index: byte(index), Usage: jp108Usage(mapping.Value[:])})
	}
	return out, nil
}

// JP108WriteDedicatedMapping assigns one of the JP108's dedicated buttons
// (index 0-9: A, B, K1..K8) to a keyboard key. Usage 0 unassigns it.
func (s *DeviceSession) JP108WriteDedicatedMapping(ctx context.Context, index byte, targetHIDUsage uint16) error {
	if int(index) >= len(jp108KeyIDs) {
		return errInvalidInput("JP108 button index %d out of range", index)
	}
	if targetHIDUsage > 0xff {
		return errInvalidInput("JP108 key usage %#04x out of range", targetHIDUsage)
	}
	mapping := JP108Mapping{Type: JP108TypeKeyboard}
	mapping.Value[0], mapping.Value[1] = jp108Value(targetHIDUsage)
	return s.JP108WriteKey(ctx, jp108KeyIDs[index], mapping)
}

// JP108ReadProfileName reads the name of the profile the keyboard holds, or
// "" when it has none. A name longer than one report (29 bytes) is returned
// cut to what the first report carries, which is still enough to tell that
// a profile exists.
func (s *DeviceSession) JP108ReadProfileName(ctx context.Context) (string, error) {
	resp, err := s.SendCommand(ctx, CommandJp108ReadProfileName, nil)
	if err != nil {
		return "", err
	}
	// Reply: report id, 0x80, length in bytes, a flag byte, then the name.
	raw := resp.Raw
	length := int(raw[2])
	if length == 0 {
		return "", nil
	}
	length = min(length, len(raw)-4) &^ 1 // whole UTF-16 units that arrived
	if length <= 0 {
		return "", errMalformedResponse(CommandJp108ReadProfileName, len(raw))
	}
	return jp108DecodeName(raw[4 : 4+length]), nil
}

// JP108WriteProfileName names the keyboard's profile. The name is cut to
// what one report carries (14 UTF-16 units); longer names span reports,
// which has not been exercised on hardware.
func (s *DeviceSession) JP108WriteProfileName(ctx context.Context, name string) error {
	row, err := s.ensureCommandAllowed(CommandJp108WriteProfileName)
	if err != nil {
		return err
	}
	encoded := jp108EncodeName(name)
	if len(encoded) == 0 {
		return errInvalidInput("a profile name cannot be empty; use JP108ClearProfile to remove the profile")
	}
	if len(encoded) > jp108NameChunk {
		encoded = encoded[:jp108NameChunk&^1]
	}
	payload := append([]byte(nil), row.Request...)
	payload[2] = byte(len(encoded))
	copy(payload[4:], encoded)
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// JP108ClearProfile removes the keyboard's profile and every mapping in it,
// by writing a zero-length name: the first step of the vendor's own save.
func (s *DeviceSession) JP108ClearProfile(ctx context.Context) error {
	row, err := s.ensureCommandAllowed(CommandJp108WriteProfileName)
	if err != nil {
		return err
	}
	_, err = s.sendRow(ctx, row, row.Request)
	return err
}

// jp108EnsureProfile gives the keyboard a profile to hold mappings in if it
// has none.
func (s *DeviceSession) jp108EnsureProfile(ctx context.Context) error {
	name, err := s.JP108ReadProfileName(ctx)
	if err != nil || name != "" {
		return err
	}
	return s.JP108WriteProfileName(ctx, jp108DefaultProfileName)
}

// JP108 feature flags: key combinations the keyboard can be told to ignore.
const (
	JP108LockWinKey byte = 1 << 0
	JP108LockAltTab byte = 1 << 1
	JP108LockAltF4  byte = 1 << 2
)

// JP108ReadFeatures reads the keyboard's feature flags.
func (s *DeviceSession) JP108ReadFeatures(ctx context.Context) (byte, error) {
	resp, err := s.SendCommand(ctx, CommandJp108ReadFeatureFlags, nil)
	if err != nil {
		return 0, err
	}
	return resp.Raw[2], nil
}

// JP108WriteFeatures sets the keyboard's feature flags.
func (s *DeviceSession) JP108WriteFeatures(ctx context.Context, flags byte) error {
	row, err := s.ensureCommandAllowed(CommandJp108WriteFeatureFlags)
	if err != nil {
		return err
	}
	if err := s.jp108EnsureProfile(ctx); err != nil {
		return err
	}
	payload := append([]byte(nil), row.Request...)
	payload[2] = flags
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// JP108ReadVolume reads the keyboard's volume level, 1 (quietest) to 5.
func (s *DeviceSession) JP108ReadVolume(ctx context.Context) (byte, error) {
	resp, err := s.SendCommand(ctx, CommandJp108ReadVoice, nil)
	if err != nil {
		return 0, err
	}
	return resp.Raw[2], nil
}

// JP108WriteVolume sets the keyboard's volume level, 1 to 5.
func (s *DeviceSession) JP108WriteVolume(ctx context.Context, level byte) error {
	row, err := s.ensureCommandAllowed(CommandJp108WriteVoice)
	if err != nil {
		return err
	}
	if level < 1 || level > 5 {
		return errInvalidInput("JP108 volume level %d out of range 1-5", level)
	}
	payload := append([]byte(nil), row.Request...)
	payload[2] = level
	_, err = s.sendRow(ctx, row, payload)
	return err
}
