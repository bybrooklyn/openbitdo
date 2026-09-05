package tui

import (
	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// This file ports the exact blocked-action gating semantics from the prior
// Rust TUI's state.rs (dashboard_firmware_disabled_reason,
// dashboard_mapping_disabled_reason, dashboard_unlock_disabled_reason) —
// same messages, same precedence order. The screen layout around them is
// new; the safety semantics are not.

// firmwareDisabledReason returns why firmware update is blocked for device,
// or "" if it's available.
func firmwareDisabledReason(device core.AppDevice, firmwareAvailable, unsafeAcknowledged, writeLockUntilRestart bool) string {
	switch {
	case !firmwareAvailable:
		return "Deferred in 0.0.3"
	case device.SupportTier != protocol.TierFull:
		return "Blocked until runtime and hardware confirmation"
	case !device.Capability.SupportsFirmware:
		return "No verified firmware path for this PID"
	case writeLockUntilRestart:
		return "Write locked until restart"
	case !unsafeAcknowledged:
		return "Requires explicit unsafe acknowledgement"
	default:
		return ""
	}
}

// mappingDisabledReason returns why the mapping editor is blocked for
// device, or "" if it's available.
func mappingDisabledReason(device core.AppDevice, mockMode, writeLockUntilRestart bool) string {
	hasMapping := device.Capability.SupportsJP108DedicatedMap ||
		(device.Capability.SupportsU2ButtonMap && device.Capability.SupportsU2SlotConfig)
	switch {
	case device.SupportTier != protocol.TierFull:
		return "Blocked until read/write/readback confirmation"
	case !hasMapping:
		return "No confirmed mapping editor for this PID"
	case device.Capability.SupportsU2ButtonMap && !mockMode:
		return "button-map framing not hardware-confirmed"
	case writeLockUntilRestart:
		return "Write locked until restart"
	default:
		return ""
	}
}

// candidateUnlockDisabledReason returns why the guarded candidate write
// probe is blocked for device, or "" if it's available.
func candidateUnlockDisabledReason(device core.AppDevice, advancedMode, acknowledgedRisk, writeLockUntilRestart bool) string {
	switch {
	case device.SupportTier != protocol.TierCandidateReadOnly:
		return "Only for candidate-readonly devices"
	case writeLockUntilRestart:
		return "Write locked until restart"
	case !advancedMode:
		return "Enable advanced mode first"
	case !acknowledgedRisk:
		return "Acknowledge local write risk first"
	default:
		return ""
	}
}

// blockedLinesForDevice returns the beginner-facing bullet list of what's
// blocked for device and why, shown on the device detail panel.
func blockedLinesForDevice(device core.AppDevice, firmwareAvailable, mockMode, unsafeAcknowledged, advancedMode, acknowledgedRisk, writeLockUntilRestart bool) []string {
	var lines []string
	if reason := firmwareDisabledReason(device, firmwareAvailable, unsafeAcknowledged, writeLockUntilRestart); reason != "" {
		lines = append(lines, "Firmware update: "+reason)
	}
	if reason := mappingDisabledReason(device, mockMode, writeLockUntilRestart); reason != "" {
		lines = append(lines, "Mapping editor: "+reason)
	}
	if device.SupportTier == protocol.TierCandidateReadOnly {
		if reason := candidateUnlockDisabledReason(device, advancedMode, acknowledgedRisk, writeLockUntilRestart); reason != "" {
			lines = append(lines, "Guarded write probe: "+reason)
		}
	}
	return lines
}
