package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const settingsRowCount = 3 // Advanced Mode, Report Save Mode, Back

func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case settingsSavedMsg:
		if msg.err == nil {
			return m.setNotice(noticeSuccess, "Settings saved.", true)
		} else {
			return m.setNotice(noticeError, msg.err.Error(), false)
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.screen = screenDevices
			return m, nil
		case "up", "k":
			if m.settingsCursor > 0 {
				m.settingsCursor--
			}
		case "down", "j":
			if m.settingsCursor < settingsRowCount-1 {
				m.settingsCursor++
			}
		case "pgup":
			m.settingsInfoOffset = clampInt(m.settingsInfoOffset-3, 0, m.settingsInfoMaxOffset())
		case "pgdown":
			m.settingsInfoOffset = clampInt(m.settingsInfoOffset+3, 0, m.settingsInfoMaxOffset())
		case "home":
			m.settingsInfoOffset = 0
		case "end":
			m.settingsInfoOffset = m.settingsInfoMaxOffset()
		case "left", "right", "enter":
			return m.triggerSettingsRow()
		}
	}
	return m, nil
}

func (m Model) triggerSettingsRow() (tea.Model, tea.Cmd) {
	switch m.settingsCursor {
	case 0:
		m.advancedMode = !m.advancedMode
		m.settings.AdvancedMode = m.advancedMode
		m.core.SetAdvancedMode(m.advancedMode)
		return m, cmdSaveSettings(m.settingsPath, m.settings)
	case 1:
		switch m.settings.ReportSaveMode {
		case ReportSaveOff:
			m.settings.ReportSaveMode = ReportSaveAlways
		case ReportSaveAlways:
			m.settings.ReportSaveMode = ReportSaveFailureOnly
		default:
			m.settings.ReportSaveMode = ReportSaveOff
		}
		return m, cmdSaveSettings(m.settingsPath, m.settings)
	case 2:
		m.screen = screenDevices
	}
	return m, nil
}

func (m Model) viewSettings(height int) string {
	lines := []string{stylePanelTitle.Render("Settings"), ""}

	rows := []string{
		fmt.Sprintf("Advanced Mode: %v  (enables inferred/experimental safe-read checks)", m.advancedMode),
		fmt.Sprintf("Report Save Mode: %s", m.settings.ReportSaveMode),
		"Back",
	}
	for i, row := range rows {
		if i == m.settingsCursor {
			lines = append(lines, styleSelectedRow.Render("› "+row))
		} else {
			lines = append(lines, "  "+styleBody.Render(row))
		}
	}

	lines = append(lines,
		"",
		stylePanelTitle.Render("Build"),
		styleFaint.Render(fmt.Sprintf("%s  %s  %s  %s  dirty=%s", m.build.AppVersion, m.build.Commit, m.build.BuildDate, m.build.Platform, m.build.Dirty)),
		"",
	)

	info := m.settingsInfoLines()
	visible := m.settingsInfoVisibleRows()
	start, end, more := viewportWindow(len(info), m.settingsInfoOffset, m.settingsInfoOffset, visible)
	lines = append(lines, info[start:end]...)
	if more != "" {
		lines = append(lines, styleFaint.Render(fmt.Sprintf("%d-%d/%d · %s", start+1, end, len(info), more)))
	}

	return renderBoundedPanel(m.width-2, height-2, strings.Join(lines, "\n"))
}

func (m Model) settingsInfoLines() []string {
	lines := []string{
		stylePanelTitle.Render("Paths"),
		styleFaint.Render("Config: " + m.settingsPath),
		styleFaint.Render("Unlocks: " + candidateUnlockDir(m.settingsPath)),
		"",
		stylePanelTitle.Render("Gamepad Navigation"),
	}
	if len(m.navNotes) == 0 {
		return append(lines, styleFaint.Render("No standard gamepad interface active."))
	}
	for _, note := range m.navNotes {
		lines = append(lines, styleFaint.Render("· "+note))
	}
	return lines
}

func (m Model) settingsInfoVisibleRows() int {
	// View reserves two outer panel rows. Settings title/rows and Build consume
	// nine rows inside that panel; reserve one more row for a scroll indicator.
	panelHeight := max(1, m.height-calculateLayout(m.width, m.height).headerHeight-
		calculateLayout(m.width, m.height).footerHeight-2)
	available := max(1, panelHeight-9)
	if len(m.settingsInfoLines()) > available {
		return max(1, available-1)
	}
	return available
}

func (m Model) settingsInfoMaxOffset() int {
	return max(0, len(m.settingsInfoLines())-m.settingsInfoVisibleRows())
}
