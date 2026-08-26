// Package protocol implements the OpenBitdo wire protocol: command framing,
// the PID/command registries generated from spec/*.csv, and the device
// session state machine that gates and executes commands safely.
package protocol

import "fmt"

// VidPid identifies a USB HID device by vendor and product ID.
type VidPid struct {
	VID uint16
	PID uint16
}

func (v VidPid) String() string {
	return fmt.Sprintf("%04x:%04x", v.VID, v.PID)
}

// ProtocolFamily is the wire dialect a device speaks.
type ProtocolFamily string

const (
	Standard64  ProtocolFamily = "Standard64"
	JpHandshake ProtocolFamily = "JpHandshake"
	DInput      ProtocolFamily = "DInput"
	DS4Boot     ProtocolFamily = "DS4Boot"
	FamilyUnknown ProtocolFamily = "Unknown"
)

// SupportLevel is the coarse level from spec/pid_matrix.csv.
type SupportLevel string

const (
	SupportFull       SupportLevel = "full"
	SupportDetectOnly SupportLevel = "detect-only"
)

// SupportTier is the operative gating tier for a PID.
type SupportTier string

const (
	TierDetectOnly      SupportTier = "detect-only"
	TierCandidateReadOnly SupportTier = "candidate-readonly"
	TierFull            SupportTier = "full"
)

// SafetyClass is a command's risk tier.
type SafetyClass string

const (
	SafeRead       SafetyClass = "SafeRead"
	SafeWrite      SafetyClass = "SafeWrite"
	UnsafeBoot     SafetyClass = "UnsafeBoot"
	UnsafeFirmware SafetyClass = "UnsafeFirmware"
)

// IsUnsafe reports whether the class requires the dual unsafe/brick-risk gate.
func (s SafetyClass) IsUnsafe() bool {
	return s == UnsafeBoot || s == UnsafeFirmware
}

// Confidence is the evidence confidence for a declared command path, as
// recorded in spec/command_matrix.csv.
type Confidence string

const (
	Confirmed Confidence = "confirmed"
	Inferred  Confidence = "inferred"
)

// RuntimePolicy is the runtime execution policy derived from a command's
// confidence and safety class.
type RuntimePolicy int

const (
	EnabledDefault RuntimePolicy = iota
	ExperimentalGate
	BlockedUntilConfirmed
)

// SupportEvidence is the evidence confidence surfaced to reporting/UI.
type SupportEvidence string

const (
	EvidenceConfirmed SupportEvidence = "Confirmed"
	EvidenceInferred  SupportEvidence = "Inferred"
	EvidenceUntested  SupportEvidence = "Untested"
)

// PidCapability declares which operation groups a PID supports.
type PidCapability struct {
	SupportsMode              bool
	SupportsProfileRW         bool
	SupportsBoot              bool
	SupportsFirmware          bool
	SupportsJP108DedicatedMap bool
	SupportsU2SlotConfig      bool
	SupportsU2ButtonMap       bool
}

// FullCapability returns every capability flag enabled.
func FullCapability() PidCapability {
	return PidCapability{true, true, true, true, true, true, true}
}

// IdentifyOnlyCapability returns every capability flag disabled.
func IdentifyOnlyCapability() PidCapability {
	return PidCapability{}
}

// DeviceProfile is the resolved profile for a specific VID:PID target.
type DeviceProfile struct {
	VidPid         VidPid
	Name           string
	SupportLevel   SupportLevel
	SupportTier    SupportTier
	ProtocolFamily ProtocolFamily
	Capability     PidCapability
	Evidence       SupportEvidence
}
