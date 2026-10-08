package tui

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
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
	// What the device did answer is evidence too, listed apart from failures.
	failing := body[strings.Index(body, "Failing checks"):]
	if strings.Contains(failing, "GetMode") {
		t.Fatalf("did not expect a passing check among the failing ones:\n%s", body)
	}
	if !strings.Contains(body, "1 of 2 diagnostic checks answered") || !strings.Contains(body, "`GetMode`") {
		t.Fatalf("expected the answered check to be reported:\n%s", body)
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

	next, _ := m.updateDiagnostics(tea.KeyMsg{Runes: []rune("v"), Type: tea.KeyRunes})
	m = next.(Model)
	if !m.diag.showSupportRequest {
		t.Fatal("expected v to show the report for a candidate-readonly device")
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

func TestDiagnosticsReportIsAvailableForEveryTier(t *testing.T) {
	m := Model{
		screen: screenDiagnostics,
		diag:   diagnosticsState{device: core.AppDevice{Name: "X", SupportTier: protocol.TierFull}},
	}

	next, _ := m.updateDiagnostics(tea.KeyMsg{Runes: []rune("v"), Type: tea.KeyRunes})
	m = next.(Model)
	if !m.diag.showSupportRequest {
		t.Fatal("a fully supported device that answers only some checks needs a report as much as any other")
	}
}

func TestDiagnosticsReportCanBeCopiedAndSaved(t *testing.T) {
	var terminal bytes.Buffer
	previous := clipboardOut
	clipboardOut = &terminal
	t.Cleanup(func() { clipboardOut = previous })

	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.settings.ReportSaveMode = ReportSaveOff // an explicit save must still write
	m.screen = screenDiagnostics
	m.diag.device = core.AppDevice{Name: "PID_X", DisplayName: "Test Pad", VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6013}}
	m.diag.result = protocol.DiagProbeResult{CommandChecks: []protocol.DiagCommandStatus{{Command: protocol.CommandGetPid, OK: false, Detail: "no reply"}}}
	m.diag.showSupportRequest = true

	next, cmd := m.updateDiagnostics(tea.KeyMsg{Runes: []rune("c"), Type: tea.KeyRunes})
	m = next.(Model)
	if _, ok := cmd().(clipboardCopiedMsg); !ok {
		t.Fatal("expected c to report a copy")
	}
	// OSC 52: ESC ] 52 ; c ; <base64 of the report> BEL
	want := base64.StdEncoding.EncodeToString([]byte(supportRequestBody(m.diag.device, m.diag.result)))
	if got := terminal.String(); !strings.HasPrefix(got, "\x1b]52;c;") || !strings.Contains(got, want) {
		t.Fatalf("expected the report in an OSC 52 clipboard sequence, got %q", got)
	}

	_, cmd = m.updateDiagnostics(tea.KeyMsg{Runes: []rune("w"), Type: tea.KeyRunes})
	saved, ok := cmd().(reportSavedMsg)
	if !ok || saved.err != nil || saved.path == "" {
		t.Fatalf("expected w to save a report even with automatic saving off, got %+v", saved)
	}
	if _, err := os.Stat(saved.path); err != nil {
		t.Fatalf("saved report missing: %v", err)
	}
}
