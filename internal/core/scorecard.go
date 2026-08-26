package core

import "github.com/bybrooklyn/openbitdo/internal/protocol"

// SupportScorecardForDevice computes the 7-dimension evidence/promotion
// scorecard for a device: static evidence, runtime evidence, hardware
// confirmation, safe-read coverage, safe-write readiness, backup/readback
// readiness, and firmware status.
func SupportScorecardForDevice(device AppDevice) SupportScorecard {
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
	switch {
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
		missing = append(missing, "firmware preflight and hardware confirmation")
	}

	promotionReady := device.SupportTier == protocol.TierFull ||
		(staticEvidence == EvidencePresent && runtimeEvidence == EvidencePresent &&
			hardwareConfirmation == EvidencePresent && safeWriteReadiness.isPresent() &&
			backupReadbackReadiness.isPresent())

	return SupportScorecard{
		VidPid: device.VidPid, SupportTier: device.SupportTier,
		StaticEvidence: staticEvidence, RuntimeEvidence: runtimeEvidence,
		HardwareConfirmation: hardwareConfirmation, SafeReadCoverage: safeReadCoverage,
		SafeWriteReadiness: safeWriteReadiness, BackupReadbackReadiness: backupReadbackReadiness,
		FirmwareStatus: firmwareStatus, ScorePercent: scorePercent,
		PromotionReady: promotionReady, MissingEvidence: missing,
	}
}
