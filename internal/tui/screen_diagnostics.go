package tui

import (
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type diagFilter int

const (
	diagFilterAll diagFilter = iota
	diagFilterIssues
)

type diagnosticsState struct {
	device  core.AppDevice
	loading bool
	result  protocol.DiagProbeResult
	err     error
	cursor  int
	filter  diagFilter
}

func newDiagnosticsState() diagnosticsState { return diagnosticsState{} }

func (d diagnosticsState) visibleChecks() []protocol.DiagCommandStatus {
	if d.filter == diagFilterAll {
		return d.result.CommandChecks
	}
	out := make([]protocol.DiagCommandStatus, 0, len(d.result.CommandChecks))
	for _, c := range d.result.CommandChecks {
		if !c.OK || c.Severity != protocol.SeverityOK {
			out = append(out, c)
		}
	}
	return out
}

func (m Model) updateDiagnostics(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case diagResultMsg:
		m.diag.loading = false
		m.diag.err = msg.err
		m.diag.result = msg.result
		status := "passed"
		if msg.err != nil {
			status = "error"
		} else {
			for _, c := range msg.result.CommandChecks {
				if !c.OK {
					status = "attention"
					break
				}
			}
		}
		message := m.core.BeginnerDiagSummary(m.diag.device, msg.result)
		saveCmd := cmdSaveReport(m.settings.ReportSaveMode, m.settingsPath, "diag-probe", &m.diag.device, status, message, &msg.result, nil, nil)
		return m, saveCmd

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.screen = screenDevices
			return m, nil
		case "up", "k":
			if m.diag.cursor > 0 {
				m.diag.cursor--
			}
		case "down", "j":
			if m.diag.cursor < len(m.diag.visibleChecks())-1 {
				m.diag.cursor++
			}
		case "tab":
			if m.diag.filter == diagFilterAll {
				m.diag.filter = diagFilterIssues
			} else {
				m.diag.filter = diagFilterAll
			}
			m.diag.cursor = 0
		case "r":
			m.diag.loading = true
			return m, cmdRunDiagnostics(m.ctx, m.core, m.diag.device.VidPid)
		}
	}
	return m, nil
}

func (m Model) viewDiagnostics(height int) string {
	var b strings.Builder
	b.WriteString(stylePanelTitle.Render("Diagnostics: "+m.diag.device.Name) + "  " + styleFaint.Render(pidLabel(m.diag.device.VidPid)))
	b.WriteString("\n\n")

	if m.diag.loading {
		b.WriteString(styleFaint.Render("Running diagnostics…"))
		return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
	}
	if m.diag.err != nil {
		b.WriteString(styleDanger.Render(fmt.Sprintf("Diagnostics failed: %v", m.diag.err)))
		return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
	}

	if m.diag.device.SupportTier != protocol.TierFull {
		b.WriteString(candidateTierExplanation(m.diag.device))
		b.WriteString("\n\n")
	}

	passed, total := 0, len(m.diag.result.CommandChecks)
	for _, c := range m.diag.result.CommandChecks {
		if c.OK {
			passed++
		}
	}
	b.WriteString(fmt.Sprintf("Checks: %d/%d passed", passed, total))
	if m.diag.filter == diagFilterIssues {
		b.WriteString("  " + styleWarning.Render("[showing issues only — tab to show all]"))
	} else {
		b.WriteString("  " + styleFaint.Render("[tab to show issues only]"))
	}
	b.WriteString("\n\n")

	checks := m.diag.visibleChecks()
	if len(checks) == 0 {
		b.WriteString(stylePositive.Render("No issues.") + "\n")
	}
	for i, c := range checks {
		line := diagCheckLine(c)
		if i == m.diag.cursor {
			line = "› " + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}

	if m.diag.cursor < len(checks) {
		c := checks[m.diag.cursor]
		b.WriteString("\n" + stylePanelTitle.Render("Detail") + "\n")
		b.WriteString(fmt.Sprintf("command=%s confidence=%s experimental=%v attempts=%d\n", c.Command, c.Confidence, c.IsExperimental, c.Attempts))
		b.WriteString(fmt.Sprintf("validator=%s\n", c.Validator))
		b.WriteString(styleFaint.Render(c.Detail) + "\n")
		if !c.OK && c.Confidence == protocol.EvidenceConfirmed && m.diag.device.SupportTier != protocol.TierFull {
			b.WriteString(styleWarning.Render("This check's validator is tuned to hardware-confirmed devices — a failure here on an unconfirmed PID is expected, not a sign of a broken connection.") + "\n")
		}
	}

	return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
}

func diagCheckLine(c protocol.DiagCommandStatus) string {
	mark := stylePositive.Render("✓")
	if !c.OK {
		switch c.Severity {
		case protocol.SeverityNeedsAttention:
			mark = styleDanger.Render("✗")
		default:
			mark = styleWarning.Render("!")
		}
	}
	experimental := ""
	if c.IsExperimental {
		experimental = styleFaint.Render(" (experimental)")
	}
	return fmt.Sprintf("%s %-28s%s", mark, c.Command, experimental)
}
