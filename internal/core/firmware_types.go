package core

import (
	"strings"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// FirmwareManifest is the TOML manifest published alongside GitHub releases,
// listing available firmware artifacts per PID/channel.
type FirmwareManifest struct {
	Version   uint32             `toml:"version"`
	Artifacts []FirmwareArtifact `toml:"artifacts"`
}

func (m FirmwareManifest) recommendedFor(target protocol.VidPid) (FirmwareArtifact, bool) {
	for _, a := range m.Artifacts {
		if strings.EqualFold(a.Channel, "stable") && a.VID == target.VID && a.PID == target.PID {
			return a, true
		}
	}
	return FirmwareArtifact{}, false
}

// FirmwareArtifact describes one downloadable, signed firmware image.
type FirmwareArtifact struct {
	VID            uint16                  `toml:"vid"`
	PID            uint16                  `toml:"pid"`
	ProtocolFamily protocol.ProtocolFamily `toml:"protocol_family"`
	Version        string                  `toml:"version"`
	Channel        string                  `toml:"channel"`
	URL            string                  `toml:"url"`
	SHA256         string                  `toml:"sha256"`
	Signature      ManifestSignature       `toml:"signature"`
}

// ManifestSignature names the signature algorithm and where to fetch it.
type ManifestSignature struct {
	Algorithm string `toml:"algorithm"`
	URL       string `toml:"url"`
}

// FirmwareDownloadResult is a downloaded-and-verified firmware image ready
// for preflight.
type FirmwareDownloadResult struct {
	FirmwarePath      string
	Version           string
	SourceURL         string
	SHA256            string
	VerifiedSignature bool
}

// FirmwarePreflightRequest starts the firmware-update state machine.
type FirmwarePreflightRequest struct {
	VidPid       protocol.VidPid
	FirmwarePath string
	AllowUnsafe  bool
	BrickRiskAck bool
	Experimental bool
	ChunkSize    int // 0 means "use core default"
}

// FirmwarePreflightResult is the outcome of a preflight check.
type FirmwarePreflightResult struct {
	Gate       AppPolicyGateResult
	Plan       FirmwareUpdatePlan
	HasPlan    bool
	Capability protocol.PidCapability
	Evidence   protocol.SupportEvidence
}

func deniedPreflight(reason AppPolicyGateReason, message string) FirmwarePreflightResult {
	return FirmwarePreflightResult{
		Gate:       AppPolicyGateResult{Allowed: false, Reason: reason, Message: message},
		Capability: protocol.IdentifyOnlyCapability(),
		Evidence:   protocol.EvidenceInferred,
	}
}

// FirmwareUpdateSessionID identifies one in-flight firmware update.
type FirmwareUpdateSessionID string

// FirmwareStartRequest moves a preflighted session to AwaitingConfirmation.
type FirmwareStartRequest struct {
	SessionID FirmwareUpdateSessionID
}

// FirmwareConfirmRequest starts the actual transfer.
type FirmwareConfirmRequest struct {
	SessionID        FirmwareUpdateSessionID
	AcknowledgedRisk bool
}

// FirmwareCancelRequest requests cancellation of an in-flight transfer.
type FirmwareCancelRequest struct {
	SessionID FirmwareUpdateSessionID
}

// FirmwareUpdatePlan is the computed transfer plan surfaced at preflight.
type FirmwareUpdatePlan struct {
	SessionID       FirmwareUpdateSessionID
	ChunkSize       int
	BytesTotal      int
	ChunksTotal     int
	ExpectedSeconds uint64
	Warnings        []string
	ImageSHA256     string
	CurrentVersion  string
	TargetVersion   string
}

// FirmwareProgressEvent is one broadcast progress update during a transfer.
type FirmwareProgressEvent struct {
	SessionID FirmwareUpdateSessionID
	Sequence  uint64
	Stage     string
	Progress  int
	Message   string
	Terminal  bool
	Timestamp time.Time
}

// FirmwareOutcome is the terminal state of a firmware transfer.
type FirmwareOutcome string

const (
	OutcomeCompleted FirmwareOutcome = "Completed"
	// OutcomeCompletedUnverified means the transfer itself succeeded (every
	// chunk sent, commit acknowledged, bootloader exited) but a post-flash
	// read of the device's reported firmware version either failed or didn't
	// match TargetVersion -- distinct from Completed because "the write
	// didn't error" is not the same claim as "the device is now confirmably
	// running the new firmware", and distinct from Failed because the write
	// itself did not fail.
	OutcomeCompletedUnverified FirmwareOutcome = "CompletedUnverified"
	OutcomeCancelled           FirmwareOutcome = "Cancelled"
	OutcomeFailed              FirmwareOutcome = "Failed"
)

// FirmwareFinalReport is the terminal report for a firmware session.
type FirmwareFinalReport struct {
	SessionID   FirmwareUpdateSessionID
	Status      FirmwareOutcome
	StartedAt   time.Time
	CompletedAt time.Time
	BytesTotal  int
	ChunksTotal int
	ChunksSent  int
	ErrorCode   protocol.ErrorCode
	Message     string
	// TargetVersion and ObservedVersion are set only for OutcomeCompleted /
	// OutcomeCompletedUnverified -- the version the plan expected to see
	// post-flash, and what a post-flash read actually reported (empty if
	// that read itself failed; see Message for why).
	TargetVersion   string
	ObservedVersion string
}
