package protocol

import (
	"context"
	"fmt"
	"unicode/utf16"
)

// GetMode reads the device's current mode, falling back to GetModeAlt.
func (s *DeviceSession) GetMode(ctx context.Context) (ModeState, error) {
	resp, err := s.SendCommand(ctx, CommandGetMode, nil)
	if err == nil {
		if mode, ok := resp.ParsedFields["mode"]; ok {
			return ModeState{Mode: byte(mode), Source: "GetMode"}, nil
		}
	}
	resp, err = s.SendCommand(ctx, CommandGetModeAlt, nil)
	if err != nil {
		return ModeState{}, err
	}
	return ModeState{Mode: byte(resp.ParsedFields["mode"]), Source: "GetModeAlt"}, nil
}

// GetControllerVersion reads the device's reported firmware version,
// formatted the same way diagnostics does (e.g. "firmware 1.23"), falling
// back to CommandVersion if CommandGetControllerVersion doesn't respond.
func (s *DeviceSession) GetControllerVersion(ctx context.Context) (string, error) {
	resp, err := s.SendCommand(ctx, CommandGetControllerVersion, nil)
	if err != nil {
		resp, err = s.SendCommand(ctx, CommandVersion, nil)
		if err != nil {
			return "", err
		}
	}
	version, hasVersion := resp.ParsedFields["version_x100"]
	if !hasVersion {
		return "", errInvalidInput("controller version response missing version field")
	}
	if beta, hasBeta := resp.ParsedFields["beta"]; hasBeta {
		return formatFirmwareVersion(version, &beta), nil
	}
	return formatFirmwareVersion(version, nil), nil
}

// SetMode writes a new device mode via SetModeDInput, then reads it back.
func (s *DeviceSession) SetMode(ctx context.Context, mode byte) (ModeState, error) {
	row, err := s.ensureCommandAllowed(CommandSetModeDInput)
	if err != nil {
		return ModeState{}, err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) < 5 {
		return ModeState{}, errInvalidInput("SetModeDInput payload shorter than expected")
	}
	payload[4] = mode
	if _, err := s.sendRow(ctx, row, payload); err != nil {
		return ModeState{}, err
	}
	return s.GetMode(ctx)
}

// ReadProfile reads a profile slot as a raw ProfileBlob wrapper (payload is
// the raw response bytes, matching Rust's behavior).
func (s *DeviceSession) ReadProfile(ctx context.Context, slot byte) (ProfileBlob, error) {
	row, err := s.ensureCommandAllowed(CommandReadProfile)
	if err != nil {
		return ProfileBlob{}, err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) > 3 {
		payload[3] = slot
	}
	resp, err := s.sendRow(ctx, row, payload)
	if err != nil {
		return ProfileBlob{}, err
	}
	return ProfileBlob{Slot: slot, Payload: resp.Raw}, nil
}

// WriteProfile writes a serialized ProfileBlob into a profile slot.
func (s *DeviceSession) WriteProfile(ctx context.Context, slot byte, profile ProfileBlob) error {
	row, err := s.ensureCommandAllowed(CommandWriteProfile)
	if err != nil {
		return err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) > 3 {
		payload[3] = slot
	}

	serialized := profile.ToBytes()
	copyLen := min(max(len(payload)-8, 0), len(serialized))
	if copyLen > 0 {
		copy(payload[8:8+copyLen], serialized[:copyLen])
	}
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// jp108KeyIDs are the keyboard's ids for its ten dedicated buttons, in the
// order the rest of the program numbers them: A, B, K1..K8. The ids are not
// contiguous or in order; see docs/clean-room-evidence/dossiers/5209.
var jp108KeyIDs = [...]byte{233, 232, 240, 241, 238, 239, 236, 237, 234, 235}

// jp108TypeKeyboard marks a mapping whose value is a keyboard key.
const jp108TypeKeyboard = 0x07

// jp108DefaultProfileName is the profile name written when the keyboard has
// none. A JP108 holds its mappings in a named profile.
const jp108DefaultProfileName = "OpenBitdo"

// jp108Usage decodes the value of a keyboard-type mapping. A modifier
// (usages 0xe0-0xe7) is stored in the first value byte, any other key in
// the second; an unassigned button has both zero.
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

// JP108ReadDedicatedMappings reads what each of the JP108's ten dedicated
// buttons is assigned to, one request per button. Usage 0 means unassigned.
func (s *DeviceSession) JP108ReadDedicatedMappings(ctx context.Context) ([]IndexedUsage, error) {
	row, err := s.ensureCommandAllowed(CommandJp108ReadDedicatedMappings)
	if err != nil {
		return nil, err
	}
	out := make([]IndexedUsage, 0, len(jp108KeyIDs))
	for index, key := range jp108KeyIDs {
		payload := append([]byte(nil), row.Request...)
		payload[2] = key
		resp, err := s.sendRow(ctx, row, payload)
		if err != nil {
			return nil, err
		}
		// Reply: report id, 0x83, key id, type, four value bytes.
		raw := resp.Raw
		if raw[2] != key {
			return nil, errInvalidResponse(row.ID, fmt.Sprintf("asked for key %d, reply is for key %d", key, raw[2]))
		}
		kind, value := raw[3], raw[4:8]
		if kind != jp108TypeKeyboard && (kind != 0 || value[0] != 0 || value[1] != 0) {
			// A mouse or media assignment. It cannot be represented here,
			// and a backup that dropped it would erase it on restore.
			return nil, errInvalidResponse(row.ID, fmt.Sprintf("key %d holds a mapping of type %#02x, which is not supported yet", key, kind))
		}
		out = append(out, IndexedUsage{Index: byte(index), Usage: jp108Usage(value)})
	}
	return out, nil
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
	// Reply: report id, 0x80, length in bytes, a flag byte, then UTF-16LE.
	raw := resp.Raw
	length := int(raw[2])
	if length == 0 {
		return "", nil
	}
	length = min(length, len(raw)-4) &^ 1 // whole UTF-16 units that arrived
	if length <= 0 {
		return "", errMalformedResponse(CommandJp108ReadProfileName, len(raw))
	}
	units := make([]uint16, 0, length/2)
	for i := 0; i < length; i += 2 {
		units = append(units, uint16(raw[4+i])|uint16(raw[5+i])<<8)
	}
	return string(utf16.Decode(units)), nil
}

// jp108EnsureProfile gives the keyboard a profile to hold mappings in if it
// has none. The vendor's own flow names a profile before it writes one.
func (s *DeviceSession) jp108EnsureProfile(ctx context.Context) error {
	name, err := s.JP108ReadProfileName(ctx)
	if err != nil || name != "" {
		return err
	}
	row, err := s.ensureCommandAllowed(CommandJp108WriteProfileName)
	if err != nil {
		return err
	}
	encoded := utf16.Encode([]rune(jp108DefaultProfileName))
	payload := append([]byte(nil), row.Request...)
	payload[2] = byte(len(encoded) * 2)
	for i, unit := range encoded {
		payload[4+i*2], payload[5+i*2] = byte(unit), byte(unit>>8)
	}
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// JP108WriteDedicatedMapping assigns one of the JP108's dedicated buttons
// (index 0-9: A, B, K1..K8) to a keyboard key. Usage 0 unassigns it.
func (s *DeviceSession) JP108WriteDedicatedMapping(ctx context.Context, index byte, targetHIDUsage uint16) error {
	row, err := s.ensureCommandAllowed(CommandJp108WriteDedicatedMapping)
	if err != nil {
		return err
	}
	if int(index) >= len(jp108KeyIDs) {
		return errInvalidInput("JP108 button index %d out of range", index)
	}
	if targetHIDUsage > 0xff {
		return errInvalidInput("JP108 key usage %#04x out of range", targetHIDUsage)
	}
	if err := s.jp108EnsureProfile(ctx); err != nil {
		return err
	}
	payload := append([]byte(nil), row.Request...)
	payload[8] = jp108KeyIDs[index]
	payload[9] = jp108TypeKeyboard
	payload[10], payload[11] = jp108Value(targetHIDUsage)
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// U2GetCurrentSlot reads the Ultimate2 device's active config slot.
func (s *DeviceSession) U2GetCurrentSlot(ctx context.Context) (byte, error) {
	resp, err := s.SendCommand(ctx, CommandU2GetCurrentSlot, nil)
	if err != nil {
		return 0, err
	}
	return byte(resp.ParsedFields["slot"]), nil
}

// U2ReadConfigSlot reads a raw Ultimate2 config-slot blob.
func (s *DeviceSession) U2ReadConfigSlot(ctx context.Context, slot byte) ([]byte, error) {
	row, err := s.ensureCommandAllowed(CommandU2ReadConfigSlot)
	if err != nil {
		return nil, err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) > 4 {
		payload[4] = slot
	}
	resp, err := s.sendRow(ctx, row, payload)
	if err != nil {
		return nil, err
	}
	return resp.Raw, nil
}

// U2WriteConfigSlot writes a raw Ultimate2 config-slot blob.
func (s *DeviceSession) U2WriteConfigSlot(ctx context.Context, slot byte, configBlob []byte) error {
	row, err := s.ensureCommandAllowed(CommandU2WriteConfigSlot)
	if err != nil {
		return err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) < 8 {
		return errInvalidInput("U2WriteConfigSlot payload shorter than expected")
	}
	payload[4] = slot
	copyLen := min(len(configBlob), max(len(payload)-8, 0))
	if copyLen > 0 {
		copy(payload[8:8+copyLen], configBlob[:copyLen])
	}
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// IndexedUsage is one (button index, HID usage) mapping entry — used by
// JP108's dedicated mapping, which really does use raw HID usage codes.
type IndexedUsage struct {
	Index byte
	Usage uint16
}

// IndexedFunction is one (slot index, function bitmask) entry in the
// Ultimate2 button-map wire structure — parallel to IndexedUsage but for
// U2's confirmed uint32 single-bit-function-catalog encoding, not a raw HID
// usage code. See docs/clean-room-evidence/dossiers/6012/u2_core.toml.
type IndexedFunction struct {
	Index    byte
	Function uint32
}

// U2ReadButtonMap would read the Ultimate2 button map for a slot, but is
// hard-blocked against real hardware and performs zero HID I/O — see
// errU2ButtonMapChunkingUnconfirmed for why. Kept as a method (rather than
// removed outright) so callers and this type's shape stay ready for the day
// chunking is confirmed and this can be unblocked.
func (s *DeviceSession) U2ReadButtonMap(_ context.Context, _ byte) ([]IndexedFunction, error) {
	return nil, errU2ButtonMapChunkingUnconfirmed()
}

// U2WriteButtonMap would write a set of Ultimate2 button-map entries for a
// slot, but is hard-blocked against real hardware and performs zero HID
// I/O — see errU2ButtonMapChunkingUnconfirmed for why. A write using
// unconfirmed chunking assumptions could corrupt a real device's
// persistent button-map configuration.
func (s *DeviceSession) U2WriteButtonMap(_ context.Context, _ byte, _ []IndexedFunction) error {
	return errU2ButtonMapChunkingUnconfirmed()
}

// U2SetMode writes a new Ultimate2 mode.
func (s *DeviceSession) U2SetMode(ctx context.Context, mode byte) (ModeState, error) {
	row, err := s.ensureCommandAllowed(CommandU2SetMode)
	if err != nil {
		return ModeState{}, err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) < 5 {
		return ModeState{}, errInvalidInput("U2SetMode payload shorter than expected")
	}
	payload[4] = mode
	if _, err := s.sendRow(ctx, row, payload); err != nil {
		return ModeState{}, err
	}
	return ModeState{Mode: mode, Source: "U2SetMode"}, nil
}
