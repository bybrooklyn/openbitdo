package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
		m.diag.cursor, m.diag.rowOffset = 0, 0
		if msg.err != nil {
			// Nothing was probed, so there is no report worth saving.
			return m, nil
		}
		status := "passed"
		for _, c := range msg.result.CommandChecks {
			if !c.OK {
				status = "attention"
				break
			}
		}
		message := m.core.BeginnerDiagSummary(m.diag.device, msg.result)
		saveCmd := cmdSaveReport(m.settings.ReportSaveMode, m.settingsPath, "diag-probe", &m.diag.device, status, message, &msg.result, nil, nil)
		return m, saveCmd

	case clipboardCopiedMsg:
		return m.setNotice(noticeSuccess, "Report copied. If nothing was pasted, your terminal blocks clipboard access; save it with w instead.", true)

	case tea.KeyMsg:
		if m.diag.showSupportRequest {
			return m.updateDiagnosticsReport(msg)
		}
		switch msg.String() {
		case "esc":
			m.screen = screenDevices
			return m, nil
		case "r":
			m.diag.loading = true
			m.diag.err = nil
			return m, cmdDiagProbeFresh(m.ctx, m.core, m.diag.device)
		}
		if m.diag.loading || m.diag.err != nil {
			return m, nil
		}
		switch msg.String() {
		case "v":
			m.diag.showSupportRequest = true
			m.diag.supportOffset = 0
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
		case "f":
			if m.diag.filter == diagFilterAll {
				m.diag.filter = diagFilterIssues
			} else {
				m.diag.filter = diagFilterAll
			}
			m.diag.cursor = 0
			m.diag.rowOffset = 0
		case "enter":
			m.diag.showDetail = !m.diag.showDetail
			m.ensureDiagnosticsCursorVisible()
		}
	}
	return m, nil
}

// updateDiagnosticsReport handles the report sub-view: scroll it, copy it,
// save it, or go back.
func (m Model) updateDiagnosticsReport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	body := supportRequestBody(m.diag.device, m.diag.result)
	maxOffset := max(0, len(m.diagnosticsReportLines(body))-m.diagnosticsReportRows())
	switch msg.String() {
	case "esc":
		m.diag.showSupportRequest = false
	case "up", "k":
		m.diag.supportOffset = clampInt(m.diag.supportOffset-1, 0, maxOffset)
	case "down", "j":
		m.diag.supportOffset = clampInt(m.diag.supportOffset+1, 0, maxOffset)
	case "pgup":
		m.diag.supportOffset = clampInt(m.diag.supportOffset-m.diagnosticsReportRows(), 0, maxOffset)
	case "pgdown", " ":
		m.diag.supportOffset = clampInt(m.diag.supportOffset+m.diagnosticsReportRows(), 0, maxOffset)
	case "c":
		return m, cmdCopyToClipboard(body)
	case "w":
		// An explicit save always writes, whatever the automatic
		// report-saving setting says.
		message := m.core.BeginnerDiagSummary(m.diag.device, m.diag.result)
		return m, cmdSaveReport(ReportSaveAlways, m.settingsPath, "diag-probe", &m.diag.device, "saved-on-request", message, &m.diag.result, nil, nil)
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

// diagnosticsVisibleRows is how many check rows fit: the panel, less the
// lines above the list (summary, what it means, blank, list header) and the
// detail block below it.
func (m Model) diagnosticsVisibleRows() int {
	panel := max(1, calculateLayout(m.width, m.height).bodyHeight-2)
	return max(1, panel-diagHeaderLines-m.diagnosticsDetailLines())
}

const diagHeaderLines = 4

// diagnosticsDetailLines is the height of the block under the check list: a
// blank line, then what the selected check found, and with details on, the
// raw facts behind it.
func (m Model) diagnosticsDetailLines() int {
	if m.diag.showDetail {
		return 5
	}
	return 3
}

func (m Model) diagnosticsReportRows() int {
	panel := max(1, calculateLayout(m.width, m.height).bodyHeight-2)
	return max(1, panel-4)
}

func (m Model) diagnosticsReportLines(body string) []string {
	return wrapText(body, max(1, m.width-4))
}

// checkLabel names a diagnostic check in plain words. The command ID stays
// available behind the details toggle and in the report.
func checkLabel(command protocol.CommandID) string {
	switch command {
	case protocol.CommandGetPid:
		return "Identify device"
	case protocol.CommandGetReportRevision:
		return "Report revision"
	case protocol.CommandGetMode:
		return "Current mode"
	case protocol.CommandGetModeAlt:
		return "Current mode (alt. read)"
	case protocol.CommandGetControllerVersion:
		return "Controller version"
	case protocol.CommandVersion:
		return "Firmware version"
	case protocol.CommandGetSuperButton:
		return "Extra-button support"
	case protocol.CommandIdle:
		return "Idle check"
	case protocol.CommandReadProfile:
		return "Read profile"
	case protocol.CommandJp108ReadDedicatedMappings:
		return "Read a button's mapping"
	case protocol.CommandJp108ReadProfileName:
		return "Read profile name"
	case protocol.CommandJp108ReadFeatureFlags:
		return "Read feature flags"
	case protocol.CommandJp108ReadVoice:
		return "Read voice setting"
	case protocol.CommandU2GetCurrentSlot:
		return "Current profile slot"
	case protocol.CommandU2ReadConfigSlot:
		return "Read slot settings"
	case protocol.CommandU2ReadButtonMap:
		return "Read button map"
	}
	return string(command)
}

// checkOutcome says in a few words what a check found.
func checkOutcome(c protocol.DiagCommandStatus) string {
	if c.OK {
		// The mode read has two forms; say which one answered without the
		// internal error text of the one that did not.
		if strings.HasPrefix(c.Detail, "ok via GetModeAlt fallback") {
			return "answered (alt. read)"
		}
		if c.Detail == "ok" {
			return "answered"
		}
		return c.Detail
	}
	if c.BytesRead == 0 && c.ErrorCode == protocol.CodeMalformedResponse {
		return "no reply"
	}
	switch c.ErrorCode {
	case protocol.CodeInvalidResponse:
		return "unexpected reply"
	case protocol.CodeMalformedResponse:
		return fmt.Sprintf("reply too short (%d bytes)", c.BytesRead)
	case protocol.CodeTimeout:
		return "no reply"
	}
	return c.Detail
}

func (m Model) viewDiagnostics(height int) string {
	panelHeight := max(1, height-2)
	text := max(1, m.width-4)
	device := m.diag.device
	var lines []string
	render := func() string {
		return renderBoundedPanel(m.width-2, panelHeight, strings.Join(lines, "\n"))
	}
	addWrapped := func(style lipgloss.Style, s string) {
		lines = append(lines, strings.Split(wrapStyled(style, s, text), "\n")...)
	}

	if m.diag.loading {
		addWrapped(styleBody, "Checking the connection…")
		addWrapped(styleFaint, "Each check the device does not answer waits for its timeout, so this can take a few seconds.")
		return render()
	}
	if m.diag.err != nil {
		for _, line := range diagnosticsErrorLines(m.diag.err) {
			style := styleFaint
			if line.strong {
				style = styleDanger
			}
			addWrapped(style, line.text)
		}
		return render()
	}

	if m.diag.showSupportRequest {
		lines = append(lines, stylePanelTitle.Render(truncate("Report: "+device.DisplayName, text)), "")
		body := m.diagnosticsReportLines(supportRequestBody(device, m.diag.result))
		rows := m.diagnosticsReportRows()
		start := clampInt(m.diag.supportOffset, 0, max(0, len(body)-rows))
		end := min(len(body), start+rows)
		for _, line := range body[start:end] {
			lines = append(lines, styleBody.Render(line))
		}
		position := fmt.Sprintf("lines %d-%d of %d", start+1, end, len(body))
		lines = append(lines, "", styleFaint.Render(position+" · c copies it, w saves it to a file"))
		return render()
	}

	passed, total := 0, len(m.diag.result.CommandChecks)
	for _, c := range m.diag.result.CommandChecks {
		if c.OK {
			passed++
		}
	}
	summary := fmt.Sprintf("%d of %d checks answered", passed, total)
	summaryStyle := stylePositive
	meaning := "The device answered everything OpenBitdo asked."
	switch {
	case total == 0:
		summary, summaryStyle = "No checks apply to this device", styleWarning
		meaning = ""
	case passed == 0:
		summary, summaryStyle = fmt.Sprintf("The device answered none of the %d checks", total), styleWarning
		meaning = "Connected but silent. Press v for a report to share."
	case passed < total:
		summaryStyle = styleWarning
		meaning = "It's talking. No answer means unsupported, not broken."
	}
	if !m.diag.ranAt.IsZero() {
		summary += "  ·  " + fmt.Sprintf("Last run: %s", formatAge(time.Since(m.diag.ranAt)))
	}
	// The pane's own tab names the screen and the sidebar names the device,
	// so the panel opens straight on the result.
	if lipgloss.Width(meaning) > text && passed > 0 && passed < total {
		meaning = "No answer = unsupported, not broken."
	}
	lines = []string{
		summaryStyle.Render(truncate(summary, text)),
		styleFaint.Render(truncate(meaning, text)),
	}

	checks := m.diag.visibleChecks()
	header := "All checks"
	if m.diag.filter == diagFilterIssues {
		header = fmt.Sprintf("Unanswered checks only (%d)", len(checks))
	}
	lines = append(lines, "", styleSection.Render(header))

	if len(checks) == 0 {
		lines = append(lines, stylePositive.Render("Every check was answered."))
		return render()
	}

	start, end, _ := viewportWindow(len(checks), m.diag.cursor, m.diag.rowOffset, m.diagnosticsVisibleRows())
	for i := start; i < end; i++ {
		c := checks[i]
		line := diagCheckLine(c, text-2)
		// Scroll markers ride on the first and last visible rows.
		if i == start && start > 0 {
			line += styleFaint.Render(" ↑")
		}
		if i == end-1 && end < len(checks) {
			line += styleFaint.Render(" ↓")
		}
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

	if m.diag.cursor < len(checks) {
		c := checks[m.diag.cursor]
		lines = append(lines, "")
		about := checkLabel(c.Command) + ": " + checkOutcome(c) + "."
		if !c.OK {
			about += " " + unansweredNote(c, device)
		}
		// Two lines at most, which is what diagnosticsDetailLines reserves.
		wrapped := wrapText(about, text)
		if len(wrapped) > 2 {
			wrapped = append(wrapped[:1], truncate(strings.Join(wrapped[1:], " "), text))
		}
		for _, line := range wrapped {
			lines = append(lines, styleBody.Render(line))
		}
		if m.diag.showDetail {
			lines = append(lines,
				styleFaint.Render(truncate(fmt.Sprintf("command %s · %s · sent %d bytes, got %d · %d attempt(s)",
					c.Command, strings.ToLower(string(c.Confidence)), c.BytesWritten, c.BytesRead, c.Attempts), text)),
				styleFaint.Render(truncate("expects "+c.Validator, text)),
			)
		}
	}
	return render()
}

// unansweredNote puts a failed check in context, so a list of warnings does
// not read as a list of faults.
func unansweredNote(c protocol.DiagCommandStatus, device core.AppDevice) string {
	switch {
	case c.Severity == protocol.SeverityNeedsAttention:
		return "That contradicts what's known. Please share the report (v)."
	case c.IsExperimental:
		return "This question is a guess, so silence is expected."
	case device.SupportTier != protocol.TierFull:
		return "Normal for a model not yet confirmed on hardware."
	}
	return "This model ignores this question. It isn't a fault."
}

type diagErrorLine struct {
	text   string
	strong bool
}

// diagnosticsErrorLines explains why diagnostics could not run at all, and
// what to do about it, instead of printing the raw error on one cut-off line.
func diagnosticsErrorLines(err error) []diagErrorLine {
	var coreErr *core.Error
	if errors.As(err, &coreErr) {
		switch coreErr.Kind {
		case core.KindDeviceDisconnected:
			return []diagErrorLine{
				{text: "The device was disconnected.", strong: true},
				{text: "Reconnect it, then press r to check again."},
			}
		case core.KindPermissionDenied:
			lines := []diagErrorLine{{text: "Your user account is not allowed to open this device.", strong: true}}
			for _, fix := range permissionFixLines() {
				lines = append(lines, diagErrorLine{text: fix})
			}
			return append(lines, diagErrorLine{text: "Then press r to try again."})
		case core.KindNoConfigChannel:
			return []diagErrorLine{
				{text: "OpenBitdo can't talk to this device in its current mode.", strong: true},
				{text: "It is plugged in, but it does not offer the configuration interface OpenBitdo sends commands through, so there is nothing to check. If it has another connection (a wireless adapter, Bluetooth), try that one."},
			}
		}
	}
	return []diagErrorLine{
		{text: "The checks could not run.", strong: true},
		{text: err.Error()},
		{text: "Press r to try again."},
	}
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

// diagCheckLine is one row of the check list: a mark, the check's plain
// name, and what it found, cut to width.
func diagCheckLine(c protocol.DiagCommandStatus, width int) string {
	mark := stylePositive.Render(IconPass)
	if !c.OK {
		switch c.Severity {
		case protocol.SeverityNeedsAttention:
			mark = styleDanger.Render(IconFail)
		default:
			mark = styleWarning.Render(IconWarn)
		}
	}
	labelWidth := clampInt(width/2, 16, 26)
	label := checkLabel(c.Command)
	// The row carries the gist; the line under the list has it in full.
	gist, _, _ := strings.Cut(checkOutcome(c), ";")
	outcome := truncate(gist, max(0, width-labelWidth-6))
	return fmt.Sprintf("%s %s", mark, styleBody.Render(fmt.Sprintf("%-*s", labelWidth, truncate(label, labelWidth)))) + styleFaint.Render(outcome)
}
