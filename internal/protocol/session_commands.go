package protocol

import "context"

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

// JP108ReadDedicatedMappings reads the JP108 dedicated-button mapping table.
func (s *DeviceSession) JP108ReadDedicatedMappings(ctx context.Context) ([]IndexedUsage, error) {
	resp, err := s.SendCommand(ctx, CommandJp108ReadDedicatedMappings, nil)
	if err != nil {
		return nil, err
	}
	return parseIndexedU16Table(resp.Raw, 10), nil
}

// JP108WriteDedicatedMapping writes one JP108 dedicated-button mapping entry.
func (s *DeviceSession) JP108WriteDedicatedMapping(ctx context.Context, index byte, targetHIDUsage uint16) error {
	row, err := s.ensureCommandAllowed(CommandJp108WriteDedicatedMapping)
	if err != nil {
		return err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) < 7 {
		return errInvalidInput("Jp108WriteDedicatedMapping payload shorter than expected")
	}
	payload[4] = index
	payload[5] = byte(targetHIDUsage)
	payload[6] = byte(targetHIDUsage >> 8)
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
