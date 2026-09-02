package core

import (
	"fmt"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// AppPolicyGateReason classifies why a policy gate denied an operation.
type AppPolicyGateReason string

const (
	ReasonUnsupportedPid        AppPolicyGateReason = "UnsupportedPid"
	ReasonFeatureUnavailable    AppPolicyGateReason = "FeatureUnavailable"
	ReasonNotHardwareConfirmed  AppPolicyGateReason = "NotHardwareConfirmed"
	ReasonUnsafeFlagsMissing    AppPolicyGateReason = "UnsafeFlagsMissing"
	ReasonExperimentalRequired  AppPolicyGateReason = "ExperimentalRequired"
	ReasonVersionMismatch       AppPolicyGateReason = "VersionMismatch"
	ReasonImageValidationFailed AppPolicyGateReason = "ImageValidationFailed"
)

// AppPolicyGateResult is the outcome of a preflight-style policy check.
type AppPolicyGateResult struct {
	Allowed bool
	Reason  AppPolicyGateReason
	Message string
}

// ErrorKind classifies an Error for callers that need to branch on it.
type ErrorKind string

const (
	KindPolicyDenied ErrorKind = "PolicyDenied"
	KindIO           ErrorKind = "Io"
	KindProtocol     ErrorKind = "Protocol"
	KindDownload     ErrorKind = "Download"
	KindManifest     ErrorKind = "Manifest"
	KindNotFound     ErrorKind = "NotFound"
	KindInvalidState ErrorKind = "InvalidState"
	// KindDeviceDisconnected is distinct from KindIO/KindProtocol: it means
	// the operation failed AND a fresh re-enumeration (protocol.
	// IsDevicePresent) confirmed the device is actually gone, not just that
	// some I/O call returned an error. Lets the TUI show a clear
	// disconnected state instead of a generic error.
	KindDeviceDisconnected ErrorKind = "DeviceDisconnected"
)

// Error is internal/core's error type.
type Error struct {
	Kind    ErrorKind
	Reason  AppPolicyGateReason // set only when Kind == KindPolicyDenied
	Message string
	Cause   error
}

func (e *Error) Error() string {
	switch e.Kind {
	case KindPolicyDenied:
		return fmt.Sprintf("policy denied: %s: %s", e.Reason, e.Message)
	case KindIO:
		return fmt.Sprintf("io error: %s", e.Message)
	case KindProtocol:
		return fmt.Sprintf("protocol error: %s", e.Message)
	case KindDownload:
		return fmt.Sprintf("download error: %s", e.Message)
	case KindManifest:
		return fmt.Sprintf("manifest error: %s", e.Message)
	case KindNotFound:
		return fmt.Sprintf("not found: %s", e.Message)
	case KindDeviceDisconnected:
		return fmt.Sprintf("device disconnected: %s", e.Message)
	default: // KindInvalidState
		return fmt.Sprintf("invalid state: %s", e.Message)
	}
}

func (e *Error) Unwrap() error { return e.Cause }

func errPolicyDenied(reason AppPolicyGateReason, format string, a ...any) *Error {
	return &Error{Kind: KindPolicyDenied, Reason: reason, Message: fmt.Sprintf(format, a...)}
}

func errIO(cause error) *Error {
	return &Error{Kind: KindIO, Message: cause.Error(), Cause: cause}
}

func errProtocol(cause error) *Error {
	return &Error{Kind: KindProtocol, Message: cause.Error(), Cause: cause}
}

func errDownload(format string, a ...any) *Error {
	return &Error{Kind: KindDownload, Message: fmt.Sprintf(format, a...)}
}

func errManifest(format string, a ...any) *Error {
	return &Error{Kind: KindManifest, Message: fmt.Sprintf(format, a...)}
}

func errNotFound(format string, a ...any) *Error {
	return &Error{Kind: KindNotFound, Message: fmt.Sprintf(format, a...)}
}

func errInvalidState(format string, a ...any) *Error {
	return &Error{Kind: KindInvalidState, Message: fmt.Sprintf(format, a...)}
}

func errDeviceDisconnected(target protocol.VidPid) *Error {
	return &Error{Kind: KindDeviceDisconnected, Message: fmt.Sprintf("%s is no longer connected", target), Cause: protocol.ErrDeviceDisconnected(target)}
}
