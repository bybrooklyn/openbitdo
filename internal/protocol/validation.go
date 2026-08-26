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
		CommandJp108WriteFeatureFlags, CommandJp108ReadVoice, CommandJp108WriteVoice:
		return cap.SupportsJP108DedicatedMap
	case CommandU2GetCurrentSlot, CommandU2ReadConfigSlot, CommandU2WriteConfigSlot:
		return cap.SupportsU2SlotConfig
	case CommandU2ReadButtonMap, CommandU2WriteButtonMap, CommandU2SetMode:
		return cap.SupportsU2ButtonMap
	default:
		return false
	}
}

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
// byte-signature, per spec/command_matrix.csv's expected_response column.
func ValidateResponse(command CommandID, response []byte) ResponseStatus {
	if len(response) < 2 {
		return StatusMalformed
	}

	switch command {
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
	case CommandJp108ReadDedicatedMappings, CommandJp108ReadFeatureFlags, CommandJp108ReadVoice,
		CommandU2ReadConfigSlot, CommandU2ReadButtonMap:
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

func minimumResponseLen(command CommandID) int {
	switch command {
	case CommandGetPid:
		return 24
	case CommandGetReportRevision:
		return 6
	case CommandGetMode, CommandGetModeAlt:
		return 6
	case CommandU2GetCurrentSlot:
		return 6
	case CommandJp108ReadDedicatedMappings, CommandJp108ReadFeatureFlags, CommandJp108ReadVoice,
		CommandU2ReadConfigSlot, CommandU2ReadButtonMap:
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
	case (command == CommandGetMode || command == CommandGetModeAlt) && len(response) >= 6:
		parsed["mode"] = uint32(response[5])
	case (command == CommandGetControllerVersion || command == CommandVersion) && len(response) >= 5:
		parsed["version_x100"] = uint32(binary.LittleEndian.Uint16(response[2:4]))
		parsed["beta"] = uint32(response[4])
	case command == CommandU2GetCurrentSlot && len(response) >= 6:
		parsed["slot"] = uint32(response[5])
	}
	return parsed
}

func parseIndexedU16Table(raw []byte, expectedItems int) []IndexedUsage {
	out := make([]IndexedUsage, 0, expectedItems)
	offset := 2
	if len(raw) >= 8 {
		offset = 8
	}
	for idx := 0; idx < expectedItems; idx++ {
		pos := offset + idx*2
		var usage uint16
		if pos+1 < len(raw) {
			usage = binary.LittleEndian.Uint16(raw[pos : pos+2])
		}
		out = append(out, IndexedUsage{Index: byte(idx), Usage: usage})
	}
	return out
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
	default:
		return "ok"
	}
}
