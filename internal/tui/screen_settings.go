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
			m.statusLine = "Settings saved."
		} else {
			m.err = msg.err
		}
		return m, nil

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
	var b strings.Builder
	b.WriteString(stylePanelTitle.Render("Settings") + "\n\n")

	rows := []string{
		fmt.Sprintf("Advanced Mode: %v  (enables inferred/experimental safe-read checks)", m.advancedMode),
		fmt.Sprintf("Report Save Mode: %s", m.settings.ReportSaveMode),
		"Back",
	}
	for i, row := range rows {
		if i == m.settingsCursor {
			b.WriteString(styleSelectedRow.Render("› "+row) + "\n")
		} else {
			b.WriteString("  " + styleBody.Render(row) + "\n")
		}
	}

	b.WriteString("\n" + styleFaint.Render("Config file: "+m.settingsPath) + "\n")
	b.WriteString(styleFaint.Render("Candidate unlock files: "+candidateUnlockDir(m.settingsPath)) + "\n\n")

	b.WriteString(stylePanelTitle.Render("Build") + "\n")
	b.WriteString(styleFaint.Render(fmt.Sprintf("%s  %s  %s  %s\n", m.build.AppVersion, m.build.Commit, m.build.BuildDate, m.build.Platform)))

	if len(m.navNotes) > 0 {
		b.WriteString("\n" + stylePanelTitle.Render("Gamepad Navigation") + "\n")
		for _, note := range m.navNotes {
			b.WriteString(styleFaint.Render("· "+note) + "\n")
		}
	}

	return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
}
