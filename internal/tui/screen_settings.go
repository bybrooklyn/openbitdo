package tui

import (
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
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

// settingsFirstRow is the panel line the first setting row is drawn on.
const settingsFirstRow = 2

type settingRow struct {
	label, value, about string
}

func (m Model) settingRows() []settingRow {
	advanced := "off"
	if m.advancedMode {
		advanced = "on"
	}
	reports := map[ReportSaveMode]string{
		ReportSaveOff:         "never",
		ReportSaveAlways:      "after every operation",
		ReportSaveFailureOnly: "only when something fails",
	}[m.settings.ReportSaveMode]
	return []settingRow{
		{
			label: "Advanced mode", value: advanced,
			about: "Allows operations that use unconfirmed commands. Leave off for normal use.",
		},
		{
			label: "Save reports", value: reports,
			about: "When a report file is written by itself. To save one by hand: diagnostics, v, w.",
		},
		{label: "Back", about: "Return to the device list."},
	}
}

func (m Model) viewSettings(height int) string {
	text := max(1, m.width-4)
	lines := []string{stylePanelTitle.Render("Settings"), ""}

	rows := m.settingRows()
	for i, row := range rows {
		line := row.label
		if row.value != "" {
			line = fmt.Sprintf("%-16s %s", row.label, row.value)
		}
		if i == m.settingsCursor {
			lines = append(lines, styleSelectedRow.Render(truncate("› "+line, text)))
		} else {
			lines = append(lines, "  "+styleBody.Render(truncate(line, text-2)))
		}
	}
	// One line of explanation for whichever row is selected, so the rows
	// themselves stay short.
	about := ""
	if m.settingsCursor < len(rows) {
		about = rows[m.settingsCursor].about
	}
	lines = append(lines, styleFaint.Render(truncate(about, text)), "")

	info := m.settingsInfoLines()
	visible := m.settingsInfoVisibleRows()
	start, end, more := viewportWindow(len(info), m.settingsInfoOffset, m.settingsInfoOffset, visible)
	lines = append(lines, info[start:end]...)
	if more != "" {
		lines = append(lines, styleFaint.Render(fmt.Sprintf("lines %d-%d of %d · pgup/pgdn to scroll", start+1, end, len(info))))
	}

	return renderBoundedPanel(m.width-2, height-2, strings.Join(lines, "\n"))
}

func (m Model) settingsInfoLines() []string {
	text := max(1, m.width-4)
	var lines []string
	section := func(title string) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, stylePanelTitle.Render(title))
	}
	add := func(s string) {
		lines = append(lines, strings.Split(wrapStyled(styleFaint, s, text), "\n")...)
	}

	section("Recent activity")
	if len(m.activity) == 0 {
		add("Nothing yet. Messages from the bottom line are kept here.")
	}
	for i := len(m.activity) - 1; i >= 0 && i >= len(m.activity)-8; i-- {
		entry := m.activity[i]
		add(entry.at.Format("15:04:05") + "  " + entry.message)
	}

	section("Controller navigation")
	if len(m.navNotes) == 0 {
		add("No controller is driving the menus. Keyboard and mouse always work.")
	}
	for _, note := range m.navNotes {
		add("· " + m.friendlyNavNote(note))
	}

	section("Files")
	add("Settings: " + m.settingsPath)
	add("Reports:  " + reportsDir(m.settingsPath))
	add("Unlock files: " + candidateUnlockDir(m.settingsPath))

	section("About")
	add(fmt.Sprintf("OpenBitdo %s (%s, built %s, %s)", m.build.AppVersion, m.build.Commit, m.build.BuildDate, m.build.Platform))
	return lines
}

// friendlyNavNote turns internal/input's "pid=0x6013: gamepad nav ..." note
// into a sentence naming the device.
func (m Model) friendlyNavNote(note string) string {
	prefix, rest, ok := strings.Cut(note, ": ")
	var pid uint16
	if !ok {
		return note
	}
	if _, err := fmt.Sscanf(prefix, "pid=0x%x", &pid); err != nil {
		return note
	}
	name := fmt.Sprintf("Device %#04x", pid)
	if catalog := protocol.DeviceProfileFor(protocol.VidPid{VID: 0x2dc8, PID: pid}).DisplayName; catalog != "" {
		name = catalog
	}
	for _, device := range m.devices.devices {
		if device.VidPid.PID == pid {
			name = device.DisplayName
		}
	}
	switch {
	case strings.HasPrefix(rest, "gamepad nav active"):
		return name + ": can drive the menus (d-pad to move, A to select, B to go back)."
	case strings.Contains(rest, "no gamepad interface"):
		return name + ": cannot drive the menus. It exposes no gamepad interface in its current mode."
	case strings.HasPrefix(rest, "disconnected"):
		return name + ": disconnected."
	case strings.HasPrefix(rest, "gamepad nav unavailable"):
		reason := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(rest, "gamepad nav unavailable"), " ("), ")")
		return name + ": cannot drive the menus (" + reason + ")."
	}
	return name + ": " + rest
}

func (m Model) settingsInfoVisibleRows() int {
	// View reserves two outer panel rows. The title, its blank line, the
	// setting rows, the explanation and a blank line sit above the info
	// block; one more row is kept for the scroll indicator.
	panelHeight := max(1, m.height-calculateLayout(m.width, m.height).headerHeight-
		calculateLayout(m.width, m.height).footerHeight-2)
	available := max(1, panelHeight-(settingsFirstRow+settingsRowCount+2))
	if len(m.settingsInfoLines()) > available {
		return max(1, available-1)
	}
	return available
}

func (m Model) settingsInfoMaxOffset() int {
	return max(0, len(m.settingsInfoLines())-m.settingsInfoVisibleRows())
}
