package tui

import (
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

func TestReportWorksNowSuppressesFirmwareAndLabelsMappingScope(t *testing.T) {
	jp108 := core.AppDevice{
		Name:        "JP108",
		VidPid:      protocol.VidPid{VID: 0x2dc8, PID: 0x5203},
		SupportTier: protocol.TierFull,
		Capability:  protocol.PidCapability{SupportsFirmware: true, SupportsJP108DedicatedMap: true},
	}
	jp108Report := deviceToReport(jp108)
	joinedJP108 := strings.Join(append(jp108Report.WorksNow, jp108Report.BlockedOperations...), "\n")
	if !strings.Contains(joinedJP108, "confirmed JP108 mapping editor") {
		t.Fatalf("JP108 report should advertise confirmed JP108 mapping, got %+v", jp108Report.WorksNow)
	}
	if strings.Contains(joinedJP108, "firmware update") {
		t.Fatalf("firmware must not be advertised as working in v0.1.0, got %q", joinedJP108)
	}
	if !strings.Contains(joinedJP108, "firmware: deferred in 0.1.0") {
		t.Fatalf("firmware deferral should be explicit, got %q", joinedJP108)
	}

	u2 := core.AppDevice{
		Name:        "Ultimate2",
		VidPid:      protocol.VidPid{VID: 0x2dc8, PID: 0x6012},
		SupportTier: protocol.TierFull,
		Capability:  protocol.PidCapability{SupportsFirmware: true, SupportsU2ButtonMap: true, SupportsU2SlotConfig: true},
	}
	u2Report := deviceToReport(u2)
	joinedU2 := strings.Join(append(u2Report.WorksNow, u2Report.BlockedOperations...), "\n")
	if strings.Contains(strings.Join(u2Report.WorksNow, "\n"), "Ultimate2") || strings.Contains(strings.Join(u2Report.WorksNow, "\n"), "mapping") {
		t.Fatalf("U2 report should not advertise device-specific mapping in works_now, got %+v", u2Report.WorksNow)
	}
	if strings.Contains(joinedU2, "confirmed mapping editor") || strings.Contains(joinedU2, "firmware update") {
		t.Fatalf("U2 report must not claim real mapping or firmware works, got %q", joinedU2)
	}
	if !strings.Contains(joinedU2, "button-map framing not hardware-confirmed") {
		t.Fatalf("real U2 mapping block reason should be explicit, got %q", joinedU2)
	}
}

func TestReportScorecardSerializesRuntimeReleaseBlockers(t *testing.T) {
	u2 := core.AppDevice{
		Name: "Ultimate2", VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012},
		SupportTier: protocol.TierFull, Evidence: protocol.EvidenceConfirmed,
		Capability: protocol.PidCapability{
			SupportsMode: true, SupportsProfileRW: true, SupportsFirmware: true,
			SupportsU2ButtonMap: true, SupportsU2SlotConfig: true,
		},
	}

	scorecard := scorecardToReport(u2.Scorecard())
	if scorecard.FirmwareStatus != string(core.EvidenceMissing) || scorecard.PromotionReady {
		t.Fatalf("report scorecard overclaims runtime readiness: %+v", scorecard)
	}
	joined := strings.Join(scorecard.ReleaseBlockers, ",")
	for _, blocker := range []string{core.ReleaseBlockerFirmwareDisabled, core.ReleaseBlockerU2ButtonMapFraming} {
		if !strings.Contains(joined, blocker) {
			t.Fatalf("report scorecard missing release blocker %q: %+v", blocker, scorecard)
		}
	}

	tomlReport, err := DiagnosticsReportTOML(u2, protocol.DiagProbeResult{Target: u2.VidPid})
	if err != nil {
		t.Fatalf("render TOML: %v", err)
	}
	if !strings.Contains(tomlReport, "release_blockers") ||
		!strings.Contains(tomlReport, core.ReleaseBlockerFirmwareDisabled) ||
		!strings.Contains(tomlReport, core.ReleaseBlockerU2ButtonMapFraming) {
		t.Fatalf("TOML omitted machine-readable release blockers:\n%s", tomlReport)
	}
}
