package core

import (
	"strings"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// AppDevice is a discovered or targeted 8BitDo device with its resolved
// support profile.
type AppDevice struct {
	VidPid protocol.VidPid
	// Name is the registry's canonical ID (e.g. "PID_108JP"); DisplayName
	// is what a person should be shown.
	Name           string
	DisplayName    string
	SupportLevel   protocol.SupportLevel
	SupportTier    protocol.SupportTier
	ProtocolFamily protocol.ProtocolFamily
	Capability     protocol.PidCapability
	Evidence       protocol.SupportEvidence
	Serial         string
	Connected      bool
	// ConfigChannel says whether the connected device exposes the HID
	// interface configuration commands travel over.
	ConfigChannel ChannelState
	// WorksAs is what the device is doing for the computer right now,
	// independent of whether OpenBitdo can configure it.
	WorksAs DeviceRole
}

// DeviceRole is the kind of input device the operating system sees.
type DeviceRole int

const (
	RoleUnknown DeviceRole = iota
	RoleGamepad
	RoleKeyboard
)

// ChannelState is whether a device's configuration interface was found.
type ChannelState int

const (
	// ChannelUnknown: the platform could not describe the device's
	// interfaces, so only opening it will tell.
	ChannelUnknown ChannelState = iota
	ChannelPresent
	// ChannelAbsent: every interface is known and none is the configuration
	// interface. Diagnostics and mapping cannot reach this device.
	ChannelAbsent
)

// friendlyDeviceName picks the name to show for a device: what the device
// calls itself, else the catalog's name, else the registry ID without its
// "PID_" prefix.
func friendlyDeviceName(product string, profile protocol.DeviceProfile) string {
	if name := cleanProductName(product); name != "" {
		return name
	}
	if profile.DisplayName != "" {
		return profile.DisplayName
	}
	return strings.TrimPrefix(profile.Name, "PID_")
}

// cleanProductName tidies an OS-reported product string by dropping the
// vendor prefix, which some devices even repeat ("8BitDo 8BitDo Retro 108
// Keyboard"). Every device listed is an 8BitDo one, so the prefix only
// costs room; this also matches how the catalog names devices.
func cleanProductName(product string) string {
	fields := strings.Fields(product)
	for len(fields) > 1 && strings.EqualFold(fields[0], "8BitDo") {
		fields = fields[1:]
	}
	return strings.Join(fields, " ")
}

// Scorecard computes this device's support scorecard.
func (d AppDevice) Scorecard() SupportScorecard { return SupportScorecardForDevice(d) }

// SupportStatus computes this device's user-facing support status.
func (d AppDevice) SupportStatus() UserSupportStatus { return SupportStatusForTier(d.SupportTier) }

func appDeviceFromProfile(vidPid protocol.VidPid, serial string, connected bool) AppDevice {
	p := protocol.DeviceProfileFor(vidPid)
	return AppDevice{
		VidPid: vidPid, Name: p.Name, DisplayName: friendlyDeviceName("", p),
		SupportLevel: p.SupportLevel, SupportTier: p.SupportTier,
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

// configBackupPayload holds exactly one backup shape, selected by kind.
type configBackupPayload struct {
	kind          deviceKind
	jp108Mappings []DedicatedButtonMapping
	keyboard      KeyboardProfile
	padRecord     []byte
}

type deviceKind int

const (
	backupJP108 deviceKind = iota
	backupKeyboard
	backupPad
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

const (
	// ReleaseBlockerFirmwareDisabled is emitted when static device support
	// exists but the v0.0.3 runtime firmware feature gate is off.
	ReleaseBlockerFirmwareDisabled = "firmware_disabled_v0_0_3"
	// ReleaseBlockerU2ButtonMapFraming is emitted when Ultimate2 mapping is
	// mock-preview-only because real button-map framing is not confirmed.
	ReleaseBlockerU2ButtonMapFraming = "u2_button_map_framing_unconfirmed"
)

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
	ReleaseBlockers         []string
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

func (c *OpenBitdoCore) blockedOperationSummary(device AppDevice) string {
	blocked := make([]string, 0, 3)
	if !c.FirmwareEnabled() {
		blocked = append(blocked, "firmware updates (deferred in 0.0.3)")
	} else if device.SupportTier != protocol.TierFull || !device.Capability.SupportsFirmware {
		blocked = append(blocked, "firmware updates without a verified path")
	}

	hasJP108Editor := device.Capability.SupportsJP108DedicatedMap
	hasU2Editor := device.Capability.SupportsU2ButtonMap && device.Capability.SupportsU2SlotConfig
	realU2MappingDeferred := hasU2Editor && !c.config.MockMode

	switch device.SupportTier {
	case protocol.TierCandidateReadOnly:
		if realU2MappingDeferred {
			blocked = append(blocked, "Ultimate2 mapping ("+u2MappingDeferredReason+")")
		} else {
			blocked = append(blocked, "mapping and profile writes pending hardware confirmation")
		}
	case protocol.TierDetectOnly:
		blocked = append(blocked, "diagnostics beyond identification", "mapping and writes without a verified path")
	default: // Full
		if realU2MappingDeferred {
			blocked = append(blocked, "Ultimate2 mapping ("+u2MappingDeferredReason+")")
		} else if !hasJP108Editor && !hasU2Editor {
			blocked = append(blocked, "mapping writes without a confirmed editor")
		}
	}

	if len(blocked) == 0 {
		return "none for confirmed capabilities"
	}
	return strings.Join(blocked, ", ")
}
