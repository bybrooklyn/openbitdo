package protocol

import (
	"context"
	"fmt"
)

// RetryPolicy controls how many times a command is retried and the backoff
// between attempts.
type RetryPolicy struct {
	MaxAttempts uint8
	BackoffMs   uint64
}

// DefaultRetryPolicy matches the Rust implementation's defaults.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BackoffMs: 10}
}

// TimeoutProfile controls per-safety-class response timeouts.
type TimeoutProfile struct {
	ProbeMs    uint64
	IOMs       uint64
	FirmwareMs uint64
}

// DefaultTimeoutProfile matches the Rust implementation's defaults.
func DefaultTimeoutProfile() TimeoutProfile {
	return TimeoutProfile{ProbeMs: 200, IOMs: 400, FirmwareMs: 1200}
}

// SessionConfig controls a DeviceSession's safety gates and I/O behavior.
type SessionConfig struct {
	RetryPolicy          RetryPolicy
	TimeoutProfile       TimeoutProfile
	AllowUnsafe          bool
	BrickRiskAck         bool
	Experimental         bool
	CandidateWriteUnlock bool
	TraceEnabled         bool
}

// DefaultSessionConfig matches the Rust implementation's defaults: every
// safety gate closed, tracing on.
func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		RetryPolicy:    DefaultRetryPolicy(),
		TimeoutProfile: DefaultTimeoutProfile(),
		TraceEnabled:   true,
	}
}

// CommandExecutionReport records the outcome of one command send/retry cycle.
type CommandExecutionReport struct {
	Command      CommandID
	Attempts     uint8
	Validator    string
	Status       ResponseStatus
	BytesWritten int
	BytesRead    int
	ErrorCode    ErrorCode // empty when Status == StatusOk and no error
}

// DiagSeverity classifies how urgently a failed diagnostic check should be
// surfaced to the user.
type DiagSeverity string

const (
	SeverityOK             DiagSeverity = "Ok"
	SeverityWarning        DiagSeverity = "Warning"
	SeverityNeedsAttention DiagSeverity = "NeedsAttention"
)

// DiagCommandStatus is the outcome of one diagnostic check.
type DiagCommandStatus struct {
	Command        CommandID
	OK             bool
	Confidence     SupportEvidence
	IsExperimental bool
	Severity       DiagSeverity
	Attempts       uint8
	Validator      string
	ResponseStatus ResponseStatus
	BytesWritten   int
	BytesRead      int
	ErrorCode      ErrorCode
	Detail         string
	ParsedFacts    map[string]uint32
}

// DiagProbeResult is the full outcome of running every applicable safe-read
// diagnostic check against a device.
type DiagProbeResult struct {
	Target         VidPid
	ProfileName    string
	SupportLevel   SupportLevel
	SupportTier    SupportTier
	ProtocolFamily ProtocolFamily
	Capability     PidCapability
	Evidence       SupportEvidence
	TransportReady bool
	CommandChecks  []DiagCommandStatus
}

// IdentifyResult is the outcome of a GetPid-based identify pass.
type IdentifyResult struct {
	Target         VidPid
	ProfileName    string
	SupportLevel   SupportLevel
	SupportTier    SupportTier
	ProtocolFamily ProtocolFamily
	Capability     PidCapability
	Evidence       SupportEvidence
	DetectedPID    *uint16
}

// ModeState is a device's current operating mode.
type ModeState struct {
	Mode   byte
	Source string
}

// FirmwareTransferReport summarizes a completed (or dry-run) firmware
// transfer.
type FirmwareTransferReport struct {
	BytesTotal int
	ChunkSize  int
	ChunksSent int
	DryRun     bool
}

// DeviceSession drives the command protocol against one opened device,
// gating every command through ensureCommandAllowed before it is ever
// written to the wire. Its methods are split across this file (types,
// lifecycle, identify, diag probe), session_commands.go (per-command
// read/write), session_firmware.go (firmware/bootloader), session_transport.go
// (retry/timeout/response handling), and session_authorization.go (the
// 4-gate command authorization check).
type DeviceSession struct {
	transport     Transport
	target        VidPid
	profile       DeviceProfile
	config        SessionConfig
	trace         []CommandExecutionReport
	lastExecution *CommandExecutionReport
}

// NewDeviceSession opens transport against target and resolves its device
// profile from the registry.
func NewDeviceSession(ctx context.Context, transport Transport, target VidPid, config SessionConfig) (*DeviceSession, error) {
	if err := transport.Open(ctx, target); err != nil {
		return nil, err
	}
	return &DeviceSession{
		transport: transport,
		target:    target,
		profile:   DeviceProfileFor(target),
		config:    config,
	}, nil
}

func (s *DeviceSession) Profile() DeviceProfile { return s.profile }

func (s *DeviceSession) Trace() []CommandExecutionReport { return s.trace }

func (s *DeviceSession) LastExecutionReport() *CommandExecutionReport { return s.lastExecution }

func (s *DeviceSession) Close() error { return s.transport.Close() }

// Identify sends GetPid and re-resolves the device profile from whatever PID
// the device actually reports, falling back to the profile the caller
// opened with if the read fails.
func (s *DeviceSession) Identify(ctx context.Context) (IdentifyResult, error) {
	var detectedPID *uint16
	if resp, err := s.SendCommand(ctx, CommandGetPid, nil); err == nil {
		if v, ok := resp.ParsedFields["detected_pid"]; ok {
			pid := uint16(v)
			detectedPID = &pid
		}
	}

	profile := s.profile
	if detectedPID != nil {
		if row, ok := FindPID(*detectedPID); ok {
			profile = DeviceProfileFor(VidPid{VID: s.target.VID, PID: row.Pid})
		}
	}

	return IdentifyResult{
		Target:         s.target,
		ProfileName:    profile.Name,
		SupportLevel:   profile.SupportLevel,
		SupportTier:    profile.SupportTier,
		ProtocolFamily: profile.ProtocolFamily,
		Capability:     profile.Capability,
		Evidence:       profile.Evidence,
		DetectedPID:    detectedPID,
	}, nil
}

// DiagProbe runs every safe-read command applicable to this device and
// reports pass/fail per check. It never returns an error itself — a failed
// check is recorded as a DiagCommandStatus, not surfaced as a Go error,
// mirroring the Rust implementation.
func (s *DeviceSession) DiagProbe(ctx context.Context) DiagProbeResult {
	checks := s.diagCommandsToRun()
	statuses := make([]DiagCommandStatus, 0, len(checks))
	for _, c := range checks {
		statuses = append(statuses, s.runDiagCheck(ctx, c.command, c.policy, c.confidence))
	}
	transportReady := false
	for _, c := range statuses {
		if c.OK {
			transportReady = true
			break
		}
	}

	return DiagProbeResult{
		Target:         s.target,
		ProfileName:    s.profile.Name,
		SupportLevel:   s.profile.SupportLevel,
		SupportTier:    s.profile.SupportTier,
		ProtocolFamily: s.profile.ProtocolFamily,
		Capability:     s.profile.Capability,
		Evidence:       s.profile.Evidence,
		TransportReady: transportReady,
		CommandChecks:  statuses,
	}
}

type diagCheckPlan struct {
	command    CommandID
	policy     RuntimePolicy
	confidence SupportEvidence
}

func (s *DeviceSession) diagCommandsToRun() []diagCheckPlan {
	var plans []diagCheckPlan
	for _, row := range CommandRegistry {
		if row.SafetyClass != SafeRead {
			continue
		}
		if !CommandAppliesToPID(row, s.target.PID) {
			continue
		}
		if !isCommandAllowedByFamily(s.profile.ProtocolFamily, row.ID) || !isCommandAllowedByCapability(s.profile.Capability, row.ID) {
			continue
		}
		if s.profile.SupportTier == TierCandidateReadOnly &&
			!isCommandAllowedForCandidatePID(s.target.PID, row.ID, row.SafetyClass, false) {
			continue
		}
		// The registry can list the same CommandID more than once (per-PID
		// dossier rows sharing one wire command) — only run each distinct
		// command once per probe, matching CommandID.all() iteration in Rust.
		if containsPlan(plans, row.ID) {
			continue
		}
		plans = append(plans, diagCheckPlan{command: row.ID, policy: row.RuntimePolicy(), confidence: row.EvidenceConfidence()})
	}
	return plans
}

func containsPlan(plans []diagCheckPlan, id CommandID) bool {
	for _, p := range plans {
		if p.command == id {
			return true
		}
	}
	return false
}

func (s *DeviceSession) runDiagCheck(ctx context.Context, command CommandID, policy RuntimePolicy, confidence SupportEvidence) DiagCommandStatus {
	if command == CommandGetMode {
		return s.runDiagModeCheck(ctx, policy, confidence)
	}

	resp, err := s.SendCommand(ctx, command, nil)
	if err == nil {
		return s.diagSuccessStatus(command, policy, confidence, resp.ParsedFields, s.lastExecution, diagSuccessDetail(command, resp.ParsedFields))
	}
	return s.diagFailureStatus(command, policy, confidence, err, s.lastExecution, "")
}

func (s *DeviceSession) runDiagModeCheck(ctx context.Context, policy RuntimePolicy, confidence SupportEvidence) DiagCommandStatus {
	resp, err := s.SendCommand(ctx, CommandGetMode, nil)
	if err == nil {
		return s.diagSuccessStatus(CommandGetMode, policy, confidence, resp.ParsedFields, s.lastExecution, diagSuccessDetail(CommandGetMode, resp.ParsedFields))
	}

	primaryDetail := err.Error()
	primaryExecution := s.lastExecution
	resp, fallbackErr := s.SendCommand(ctx, CommandGetModeAlt, nil)
	if fallbackErr == nil {
		exec := s.lastExecution
		if exec == nil {
			exec = primaryExecution
		}
		return s.diagSuccessStatus(CommandGetMode, policy, confidence, resp.ParsedFields, exec, fmt.Sprintf("ok via GetModeAlt fallback (%s)", primaryDetail))
	}
	exec := s.lastExecution
	if exec == nil {
		exec = primaryExecution
	}
	return s.diagFailureStatus(CommandGetMode, policy, confidence, fallbackErr, exec,
		fmt.Sprintf("GetMode failed (%s); GetModeAlt failed", primaryDetail))
}

func (s *DeviceSession) diagSuccessStatus(command CommandID, policy RuntimePolicy, confidence SupportEvidence, facts map[string]uint32, exec *CommandExecutionReport, detail string) DiagCommandStatus {
	status := DiagCommandStatus{
		Command:        command,
		OK:             true,
		Confidence:     confidence,
		IsExperimental: policy == ExperimentalGate,
		Severity:       SeverityOK,
		ResponseStatus: StatusOk,
		Detail:         detail,
		ParsedFacts:    facts,
	}
	if exec != nil {
		status.Attempts = exec.Attempts
		status.Validator = exec.Validator
		status.ResponseStatus = exec.Status
		status.BytesWritten = exec.BytesWritten
		status.BytesRead = exec.BytesRead
	} else {
		status.Validator = "unknown"
	}
	return status
}

func (s *DeviceSession) diagFailureStatus(command CommandID, policy RuntimePolicy, confidence SupportEvidence, err error, exec *CommandExecutionReport, detailPrefix string) DiagCommandStatus {
	code := errorCode(err)
	detail := err.Error()
	if detailPrefix != "" {
		detail = fmt.Sprintf("%s (%s)", detailPrefix, err.Error())
	}
	status := DiagCommandStatus{
		Command:        command,
		OK:             false,
		Confidence:     confidence,
		IsExperimental: policy == ExperimentalGate,
		Severity:       classifyDiagFailure(command, policy, confidence, code, s.target.PID),
		ResponseStatus: StatusMalformed,
		ErrorCode:      code,
		Detail:         detail,
		ParsedFacts:    map[string]uint32{},
	}
	if exec != nil {
		status.Attempts = exec.Attempts
		status.Validator = exec.Validator
		status.ResponseStatus = exec.Status
		status.BytesWritten = exec.BytesWritten
		status.BytesRead = exec.BytesRead
	} else {
		status.Validator = "unknown"
	}
	return status
}

func errorCode(err error) ErrorCode {
	var pe *Error
	if e, ok := err.(*Error); ok {
		pe = e
	}
	if pe == nil {
		return ""
	}
	return pe.Code()
}
