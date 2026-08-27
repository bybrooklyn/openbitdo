package tui

import (
	"fmt"
	"strings"
	"time"

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
	device             core.AppDevice
	loading            bool
	result             protocol.DiagProbeResult
	ranAt              time.Time // when result was produced; zero if not yet set
	err                error
	cursor             int
	filter             diagFilter
	showSupportRequest bool
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
		m.diag.ranAt = msg.ranAt
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
		if m.diag.showSupportRequest {
			if msg.String() == "esc" {
				m.diag.showSupportRequest = false
			}
			return m, nil
		}
		switch msg.String() {
		case "esc":
			m.screen = screenDevices
			return m, nil
		case "s":
			if m.diag.device.SupportTier != protocol.TierFull {
				m.diag.showSupportRequest = true
			}
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
			return m, cmdDiagProbeFresh(m.ctx, m.core, m.diag.device)
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
		if coreErr, ok := m.diag.err.(*core.Error); ok && coreErr.Kind == core.KindDeviceDisconnected {
			b.WriteString("\n" + styleFaint.Render("Reconnect the device, then go back and press r on the dashboard to rescan."))
		}
		return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
	}

	if m.diag.showSupportRequest {
		b.WriteString(stylePanelTitle.Render("Support request — select and copy the text below") + "\n\n")
		b.WriteString(supportRequestBody(m.diag.device, m.diag.result))
		b.WriteString("\n" + styleHelp.Render("esc to go back"))
		return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
	}

	if m.diag.device.SupportTier != protocol.TierFull {
		b.WriteString(candidateTierExplanation(m.diag.device))
		b.WriteString("\n" + styleFaint.Render("Press s to generate a support-request report you can paste into a new GitHub issue.") + "\n\n")
	}

	if !m.diag.ranAt.IsZero() {
		b.WriteString(styleFaint.Render(fmt.Sprintf("Last run: %s  (r to rerun)", formatAge(time.Since(m.diag.ranAt)))) + "\n\n")
	}

	passed, total := 0, len(m.diag.result.CommandChecks)
	for _, c := range m.diag.result.CommandChecks {
		if c.OK {
			passed++
		}
	}
	b.WriteString(styleBody.Render(fmt.Sprintf("Checks: %d/%d passed", passed, total)))
	if m.diag.filter == diagFilterIssues {
		b.WriteString("  " + styleWarning.Render("[showing issues only — tab to show all]"))
	} else {
		b.WriteString("  " + styleFaint.Render("[tab to show issues only]"))
	}
	b.WriteString("\n\n")

	checks := m.diag.visibleChecks()
	if len(checks) == 0 {
		b.WriteString(stylePositiveBlock.Render(stylePositive.Render("No issues.")) + "\n")
	}
	for i, c := range checks {
		line := diagCheckLine(c)
		if i == m.diag.cursor {
			// diagCheckLine already embeds its own styled pass/fail icon
			// (with its own reset code), so wrapping the whole line in
			// styleSelectedRow would have that inner reset cut the outer
			// background off partway through — style just the marker
			// instead. See styleSelectedMarker's doc comment in theme.go.
			line = styleSelectedMarker.Render("›") + " " + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}

	if m.diag.cursor < len(checks) {
		c := checks[m.diag.cursor]
		b.WriteString("\n" + stylePanelTitle.Render("Detail") + "\n")
		b.WriteString(styleBody.Render(fmt.Sprintf("command=%s confidence=%s experimental=%v attempts=%d", c.Command, c.Confidence, c.IsExperimental, c.Attempts)) + "\n")
		b.WriteString(styleBody.Render(fmt.Sprintf("validator=%s", c.Validator)) + "\n")
		b.WriteString(styleFaint.Render(c.Detail) + "\n")
		if !c.OK && c.Confidence == protocol.EvidenceConfirmed && m.diag.device.SupportTier != protocol.TierFull {
			b.WriteString(styleWarning.Render("This check's validator is tuned to hardware-confirmed devices — a failure here on an unconfirmed PID is expected, not a sign of a broken connection.") + "\n")
		}
	}

	return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
}

// formatAge renders a cache-staleness duration as a short, human-readable
// string for the "Last run: Xs ago" indicator — mirrors DiagCacheEntry.Age.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

func diagCheckLine(c protocol.DiagCommandStatus) string {
	mark := stylePositive.Render(IconPass)
	if !c.OK {
		switch c.Severity {
		case protocol.SeverityNeedsAttention:
			mark = styleDanger.Render(IconFail)
		default:
			mark = styleWarning.Render(IconWarn)
		}
	}
	experimental := ""
	if c.IsExperimental {
		experimental = styleFaint.Render(" (experimental)")
	}
	return fmt.Sprintf("%s %-28s%s", mark, c.Command, experimental)
}
