package protocol

import (
	"context"
	"fmt"
	"time"
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
// written to the wire.
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

// GetMode reads the device's current mode, falling back to GetModeAlt.
func (s *DeviceSession) GetMode(ctx context.Context) (ModeState, error) {
	resp, err := s.SendCommand(ctx, CommandGetMode, nil)
	if err == nil {
		if mode, ok := resp.ParsedFields["mode"]; ok {
			return ModeState{Mode: byte(mode), Source: "GetMode"}, nil
		}
	}
	resp, err = s.SendCommand(ctx, CommandGetModeAlt, nil)
	if err != nil {
		return ModeState{}, err
	}
	return ModeState{Mode: byte(resp.ParsedFields["mode"]), Source: "GetModeAlt"}, nil
}

// SetMode writes a new device mode via SetModeDInput, then reads it back.
func (s *DeviceSession) SetMode(ctx context.Context, mode byte) (ModeState, error) {
	row, err := s.ensureCommandAllowed(CommandSetModeDInput)
	if err != nil {
		return ModeState{}, err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) < 5 {
		return ModeState{}, errInvalidInput("SetModeDInput payload shorter than expected")
	}
	payload[4] = mode
	if _, err := s.sendRow(ctx, row, payload); err != nil {
		return ModeState{}, err
	}
	return s.GetMode(ctx)
}

// ReadProfile reads a profile slot as a raw ProfileBlob wrapper (payload is
// the raw response bytes, matching Rust's behavior).
func (s *DeviceSession) ReadProfile(ctx context.Context, slot byte) (ProfileBlob, error) {
	row, err := s.ensureCommandAllowed(CommandReadProfile)
	if err != nil {
		return ProfileBlob{}, err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) > 3 {
		payload[3] = slot
	}
	resp, err := s.sendRow(ctx, row, payload)
	if err != nil {
		return ProfileBlob{}, err
	}
	return ProfileBlob{Slot: slot, Payload: resp.Raw}, nil
}

// WriteProfile writes a serialized ProfileBlob into a profile slot.
func (s *DeviceSession) WriteProfile(ctx context.Context, slot byte, profile ProfileBlob) error {
	row, err := s.ensureCommandAllowed(CommandWriteProfile)
	if err != nil {
		return err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) > 3 {
		payload[3] = slot
	}

	serialized := profile.ToBytes()
	copyLen := min(max(len(payload)-8, 0), len(serialized))
	if copyLen > 0 {
		copy(payload[8:8+copyLen], serialized[:copyLen])
	}
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// JP108ReadDedicatedMappings reads the JP108 dedicated-button mapping table.
func (s *DeviceSession) JP108ReadDedicatedMappings(ctx context.Context) ([]IndexedUsage, error) {
	resp, err := s.SendCommand(ctx, CommandJp108ReadDedicatedMappings, nil)
	if err != nil {
		return nil, err
	}
	return parseIndexedU16Table(resp.Raw, 10), nil
}

// JP108WriteDedicatedMapping writes one JP108 dedicated-button mapping entry.
func (s *DeviceSession) JP108WriteDedicatedMapping(ctx context.Context, index byte, targetHIDUsage uint16) error {
	row, err := s.ensureCommandAllowed(CommandJp108WriteDedicatedMapping)
	if err != nil {
		return err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) < 7 {
		return errInvalidInput("Jp108WriteDedicatedMapping payload shorter than expected")
	}
	payload[4] = index
	payload[5] = byte(targetHIDUsage)
	payload[6] = byte(targetHIDUsage >> 8)
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// U2GetCurrentSlot reads the Ultimate2 device's active config slot.
func (s *DeviceSession) U2GetCurrentSlot(ctx context.Context) (byte, error) {
	resp, err := s.SendCommand(ctx, CommandU2GetCurrentSlot, nil)
	if err != nil {
		return 0, err
	}
	return byte(resp.ParsedFields["slot"]), nil
}

// U2ReadConfigSlot reads a raw Ultimate2 config-slot blob.
func (s *DeviceSession) U2ReadConfigSlot(ctx context.Context, slot byte) ([]byte, error) {
	row, err := s.ensureCommandAllowed(CommandU2ReadConfigSlot)
	if err != nil {
		return nil, err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) > 4 {
		payload[4] = slot
	}
	resp, err := s.sendRow(ctx, row, payload)
	if err != nil {
		return nil, err
	}
	return resp.Raw, nil
}

// U2WriteConfigSlot writes a raw Ultimate2 config-slot blob.
func (s *DeviceSession) U2WriteConfigSlot(ctx context.Context, slot byte, configBlob []byte) error {
	row, err := s.ensureCommandAllowed(CommandU2WriteConfigSlot)
	if err != nil {
		return err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) < 8 {
		return errInvalidInput("U2WriteConfigSlot payload shorter than expected")
	}
	payload[4] = slot
	copyLen := min(len(configBlob), max(len(payload)-8, 0))
	if copyLen > 0 {
		copy(payload[8:8+copyLen], configBlob[:copyLen])
	}
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// IndexedUsage is one (button index, HID usage) mapping entry.
type IndexedUsage struct {
	Index byte
	Usage uint16
}

// U2ReadButtonMap reads the Ultimate2 button map for a slot.
func (s *DeviceSession) U2ReadButtonMap(ctx context.Context, slot byte) ([]IndexedUsage, error) {
	row, err := s.ensureCommandAllowed(CommandU2ReadButtonMap)
	if err != nil {
		return nil, err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) > 4 {
		payload[4] = slot
	}
	resp, err := s.sendRow(ctx, row, payload)
	if err != nil {
		return nil, err
	}
	return parseIndexedU16Table(resp.Raw, 17), nil
}

// U2WriteButtonMap writes a set of Ultimate2 button-map entries for a slot.
func (s *DeviceSession) U2WriteButtonMap(ctx context.Context, slot byte, mappings []IndexedUsage) error {
	row, err := s.ensureCommandAllowed(CommandU2WriteButtonMap)
	if err != nil {
		return err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) < 8 {
		return errInvalidInput("U2WriteButtonMap payload shorter than expected")
	}
	payload[4] = slot
	for _, m := range mappings {
		pos := 8 + int(m.Index)*2
		if pos+1 < len(payload) {
			payload[pos] = byte(m.Usage)
			payload[pos+1] = byte(m.Usage >> 8)
		}
	}
	_, err = s.sendRow(ctx, row, payload)
	return err
}

// U2SetMode writes a new Ultimate2 mode.
func (s *DeviceSession) U2SetMode(ctx context.Context, mode byte) (ModeState, error) {
	row, err := s.ensureCommandAllowed(CommandU2SetMode)
	if err != nil {
		return ModeState{}, err
	}
	payload := append([]byte(nil), row.Request...)
	if len(payload) < 5 {
		return ModeState{}, errInvalidInput("U2SetMode payload shorter than expected")
	}
	payload[4] = mode
	if _, err := s.sendRow(ctx, row, payload); err != nil {
		return ModeState{}, err
	}
	return ModeState{Mode: mode, Source: "U2SetMode"}, nil
}

// EnterBootloader dispatches to the JP108/U2/generic 3-stage bootloader-enter
// sequence based on the device's capability.
func (s *DeviceSession) EnterBootloader(ctx context.Context) error {
	switch {
	case s.usesJP108FirmwarePath():
		_, err := s.SendCommand(ctx, CommandJp108EnterBootloader, nil)
		return err
	case s.usesU2FirmwarePath():
		_, err := s.SendCommand(ctx, CommandU2EnterBootloader, nil)
		return err
	}
	if _, err := s.SendCommand(ctx, CommandEnterBootloaderA, nil); err != nil {
		return err
	}
	if _, err := s.SendCommand(ctx, CommandEnterBootloaderB, nil); err != nil {
		return err
	}
	_, err := s.SendCommand(ctx, CommandEnterBootloaderC, nil)
	return err
}

// FirmwareTransfer chunks image and sends it (unless dryRun), then commits.
func (s *DeviceSession) FirmwareTransfer(ctx context.Context, image []byte, chunkSize int, dryRun bool) (FirmwareTransferReport, error) {
	if chunkSize == 0 {
		return FirmwareTransferReport{}, errInvalidInput("chunk size must be greater than zero")
	}

	command := s.firmwareChunkCommand()
	maxPayload := 0
	if row, ok := FindCommand(command); ok {
		maxPayload = max(len(row.Request)-firmwareChunkOffset(command), 0)
	}
	if maxPayload == 0 || chunkSize > maxPayload {
		return FirmwareTransferReport{}, errInvalidInput("chunk size %d exceeds firmware payload limit %d", chunkSize, maxPayload)
	}

	chunkCount := (len(image) + chunkSize - 1) / chunkSize
	if dryRun {
		return FirmwareTransferReport{BytesTotal: len(image), ChunkSize: chunkSize, ChunksSent: chunkCount, DryRun: true}, nil
	}

	for offset := 0; offset < len(image); offset += chunkSize {
		end := min(offset+chunkSize, len(image))
		if _, err := s.SendFirmwareChunk(ctx, image[offset:end]); err != nil {
			return FirmwareTransferReport{}, err
		}
	}
	if err := s.FirmwareCommit(ctx); err != nil {
		return FirmwareTransferReport{}, err
	}
	return FirmwareTransferReport{BytesTotal: len(image), ChunkSize: chunkSize, ChunksSent: chunkCount, DryRun: false}, nil
}

// SendFirmwareChunk sends one firmware chunk and returns the bytes copied
// into the wire payload.
func (s *DeviceSession) SendFirmwareChunk(ctx context.Context, chunk []byte) (int, error) {
	command := s.firmwareChunkCommand()
	row, err := s.ensureCommandAllowed(command)
	if err != nil {
		return 0, err
	}
	payload := append([]byte(nil), row.Request...)
	offset := firmwareChunkOffset(command)
	copyLen := min(len(chunk), max(len(payload)-offset, 0))
	if copyLen == 0 {
		return 0, errInvalidInput("firmware chunk payload shorter than expected for %s", command)
	}
	copy(payload[offset:offset+copyLen], chunk[:copyLen])
	if _, err := s.sendRow(ctx, row, payload); err != nil {
		return 0, err
	}
	return copyLen, nil
}

// FirmwareCommit sends the firmware-commit command for this device's path.
func (s *DeviceSession) FirmwareCommit(ctx context.Context) error {
	_, err := s.SendCommand(ctx, s.firmwareCommitCommand(), nil)
	return err
}

// ExitBootloader dispatches to the JP108/U2/generic bootloader-exit command.
func (s *DeviceSession) ExitBootloader(ctx context.Context) error {
	switch {
	case s.usesJP108FirmwarePath():
		_, err := s.SendCommand(ctx, CommandJp108ExitBootloader, nil)
		return err
	case s.usesU2FirmwarePath():
		_, err := s.SendCommand(ctx, CommandU2ExitBootloader, nil)
		return err
	}
	_, err := s.SendCommand(ctx, CommandExitBootloader, nil)
	return err
}

// SendCommand sends a registered command, using its declared request payload
// unless overridePayload is provided.
func (s *DeviceSession) SendCommand(ctx context.Context, command CommandID, overridePayload []byte) (ResponseFrame, error) {
	row, err := s.ensureCommandAllowed(command)
	if err != nil {
		return ResponseFrame{}, err
	}
	payload := overridePayload
	if payload == nil {
		payload = row.Request
	}
	return s.sendRow(ctx, row, payload)
}

func (s *DeviceSession) sendRow(ctx context.Context, row CommandRow, payload []byte) (ResponseFrame, error) {
	bytesWritten, err := s.transport.Write(payload)
	if err != nil {
		return ResponseFrame{}, err
	}

	if row.ExpectedResponse == "none" {
		s.recordExecution(CommandExecutionReport{
			Command: row.ID, Attempts: 1, Validator: s.validatorName(row),
			Status: StatusOk, BytesWritten: bytesWritten,
		})
		return ResponseFrame{Status: StatusOk, ParsedFields: map[string]uint32{}}, nil
	}

	timeoutMs := s.timeoutForCommand(row)
	expectedMinLen := minimumResponseLen(row.ID)
	attemptsTotal := row.rowRetryAttempts(s.config.RetryPolicy)

	lastStatus := StatusMalformed
	lastLen := 0

	for attempt := uint8(1); attempt <= attemptsTotal; attempt++ {
		raw, err := s.readResponseReassembled(ctx, timeoutMs, expectedMinLen)
		switch err {
		case nil:
			status := ValidateResponse(row.ID, raw)
			if status == StatusOk {
				s.recordExecution(CommandExecutionReport{
					Command: row.ID, Attempts: attempt, Validator: s.validatorName(row),
					Status: StatusOk, BytesWritten: bytesWritten, BytesRead: len(raw),
				})
				return ResponseFrame{Raw: raw, Status: status, ParsedFields: parseFields(row.ID, raw)}, nil
			}
			lastStatus, lastLen = status, len(raw)
		case ErrTimeout:
			lastStatus, lastLen = StatusMalformed, 0
		default:
			report := CommandExecutionReport{
				Command: row.ID, Attempts: attempt, Validator: s.validatorName(row),
				Status: StatusMalformed, BytesWritten: bytesWritten, ErrorCode: errorCode(err),
			}
			s.recordExecution(report)
			return ResponseFrame{}, err
		}

		if attempt < attemptsTotal && s.config.RetryPolicy.BackoffMs > 0 {
			if err := ctxSleep(ctx, time.Duration(s.config.RetryPolicy.BackoffMs)*time.Millisecond); err != nil {
				return ResponseFrame{}, err
			}
		}
	}

	if lastStatus == StatusInvalid {
		err := errInvalidResponse(row.ID, "response signature mismatch")
		s.recordExecution(CommandExecutionReport{
			Command: row.ID, Attempts: attemptsTotal, Validator: s.validatorName(row),
			Status: StatusInvalid, BytesWritten: bytesWritten, BytesRead: lastLen, ErrorCode: err.Code(),
		})
		return ResponseFrame{}, err
	}
	malformedErr := errMalformedResponse(row.ID, lastLen)
	s.recordExecution(CommandExecutionReport{
		Command: row.ID, Attempts: attemptsTotal, Validator: s.validatorName(row),
		Status: StatusMalformed, BytesWritten: bytesWritten, BytesRead: lastLen, ErrorCode: malformedErr.Code(),
	})
	return ResponseFrame{}, malformedErr
}

func (row CommandRow) rowRetryAttempts(policy RetryPolicy) uint8 {
	if policy.MaxAttempts < 1 {
		return 1
	}
	return policy.MaxAttempts
}

// readResponseReassembled reads up to 3 bounded 64-byte chunks, since some
// devices split replies across multiple reads.
func (s *DeviceSession) readResponseReassembled(ctx context.Context, timeoutMs uint64, expectedMinLen int) ([]byte, error) {
	var raw []byte
	for i := 0; i < 3; i++ {
		chunk, err := s.transport.Read(ctx, 64, timeoutMs)
		if err != nil {
			return nil, err
		}
		if len(chunk) == 0 {
			continue
		}
		raw = append(raw, chunk...)
		if len(raw) >= expectedMinLen {
			break
		}
	}
	if len(raw) == 0 {
		return nil, ErrTimeout
	}
	return raw, nil
}

func (s *DeviceSession) recordExecution(report CommandExecutionReport) {
	s.lastExecution = &report
	if s.config.TraceEnabled {
		s.trace = append(s.trace, report)
	}
}

func (s *DeviceSession) timeoutForCommand(row CommandRow) uint64 {
	switch row.SafetyClass {
	case UnsafeFirmware:
		return s.config.TimeoutProfile.FirmwareMs
	case SafeRead:
		return s.config.TimeoutProfile.ProbeMs
	default: // SafeWrite, UnsafeBoot
		return s.config.TimeoutProfile.IOMs
	}
}

func (s *DeviceSession) validatorName(row CommandRow) string {
	return fmt.Sprintf("pid=%#04x;signature=%s", s.target.PID, row.ExpectedResponse)
}

// ensureCommandAllowed is the 4-gate authorization check every command must
// pass before it is ever written to the wire:
//  1. confidence/runtime-policy gate (experimental / blocked-until-confirmed)
//  2. family/capability/PID applicability
//  3. support-tier restriction (candidate-readonly whitelist)
//  4. explicit unsafe double-confirmation (allow_unsafe && brick_risk_ack)
func (s *DeviceSession) ensureCommandAllowed(command CommandID) (CommandRow, error) {
	row, ok := FindCommand(command)
	if !ok {
		return CommandRow{}, errUnknownCommand(command)
	}
	promotedFullSupportPath := s.allowPidScopedFullSupportPath(row)
	candidateWriteUnlock := s.allowCandidateRuntimeWritePath(command, row.SafetyClass)

	switch row.RuntimePolicy() {
	case EnabledDefault:
	case ExperimentalGate:
		if !s.config.Experimental && !promotedFullSupportPath {
			return CommandRow{}, errExperimentalRequired(command)
		}
	case BlockedUntilConfirmed:
		if !promotedFullSupportPath && !candidateWriteUnlock {
			return CommandRow{}, errUnsupportedForPid(command, s.target.PID)
		}
	}

	if !isCommandAllowedByFamily(s.profile.ProtocolFamily, command) ||
		!isCommandAllowedByCapability(s.profile.Capability, command) ||
		!CommandAppliesToPID(row, s.target.PID) {
		return CommandRow{}, errUnsupportedForPid(command, s.target.PID)
	}

	if s.profile.SupportTier == TierCandidateReadOnly &&
		!isCommandAllowedForCandidatePID(s.target.PID, command, row.SafetyClass, candidateWriteUnlock) {
		return CommandRow{}, errUnsupportedForPid(command, s.target.PID)
	}

	if row.SafetyClass.IsUnsafe() {
		if s.profile.SupportTier != TierFull {
			return CommandRow{}, errUnsupportedForPid(command, s.target.PID)
		}
		if !s.config.AllowUnsafe || !s.config.BrickRiskAck {
			return CommandRow{}, errUnsafeCommandDenied(command)
		}
	}

	if row.SafetyClass == SafeWrite && s.profile.SupportTier != TierFull && !candidateWriteUnlock {
		return CommandRow{}, errUnsupportedForPid(command, s.target.PID)
	}

	return row, nil
}

func (s *DeviceSession) usesJP108FirmwarePath() bool {
	return s.profile.Capability.SupportsJP108DedicatedMap
}

func (s *DeviceSession) usesU2FirmwarePath() bool {
	return s.profile.Capability.SupportsU2SlotConfig && s.profile.Capability.SupportsU2ButtonMap
}

func (s *DeviceSession) firmwareChunkCommand() CommandID {
	switch {
	case s.usesJP108FirmwarePath():
		return CommandJp108FirmwareChunk
	case s.usesU2FirmwarePath():
		return CommandU2FirmwareChunk
	default:
		return CommandFirmwareChunk
	}
}

func (s *DeviceSession) firmwareCommitCommand() CommandID {
	switch {
	case s.usesJP108FirmwarePath():
		return CommandJp108FirmwareCommit
	case s.usesU2FirmwarePath():
		return CommandU2FirmwareCommit
	default:
		return CommandFirmwareCommit
	}
}

func (s *DeviceSession) allowPidScopedFullSupportPath(row CommandRow) bool {
	if s.profile.SupportTier != TierFull || len(row.AppliesTo) == 0 {
		return false
	}
	applies := false
	for _, p := range row.AppliesTo {
		if p == s.target.PID {
			applies = true
			break
		}
	}
	if !applies {
		return false
	}
	return row.OperationGroup == "JP108Dedicated" || row.OperationGroup == "Ultimate2Core" || row.OperationGroup == "Firmware"
}

var candidateUnlockableWrites = map[CommandID]bool{
	CommandSetModeDInput:              true,
	CommandWriteProfile:               true,
	CommandU2WriteButtonMap:           true,
	CommandU2WriteConfigSlot:          true,
	CommandU2SetMode:                  true,
	CommandJp108WriteDedicatedMapping: true,
	CommandJp108WriteFeatureFlags:     true,
	CommandJp108WriteVoice:            true,
}

func (s *DeviceSession) allowCandidateRuntimeWritePath(command CommandID, safety SafetyClass) bool {
	return s.profile.SupportTier == TierCandidateReadOnly &&
		s.config.CandidateWriteUnlock &&
		s.config.Experimental &&
		safety == SafeWrite &&
		candidateUnlockableWrites[command]
}

func firmwareChunkOffset(command CommandID) int {
	if command == CommandJp108FirmwareChunk || command == CommandU2FirmwareChunk {
		return 5
	}
	return 4
}

// ctxSleep sleeps for d or returns early with ctx.Err() if ctx is cancelled.
func ctxSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
