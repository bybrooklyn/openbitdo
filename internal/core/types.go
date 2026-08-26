package core

import (
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// AppDevice is a discovered or targeted 8BitDo device with its resolved
// support profile.
type AppDevice struct {
	VidPid         protocol.VidPid
	Name           string
	SupportLevel   protocol.SupportLevel
	SupportTier    protocol.SupportTier
	ProtocolFamily protocol.ProtocolFamily
	Capability     protocol.PidCapability
	Evidence       protocol.SupportEvidence
	Serial         string
	Connected      bool
}

// Scorecard computes this device's support scorecard.
func (d AppDevice) Scorecard() SupportScorecard { return SupportScorecardForDevice(d) }

// SupportStatus computes this device's user-facing support status.
func (d AppDevice) SupportStatus() UserSupportStatus { return SupportStatusForTier(d.SupportTier) }

func appDeviceFromProfile(vidPid protocol.VidPid, serial string, connected bool) AppDevice {
	p := protocol.DeviceProfileFor(vidPid)
	return AppDevice{
		VidPid: vidPid, Name: p.Name, SupportLevel: p.SupportLevel, SupportTier: p.SupportTier,
		ProtocolFamily: p.ProtocolFamily, Capability: p.Capability, Evidence: p.Evidence,
		Serial: serial, Connected: connected,
	}
}

// UserSupportStatus is a beginner-facing summary of a device's support tier.
type UserSupportStatus string

const (
	StatusSupported  UserSupportStatus = "Supported"
	StatusInProgress UserSupportStatus = "In Progress"
	StatusPlanned    UserSupportStatus = "Planned"
	StatusBlocked    UserSupportStatus = "Blocked"
)

// SupportStatusForTier maps a support tier to its user-facing status.
func SupportStatusForTier(tier protocol.SupportTier) UserSupportStatus {
	switch tier {
	case protocol.TierFull:
		return StatusSupported
	case protocol.TierCandidateReadOnly:
		return StatusInProgress
	default:
		return StatusPlanned
	}
}

// DeviceKind distinguishes JP108 from Ultimate2 for guided flows.
type DeviceKind string

const (
	KindJP108     DeviceKind = "Jp108"
	KindUltimate2 DeviceKind = "Ultimate2"
)

// DedicatedButtonMapping is one JP108 dedicated-button -> HID-usage mapping.
type DedicatedButtonMapping struct {
	Button         DedicatedButtonID
	TargetHIDUsage uint16
}

// U2ButtonMapping is one Ultimate2 button -> HID-usage mapping.
type U2ButtonMapping struct {
	Button         U2ButtonID
	TargetHIDUsage uint16
}

// U2CoreProfile is the full readable/writable Ultimate2 core state.
type U2CoreProfile struct {
	Slot                 U2SlotID
	Mode                 byte
	FirmwareVersion      string
	L2Analog             float32
	R2Analog             float32
	SupportsTriggerWrite bool
	Mappings             []U2ButtonMapping
}

// GuidedButtonTestResult is the outcome of a guided button-test walkthrough.
type GuidedButtonTestResult struct {
	DeviceKind     DeviceKind
	ExpectedInputs []string
	Passed         bool
	Guidance       string
}

// ConfigBackupID identifies a stored pre-write backup.
type ConfigBackupID string

type configBackup struct {
	createdAt time.Time
	target    protocol.VidPid
	payload   configBackupPayload
}

// configBackupPayload holds exactly one of the two backup shapes.
type configBackupPayload struct {
	kind          deviceKind
	jp108Mappings []DedicatedButtonMapping
	u2Profile     U2CoreProfile
	u2ConfigBlob  []byte
}

type deviceKind int

const (
	backupJP108 deviceKind = iota
	backupU2
)

// WriteRecoveryReport describes the outcome of a backup-then-write-then-
// rollback-on-failure mapping/profile apply.
type WriteRecoveryReport struct {
	BackupID          ConfigBackupID
	HasBackupID       bool
	WriteApplied      bool
	RollbackAttempted bool
	RollbackSucceeded bool
	WriteError        string
	RollbackError     string
}

// RollbackFailed reports whether a rollback was attempted but did not succeed.
func (r WriteRecoveryReport) RollbackFailed() bool {
	return r.RollbackAttempted && !r.RollbackSucceeded
}

// EvidenceState is one scorecard dimension's status.
type EvidenceState string

const (
	EvidencePresent       EvidenceState = "Present"
	EvidenceMissing       EvidenceState = "Missing"
	EvidenceNotApplicable EvidenceState = "NotApplicable"
)

func (s EvidenceState) isPresent() bool { return s == EvidencePresent || s == EvidenceNotApplicable }

// SupportScorecard is the 7-dimension evidence/promotion scorecard for a device.
type SupportScorecard struct {
	VidPid                  protocol.VidPid
	SupportTier             protocol.SupportTier
	StaticEvidence          EvidenceState
	RuntimeEvidence         EvidenceState
	HardwareConfirmation    EvidenceState
	SafeReadCoverage        EvidenceState
	SafeWriteReadiness      EvidenceState
	BackupReadbackReadiness EvidenceState
	FirmwareStatus          EvidenceState
	ScorePercent            int
	PromotionReady          bool
	MissingEvidence         []string
}

// RuntimeUnlockPolicy is the caller-supplied gate state for a candidate
// write probe: advanced mode, explicit risk acknowledgement, and the
// per-PID on-disk unlock file.
type RuntimeUnlockPolicy struct {
	AdvancedMode      bool
	AcknowledgedRisk  bool
	UnlockFilePresent bool
	UnlockFilePath    string
}

// RuntimeUnlockReport is the outcome of a candidate write probe.
type RuntimeUnlockReport struct {
	VidPid            protocol.VidPid
	Allowed           bool
	Operation         string
	CommandsAttempted []string
	WriteApplied      bool
	ReadbackVerified  bool
	WriteLockRequired bool
	Message           string
	Scorecard         SupportScorecard
}

func deniedUnlockReport(vidPid protocol.VidPid, scorecard SupportScorecard, message string) RuntimeUnlockReport {
	return RuntimeUnlockReport{
		VidPid: vidPid, Allowed: false, Operation: "candidate-write-probe",
		Message: message, Scorecard: scorecard,
	}
}

func blockedOperationSummary(device AppDevice) string {
	switch device.SupportTier {
	case protocol.TierCandidateReadOnly:
		return "firmware, mapping, and profile writes"
	case protocol.TierDetectOnly:
		return "diagnostics beyond identification, firmware, mapping, and writes"
	default: // Full
		hasFirmware := device.Capability.SupportsFirmware
		hasEditor := device.Capability.SupportsJP108DedicatedMap ||
			(device.Capability.SupportsU2ButtonMap && device.Capability.SupportsU2SlotConfig)
		switch {
		case hasFirmware && hasEditor:
			return "none for confirmed capabilities"
		case hasFirmware:
			return "mapping writes without a confirmed editor"
		default:
			return "firmware without a verified path"
		}
	}
}
