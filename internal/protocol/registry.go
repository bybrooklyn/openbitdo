package protocol

//go:generate go run ./gen -spec-dir ../../docs/spec -out registry_generated.go

// PidRow is one row of the PID registry, generated from docs/spec/pid_matrix.csv.
type PidRow struct {
	Name string
	// DisplayName is the user-facing product name from
	// docs/spec/device_name_catalog.md; empty when the catalog has only a
	// placeholder for this PID.
	DisplayName    string
	Pid            uint16
	SupportLevel   SupportLevel
	SupportTier    SupportTier
	ProtocolFamily ProtocolFamily
}

// CommandRow is one row of the command registry, generated from
// docs/spec/command_matrix.csv.
type CommandRow struct {
	ID                  CommandID
	SafetyClass         SafetyClass
	Confidence          Confidence
	ExperimentalDefault bool
	ReportID            byte
	Request             []byte
	ExpectedResponse    string
	AppliesTo           []uint16
	OperationGroup      string
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

// EvidenceConfidence reports the evidence confidence surfaced to
// reporting/UI for this row (Confirmed rows are EvidenceConfirmed,
// everything else is EvidenceInferred).
func (r CommandRow) EvidenceConfidence() SupportEvidence {
	if r.Confidence == Confirmed {
		return EvidenceConfirmed
	}
	return EvidenceInferred
}

// ArcadeProPID is the Arcade Controller Pro. It also enumerates as
// ArcadeProAltPID, which the vendor's software treats as the same product.
const (
	ArcadeProPID    uint16 = 0x2062
	ArcadeProAltPID uint16 = 0x20aa
)

// UltimateBTPID is the first-generation Ultimate Bluetooth Controller, and
// UltimateBTAdapterPID the adapter it comes with.
const (
	UltimateBTPID        uint16 = 0x6007
	UltimateBTAdapterPID uint16 = 0x3106
)

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

// recordKeyboardPIDs are the keyboards that keep their profile in one
// record (see kbrecord.go) and their 2.4G receivers: Retro 87 Xbox edition,
// Retro 87 UK, Retro 68 and the Riviera keyboard. The Riviera is listed for
// completeness; while its tier is detect-only it is granted nothing.
var recordKeyboardPIDs = map[uint16]bool{
	0x2028: true, 0x202e: true, 0x3026: true, 0x3027: true, 0x203a: true, 0x2049: true, 0x205a: true,
}

// DefaultCapabilityFor derives a PID's capability set from its tier, PID,
// and protocol family. Ported 1:1 from registry.rs's default_capability_for,
// including every per-PID special case.
func DefaultCapabilityFor(pid uint16, tier SupportTier, family ProtocolFamily) PidCapability {
	if tier == TierDetectOnly {
		return IdentifyOnlyCapability()
	}

	// A mouse takes the mouse commands and nothing else: the vendor library
	// sends it none of the other families'. While its tier is detect-only
	// it is granted nothing, like everything else.
	if mousePIDs[pid] {
		return PidCapability{SupportsMouse: true}
	}
	// An Arcade Controller Pro is granted its profile and nothing else:
	// nothing is known here of how it updates its firmware. While its tier
	// is detect-only it is granted nothing, like everything else.
	if pid == ArcadeProPID {
		return PidCapability{SupportsU2SlotConfig: true}
	}

	if tier == TierCandidateReadOnly {
		record := recordKeyboardPIDs[pid]
		switch {
		case standardCandidateReadDiagPIDs[pid] && !pidWithSlotConfigCandidate[pid]:
			return PidCapability{SupportsMode: true, SupportsProfileRW: true, SupportsRecordKeyboard: record}
		case pidWithSlotConfigCandidate[pid]:
			return PidCapability{
				SupportsMode: true, SupportsProfileRW: true,
				SupportsU2SlotConfig: true, SupportsU2ButtonMap: true,
			}
		case jpCandidateDiagPIDs[pid]:
			// A record keyboard does not take the Retro 108's 33-byte
			// commands; the vendor library never sends it one.
			return PidCapability{SupportsJP108DedicatedMap: !record, SupportsRecordKeyboard: record}
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
	// A Pro 3 and an Arcade Controller keep their settings in the same kind
	// of record as an Ultimate 2, but update their firmware the standard way.
	// So does a first-generation Ultimate Bluetooth, reached directly or
	// through its adapter; which of the record's commands it takes is
	// narrowed in isCommandAllowedForDevice.
	cap.SupportsU2SlotConfig = pid == 0x6009 || pid == 0x600b || pid == UltimateBTPID || pid == UltimateBTAdapterPID
	cap.SupportsU2ButtonMap = false
	cap.SupportsRecordKeyboard = false
	cap.SupportsMouse = false
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
			DisplayName:    row.DisplayName,
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
