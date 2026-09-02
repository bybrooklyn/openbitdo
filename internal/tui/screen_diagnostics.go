package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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
	rowOffset          int
	supportOffset      int
	filter             diagFilter
	showDetail         bool
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
				m.diag.supportOffset = 0
			}
			return m, nil
		case "up", "k":
			if m.diag.cursor > 0 {
				m.diag.cursor--
				m.ensureDiagnosticsCursorVisible()
			}
		case "down", "j":
			if m.diag.cursor < len(m.diag.visibleChecks())-1 {
				m.diag.cursor++
				m.ensureDiagnosticsCursorVisible()
			}
		case "tab":
			if m.diag.filter == diagFilterAll {
				m.diag.filter = diagFilterIssues
			} else {
				m.diag.filter = diagFilterAll
			}
			m.diag.cursor = 0
			m.diag.rowOffset = 0
		case "d":
			m.diag.showDetail = !m.diag.showDetail
		case "r":
			m.diag.loading = true
			return m, cmdDiagProbeFresh(m.ctx, m.core, m.diag.device)
		}
	}
	return m, nil
}

func (m *Model) ensureDiagnosticsCursorVisible() {
	checks := len(m.diag.visibleChecks())
	if checks == 0 {
		m.diag.rowOffset = 0
		return
	}
	start, _, _ := viewportWindow(checks, m.diag.cursor, m.diag.rowOffset, m.diagnosticsVisibleRows())
	m.diag.rowOffset = start
}

func (m Model) diagnosticsVisibleRows() int {
	reserved := 13
	if calculateLayout(m.width, m.height).mode == layoutWide {
		reserved = 15
	}
	return max(1, m.height-reserved)
}

func (m Model) viewDiagnostics(height int) string {
	panelHeight := max(1, height-2)
	lines := []string{
		stylePanelTitle.Render("Diagnostics: " + m.diag.device.Name),
		styleFaint.Render(pidLabel(m.diag.device.VidPid)),
		"",
	}

	if m.diag.loading {
		lines = append(lines, styleFaint.Render("Running diagnostics…"))
		return renderBoundedPanel(m.width-2, panelHeight, strings.Join(lines, "\n"))
	}
	if m.diag.err != nil {
		lines = append(lines, styleDanger.Render(fmt.Sprintf("Diagnostics failed: %v", m.diag.err)))
		if coreErr, ok := m.diag.err.(*core.Error); ok && coreErr.Kind == core.KindDeviceDisconnected {
			lines = append(lines, styleFaint.Render("Reconnect the device, then go back and press r on the dashboard to rescan."))
		}
		return renderBoundedPanel(m.width-2, panelHeight, m.fitDiagnosticsLines(lines))
	}

	if m.diag.showSupportRequest {
		lines = append(lines, stylePanelTitle.Render("Support request — select and copy"))
		bodyLines := strings.Split(supportRequestBody(m.diag.device, m.diag.result), "\n")
		limit := max(1, panelHeight-len(lines)-2)
		start, end, more := viewportWindow(len(bodyLines), m.diag.supportOffset, m.diag.supportOffset, limit)
		lines = append(lines, bodyLines[start:end]...)
		if more != "" {
			lines = append(lines, styleFaint.Render(more))
		}
		lines = append(lines, styleHelp.Render("esc to go back"))
		return renderBoundedPanel(m.width-2, panelHeight, m.fitDiagnosticsLines(lines))
	}

	if m.diag.device.SupportTier != protocol.TierFull {
		lines = append(lines,
			styleWarning.Render("Not hardware-confirmed yet."),
			styleFaint.Render("Press s for a support-request report."),
			"",
		)
	}

	if !m.diag.ranAt.IsZero() {
		lines = append(lines, styleFaint.Render(fmt.Sprintf("Last run: %s  (r to rerun)", formatAge(time.Since(m.diag.ranAt)))))
	}

	passed, total := 0, len(m.diag.result.CommandChecks)
	for _, c := range m.diag.result.CommandChecks {
		if c.OK {
			passed++
		}
	}
	transport := "not ready"
	if m.diag.result.TransportReady {
		transport = "ready"
	}
	lines = append(lines, stylePositiveBlock.Render(styleBody.Render(fmt.Sprintf("Summary: %d/%d checks passed; transport %s.", passed, total, transport))))
	if m.diag.device.SupportTier != protocol.TierFull {
		lines = append(lines, styleFaint.Render("Next: save a support request for hardware evidence."))
	} else {
		lines = append(lines, styleFaint.Render("Next: open details only when raw evidence is needed."))
	}

	checkHeader := styleBody.Render(fmt.Sprintf("Checks: %d/%d passed", passed, total))
	if m.diag.filter == diagFilterIssues {
		checkHeader += "  " + styleWarning.Render("[issues]")
	} else {
		checkHeader += "  " + styleFaint.Render("[tab: issues]")
	}
	lines = append(lines, "", checkHeader)

	checks := m.diag.visibleChecks()
	if len(checks) == 0 {
		lines = append(lines, stylePositiveBlock.Render(stylePositive.Render("No issues.")))
	}
	detailReserve := 3
	if m.diag.showDetail {
		detailReserve = 4
	}
	checkLimit := max(1, panelHeight-len(lines)-detailReserve)
	start, end, more := viewportWindow(len(checks), m.diag.cursor, m.diag.rowOffset, checkLimit)
	for i := start; i < end; i++ {
		c := checks[i]
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
		lines = append(lines, line)
	}
	if more != "" {
		lines = append(lines, styleFaint.Render(more))
	}

	if m.diag.cursor < len(checks) {
		c := checks[m.diag.cursor]
		lines = append(lines, stylePanelTitle.Render("Detail"))
		if m.diag.showDetail {
			lines = append(lines, styleBody.Render(fmt.Sprintf("command=%s confidence=%s experimental=%v attempts=%d", c.Command, c.Confidence, c.IsExperimental, c.Attempts)))
			lines = append(lines, styleBody.Render(fmt.Sprintf("validator=%s", c.Validator)))
			lines = append(lines, styleFaint.Render(c.Detail))
			if !c.OK && c.Confidence == protocol.EvidenceConfirmed && m.diag.device.SupportTier != protocol.TierFull {
				lines = append(lines, styleWarning.Render("Confirmed-device validator; candidate failures can be expected."))
			}
		} else {
			lines = append(lines, styleFaint.Render("Press d for raw command, validator, confidence, and response."))
		}
	}

	if len(lines) > panelHeight {
		lines = lines[:panelHeight]
	}
	return renderBoundedPanel(m.width-2, panelHeight, m.fitDiagnosticsLines(lines))
}

func (m Model) fitDiagnosticsLines(lines []string) string {
	limit := max(1, m.width-4)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = ansi.Cut(line, 0, limit)
	}
	return strings.Join(out, "\n")
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
