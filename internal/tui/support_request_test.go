package tui

import (
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSupportRequestBodyIncludesDeviceAndFailingChecks(t *testing.T) {
	device := core.AppDevice{
		Name: "PID_Ultimate2_4", VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x3012},
		SupportTier: protocol.TierCandidateReadOnly, ProtocolFamily: protocol.Standard64,
		Evidence: protocol.EvidenceInferred,
	}
	result := protocol.DiagProbeResult{
		CommandChecks: []protocol.DiagCommandStatus{
			{Command: protocol.CommandGetPid, OK: false, Confidence: protocol.EvidenceConfirmed, Detail: "response signature mismatch"},
			{Command: protocol.CommandGetMode, OK: true, Detail: "ok"},
		},
	}

	body := supportRequestBody(device, result)

	for _, want := range []string{
		"PID_Ultimate2_4", "0x2dc8", "0x3012", string(protocol.TierCandidateReadOnly),
		"GetPid", "response signature mismatch", "Failing checks (1/2)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected support request body to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "GetMode") {
		t.Fatalf("did not expect a passing check to be listed:\n%s", body)
	}
}

func TestSupportRequestBodyHandlesNoFailingChecks(t *testing.T) {
	device := core.AppDevice{Name: "X", SupportTier: protocol.TierCandidateReadOnly}
	result := protocol.DiagProbeResult{
		CommandChecks: []protocol.DiagCommandStatus{{Command: protocol.CommandGetPid, OK: true}},
	}

	body := supportRequestBody(device, result)

	if !strings.Contains(body, "All diagnostic checks passed") {
		t.Fatalf("expected an all-passed message, got:\n%s", body)
	}
}

func TestDiagnosticsSupportRequestKeyTogglesView(t *testing.T) {
	m := Model{
		screen: screenDiagnostics,
		diag: diagnosticsState{
			device: core.AppDevice{Name: "X", SupportTier: protocol.TierCandidateReadOnly},
			result: protocol.DiagProbeResult{CommandChecks: []protocol.DiagCommandStatus{{Command: protocol.CommandGetPid, OK: false, Detail: "d"}}},
		},
	}

	next, _ := m.updateDiagnostics(tea.KeyMsg{Runes: []rune("s"), Type: tea.KeyRunes})
	m = next.(Model)
	if !m.diag.showSupportRequest {
		t.Fatal("expected s to show the support request view for a candidate-readonly device")
	}

	next, _ = m.updateDiagnostics(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.diag.showSupportRequest {
		t.Fatal("expected esc to leave the support request view, not the diagnostics screen")
	}
	if m.screen != screenDiagnostics {
		t.Fatalf("esc from the support request view should stay on the diagnostics screen, got %v", m.screen)
	}
}

func TestDiagnosticsSupportRequestKeyIgnoredForFullTierDevice(t *testing.T) {
	m := Model{
		screen: screenDiagnostics,
		diag:   diagnosticsState{device: core.AppDevice{Name: "X", SupportTier: protocol.TierFull}},
	}

	next, _ := m.updateDiagnostics(tea.KeyMsg{Runes: []rune("s"), Type: tea.KeyRunes})
	m = next.(Model)
	if m.diag.showSupportRequest {
		t.Fatal("did not expect s to do anything for an already-full-tier device")
	}
}
