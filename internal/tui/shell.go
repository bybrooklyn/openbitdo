package tui

import (
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The shell is everything around a screen's own content: the frame, the
// device list down the left, and the tabs across the top of the right-hand
// pane. A screen draws only its pane.
//
// There is one cursor, and it is always in the pane. The device list and the
// tabs are switched by their own keys (d, tab) from anywhere, so there is no
// focus to move between regions and nothing to get lost in.

// tabs are the per-device sections, in display order. Each is a screen.
var tabs = []struct {
	screen screen
	title  string
}{
	{screenDevices, "Overview"},
	{screenDiagnostics, "Checks"},
	{screenMapping, "Mapping"},
}

func isTab(s screen) bool {
	for _, tab := range tabs {
		if tab.screen == s {
			return true
		}
	}
	return false
}

// shellGeom is where each region of the shell sits on the terminal.
type shellGeom struct {
	sidebar rect // device list, inside the frame
	tabRow  int  // terminal row of the tab titles
	pane    rect // the active screen's content
	footerY int
}

func (m Model) shellGeom() shellGeom {
	bodyRows := max(1, m.height-3) // top border, footer, bottom border
	sidebarWidth := clampInt(m.width/4, 22, 28)
	if m.width < 72 {
		sidebarWidth = 18 // a narrow terminal gives the pane the room
	}
	if m.screen == screenRecovery {
		// Recovery takes the whole frame: nothing else is reachable.
		return shellGeom{
			pane:    rect{x: 1, y: 1, w: max(1, m.width-2), h: bodyRows},
			footerY: m.height - 2,
			tabRow:  -1,
		}
	}
	paneX := 1 + sidebarWidth + 1
	return shellGeom{
		sidebar: rect{x: 1, y: 1, w: sidebarWidth, h: bodyRows},
		tabRow:  1,
		pane:    rect{x: paneX, y: 3, w: max(1, m.width-paneX-1), h: max(1, bodyRows-2)},
		footerY: m.height - 2,
	}
}

// inPane returns a copy of m sized as if the pane were the whole terminal,
// which is what every screen's update, view and click code is written
// against. The offsets are the header rows and the two spare rows each
// screen's panel leaves, so a screen given this model fills the pane exactly.
func (m Model) inPane() Model {
	if m.paned {
		return m
	}
	g := m.shellGeom()
	inner := m
	inner.paned = true
	// One column narrower than the pane, so text stops short of the frame.
	inner.width = g.pane.w + 1
	inner.height = g.pane.h + 5
	return inner
}

// outOfPane undoes inPane on a model a screen handler returned.
func (m Model) outOfPane(inner Model) Model {
	if m.paned {
		return inner
	}
	inner.paned = false
	inner.width, inner.height = m.width, m.height
	return inner
}

// paneView renders the active screen into the pane.
func (m Model) paneView() string {
	g := m.shellGeom()
	inner := m.inPane()
	height := inner.height - 3
	var body string
	switch m.screen {
	case screenDevices:
		body = inner.viewDevices(height)
	case screenDiagnostics:
		body = inner.viewDiagnostics(height)
	case screenMapping:
		body = inner.viewMapping(height)
	case screenFirmware:
		body = inner.viewFirmware(height)
	case screenSettings:
		body = inner.viewSettings(height)
	case screenRecovery:
		body = inner.viewRecovery(height)
	}
	return clampRendered(body, g.pane.w, g.pane.h)
}

// viewShell draws the frame and everything in it.
func (m Model) viewShell() string {
	g := m.shellGeom()
	border := lipgloss.NewStyle().Foreground(theme.BorderDim)
	bar := border.Render("│")
	inner := max(1, m.width-2)

	title := " " + styleTitle.Render("OpenBitdo") + " "
	if m.mockMode {
		title += styleWarning.Render("mock") + " "
	}
	top := border.Render("╭─") + title + border.Render(strings.Repeat("─", max(0, inner-1-lipgloss.Width(title)))+"╮")
	bottom := border.Render("╰" + strings.Repeat("─", inner) + "╯")

	footerModel := m
	footerModel.width = inner
	footer := padTo(footerModel.viewFooter(), inner)

	rows := make([]string, 0, m.height)
	rows = append(rows, top)

	pane := strings.Split(m.paneView(), "\n")
	if m.screen == screenRecovery {
		for _, line := range pane {
			rows = append(rows, bar+padTo(line, g.pane.w)+bar)
		}
	} else {
		sidebar := strings.Split(m.viewSidebar(g.sidebar.w, g.sidebar.h), "\n")
		head := m.viewPaneHead(g.pane.w)
		right := append(head, pane...)
		for i := 0; i < g.sidebar.h; i++ {
			left, content := "", ""
			if i < len(sidebar) {
				left = sidebar[i]
			}
			if i < len(right) {
				content = right[i]
			}
			rows = append(rows, bar+padTo(left, g.sidebar.w)+bar+padTo(content, g.pane.w)+bar)
		}
	}

	rows = append(rows, bar+footer+bar, bottom)
	return strings.Join(rows, "\n")
}

// padTo cuts or pads a rendered line to exactly width cells.
func padTo(line string, width int) string {
	line = ansi.Cut(line, 0, width)
	return line + strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
}

// viewPaneHead is the two rows above the pane: the tabs with the active one
// underlined, or a plain title for a page that is not a tab.
func (m Model) viewPaneHead(width int) []string {
	rule := lipgloss.NewStyle().Foreground(theme.BorderDim)
	if !isTab(m.screen) {
		title := "Settings"
		if m.screen == screenFirmware {
			title = "Firmware"
		}
		return []string{
			"  " + styleTabActive.Render(title),
			"  " + styleAccent.Render(strings.Repeat("━", lipgloss.Width(title))) + rule.Render(strings.Repeat("─", max(0, width-4-lipgloss.Width(title)))),
		}
	}
	var titles, underline strings.Builder
	titles.WriteString("  ")
	underline.WriteString("  ")
	for i, tab := range tabs {
		if i > 0 {
			titles.WriteString("   ")
			underline.WriteString(rule.Render("───"))
		}
		if tab.screen == m.screen {
			titles.WriteString(styleTabActive.Render(tab.title))
			underline.WriteString(styleAccent.Render(strings.Repeat("━", lipgloss.Width(tab.title))))
		} else {
			titles.WriteString(styleTab.Render(tab.title))
			underline.WriteString(rule.Render(strings.Repeat("─", lipgloss.Width(tab.title))))
		}
	}
	used := lipgloss.Width(underline.String())
	underline.WriteString(rule.Render(strings.Repeat("─", max(0, width-used-2))))
	return []string{titles.String(), underline.String()}
}

// tabAt maps a click on the tab row to a tab.
func (m Model) tabAt(x int) (screen, bool) {
	g := m.shellGeom()
	col := g.pane.x + 2
	for _, tab := range tabs {
		w := lipgloss.Width(tab.title)
		if x >= col-1 && x < col+w+1 {
			return tab.screen, true
		}
		col += w + 3
	}
	return 0, false
}

// sidebarRowHeight is the lines one device takes in the list: its name, its
// verdict, and a blank line.
const sidebarRowHeight = 3

// sidebarFirstRow is the line the first device starts on, under the heading.
const sidebarFirstRow = 2

func (m Model) sidebarVisibleDevices(height int) int {
	return max(1, (height-sidebarFirstRow+1)/sidebarRowHeight)
}

// viewSidebar lists the devices. The selected one is marked with a bar down
// its two lines, a different mark from the pane's cursor so the two are not
// mistaken for each other.
func (m Model) viewSidebar(width, height int) string {
	text := max(1, width-3)
	heading := " " + styleFaint.Render("DEVICES")
	switch {
	case m.devices.filtering:
		heading = " " + styleAccent.Render(truncate("/"+m.devices.filterText+"▏", width-2))
	case m.devices.filterText != "":
		heading = " " + styleFaint.Render(truncate("/"+m.devices.filterText, width-2))
	case m.devices.loading && m.devices.scanned:
		heading = " " + styleFaint.Render("DEVICES  scanning…")
	}
	lines := []string{heading, ""}

	if len(m.devices.filtered) == 0 {
		note := "none found"
		if !m.devices.scanned {
			note = "looking…"
		} else if m.devices.filterText != "" {
			note = "no match"
		}
		lines = append(lines, "   "+styleFaint.Render(note))
		return strings.Join(lines, "\n")
	}

	start, end, _ := viewportWindow(len(m.devices.filtered), m.devices.cursor, m.devices.listOffset, m.sidebarVisibleDevices(height))
	for i := start; i < end; i++ {
		device := m.devices.filtered[i]
		v := m.deviceVerdict(device)
		name := truncate(device.DisplayName, text)
		status := truncate(v.glyph+" "+strings.ToLower(v.word), text)
		if i == m.devices.cursor {
			mark := styleAccent.Render("┃")
			lines = append(lines,
				" "+mark+" "+styleTabActive.Render(name),
				" "+mark+" "+v.style.Render(status))
		} else {
			lines = append(lines,
				"   "+styleBody.Render(name),
				"   "+styleFaint.Render(status))
		}
		more := ""
		if i == end-1 && end < len(m.devices.filtered) {
			more = "   " + styleFaint.Render("↓ more")
		}
		lines = append(lines, more)
	}
	return strings.Join(lines, "\n")
}

// deviceAt maps a click in the sidebar to a device row.
func (m Model) deviceAt(y int) (int, bool) {
	g := m.shellGeom()
	row := y - g.sidebar.y - sidebarFirstRow
	if row < 0 || row%sidebarRowHeight == sidebarRowHeight-1 {
		return 0, false
	}
	start, end, _ := viewportWindow(len(m.devices.filtered), m.devices.cursor, m.devices.listOffset, m.sidebarVisibleDevices(g.sidebar.h))
	if i := start + row/sidebarRowHeight; i < end {
		return i, true
	}
	return 0, false
}

// inSubView reports whether the pane is showing something layered on top of
// a tab (the report, a slot preview, a running transfer). Shell keys wait
// until it is closed, so they cannot pull the tab out from under it.
func (m Model) inSubView() bool {
	switch m.screen {
	case screenDiagnostics:
		return m.diag.showSupportRequest
	case screenMapping:
		return m.mapping.previewing() || m.mapping.applying
	}
	return false
}

// shellKey handles the keys that belong to the shell rather than a screen:
// switching device, switching tab, opening settings, starting a filter.
func (m Model) shellKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if !isTab(m.screen) || m.inSubView() {
		return m, nil, false
	}
	count := len(m.devices.filtered)
	switch msg.String() {
	case "tab":
		next, cmd := m.navigate(m.tabOffset(1), m.devices.cursor)
		return next, cmd, true
	case "shift+tab":
		next, cmd := m.navigate(m.tabOffset(-1), m.devices.cursor)
		return next, cmd, true
	case "right", "left":
		// Where a tab has no use of its own for left and right, they step
		// through the tabs. The mapping editor does use them (to change a
		// row), so there they stay with the editor. This is also how a
		// controller's d-pad changes section.
		if m.screen == screenMapping && m.mapping.unavailable == "" {
			return m, nil, false
		}
		delta := 1
		if msg.String() == "left" {
			delta = -1
		}
		next, cmd := m.navigate(m.tabOffset(delta), m.devices.cursor)
		return next, cmd, true
	case "1", "2", "3":
		next, cmd := m.navigate(tabs[int(msg.String()[0]-'1')].screen, m.devices.cursor)
		return next, cmd, true
	case "d":
		if count > 1 {
			next, cmd := m.navigate(m.screen, (m.devices.cursor+1)%count)
			return next, cmd, true
		}
		return m, nil, true
	case "D":
		if count > 1 {
			next, cmd := m.navigate(m.screen, (m.devices.cursor+count-1)%count)
			return next, cmd, true
		}
		return m, nil, true
	case "s":
		next, cmd := m.navigate(screenSettings, m.devices.cursor)
		return next, cmd, true
	case "/":
		if m.screen == screenMapping && m.mapping.dirty() {
			return m, nil, true
		}
		m.devices.filtering = true
		m.screen = screenDevices
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) tabOffset(delta int) screen {
	for i, tab := range tabs {
		if tab.screen == m.screen {
			return tabs[((i+delta)%len(tabs)+len(tabs))%len(tabs)].screen
		}
	}
	return screenDevices
}

// navigate is the one way to change what the pane shows: another tab,
// another device, or the settings page. Leaving the mapping editor with
// unapplied changes asks first, wherever the user was heading.
func (m Model) navigate(target screen, deviceIdx int) (Model, tea.Cmd) {
	leavingMapping := m.screen == screenMapping && (target != screenMapping || deviceIdx != m.devices.cursor)
	if leavingMapping && m.mapping.dirty() {
		m.modal = discardMappingModal(discardMappingMsg{action: discardActionNavigate, screen: target, deviceIdx: deviceIdx})
		return m, nil
	}
	return m.navigateNow(target, deviceIdx)
}

func (m Model) navigateNow(target screen, deviceIdx int) (Model, tea.Cmd) {
	if deviceIdx != m.devices.cursor {
		m.devices.cursor = clampInt(deviceIdx, 0, len(m.devices.filtered)-1)
		m.devices.actionIdx = 0
		start, _, _ := viewportWindow(len(m.devices.filtered), m.devices.cursor, m.devices.listOffset, m.sidebarVisibleDevices(m.shellGeom().sidebar.h))
		m.devices.listOffset = start
	}
	if target == screenSettings {
		if isTab(m.screen) {
			m.prevScreen = m.screen
		}
		m.screen = screenSettings
		m.settingsCursor = 0
		return m, nil
	}

	device, ok := m.devices.selected()
	if !ok {
		m.screen = screenDevices
		return m, nil
	}
	m.screen = target
	switch target {
	case screenDiagnostics:
		return m.openChecks(device)
	case screenMapping:
		return m.openMapping(device)
	}
	return m, nil
}

// openChecks shows device's diagnostics: the result already known this
// session if there is one, otherwise a fresh run.
func (m Model) openChecks(device core.AppDevice) (Model, tea.Cmd) {
	m.diag = newDiagnosticsState()
	m.diag.device = device
	if device.ConfigChannel == core.ChannelAbsent {
		// Nothing to run a check over; say so rather than try.
		m.diag.err = &core.Error{Kind: core.KindNoConfigChannel, Message: "no configuration interface"}
		return m, nil
	}
	// A cache hit renders instantly with no loading flash — a plain
	// mutex-protected map read, not I/O, safe to do synchronously here.
	if entry, ok := m.core.CachedDiag(device); ok {
		m.diag.result = entry.Result
		m.diag.ranAt = entry.RanAt
		m.diag.err = entry.Err
		return m, nil
	}
	m.diag.loading = true
	return m, cmdDiagProbeCached(m.ctx, m.core, device)
}

// openMapping shows device's mapping editor, or why there isn't one yet.
func (m Model) openMapping(device core.AppDevice) (Model, tea.Cmd) {
	m.mapping = newMappingState()
	m.mapping.device = device
	m.mapping.kind = core.KindUltimate2
	if device.Capability.SupportsJP108DedicatedMap {
		m.mapping.kind = core.KindJP108
	}
	for _, item := range m.actionsFor(device) {
		if item.kind == actionMapping && item.reason != "" {
			m.mapping.unavailable = item.reason
			return m, nil
		}
	}
	m.mapping.loading = true
	if m.mapping.kind == core.KindJP108 {
		return m, cmdJP108ReadMapping(m.ctx, m.core, device.VidPid)
	}
	return m, cmdU2ReadProfile(m.ctx, m.core, device.VidPid, core.U2Slot1)
}
