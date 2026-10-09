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

// IndexedUsage is one (button index, HID usage) mapping entry — used by
// JP108's dedicated mapping, which really does use raw HID usage codes.
type IndexedUsage struct {
	Index byte
	Usage uint16
}
