package protocol

// ensureCommandAllowed is the 4-gate authorization check every command must
// pass before it is ever written to the wire:
//  1. confidence/runtime-policy gate (experimental / blocked-until-confirmed)
//  2. family/capability/PID applicability
//  3. support-tier restriction (candidate-readonly whitelist)
//  4. explicit unsafe double-confirmation (allow_unsafe && brick_risk_ack)
func (s *DeviceSession) ensureCommandAllowed(command CommandID) (CommandRow, error) {
	row, ok := FindCommand(command)
	if !ok {
		return CommandRow{}, errUnknownCommand(command)
	}
	promotedFullSupportPath := s.allowPidScopedFullSupportPath(row)
	candidateWriteUnlock := s.allowCandidateRuntimeWritePath(command, row.SafetyClass)

	switch row.RuntimePolicy() {
	case EnabledDefault:
	case ExperimentalGate:
		if !s.config.Experimental && !promotedFullSupportPath {
			return CommandRow{}, errExperimentalRequired(command)
		}
	case BlockedUntilConfirmed:
		if !promotedFullSupportPath && !candidateWriteUnlock {
			return CommandRow{}, errUnsupportedForPid(command, s.target.PID)
		}
	}

	if !isCommandAllowedForDevice(s.target, s.profile.ProtocolFamily, command) ||
		!isCommandAllowedByCapability(s.profile.Capability, command) ||
		!CommandAppliesToPID(row, s.target.PID) {
		return CommandRow{}, errUnsupportedForPid(command, s.target.PID)
	}

	if s.profile.SupportTier == TierCandidateReadOnly &&
		!isCommandAllowedForCandidatePID(s.target.PID, command, row.SafetyClass, candidateWriteUnlock) {
		return CommandRow{}, errUnsupportedForPid(command, s.target.PID)
	}

	if row.SafetyClass.IsUnsafe() {
		if s.profile.SupportTier != TierFull {
			return CommandRow{}, errUnsupportedForPid(command, s.target.PID)
		}
		if !s.config.AllowUnsafe || !s.config.BrickRiskAck {
			return CommandRow{}, errUnsafeCommandDenied(command)
		}
	}

	if row.SafetyClass == SafeWrite && s.profile.SupportTier != TierFull && !candidateWriteUnlock {
		return CommandRow{}, errUnsupportedForPid(command, s.target.PID)
	}

	return row, nil
}

func (s *DeviceSession) allowPidScopedFullSupportPath(row CommandRow) bool {
	if s.profile.SupportTier != TierFull || len(row.AppliesTo) == 0 {
		return false
	}
	applies := false
	for _, p := range row.AppliesTo {
		if p == s.target.PID {
			applies = true
			break
		}
	}
	if !applies {
		return false
	}
	return row.OperationGroup == "JP108Dedicated" || row.OperationGroup == "Ultimate2Core" || row.OperationGroup == "Firmware"
}

var candidateUnlockableWrites = map[CommandID]bool{
	CommandSetModeDInput:              true,
	CommandWriteProfile:               true,
	CommandU2RecordWrite:              true,
	CommandU2Commit:                   true,
	CommandU2SetLightEffect:           true,
	CommandU2MacroWrite:               true,
	CommandU2MacroErase:               true,
	CommandJp108WriteDedicatedMapping: true,
	CommandJp108WriteFeatureFlags:     true,
	CommandJp108WriteVoice:            true,
	CommandJp108WriteMacroName:        true,
	CommandJp108WriteMacroValue:       true,
	CommandJp108ClearMacro:            true,
	CommandKbRecordWrite:              true,
	CommandKbRecordMacroErase:         true,
	CommandKbRecordMacroWrite:         true,
	CommandKbRecordLightsBegin:        true,
	CommandKbRecordLightsWrite:        true,
	CommandMouseWriteProfileName:      true,
	CommandMouseClearProfile:          true,
	CommandMouseWriteButton:           true,
	CommandMouseWriteLeftHanded:       true,
	CommandMouseWriteLiftOff:          true,
	CommandMouseWriteWheelSpeed:       true,
	CommandMouseWriteWheelDirection:   true,
	CommandMouseWriteDpi:              true,
	CommandMouseWriteDpiStage:         true,
	CommandMouseWritePollingRate:      true,
	CommandMouseWriteMacro:            true,
	CommandMouseRecordWrite:           true,
}

func (s *DeviceSession) allowCandidateRuntimeWritePath(command CommandID, safety SafetyClass) bool {
	return s.profile.SupportTier == TierCandidateReadOnly &&
		s.config.CandidateWriteUnlock &&
		s.config.Experimental &&
		safety == SafeWrite &&
		candidateUnlockableWrites[command]
}
