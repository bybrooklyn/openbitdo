package protocol

import "context"

// EnterBootloader dispatches to the JP108/U2/generic 3-stage bootloader-enter
// sequence based on the device's capability.
func (s *DeviceSession) EnterBootloader(ctx context.Context) error {
	switch {
	case s.usesJP108FirmwarePath():
		_, err := s.SendCommand(ctx, CommandJp108EnterBootloader, nil)
		return err
	case s.usesU2FirmwarePath():
		_, err := s.SendCommand(ctx, CommandU2EnterBootloader, nil)
		return err
	}
	if _, err := s.SendCommand(ctx, CommandEnterBootloaderA, nil); err != nil {
		return err
	}
	if _, err := s.SendCommand(ctx, CommandEnterBootloaderB, nil); err != nil {
		return err
	}
	_, err := s.SendCommand(ctx, CommandEnterBootloaderC, nil)
	return err
}

// FirmwareTransfer chunks image and sends it (unless dryRun), then commits.
func (s *DeviceSession) FirmwareTransfer(ctx context.Context, image []byte, chunkSize int, dryRun bool) (FirmwareTransferReport, error) {
	if chunkSize == 0 {
		return FirmwareTransferReport{}, errInvalidInput("chunk size must be greater than zero")
	}

	command := s.firmwareChunkCommand()
	maxPayload := 0
	if row, ok := FindCommand(command); ok {
		maxPayload = max(len(row.Request)-firmwareChunkOffset(command), 0)
	}
	if maxPayload == 0 || chunkSize > maxPayload {
		return FirmwareTransferReport{}, errInvalidInput("chunk size %d exceeds firmware payload limit %d", chunkSize, maxPayload)
	}

	chunkCount := (len(image) + chunkSize - 1) / chunkSize
	if dryRun {
		return FirmwareTransferReport{BytesTotal: len(image), ChunkSize: chunkSize, ChunksSent: chunkCount, DryRun: true}, nil
	}

	for offset := 0; offset < len(image); offset += chunkSize {
		end := min(offset+chunkSize, len(image))
		if _, err := s.SendFirmwareChunk(ctx, image[offset:end]); err != nil {
			return FirmwareTransferReport{}, err
		}
	}
	if err := s.FirmwareCommit(ctx); err != nil {
		return FirmwareTransferReport{}, err
	}
	return FirmwareTransferReport{BytesTotal: len(image), ChunkSize: chunkSize, ChunksSent: chunkCount, DryRun: false}, nil
}

// SendFirmwareChunk sends one firmware chunk and returns the bytes copied
// into the wire payload.
func (s *DeviceSession) SendFirmwareChunk(ctx context.Context, chunk []byte) (int, error) {
	command := s.firmwareChunkCommand()
	row, err := s.ensureCommandAllowed(command)
	if err != nil {
		return 0, err
	}
	payload := append([]byte(nil), row.Request...)
	offset := firmwareChunkOffset(command)
	copyLen := min(len(chunk), max(len(payload)-offset, 0))
	if copyLen == 0 {
		return 0, errInvalidInput("firmware chunk payload shorter than expected for %s", command)
	}
	copy(payload[offset:offset+copyLen], chunk[:copyLen])
	if _, err := s.sendRow(ctx, row, payload); err != nil {
		return 0, err
	}
	return copyLen, nil
}

// FirmwareCommit sends the firmware-commit command for this device's path.
func (s *DeviceSession) FirmwareCommit(ctx context.Context) error {
	_, err := s.SendCommand(ctx, s.firmwareCommitCommand(), nil)
	return err
}

// ExitBootloader dispatches to the JP108/U2/generic bootloader-exit command.
func (s *DeviceSession) ExitBootloader(ctx context.Context) error {
	switch {
	case s.usesJP108FirmwarePath():
		_, err := s.SendCommand(ctx, CommandJp108ExitBootloader, nil)
		return err
	case s.usesU2FirmwarePath():
		_, err := s.SendCommand(ctx, CommandU2ExitBootloader, nil)
		return err
	}
	_, err := s.SendCommand(ctx, CommandExitBootloader, nil)
	return err
}

func (s *DeviceSession) usesJP108FirmwarePath() bool {
	return s.profile.Capability.SupportsJP108DedicatedMap
}

func (s *DeviceSession) usesU2FirmwarePath() bool {
	return s.profile.Capability.SupportsU2SlotConfig && s.profile.Capability.SupportsU2ButtonMap
}

func (s *DeviceSession) firmwareChunkCommand() CommandID {
	switch {
	case s.usesJP108FirmwarePath():
		return CommandJp108FirmwareChunk
	case s.usesU2FirmwarePath():
		return CommandU2FirmwareChunk
	default:
		return CommandFirmwareChunk
	}
}

func (s *DeviceSession) firmwareCommitCommand() CommandID {
	switch {
	case s.usesJP108FirmwarePath():
		return CommandJp108FirmwareCommit
	case s.usesU2FirmwarePath():
		return CommandU2FirmwareCommit
	default:
		return CommandFirmwareCommit
	}
}

func firmwareChunkOffset(command CommandID) int {
	if command == CommandJp108FirmwareChunk || command == CommandU2FirmwareChunk {
		return 5
	}
	return 4
}
