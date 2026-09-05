package core

import "github.com/bybrooklyn/openbitdo/internal/protocol"

type supportRuntimeScope struct {
	firmwareEnabled  bool
	u2MappingEnabled bool
}

// SupportScorecardForDevice computes a conservative production scorecard.
// Callers that own an OpenBitdoCore should use its method of the same name so
// mock/test runtime availability is represented precisely.
func SupportScorecardForDevice(device AppDevice) SupportScorecard {
	return supportScorecardForDevice(device, supportRuntimeScope{})
}

// SupportScorecardForDevice computes the 7 evidence dimensions plus current
// runtime release blockers for this core instance.
func (c *OpenBitdoCore) SupportScorecardForDevice(device AppDevice) SupportScorecard {
	return supportScorecardForDevice(device, supportRuntimeScope{
		firmwareEnabled:  c.FirmwareEnabled(),
		u2MappingEnabled: c.config.MockMode,
	})
}

func supportScorecardForDevice(device AppDevice, scope supportRuntimeScope) SupportScorecard {
	present := func(cond bool) EvidenceState {
		if cond {
			return EvidencePresent
		}
		return EvidenceMissing
	}

	staticEvidence := present(device.Evidence != protocol.EvidenceUntested)
	runtimeEvidence := present(device.SupportTier == protocol.TierFull)
	hardwareConfirmation := present(device.SupportTier == protocol.TierFull)
	safeReadCoverage := present(device.Capability.SupportsMode || device.Capability.SupportsProfileRW ||
		device.SupportTier != protocol.TierDetectOnly)

	var safeWriteReadiness EvidenceState
	switch {
	case device.SupportTier == protocol.TierFull:
		safeWriteReadiness = EvidencePresent
	case device.Capability.SupportsMode || device.Capability.SupportsProfileRW:
		safeWriteReadiness = EvidenceMissing
	default:
		safeWriteReadiness = EvidenceNotApplicable
	}

	var backupReadbackReadiness EvidenceState
	realU2MappingDeferred := device.Capability.SupportsU2ButtonMap &&
		device.Capability.SupportsU2SlotConfig && !scope.u2MappingEnabled
	switch {
	case realU2MappingDeferred:
		backupReadbackReadiness = EvidenceMissing
	case device.SupportTier == protocol.TierFull:
		backupReadbackReadiness = EvidencePresent
	case device.Capability.SupportsProfileRW || device.Capability.SupportsJP108DedicatedMap || device.Capability.SupportsU2ButtonMap:
		backupReadbackReadiness = EvidenceMissing
	default:
		backupReadbackReadiness = EvidenceNotApplicable
	}

	var firmwareStatus EvidenceState
	switch {
	case !device.Capability.SupportsFirmware:
		firmwareStatus = EvidenceNotApplicable
	case !scope.firmwareEnabled:
		firmwareStatus = EvidenceMissing
	case device.SupportTier == protocol.TierFull:
		firmwareStatus = EvidencePresent
	default:
		firmwareStatus = EvidenceMissing
	}

	states := []EvidenceState{
		staticEvidence, runtimeEvidence, hardwareConfirmation, safeReadCoverage,
		safeWriteReadiness, backupReadbackReadiness, firmwareStatus,
	}
	presentCount := 0
	for _, s := range states {
		if s.isPresent() {
			presentCount++
		}
	}
	scorePercent := (presentCount * 100) / len(states)

	var missing []string
	if staticEvidence == EvidenceMissing {
		missing = append(missing, "sanitized static dossier/spec evidence")
	}
	if runtimeEvidence == EvidenceMissing {
		missing = append(missing, "runtime request/response trace report")
	}
	if hardwareConfirmation == EvidenceMissing {
		missing = append(missing, "hardware smoke confirmation from attached device")
	}
	if safeReadCoverage == EvidenceMissing {
		missing = append(missing, "repeatable safe-read diagnostics")
	}
	if safeWriteReadiness == EvidenceMissing {
		missing = append(missing, "guarded safe-write runtime probe")
	}
	if backupReadbackReadiness == EvidenceMissing {
		missing = append(missing, "backup and readback verification")
	}
	if firmwareStatus == EvidenceMissing {
		if !scope.firmwareEnabled && device.Capability.SupportsFirmware {
			missing = append(missing, "firmware updates deferred in 0.0.3")
		} else {
			missing = append(missing, "firmware preflight and hardware confirmation")
		}
	}
	if realU2MappingDeferred {
		missing = append(missing, u2MappingDeferredReason)
	}

	var releaseBlockers []string
	if !scope.firmwareEnabled && device.Capability.SupportsFirmware {
		releaseBlockers = append(releaseBlockers, ReleaseBlockerFirmwareDisabled)
	}
	if realU2MappingDeferred {
		releaseBlockers = append(releaseBlockers, ReleaseBlockerU2ButtonMapFraming)
	}

	promotionReady := device.SupportTier == protocol.TierFull &&
		staticEvidence.isPresent() && runtimeEvidence.isPresent() &&
		hardwareConfirmation.isPresent() && safeReadCoverage.isPresent() &&
		safeWriteReadiness.isPresent() && backupReadbackReadiness.isPresent() &&
		firmwareStatus.isPresent() && len(releaseBlockers) == 0

	return SupportScorecard{
		VidPid: device.VidPid, SupportTier: device.SupportTier,
		StaticEvidence: staticEvidence, RuntimeEvidence: runtimeEvidence,
		HardwareConfirmation: hardwareConfirmation, SafeReadCoverage: safeReadCoverage,
		SafeWriteReadiness: safeWriteReadiness, BackupReadbackReadiness: backupReadbackReadiness,
		FirmwareStatus: firmwareStatus, ScorePercent: scorePercent,
		PromotionReady: promotionReady, MissingEvidence: missing, ReleaseBlockers: releaseBlockers,
	}
}
