package core

import (
	"fmt"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

func mockDevice(vidPid protocol.VidPid, full bool) AppDevice {
	p := protocol.DeviceProfileFor(vidPid)
	capability := protocol.IdentifyOnlyCapability()
	if full || p.SupportTier == protocol.TierCandidateReadOnly {
		capability = p.Capability
	}
	var serial string
	switch {
	case full:
		serial = fmt.Sprintf("MOCK-FULL-%04x", vidPid.PID)
	case p.SupportTier == protocol.TierCandidateReadOnly:
		serial = fmt.Sprintf("MOCK-CANDIDATE-%04x", vidPid.PID)
	default:
		serial = fmt.Sprintf("MOCK-DETECT-%04x", vidPid.PID)
	}
	return AppDevice{
		VidPid: vidPid, Name: p.Name, SupportLevel: p.SupportLevel, SupportTier: p.SupportTier,
		ProtocolFamily: p.ProtocolFamily, Capability: capability, Evidence: p.Evidence,
		Serial: serial, Connected: true,
	}
}

var mockSafeReadOrder = []protocol.CommandID{
	protocol.CommandGetPid, protocol.CommandGetReportRevision, protocol.CommandGetMode,
	protocol.CommandGetModeAlt, protocol.CommandGetControllerVersion, protocol.CommandGetSuperButton,
	protocol.CommandIdle, protocol.CommandVersion, protocol.CommandReadProfile,
	protocol.CommandJp108ReadDedicatedMappings, protocol.CommandJp108ReadFeatureFlags,
	protocol.CommandJp108ReadVoice, protocol.CommandU2GetCurrentSlot, protocol.CommandU2ReadConfigSlot,
	protocol.CommandU2ReadButtonMap,
}

func mockDiagProbe(target protocol.VidPid) protocol.DiagProbeResult {
	p := protocol.DeviceProfileFor(target)
	commands := mockDiagCommandsFor(p, target)
	checks := make([]protocol.DiagCommandStatus, 0, len(commands))
	for _, command := range commands {
		facts := mockDiagParsedFacts(command, target)
		row, _ := protocol.FindCommand(command)
		checks = append(checks, protocol.DiagCommandStatus{
			Command: command, OK: true, Confidence: row.EvidenceConfidence(),
			IsExperimental: row.ExperimentalDefault, Severity: protocol.SeverityOK, Attempts: 1,
			Validator: fmt.Sprintf("mock:%s", command), ResponseStatus: protocol.StatusOk,
			BytesWritten: len(row.Request), BytesRead: 64, Detail: mockDiagDetail(command, facts),
			ParsedFacts: facts,
		})
	}
	return protocol.DiagProbeResult{
		Target: target, ProfileName: p.Name, SupportLevel: p.SupportLevel, SupportTier: p.SupportTier,
		ProtocolFamily: p.ProtocolFamily, Capability: p.Capability, Evidence: p.Evidence,
		TransportReady: true, CommandChecks: checks,
	}
}

func mockDiagCommandsFor(p protocol.DeviceProfile, target protocol.VidPid) []protocol.CommandID {
	var out []protocol.CommandID
	for _, command := range mockSafeReadOrder {
		if mockDiagCommandAllowed(p, target, command) {
			out = append(out, command)
		}
	}
	return out
}

func mockDiagCommandAllowed(p protocol.DeviceProfile, target protocol.VidPid, command protocol.CommandID) bool {
	row, ok := protocol.FindCommand(command)
	if !ok {
		return false
	}
	if len(row.AppliesTo) > 0 {
		found := false
		for _, pid := range row.AppliesTo {
			if pid == target.PID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if !mockDiagFamilyAllowed(p.ProtocolFamily, command) {
		return false
	}
	if !mockDiagCapabilityAllowed(p.Capability, command) {
		return false
	}
	if p.SupportTier == protocol.TierCandidateReadOnly && !mockDiagCandidateAllowed(target.PID, command) {
		return false
	}
	return true
}

var mockUnknownFamilyAllowed = map[protocol.CommandID]bool{
	protocol.CommandGetPid: true, protocol.CommandGetReportRevision: true,
	protocol.CommandGetControllerVersion: true, protocol.CommandVersion: true, protocol.CommandIdle: true,
}

var mockJPHandshakeDisallowed = map[protocol.CommandID]bool{
	protocol.CommandGetMode: true, protocol.CommandGetModeAlt: true, protocol.CommandReadProfile: true,
	protocol.CommandU2GetCurrentSlot: true, protocol.CommandU2ReadConfigSlot: true, protocol.CommandU2ReadButtonMap: true,
}

var mockStandardJP108Disallowed = map[protocol.CommandID]bool{
	protocol.CommandJp108ReadDedicatedMappings: true, protocol.CommandJp108ReadFeatureFlags: true,
	protocol.CommandJp108ReadVoice: true,
}

func mockDiagFamilyAllowed(family protocol.ProtocolFamily, command protocol.CommandID) bool {
	switch family {
	case protocol.FamilyUnknown, protocol.DS4Boot:
		return mockUnknownFamilyAllowed[command]
	case protocol.JpHandshake:
		return !mockJPHandshakeDisallowed[command]
	default: // Standard64, DInput
		return !mockStandardJP108Disallowed[command]
	}
}

func mockDiagCapabilityAllowed(cap protocol.PidCapability, command protocol.CommandID) bool {
	switch command {
	case protocol.CommandGetPid, protocol.CommandGetReportRevision, protocol.CommandGetControllerVersion,
		protocol.CommandGetSuperButton, protocol.CommandIdle, protocol.CommandVersion:
		return true
	case protocol.CommandGetMode, protocol.CommandGetModeAlt:
		return cap.SupportsMode
	case protocol.CommandReadProfile:
		return cap.SupportsProfileRW
	case protocol.CommandJp108ReadDedicatedMappings, protocol.CommandJp108ReadFeatureFlags, protocol.CommandJp108ReadVoice:
		return cap.SupportsJP108DedicatedMap
	case protocol.CommandU2GetCurrentSlot, protocol.CommandU2ReadConfigSlot:
		return cap.SupportsU2SlotConfig
	case protocol.CommandU2ReadButtonMap:
		return cap.SupportsU2ButtonMap
	default:
		return false
	}
}

var mockBaseDiagReads = map[protocol.CommandID]bool{
	protocol.CommandGetPid: true, protocol.CommandGetReportRevision: true,
	protocol.CommandGetControllerVersion: true, protocol.CommandVersion: true, protocol.CommandIdle: true,
}

var mockStandardCandidatePIDs = map[uint16]bool{
	0x6002: true, 0x6003: true, 0x3010: true, 0x3011: true, 0x3012: true, 0x3013: true,
	0x3004: true, 0x3019: true, 0x3100: true, 0x3105: true, 0x2100: true, 0x2101: true,
	0x901a: true, 0x6006: true, 0x5203: true, 0x5204: true, 0x301a: true, 0x9028: true,
	0x3026: true, 0x3027: true,
}

var mockJPCandidatePIDs = map[uint16]bool{
	0x5200: true, 0x5201: true, 0x203a: true, 0x2049: true, 0x2028: true, 0x202e: true,
}

func mockDiagCandidateAllowed(pid uint16, command protocol.CommandID) bool {
	if mockBaseDiagReads[command] {
		return mockStandardCandidatePIDs[pid] || mockJPCandidatePIDs[pid]
	}
	if mockStandardCandidatePIDs[pid] {
		return command == protocol.CommandGetMode || command == protocol.CommandGetModeAlt || command == protocol.CommandReadProfile
	}
	return false
}

func mockDiagParsedFacts(command protocol.CommandID, target protocol.VidPid) map[string]uint32 {
	facts := map[string]uint32{}
	switch command {
	case protocol.CommandGetPid:
		facts["detected_pid"] = uint32(target.PID)
	case protocol.CommandGetReportRevision:
		facts["revision"] = 1
	case protocol.CommandGetMode, protocol.CommandGetModeAlt:
		facts["mode"] = 2
	case protocol.CommandGetControllerVersion, protocol.CommandVersion:
		facts["version_x100"] = 4200
		facts["beta"] = 0
	case protocol.CommandU2GetCurrentSlot:
		facts["slot"] = 1
	}
	return facts
}

func mockDiagDetail(command protocol.CommandID, facts map[string]uint32) string {
	switch command {
	case protocol.CommandGetPid:
		if pid, ok := facts["detected_pid"]; ok {
			return fmt.Sprintf("detected pid %#04x", pid)
		}
	case protocol.CommandGetReportRevision:
		if rev, ok := facts["revision"]; ok {
			return fmt.Sprintf("report revision %d", rev)
		}
	case protocol.CommandGetMode, protocol.CommandGetModeAlt:
		if mode, ok := facts["mode"]; ok {
			return fmt.Sprintf("mode %d", mode)
		}
	case protocol.CommandGetControllerVersion, protocol.CommandVersion:
		if version, ok := facts["version_x100"]; ok {
			return fmt.Sprintf("firmware %d.%02d beta=%d", version/100, version%100, facts["beta"])
		}
	case protocol.CommandU2GetCurrentSlot:
		if slot, ok := facts["slot"]; ok {
			return fmt.Sprintf("current slot %d", slot)
		}
	}
	return "ok"
}

func defaultJP108Mappings() []DedicatedButtonMapping {
	out := make([]DedicatedButtonMapping, 0, len(AllDedicatedButtons))
	for idx, button := range AllDedicatedButtons {
		out = append(out, DedicatedButtonMapping{Button: button, TargetHIDUsage: (0x04 + uint16(idx)) & 0x00ff})
	}
	return out
}

// u2DefaultButtonFunction is each core button's "does what it says"
// default target — e.g. the A button defaults to acting as A. Mirrors the
// dirty-room evidence's description of the array's default initializer
// (each core slot pre-populated with its own natural function).
var u2DefaultButtonFunction = map[U2ButtonID]U2Function{
	U2A: U2FuncA, U2B: U2FuncB, U2X: U2FuncX, U2Y: U2FuncY,
	U2L1: U2FuncL1, U2R1: U2FuncR1, U2L2: U2FuncL2, U2R2: U2FuncR2,
	U2L3: U2FuncL3, U2R3: U2FuncR3, U2Select: U2FuncSelect, U2Start: U2FuncStart,
	U2Home: U2FuncHome, U2DPadUp: U2FuncDPadUp, U2DPadDown: U2FuncDPadDown,
	U2DPadLeft: U2FuncDPadLeft, U2DPadRight: U2FuncDPadRight,
}

func defaultU2Mappings() []U2ButtonMapping {
	out := make([]U2ButtonMapping, 0, len(AllU2Buttons))
	for _, button := range AllU2Buttons {
		out = append(out, U2ButtonMapping{Button: button, Target: u2DefaultButtonFunction[button]})
	}
	return out
}

// defaultU2PaddleMappings returns the 4 back paddles unbound (U2FuncNone),
// matching the dirty-room evidence's description of the array's default
// initializer leaving paddle slots 18-21 with no function assigned.
func defaultU2PaddleMappings() []U2PaddleMapping {
	out := make([]U2PaddleMapping, 0, len(AllU2Paddles))
	for _, paddle := range AllU2Paddles {
		out = append(out, U2PaddleMapping{Paddle: paddle, Target: U2FuncNone})
	}
	return out
}
