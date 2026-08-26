package protocol

import (
	"context"
	"fmt"
	"time"
)

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
