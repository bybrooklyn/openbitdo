package core

import (
	"context"
	"fmt"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// JP108ReadDedicatedMapping reads the JP108 dedicated-button mapping table.
func (c *OpenBitdoCore) JP108ReadDedicatedMapping(ctx context.Context, vidPid protocol.VidPid) ([]DedicatedButtonMapping, error) {
	p := protocol.DeviceProfileFor(vidPid)
	if !p.Capability.SupportsJP108DedicatedMap {
		return nil, errPolicyDenied(ReasonUnsupportedPid, "JP108 dedicated mapping is not supported for %s", vidPid)
	}
	if c.config.MockMode {
		return defaultJP108Mappings(), nil
	}

	session, err := c.openSessionForOps(ctx, vidPid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()
	wire, err := session.JP108ReadDedicatedMappings(ctx)
	if err != nil {
		return nil, errProtocol(err)
	}
	out := make([]DedicatedButtonMapping, 0, len(wire))
	for _, entry := range wire {
		if button, ok := DedicatedButtonFromWireIndex(entry.Index); ok {
			out = append(out, DedicatedButtonMapping{Button: button, TargetHIDUsage: entry.Usage})
		}
	}
	return out, nil
}

// JP108ApplyDedicatedMapping applies changes and returns the backup ID (if
// backup was requested), or an error describing whatever recovery already
// did on failure.
func (c *OpenBitdoCore) JP108ApplyDedicatedMapping(ctx context.Context, vidPid protocol.VidPid, changes []DedicatedButtonMapping, backup bool) (ConfigBackupID, bool, error) {
	report, err := c.JP108ApplyDedicatedMappingWithRecovery(ctx, vidPid, changes, backup)
	if err != nil {
		return "", false, err
	}
	if report.WriteApplied {
		return report.BackupID, report.HasBackupID, nil
	}
	if report.RollbackFailed() {
		msg := report.RollbackError
		if msg == "" {
			msg = "write failed and rollback failed"
		}
		return "", false, errInvalidState("%s", msg)
	}
	msg := report.WriteError
	if msg == "" {
		msg = "write failed; rollback restored previous state"
	}
	return "", false, errInvalidState("%s", msg)
}

// JP108ApplyDedicatedMappingWithRecovery applies changes with a
// backup-then-write-then-rollback-on-failure pattern.
func (c *OpenBitdoCore) JP108ApplyDedicatedMappingWithRecovery(ctx context.Context, vidPid protocol.VidPid, changes []DedicatedButtonMapping, backup bool) (WriteRecoveryReport, error) {
	p := protocol.DeviceProfileFor(vidPid)
	if !p.Capability.SupportsJP108DedicatedMap {
		return WriteRecoveryReport{}, errPolicyDenied(ReasonUnsupportedPid, "JP108 dedicated mapping is not supported for %s", vidPid)
	}

	if c.config.MockMode {
		report := WriteRecoveryReport{WriteApplied: true}
		if backup {
			report.BackupID = c.storeBackup(vidPid, configBackupPayload{kind: backupJP108, jp108Mappings: defaultJP108Mappings()})
			report.HasBackupID = true
		}
		return report, nil
	}

	var backupID ConfigBackupID
	hasBackup := false
	if backup {
		existing, err := c.JP108ReadDedicatedMapping(ctx, vidPid)
		if err != nil {
			return WriteRecoveryReport{}, err
		}
		backupID = c.storeBackup(vidPid, configBackupPayload{kind: backupJP108, jp108Mappings: existing})
		hasBackup = true
	}

	session, err := c.openSessionForOps(ctx, vidPid)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	var applyErr error
	for _, change := range changes {
		if applyErr = session.JP108WriteDedicatedMapping(ctx, change.Button.WireIndex(), change.TargetHIDUsage); applyErr != nil {
			break
		}
	}
	_ = session.Close()

	if applyErr == nil {
		return WriteRecoveryReport{BackupID: backupID, HasBackupID: hasBackup, WriteApplied: true}, nil
	}
	return c.rollbackAfterWriteFailure(ctx, backupID, hasBackup, applyErr)
}

func (c *OpenBitdoCore) rollbackAfterWriteFailure(ctx context.Context, backupID ConfigBackupID, hasBackup bool, writeErr error) (WriteRecoveryReport, error) {
	writeErrText := writeErr.Error()
	if !hasBackup {
		return WriteRecoveryReport{WriteApplied: false, WriteError: writeErrText}, nil
	}
	if rollbackErr := c.RestoreBackup(ctx, backupID); rollbackErr != nil {
		return WriteRecoveryReport{
			BackupID: backupID, HasBackupID: true, WriteApplied: false,
			RollbackAttempted: true, RollbackSucceeded: false,
			WriteError: writeErrText, RollbackError: rollbackErr.Error(),
		}, nil
	}
	return WriteRecoveryReport{
		BackupID: backupID, HasBackupID: true, WriteApplied: false,
		RollbackAttempted: true, RollbackSucceeded: true, WriteError: writeErrText,
	}, nil
}

// U2ReadCoreProfile reads the Ultimate2 device's mode, firmware version,
// button map, and L2/R2 analog config for its active slot.
func (c *OpenBitdoCore) U2ReadCoreProfile(ctx context.Context, vidPid protocol.VidPid, slot U2SlotID) (U2CoreProfile, error) {
	p := protocol.DeviceProfileFor(vidPid)
	if !(p.Capability.SupportsU2SlotConfig && p.Capability.SupportsU2ButtonMap) {
		return U2CoreProfile{}, errPolicyDenied(ReasonUnsupportedPid, "Ultimate2 core profile is not supported for %s", vidPid)
	}

	if c.config.MockMode {
		return U2CoreProfile{
			Slot: slot, Mode: 0, FirmwareVersion: "mock-1.0.0", L2Analog: 0.5, R2Analog: 0.5,
			SupportsTriggerWrite: true, Mappings: defaultU2Mappings(),
		}, nil
	}

	session, err := c.openSessionForOps(ctx, vidPid)
	if err != nil {
		return U2CoreProfile{}, err
	}
	defer func() { _ = session.Close() }()

	activeSlot := slot
	if wireSlot, slotErr := session.U2GetCurrentSlot(ctx); slotErr == nil {
		activeSlot = U2SlotFromWireValue(wireSlot)
	}
	mode, err := session.GetMode(ctx)
	if err != nil {
		return U2CoreProfile{}, errProtocol(err)
	}
	firmwareVersion := "unknown"
	if resp, verErr := session.SendCommand(ctx, protocol.CommandGetControllerVersion, nil); verErr == nil {
		if raw, ok := resp.ParsedFields["version_x100"]; ok {
			firmwareVersion = formatFirmwareVersionDecimal(raw)
		}
	}
	configBlob, err := session.U2ReadConfigSlot(ctx, activeSlot.WireValue())
	if err != nil {
		return U2CoreProfile{}, errProtocol(err)
	}
	wireMap, err := session.U2ReadButtonMap(ctx, activeSlot.WireValue())
	if err != nil {
		return U2CoreProfile{}, errProtocol(err)
	}
	mappings := make([]U2ButtonMapping, 0, len(wireMap))
	for _, entry := range wireMap {
		if button, ok := U2ButtonFromWireIndex(entry.Index); ok {
			mappings = append(mappings, U2ButtonMapping{Button: button, TargetHIDUsage: entry.Usage})
		}
	}

	var l2, r2 float32
	if len(configBlob) > 6 {
		l2 = float32(configBlob[6]) / 255.0
	}
	if len(configBlob) > 7 {
		r2 = float32(configBlob[7]) / 255.0
	}
	return U2CoreProfile{
		Slot: activeSlot, Mode: mode.Mode, FirmwareVersion: firmwareVersion, L2Analog: l2, R2Analog: r2,
		SupportsTriggerWrite: p.SupportTier == protocol.TierFull, Mappings: mappings,
	}, nil
}

func formatFirmwareVersionDecimal(raw uint32) string {
	return fmt.Sprintf("%d.%02d", raw/100, raw%100)
}

// U2ApplyCoreProfile applies mode/mapping/analog changes and returns the
// backup ID (if requested).
func (c *OpenBitdoCore) U2ApplyCoreProfile(ctx context.Context, vidPid protocol.VidPid, slot U2SlotID, mode byte, mapChanges []U2ButtonMapping, l2Analog, r2Analog float32, backup bool) (ConfigBackupID, bool, error) {
	report, err := c.U2ApplyCoreProfileWithRecovery(ctx, vidPid, slot, mode, mapChanges, l2Analog, r2Analog, backup)
	if err != nil {
		return "", false, err
	}
	if report.WriteApplied {
		return report.BackupID, report.HasBackupID, nil
	}
	if report.RollbackFailed() {
		msg := report.RollbackError
		if msg == "" {
			msg = "write failed and rollback failed"
		}
		return "", false, errInvalidState("%s", msg)
	}
	msg := report.WriteError
	if msg == "" {
		msg = "write failed; rollback restored previous state"
	}
	return "", false, errInvalidState("%s", msg)
}

// U2ApplyCoreProfileWithRecovery applies mode/mapping/analog changes with a
// backup-then-write-then-rollback-on-failure pattern.
func (c *OpenBitdoCore) U2ApplyCoreProfileWithRecovery(ctx context.Context, vidPid protocol.VidPid, slot U2SlotID, mode byte, mapChanges []U2ButtonMapping, l2Analog, r2Analog float32, backup bool) (WriteRecoveryReport, error) {
	p := protocol.DeviceProfileFor(vidPid)
	if !(p.Capability.SupportsU2SlotConfig && p.Capability.SupportsU2ButtonMap) {
		return WriteRecoveryReport{}, errPolicyDenied(ReasonUnsupportedPid, "Ultimate2 core profile is not supported for %s", vidPid)
	}

	if c.config.MockMode {
		report := WriteRecoveryReport{WriteApplied: true}
		if backup {
			report.BackupID = c.storeBackup(vidPid, configBackupPayload{
				kind: backupU2,
				u2Profile: U2CoreProfile{
					Slot: slot, FirmwareVersion: "mock-1.0.0", L2Analog: 0.5, R2Analog: 0.5,
					SupportsTriggerWrite: true, Mappings: defaultU2Mappings(),
				},
				u2ConfigBlob: make([]byte, 32),
			})
			report.HasBackupID = true
		}
		return report, nil
	}

	var backupID ConfigBackupID
	hasBackup := false
	if backup {
		current, err := c.U2ReadCoreProfile(ctx, vidPid, slot)
		if err != nil {
			return WriteRecoveryReport{}, err
		}
		session, err := c.openSessionForOps(ctx, vidPid)
		if err != nil {
			return WriteRecoveryReport{}, err
		}
		configBlob, blobErr := session.U2ReadConfigSlot(ctx, slot.WireValue())
		_ = session.Close()
		if blobErr != nil {
			return WriteRecoveryReport{}, errProtocol(blobErr)
		}
		backupID = c.storeBackup(vidPid, configBackupPayload{kind: backupU2, u2Profile: current, u2ConfigBlob: configBlob})
		hasBackup = true
	}

	session, err := c.openSessionForOps(ctx, vidPid)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	applyErr := u2ApplyWrite(ctx, session, slot, mode, mapChanges, l2Analog, r2Analog)
	_ = session.Close()

	if applyErr == nil {
		return WriteRecoveryReport{BackupID: backupID, HasBackupID: hasBackup, WriteApplied: true}, nil
	}
	return c.rollbackAfterWriteFailure(ctx, backupID, hasBackup, applyErr)
}

func u2ApplyWrite(ctx context.Context, session *protocol.DeviceSession, slot U2SlotID, mode byte, mapChanges []U2ButtonMapping, l2Analog, r2Analog float32) error {
	if _, err := session.U2SetMode(ctx, mode); err != nil {
		return err
	}
	wireMap := make([]protocol.IndexedUsage, 0, len(mapChanges))
	for _, entry := range mapChanges {
		wireMap = append(wireMap, protocol.IndexedUsage{Index: entry.Button.WireIndex(), Usage: entry.TargetHIDUsage})
	}
	if err := session.U2WriteButtonMap(ctx, slot.WireValue(), wireMap); err != nil {
		return err
	}
	configBlob, err := session.U2ReadConfigSlot(ctx, slot.WireValue())
	if err != nil {
		return err
	}
	if len(configBlob) == 0 {
		configBlob = make([]byte, 16)
	}
	if len(configBlob) > 6 {
		configBlob[4] = slot.WireValue()
		configBlob[5] = mode
		if len(configBlob) > 8 {
			configBlob[6] = clampToByte(l2Analog)
			configBlob[7] = clampToByte(r2Analog)
		}
	}
	return session.U2WriteConfigSlot(ctx, slot.WireValue(), configBlob)
}

func clampToByte(v float32) byte {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return byte(v*255.0 + 0.5)
}

// RestoreBackup replays a stored backup's payload back onto its target device.
func (c *OpenBitdoCore) RestoreBackup(ctx context.Context, backupID ConfigBackupID) error {
	c.backupsMu.RLock()
	backup, ok := c.backups[backupID]
	c.backupsMu.RUnlock()
	if !ok {
		return errNotFound("unknown backup id: %s", backupID)
	}

	if c.config.MockMode {
		return nil
	}

	session, err := c.openSessionForOps(ctx, backup.target)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()

	switch backup.payload.kind {
	case backupJP108:
		for _, entry := range backup.payload.jp108Mappings {
			if err := session.JP108WriteDedicatedMapping(ctx, entry.Button.WireIndex(), entry.TargetHIDUsage); err != nil {
				return errProtocol(err)
			}
		}
	case backupU2:
		profile := backup.payload.u2Profile
		if _, err := session.U2SetMode(ctx, profile.Mode); err != nil {
			return errProtocol(err)
		}
		wireMap := make([]protocol.IndexedUsage, 0, len(profile.Mappings))
		for _, entry := range profile.Mappings {
			wireMap = append(wireMap, protocol.IndexedUsage{Index: entry.Button.WireIndex(), Usage: entry.TargetHIDUsage})
		}
		if err := session.U2WriteButtonMap(ctx, profile.Slot.WireValue(), wireMap); err != nil {
			return errProtocol(err)
		}
		if err := session.U2WriteConfigSlot(ctx, profile.Slot.WireValue(), backup.payload.u2ConfigBlob); err != nil {
			return errProtocol(err)
		}
	}
	return nil
}
