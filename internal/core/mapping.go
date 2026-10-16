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
	if applyErr == nil {
		applyErr = verifyJP108Readback(ctx, session, changes)
	}
	_ = session.Close()

	if applyErr == nil {
		return WriteRecoveryReport{BackupID: backupID, HasBackupID: hasBackup, WriteApplied: true}, nil
	}
	return c.rollbackAfterWriteFailure(ctx, backupID, hasBackup, applyErr)
}

// verifyJP108Readback re-reads the mapping table after a write and confirms
// every changed entry holds the value that was sent. The device acknowledges
// a write with a bare status byte, which says the frame arrived, not that
// the mapping took.
func verifyJP108Readback(ctx context.Context, session *protocol.DeviceSession, changes []DedicatedButtonMapping) error {
	table, err := session.JP108ReadDedicatedMappings(ctx)
	if err != nil {
		return fmt.Errorf("readback after write failed: %w", err)
	}
	stored := make(map[byte]uint16, len(table))
	for _, entry := range table {
		stored[entry.Index] = entry.Usage
	}
	for _, change := range changes {
		index := change.Button.WireIndex()
		if got, ok := stored[index]; !ok || got != change.TargetHIDUsage {
			return fmt.Errorf("readback mismatch for %s: wrote %#04x, device holds %#04x", change.Button, change.TargetHIDUsage, got)
		}
	}
	return nil
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
	if backup.payload.kind == backupPad {
		return c.restorePadBackup(ctx, PadAddress{Enumerated: backup.target, Product: backup.payload.padProduct}, backup.payload.padRecord, backup.payload.padMacros)
	}

	if backup.payload.kind == backupRecordKeyboard {
		return c.restoreKbRecordBackup(ctx, backup.target, backup.payload.recordKeyboard)
	}
	if backup.payload.kind == backupMouse {
		return c.restoreMouseBackup(ctx, backup.target, backup.payload.mouse)
	}

	session, err := c.openSessionForOps(ctx, backup.target)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()

	switch backup.payload.kind {
	case backupKeyboard:
		return restoreKeyboardBackup(ctx, session, keyboardLayoutFor(backup.target), backup.payload.keyboard)
	case backupJP108:
		for _, entry := range backup.payload.jp108Mappings {
			if err := session.JP108WriteDedicatedMapping(ctx, entry.Button.WireIndex(), entry.TargetHIDUsage); err != nil {
				return errProtocol(err)
			}
		}
	}
	return nil
}
