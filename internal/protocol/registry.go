package protocol

//go:generate go run ./gen -spec-dir ../../spec -out registry_generated.go

// PidRow is one row of the PID registry, generated from spec/pid_matrix.csv.
type PidRow struct {
	Name           string
	Pid            uint16
	SupportLevel   SupportLevel
	SupportTier    SupportTier
	ProtocolFamily ProtocolFamily
}

// CommandRow is one row of the command registry, generated from
// spec/command_matrix.csv.
type CommandRow struct {
	ID                 CommandID
	SafetyClass        SafetyClass
	Confidence         Confidence
	ExperimentalDefault bool
	ReportID           byte
	Request            []byte
	ExpectedResponse   string
	AppliesTo          []uint16
	OperationGroup     string
}

// RuntimePolicy derives the runtime execution policy from a row's confidence
// and safety class:
//   - Confirmed paths are enabled by default.
//   - Inferred safe reads can run only under experimental mode.
//   - Inferred write/unsafe paths stay blocked until explicit confirmation.
func (r CommandRow) RuntimePolicy() RuntimePolicy {
	switch {
	case r.Confidence == Confirmed:
		return EnabledDefault
	case r.Confidence == Inferred && r.SafetyClass == SafeRead:
		return ExperimentalGate
	default:
		return BlockedUntilConfirmed
	}
}

func (r CommandRow) evidenceConfidence() SupportEvidence {
	if r.Confidence == Confirmed {
		return EvidenceConfirmed
	}
	return EvidenceInferred
}

// FindPID looks up a PID registry row by PID.
func FindPID(pid uint16) (PidRow, bool) {
	for _, row := range PIDRegistry {
		if row.Pid == pid {
			return row, true
		}
	}
	return PidRow{}, false
}

// FindCommand looks up a command registry row by ID.
func FindCommand(id CommandID) (CommandRow, bool) {
	for _, row := range CommandRegistry {
		if row.ID == id {
			return row, true
		}
	}
	return CommandRow{}, false
}

// CommandAppliesToPID reports whether row's request applies to pid: either
// because AppliesTo is empty/wildcard, pid is explicitly listed, or the
// row's operation group maps onto a capability the PID's tier grants.
func CommandAppliesToPID(row CommandRow, pid uint16) bool {
	if len(row.AppliesTo) == 0 {
		return true
	}
	for _, p := range row.AppliesTo {
		if p == pid {
			return true
		}
	}
	targetRow, ok := FindPID(pid)
	if !ok {
		return false
	}
	cap := DefaultCapabilityFor(targetRow.Pid, targetRow.SupportTier, targetRow.ProtocolFamily)
	switch row.OperationGroup {
	case "Ultimate2Core":
		return cap.SupportsU2SlotConfig || cap.SupportsU2ButtonMap
	case "JP108Dedicated":
		return cap.SupportsJP108DedicatedMap
	case "Firmware":
		return cap.SupportsFirmware || cap.SupportsBoot
	default:
		return false
	}
}

var standardCandidateReadDiagPIDs = map[uint16]bool{
	0x6002: true, 0x6003: true, 0x3010: true, 0x3011: true, 0x3012: true, 0x3013: true,
	0x3004: true, 0x3019: true, 0x3100: true, 0x3105: true, 0x2100: true, 0x2101: true,
	0x901a: true, 0x6006: true, 0x5203: true, 0x5204: true, 0x301a: true, 0x9028: true,
	0x3026: true, 0x3027: true,
}

var jpCandidateDiagPIDs = map[uint16]bool{
	0x5200: true, 0x5201: true, 0x203a: true, 0x2049: true, 0x2028: true, 0x202e: true,
}

// pidWithSlotConfigCandidate covers the two candidate-readonly PIDs that
// expose U2 slot/button-map reads ahead of full confirmation (0x3105, 0x301a).
var pidWithSlotConfigCandidate = map[uint16]bool{0x3105: true, 0x301a: true}

// DefaultCapabilityFor derives a PID's capability set from its tier, PID,
// and protocol family. Ported 1:1 from registry.rs's default_capability_for,
// including every per-PID special case.
func DefaultCapabilityFor(pid uint16, tier SupportTier, family ProtocolFamily) PidCapability {
	if tier == TierDetectOnly {
		return IdentifyOnlyCapability()
	}

	if tier == TierCandidateReadOnly {
		switch {
		case standardCandidateReadDiagPIDs[pid] && !pidWithSlotConfigCandidate[pid]:
			return PidCapability{SupportsMode: true, SupportsProfileRW: true}
		case pidWithSlotConfigCandidate[pid]:
			return PidCapability{
				SupportsMode: true, SupportsProfileRW: true,
				SupportsU2SlotConfig: true, SupportsU2ButtonMap: true,
			}
		case jpCandidateDiagPIDs[pid]:
			return PidCapability{SupportsJP108DedicatedMap: true}
		}
	}

	switch pid {
	case 0x5209, 0x520a:
		return PidCapability{
			SupportsBoot: true, SupportsFirmware: true, SupportsJP108DedicatedMap: true,
		}
	case 0x6012, 0x6013, 0x600f, 0x6011:
		return PidCapability{
			SupportsMode: true, SupportsProfileRW: true, SupportsBoot: true, SupportsFirmware: true,
			SupportsU2SlotConfig: true, SupportsU2ButtonMap: true,
		}
	}

	cap := FullCapability()
	if family == JpHandshake {
		cap.SupportsMode = false
		cap.SupportsProfileRW = false
	}
	cap.SupportsJP108DedicatedMap = false
	cap.SupportsU2SlotConfig = false
	cap.SupportsU2ButtonMap = false
	return cap
}

func defaultEvidenceFor(tier SupportTier) SupportEvidence {
	if tier == TierFull {
		return EvidenceConfirmed
	}
	return EvidenceInferred
}

// DeviceProfileFor resolves the full DeviceProfile for a VID:PID, falling
// back to an unknown/detect-only profile when the PID isn't registered.
func DeviceProfileFor(target VidPid) DeviceProfile {
	if row, ok := FindPID(target.PID); ok {
		return DeviceProfile{
			VidPid:         target,
			Name:           row.Name,
			SupportLevel:   row.SupportLevel,
			SupportTier:    row.SupportTier,
			ProtocolFamily: row.ProtocolFamily,
			Capability:     DefaultCapabilityFor(row.Pid, row.SupportTier, row.ProtocolFamily),
			Evidence:       defaultEvidenceFor(row.SupportTier),
		}
	}
	return DeviceProfile{
		VidPid:         target,
		Name:           "PID_UNKNOWN",
		SupportLevel:   SupportDetectOnly,
		SupportTier:    TierDetectOnly,
		ProtocolFamily: FamilyUnknown,
		Capability:     IdentifyOnlyCapability(),
		Evidence:       EvidenceUntested,
	}
}
