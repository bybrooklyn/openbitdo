package protocol

import (
	"encoding/binary"
	"fmt"
)

func formatDetectedPid(pid uint32) string { return fmt.Sprintf("detected pid %#04x", pid) }
func formatRevision(rev uint32) string    { return fmt.Sprintf("report revision %d", rev) }
func formatMode(mode uint32) string       { return fmt.Sprintf("mode %d", mode) }
func formatSlot(slot uint32) string       { return fmt.Sprintf("current slot %d", slot) }

func formatFirmwareVersion(versionX100 uint32, beta *uint32) string {
	base := fmt.Sprintf("firmware %d.%02d", versionX100/100, versionX100%100)
	if beta == nil {
		return base
	}
	return fmt.Sprintf("%s beta=%d", base, *beta)
}

// classifyDiagFailure decides how urgently a failed diagnostic check should
// be surfaced. Escalation to NeedsAttention is intentionally narrow for
// inferred/experimental checks: identity/transition mismatches, or an
// unsupported error for a command that genuinely applies to this PID.
func classifyDiagFailure(command CommandID, policy RuntimePolicy, confidence SupportEvidence, code ErrorCode, pid uint16) DiagSeverity {
	if policy != ExperimentalGate || confidence != EvidenceInferred {
		return SeverityWarning
	}

	identityOrTransitionIssue := false
	switch {
	case command == CommandGetPid && (code == CodeInvalidResponse || code == CodeMalformedResponse):
		identityOrTransitionIssue = true
	case command == CommandGetMode && code == CodeInvalidResponse:
		identityOrTransitionIssue = true
	case command == CommandGetModeAlt && code == CodeInvalidResponse:
		identityOrTransitionIssue = true
	case command == CommandReadProfile && code == CodeInvalidResponse:
		identityOrTransitionIssue = true
	case command == CommandGetControllerVersion && code == CodeInvalidResponse:
		identityOrTransitionIssue = true
	case command == CommandVersion && code == CodeInvalidResponse:
		identityOrTransitionIssue = true
	}
	if identityOrTransitionIssue {
		return SeverityNeedsAttention
	}

	if code == CodeUnsupportedForPid {
		if row, ok := FindCommand(command); ok && CommandAppliesToPID(row, pid) {
			return SeverityNeedsAttention
		}
	}

	return SeverityWarning
}

var baseDiagReads = map[CommandID]bool{
	CommandGetPid: true, CommandGetReportRevision: true, CommandGetControllerVersion: true,
	CommandVersion: true, CommandIdle: true,
}

var standardCandidatePIDs = standardCandidateReadDiagPIDs // same set, reused for gate 3

var jpCandidatePIDs = jpCandidateDiagPIDs // same set, reused for gate 3

var standardCandidateReadCommands = map[CommandID]bool{
	CommandGetMode: true, CommandGetModeAlt: true, CommandReadProfile: true,
	CommandU2GetCurrentSlot: true, CommandU2ReadConfigSlot: true, CommandU2ReadButtonMap: true,
}

var jpCandidateReadCommands = map[CommandID]bool{
	CommandJp108ReadDedicatedMappings: true, CommandJp108ReadFeatureFlags: true, CommandJp108ReadVoice: true,
}

// isCommandAllowedForCandidatePID is gate 3 (support-tier restriction) for
// candidate-readonly devices: a fixed read whitelist, plus writes only
// through the write-unlock ceremony.
func isCommandAllowedForCandidatePID(pid uint16, command CommandID, safety SafetyClass, writeUnlocked bool) bool {
	if safety == SafeWrite {
		return writeUnlocked &&
			(standardCandidatePIDs[pid] || jpCandidatePIDs[pid] || pidWithSlotConfigCandidate[pid]) &&
			candidateUnlockableWrites[command]
	}
	if safety != SafeRead {
		return false
	}

	if baseDiagReads[command] {
		return standardCandidatePIDs[pid] || jpCandidatePIDs[pid] || pidWithSlotConfigCandidate[pid]
	}
	if standardCandidatePIDs[pid] || pidWithSlotConfigCandidate[pid] {
		return standardCandidateReadCommands[command]
	}
	if jpCandidatePIDs[pid] {
		return jpCandidateReadCommands[command]
	}
	return false
}

func isCommandAllowedByCapability(cap PidCapability, command CommandID) bool {
	switch command {
	case CommandGetPid, CommandGetReportRevision, CommandGetControllerVersion, CommandVersion, CommandIdle, CommandGetSuperButton:
		return true
	case CommandGetMode, CommandGetModeAlt, CommandSetModeDInput:
		return cap.SupportsMode
	case CommandReadProfile, CommandWriteProfile:
		return cap.SupportsProfileRW
	case CommandEnterBootloaderA, CommandEnterBootloaderB, CommandEnterBootloaderC, CommandExitBootloader,
		CommandJp108EnterBootloader, CommandJp108ExitBootloader, CommandU2EnterBootloader, CommandU2ExitBootloader:
		return cap.SupportsBoot
	case CommandFirmwareChunk, CommandFirmwareCommit, CommandJp108FirmwareChunk, CommandJp108FirmwareCommit,
		CommandU2FirmwareChunk, CommandU2FirmwareCommit:
		return cap.SupportsFirmware
	case CommandJp108ReadDedicatedMappings, CommandJp108WriteDedicatedMapping, CommandJp108ReadFeatureFlags,
		CommandJp108WriteFeatureFlags, CommandJp108ReadVoice, CommandJp108WriteVoice,
		CommandJp108ReadProfileName, CommandJp108WriteProfileName,
		CommandJp108ReadMappedKeys, CommandJp108ReadMacroList:
		return cap.SupportsJP108DedicatedMap
	case CommandU2GetCurrentSlot, CommandU2ReadConfigSlot, CommandU2WriteConfigSlot:
		return cap.SupportsU2SlotConfig
	case CommandU2ReadButtonMap, CommandU2WriteButtonMap, CommandU2SetMode:
		return cap.SupportsU2ButtonMap
	default:
		return false
	}
}

// jp108Commands are the commands a JP108 keyboard's own configuration
// interface carries: 33-byte reports with ID 0x52, answered on report 0x54.
// Nothing else is sent to one. The generic 64-byte commands the other
// families share have no report on that interface to travel in.
var jp108Commands = map[CommandID]bool{
	CommandJp108ReadDedicatedMappings: true, CommandJp108WriteDedicatedMapping: true,
	CommandJp108ReadFeatureFlags: true, CommandJp108WriteFeatureFlags: true,
	CommandJp108ReadProfileName: true, CommandJp108WriteProfileName: true,
	CommandJp108ReadMappedKeys: true, CommandJp108ReadMacroList: true,
	CommandJp108ReadVoice: true, CommandJp108WriteVoice: true,
}

// jp108PIDs are the keyboards hardware evidence exists for.
var jp108PIDs = map[uint16]bool{0x5209: true}

var jpHandshakeDisallowed = map[CommandID]bool{
	CommandSetModeDInput: true, CommandReadProfile: true, CommandWriteProfile: true,
	CommandFirmwareChunk: true, CommandFirmwareCommit: true,
	CommandU2GetCurrentSlot: true, CommandU2ReadConfigSlot: true, CommandU2WriteConfigSlot: true,
	CommandU2ReadButtonMap: true, CommandU2WriteButtonMap: true, CommandU2SetMode: true,
	CommandU2EnterBootloader: true, CommandU2FirmwareChunk: true, CommandU2FirmwareCommit: true,
	CommandU2ExitBootloader: true,
}

var unknownFamilyAllowed = map[CommandID]bool{
	CommandGetPid: true, CommandGetReportRevision: true, CommandGetControllerVersion: true,
	CommandVersion: true, CommandIdle: true,
}

var ds4BootAllowed = map[CommandID]bool{
	CommandEnterBootloaderA: true, CommandEnterBootloaderB: true, CommandEnterBootloaderC: true,
	CommandExitBootloader: true, CommandFirmwareChunk: true, CommandFirmwareCommit: true, CommandGetPid: true,
}

// isCommandAllowedForDevice is isCommandAllowedByFamily narrowed by what is
// known about the specific device. A JP108 keyboard only takes its own
// commands (and, once firmware is enabled, its own boot/firmware ones).
func isCommandAllowedForDevice(target VidPid, family ProtocolFamily, command CommandID) bool {
	if jp108PIDs[target.PID] {
		switch command {
		case CommandJp108EnterBootloader, CommandJp108ExitBootloader, CommandJp108FirmwareChunk, CommandJp108FirmwareCommit:
			return true
		}
		return jp108Commands[command]
	}
	return isCommandAllowedByFamily(family, command)
}

func isCommandAllowedByFamily(family ProtocolFamily, command CommandID) bool {
	switch family {
	case FamilyUnknown:
		return unknownFamilyAllowed[command]
	case JpHandshake:
		return !jpHandshakeDisallowed[command]
	case DS4Boot:
		return ds4BootAllowed[command]
	default: // Standard64, DInput
		return true
	}
}

// ValidateResponse checks a raw response against command's expected
// byte-signature, per docs/spec/command_matrix.csv's expected_response column.
func ValidateResponse(command CommandID, response []byte) ResponseStatus {
	if len(response) < 2 {
		return StatusMalformed
	}

	switch command {
	case CommandJp108ReadDedicatedMappings:
		return validateJP108Reply(response, 0x83, 8)
	case CommandJp108ReadFeatureFlags:
		return validateJP108Reply(response, 0x88, 3)
	case CommandJp108ReadProfileName:
		return validateJP108Reply(response, 0x80, 3)
	case CommandJp108ReadMappedKeys:
		return validateJP108Reply(response, 0x81, 3)
	case CommandJp108ReadMacroList:
		return validateJP108Reply(response, 0x82, 3)
	case CommandJp108ReadVoice:
		return validateJP108Reply(response, 0x89, 3)
	case CommandJp108WriteDedicatedMapping, CommandJp108WriteFeatureFlags, CommandJp108WriteProfileName, CommandJp108WriteVoice:
		// Every JP108 write is acknowledged with the same two bytes.
		if len(response) < 3 {
			return StatusMalformed
		}
		if response[0] == jp108ReplyReportID && response[1] == 0xe4 && response[2] == 0x08 {
			return StatusOk
		}
		return StatusInvalid
	case CommandGetPid:
		if len(response) < 24 {
			return StatusMalformed
		}
		if response[0] == 0x02 && response[1] == 0x05 && response[4] == 0xC1 {
			return StatusOk
		}
		return StatusInvalid
	case CommandGetReportRevision:
		if len(response) < 6 {
			return StatusMalformed
		}
		if response[0] == 0x02 && response[1] == 0x04 && response[5] == 0x01 {
			return StatusOk
		}
		return StatusInvalid
	case CommandGetMode, CommandGetModeAlt:
		if len(response) < 6 {
			return StatusMalformed
		}
		if response[0] == 0x02 && response[1] == 0x05 {
			return StatusOk
		}
		return StatusInvalid
	case CommandU2GetCurrentSlot:
		if len(response) < 6 {
			return StatusMalformed
		}
		if response[0] == 0x02 && response[1] == 0x05 {
			return StatusOk
		}
		return StatusInvalid
	case CommandU2ReadConfigSlot, CommandU2ReadButtonMap:
		if len(response) < 12 {
			return StatusMalformed
		}
		if response[0] == 0x02 && response[1] == 0x05 {
			return StatusOk
		}
		return StatusInvalid
	case CommandGetControllerVersion, CommandVersion:
		if len(response) < 5 {
			return StatusMalformed
		}
		if response[0] == 0x02 && response[1] == 0x22 {
			return StatusOk
		}
		return StatusInvalid
	case CommandIdle:
		if response[0] == 0x02 {
			return StatusOk
		}
		return StatusInvalid
	case CommandEnterBootloaderA, CommandEnterBootloaderB, CommandEnterBootloaderC, CommandExitBootloader:
		return StatusOk
	default:
		if response[0] == 0x02 {
			return StatusOk
		}
		return StatusInvalid
	}
}

// jp108ReplyReportID is the input report a JP108 answers on.
const jp108ReplyReportID = 0x54

// validateJP108Reply checks a JP108 read reply: it arrives on report 0x54
// and echoes the command byte it answers.
func validateJP108Reply(response []byte, command byte, minLen int) ResponseStatus {
	if len(response) < minLen {
		return StatusMalformed
	}
	if response[0] == jp108ReplyReportID && response[1] == command {
		return StatusOk
	}
	return StatusInvalid
}

func minimumResponseLen(command CommandID) int {
	switch command {
	case CommandJp108ReadDedicatedMappings:
		return 8
	case CommandJp108ReadFeatureFlags, CommandJp108ReadProfileName, CommandJp108ReadMappedKeys,
		CommandJp108ReadMacroList, CommandJp108ReadVoice,
		CommandJp108WriteDedicatedMapping, CommandJp108WriteFeatureFlags, CommandJp108WriteProfileName, CommandJp108WriteVoice:
		return 3
	case CommandGetPid:
		return 24
	case CommandGetReportRevision:
		return 6
	case CommandGetMode, CommandGetModeAlt:
		return 6
	case CommandU2GetCurrentSlot:
		return 6
	case CommandU2ReadConfigSlot, CommandU2ReadButtonMap:
		return 12
	case CommandGetControllerVersion, CommandVersion:
		return 5
	default:
		return 2
	}
}

func parseFields(command CommandID, response []byte) map[string]uint32 {
	parsed := map[string]uint32{}
	switch {
	case command == CommandGetPid && len(response) >= 24:
		parsed["detected_pid"] = uint32(binary.LittleEndian.Uint16(response[22:24]))
	case command == CommandGetReportRevision && len(response) >= 6:
		parsed["revision"] = uint32(response[5])
		if len(response) >= 24 {
			parsed["reported_pid"] = uint32(binary.LittleEndian.Uint16(response[22:24]))
		}
	case (command == CommandGetMode || command == CommandGetModeAlt) && len(response) >= 6:
		parsed["mode"] = uint32(response[5])
	case (command == CommandGetControllerVersion || command == CommandVersion) && len(response) >= 5:
		parsed["version_x100"] = uint32(binary.LittleEndian.Uint16(response[2:4]))
		parsed["beta"] = uint32(response[4])
	case command == CommandU2GetCurrentSlot && len(response) >= 6:
		parsed["slot"] = uint32(response[5])
	case command == CommandJp108ReadDedicatedMappings && len(response) >= 8:
		parsed["key_id"] = uint32(response[2])
		parsed["mapping_type"] = uint32(response[3])
		parsed["usage"] = uint32(jp108Usage(response[4:8]))
	case command == CommandJp108ReadProfileName && len(response) >= 3:
		parsed["name_bytes"] = uint32(response[2])
	case command == CommandJp108ReadFeatureFlags && len(response) >= 3:
		parsed["flags"] = uint32(response[2])
	case command == CommandJp108ReadVoice && len(response) >= 3:
		parsed["volume"] = uint32(response[2])
	case command == CommandJp108ReadMappedKeys && len(response) >= 3:
		count := uint32(0)
		for i := 2; i+1 < len(response)-1 && response[i] != 0; i += 2 {
			count++
		}
		parsed["mapped_keys"] = count
	}
	return parsed
}

func diagSuccessDetail(command CommandID, facts map[string]uint32) string {
	switch command {
	case CommandGetPid:
		if pid, ok := facts["detected_pid"]; ok {
			return formatDetectedPid(pid)
		}
		return "ok"
	case CommandGetReportRevision:
		if rev, ok := facts["revision"]; ok {
			return formatRevision(rev)
		}
		return "ok"
	case CommandGetMode, CommandGetModeAlt:
		if mode, ok := facts["mode"]; ok {
			return formatMode(mode)
		}
		return "ok"
	case CommandGetControllerVersion, CommandVersion:
		version, hasVersion := facts["version_x100"]
		beta, hasBeta := facts["beta"]
		switch {
		case hasVersion && hasBeta:
			return formatFirmwareVersion(version, &beta)
		case hasVersion:
			return formatFirmwareVersion(version, nil)
		default:
			return "ok"
		}
	case CommandU2GetCurrentSlot:
		if slot, ok := facts["slot"]; ok {
			return formatSlot(slot)
		}
		return "ok"
	case CommandJp108ReadDedicatedMappings:
		// The check reads the A button, the first of the ten.
		if usage, ok := facts["usage"]; ok && usage != 0 {
			return fmt.Sprintf("A button is assigned key %#02x", usage)
		}
		if kind := facts["mapping_type"]; kind != 0 && kind != uint32(JP108TypeKeyboard) {
			return fmt.Sprintf("A button holds a mapping of type %#02x", kind)
		}
		return "A button is unassigned"
	case CommandJp108ReadProfileName:
		if facts["name_bytes"] == 0 {
			return "no profile stored"
		}
		return "a profile is stored"
	case CommandJp108ReadFeatureFlags:
		return fmt.Sprintf("flags %#02x", facts["flags"])
	case CommandJp108ReadVoice:
		return fmt.Sprintf("volume level %d", facts["volume"])
	case CommandJp108ReadMappedKeys:
		return fmt.Sprintf("%d keys remapped", facts["mapped_keys"])
	default:
		return "ok"
	}
}
