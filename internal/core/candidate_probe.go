package core

import (
	"context"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// CandidateWriteProbe is the guarded non-firmware write/readback probe for
// candidate-readonly devices. It requires advanced mode, an explicit local
// write-risk acknowledgement, AND a per-PID on-disk unlock file — three
// independent gates, all caller-supplied via policy so the UI is the one
// place that ceremony is enforced end-to-end.
func (c *OpenBitdoCore) CandidateWriteProbe(ctx context.Context, vidPid protocol.VidPid, policy RuntimeUnlockPolicy) (RuntimeUnlockReport, error) {
	device := appDeviceFromProfile(vidPid, "", true)
	scorecard := device.Scorecard()

	if device.SupportTier != protocol.TierCandidateReadOnly {
		return deniedUnlockReport(vidPid, scorecard, "Runtime unlock is only available for candidate-readonly devices."), nil
	}
	if !(policy.AdvancedMode && policy.AcknowledgedRisk) {
		return deniedUnlockReport(vidPid, scorecard, "Enable advanced mode and acknowledge local write risk before running the probe."), nil
	}
	if !policy.UnlockFilePresent {
		path := policy.UnlockFilePath
		if path == "" {
			path = "the per-PID unlock file"
		}
		return deniedUnlockReport(vidPid, scorecard, "Create "+path+" with candidate_write_unlock = true before running the probe."), nil
	}
	if !(device.Capability.SupportsMode || device.Capability.SupportsProfileRW) {
		return deniedUnlockReport(vidPid, scorecard, "This candidate has no non-firmware safe-write operation available for probing."), nil
	}

	if c.config.MockMode {
		var commands []string
		if device.Capability.SupportsMode {
			commands = append(commands, "SetModeDInput")
		}
		if device.Capability.SupportsProfileRW {
			commands = append(commands, "WriteProfile")
		}
		return RuntimeUnlockReport{
			VidPid: vidPid, Allowed: true, Operation: "candidate-write-probe", CommandsAttempted: commands,
			WriteApplied: true, ReadbackVerified: true, Message: "Mock candidate write probe completed with readback verification.",
			Scorecard: scorecard,
		}, nil
	}

	config := protocol.SessionConfig{
		Experimental: true, CandidateWriteUnlock: true,
		RetryPolicy: protocol.DefaultRetryPolicy(), TimeoutProfile: protocol.DefaultTimeoutProfile(), TraceEnabled: true,
	}
	session, err := protocol.NewDeviceSession(ctx, protocol.NewHidTransport(), vidPid, config)
	if err != nil {
		return RuntimeUnlockReport{}, errProtocol(err)
	}
	defer func() { _ = session.Close() }()

	var commands []string
	writeApplied, readbackVerified := false, false
	var failure error

	if device.Capability.SupportsMode {
		if err := guardedModeWriteProbe(ctx, session); err != nil {
			failure = err
		} else {
			commands = append(commands, "SetModeDInput")
			writeApplied, readbackVerified = true, true
		}
	}
	if failure == nil && device.Capability.SupportsProfileRW {
		if err := guardedProfileWriteProbe(ctx, session); err != nil {
			failure = err
		} else {
			commands = append(commands, "WriteProfile")
			writeApplied, readbackVerified = true, true
		}
	}

	if failure != nil {
		return RuntimeUnlockReport{
			VidPid: vidPid, Allowed: true, Operation: "candidate-write-probe", CommandsAttempted: commands,
			WriteApplied: writeApplied, ReadbackVerified: false, WriteLockRequired: true,
			Message: "Candidate write probe failed: " + failure.Error(), Scorecard: scorecard,
		}, nil
	}

	return RuntimeUnlockReport{
		VidPid: vidPid, Allowed: true, Operation: "candidate-write-probe", CommandsAttempted: commands,
		WriteApplied: writeApplied, ReadbackVerified: readbackVerified,
		Message: "Candidate write probe completed with readback verification.", Scorecard: scorecard,
	}, nil
}

func guardedModeWriteProbe(ctx context.Context, session *protocol.DeviceSession) error {
	before, err := session.GetMode(ctx)
	if err != nil {
		return err
	}
	if _, err := session.SetMode(ctx, before.Mode); err != nil {
		return err
	}
	after, err := session.GetMode(ctx)
	if err != nil {
		return err
	}
	if after.Mode != before.Mode {
		return errInvalidState("mode readback mismatch: before=%d after=%d", before.Mode, after.Mode)
	}
	return nil
}

func guardedProfileWriteProbe(ctx context.Context, session *protocol.DeviceSession) error {
	before, err := session.ReadProfile(ctx, 1)
	if err != nil {
		return err
	}
	if err := session.WriteProfile(ctx, before.Slot, before); err != nil {
		return err
	}
	after, err := session.ReadProfile(ctx, before.Slot)
	if err != nil {
		return err
	}
	if len(after.Payload) == 0 {
		return errInvalidState("profile readback returned an empty payload")
	}
	return nil
}
