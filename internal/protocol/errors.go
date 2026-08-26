package protocol

import "fmt"

// ErrorCode is a stable, machine-readable classification for a Error.
type ErrorCode string

const (
	CodeTransport            ErrorCode = "Transport"
	CodeTimeout              ErrorCode = "Timeout"
	CodeInvalidResponse      ErrorCode = "InvalidResponse"
	CodeMalformedResponse    ErrorCode = "MalformedResponse"
	CodeUnsupportedForPid    ErrorCode = "UnsupportedForPid"
	CodeExperimentalRequired ErrorCode = "ExperimentalRequired"
	CodeUnsafeCommandDenied  ErrorCode = "UnsafeCommandDenied"
	CodeUnknownPid           ErrorCode = "UnknownPid"
	CodeInvalidInput         ErrorCode = "InvalidInput"
	CodeUnknownCommand       ErrorCode = "UnknownCommand"
	CodeDeviceNotOpen        ErrorCode = "DeviceNotOpen"
)

// ErrTimeout is returned when a device response does not arrive in time.
var ErrTimeout = &Error{code: CodeTimeout, message: "timeout while waiting for device response"}

// Error is the protocol package's error type; every error carries a stable
// Code() so callers (diagnostics, reports) can classify failures without
// string matching.
type Error struct {
	code    ErrorCode
	message string
}

func (e *Error) Error() string { return e.message }

// Code returns the stable machine-readable error classification.
func (e *Error) Code() ErrorCode { return e.code }

func errTransport(format string, a ...any) *Error {
	return &Error{code: CodeTransport, message: fmt.Sprintf("transport error: %s", fmt.Sprintf(format, a...))}
}

func errInvalidResponse(command CommandID, reason string) *Error {
	return &Error{code: CodeInvalidResponse, message: fmt.Sprintf("invalid response for %s: %s", command, reason)}
}

func errMalformedResponse(command CommandID, length int) *Error {
	return &Error{code: CodeMalformedResponse, message: fmt.Sprintf("malformed response for %s: len=%d", command, length)}
}

func errUnsupportedForPid(command CommandID, pid uint16) *Error {
	return &Error{code: CodeUnsupportedForPid, message: fmt.Sprintf("unsupported command %s for PID %#04x", command, pid)}
}

func errExperimentalRequired(command CommandID) *Error {
	return &Error{code: CodeExperimentalRequired, message: fmt.Sprintf("inferred command %s requires --experimental", command)}
}

func errUnsafeCommandDenied(command CommandID) *Error {
	return &Error{code: CodeUnsafeCommandDenied, message: fmt.Sprintf("unsafe command %s requires --unsafe and --i-understand-brick-risk", command)}
}

func errUnknownPid(pid uint16) *Error {
	return &Error{code: CodeUnknownPid, message: fmt.Sprintf("unknown PID %#04x", pid)}
}

func errInvalidInput(format string, a ...any) *Error {
	return &Error{code: CodeInvalidInput, message: fmt.Sprintf("invalid input: %s", fmt.Sprintf(format, a...))}
}

func errUnknownCommand(command CommandID) *Error {
	return &Error{code: CodeUnknownCommand, message: fmt.Sprintf("command definition not found: %s", command)}
}

func errDeviceNotOpen(target VidPid) *Error {
	return &Error{code: CodeDeviceNotOpen, message: fmt.Sprintf("device not open for %s", target)}
}
