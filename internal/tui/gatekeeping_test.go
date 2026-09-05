package tui

import (
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// These tests port the behavioral contracts from the prior Rust TUI's
// tests.rs — quick_action_matrix_blocks_update_for_read_only and
// dashboard_candidate_write_probe_requires_advanced_and_ack — against the
// Go equivalents of state.rs's dashboard_*_disabled_reason functions.

func TestFirmwareDisabledReason_BlockedWithoutAck(t *testing.T) {
	full := core.AppDevice{SupportTier: protocol.TierFull, Capability: protocol.PidCapability{SupportsFirmware: true}}
	if reason := firmwareDisabledReason(full, true, false, false); reason == "" {
		t.Fatal("expected firmware update blocked without unsafe acknowledgement")
	}
}

func TestFirmwareDisabledReason_DeferredByDefault(t *testing.T) {
	full := core.AppDevice{SupportTier: protocol.TierFull, Capability: protocol.PidCapability{SupportsFirmware: true}}
	if reason := firmwareDisabledReason(full, false, true, false); reason != "Deferred in 0.0.3" {
		t.Fatalf("expected deferred reason, got %q", reason)
	}
}

func TestFirmwareDisabledReason_EnabledForFullTierWithAckAndFeatureGate(t *testing.T) {
	full := core.AppDevice{SupportTier: protocol.TierFull, Capability: protocol.PidCapability{SupportsFirmware: true}}
	if reason := firmwareDisabledReason(full, true, true, false); reason != "" {
		t.Fatalf("expected firmware update enabled for full-tier device with ack, got reason %q", reason)
	}
}

func TestFirmwareDisabledReason_BlockedForNonFullTierEvenWithAck(t *testing.T) {
	readOnly := core.AppDevice{SupportTier: protocol.TierCandidateReadOnly, Capability: protocol.PidCapability{SupportsFirmware: true}}
	reason := firmwareDisabledReason(readOnly, true, true, false)
	if reason == "" {
		t.Fatal("expected firmware update blocked for a non-full-tier device even with ack")
	}
	if reason != "Blocked until runtime and hardware confirmation" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestFirmwareDisabledReason_WriteLockOverridesEverything(t *testing.T) {
	full := core.AppDevice{SupportTier: protocol.TierFull, Capability: protocol.PidCapability{SupportsFirmware: true}}
	if reason := firmwareDisabledReason(full, true, true, true); reason != "Write locked until restart" {
		t.Fatalf("expected write-lock reason to take precedence, got %q", reason)
	}
}

func TestMappingDisabledReason_RequiresConfirmedMappingCapability(t *testing.T) {
	full := core.AppDevice{SupportTier: protocol.TierFull}
	if reason := mappingDisabledReason(full, false, false); reason != "No confirmed mapping editor for this PID" {
		t.Fatalf("got %q", reason)
	}
	full.Capability.SupportsJP108DedicatedMap = true
	if reason := mappingDisabledReason(full, false, false); reason != "" {
		t.Fatalf("expected enabled once JP108 mapping capability is set, got %q", reason)
	}
}

func TestMappingDisabledReason_BlocksRealUltimate2Mapping(t *testing.T) {
	full := core.AppDevice{SupportTier: protocol.TierFull, Capability: protocol.PidCapability{SupportsU2ButtonMap: true, SupportsU2SlotConfig: true}}
	if reason := mappingDisabledReason(full, false, false); reason != "button-map framing not hardware-confirmed" {
		t.Fatalf("expected real Ultimate2 mapping block, got %q", reason)
	}
	if reason := mappingDisabledReason(full, true, false); reason != "" {
		t.Fatalf("expected mock-only Ultimate2 preview enabled, got %q", reason)
	}
}

// TestCandidateUnlockDisabledReason_RequiresAdvancedThenAck ports Rust's
// dashboard_candidate_write_probe_requires_advanced_and_ack exactly,
// including the precedence order and the literal reason strings.
func TestCandidateUnlockDisabledReason_RequiresAdvancedThenAck(t *testing.T) {
	device := core.AppDevice{SupportTier: protocol.TierCandidateReadOnly}

	reason := candidateUnlockDisabledReason(device, false, false, false)
	if reason != "Enable advanced mode first" {
		t.Fatalf("expected advanced-mode gate first, got %q", reason)
	}

	reason = candidateUnlockDisabledReason(device, true, false, false)
	if reason != "Acknowledge local write risk first" {
		t.Fatalf("expected risk-ack gate once advanced mode is on, got %q", reason)
	}

	reason = candidateUnlockDisabledReason(device, true, true, false)
	if reason != "" {
		t.Fatalf("expected the guarded probe enabled once both gates are satisfied, got %q", reason)
	}
}

func TestCandidateUnlockDisabledReason_OnlyForCandidateReadOnlyTier(t *testing.T) {
	full := core.AppDevice{SupportTier: protocol.TierFull}
	if reason := candidateUnlockDisabledReason(full, true, true, false); reason != "Only for candidate-readonly devices" {
		t.Fatalf("expected full-tier devices to be rejected outright, got %q", reason)
	}
}
