package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

type devicePane int

const (
	paneDeviceList devicePane = iota
	paneActions
)

type actionKind int

const (
	actionDiagnose actionKind = iota
	actionMapping
	actionFirmware
	actionGuardedProbe
)

type actionItem struct {
	label  string
	kind   actionKind
	reason string // "" means enabled
}

type devicesState struct {
	devices    []core.AppDevice
	filtered   []core.AppDevice
	cursor     int
	listOffset int
	filterText string
	filtering  bool
	pane       devicePane
	actionIdx  int

	// loading is true while a scan is in flight; scanned once any scan has
	// finished, so "no devices" is only claimed after actually looking.
	loading bool
	scanned bool
	// announceScan makes the next finished scan report what it found: set
	// when the user asked for it, not for background reloads.
	announceScan bool
}

func newDevicesState() devicesState {
	return devicesState{pane: paneDeviceList, loading: true}
}

// sameDevice reports whether a and b are the same physical device.
func sameDevice(a, b core.AppDevice) bool {
	return a.VidPid == b.VidPid && a.Serial == b.Serial
}

// reselect moves the cursor back onto want after the list changed, so a
// reload never silently retargets the selection (and any open action) to a
// different device.
func (d *devicesState) reselect(want core.AppDevice, had bool) {
	if had {
		for i, dev := range d.filtered {
			if sameDevice(dev, want) {
				d.cursor = i
				return
			}
		}
		// The selected device is gone; its actions pane has no subject.
		d.pane = paneDeviceList
	}
	d.cursor = clampInt(d.cursor, 0, len(d.filtered)-1)
}

// applyFilter fuzzy-matches devices by name (via sahilm/fuzzy, the same
// library charmbracelet/bubbles itself uses for list filtering), matching
// the fuzzy device-search behavior the prior Rust editor had via
// fuzzy-matcher/SkimMatcherV2 rather than a plain substring match.
func (d *devicesState) applyFilter() {
	if d.filterText == "" {
		d.filtered = d.devices
		return
	}
	names := make([]string, len(d.devices))
	for i, dev := range d.devices {
		// Match what is on screen first, but keep the registry ID and the
		// PID findable too.
		names[i] = dev.DisplayName + " " + dev.Name + " " + pidLabel(dev.VidPid)
	}
	matches := fuzzy.Find(d.filterText, names)
	filtered := make([]core.AppDevice, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, d.devices[match.Index])
	}
	d.filtered = filtered
}

// sortDevicesByTier groups the "grouped dashboard: supported, read-only
// candidate, or detect-only" browsing order the README documents as current
// behavior — a stable sort so devices within the same tier keep their
// enumeration order.
func sortDevicesByTier(devices []core.AppDevice) []core.AppDevice {
	out := append([]core.AppDevice(nil), devices...)
	sort.SliceStable(out, func(i, j int) bool {
		return tierRank(out[i].SupportTier) < tierRank(out[j].SupportTier)
	})
	return out
}

func tierRank(t protocol.SupportTier) int {
	switch t {
	case protocol.TierFull:
		return 0
	case protocol.TierCandidateReadOnly:
		return 1
	default:
		return 2
	}
}

func (d devicesState) selected() (core.AppDevice, bool) {
	if d.cursor < 0 || d.cursor >= len(d.filtered) {
		return core.AppDevice{}, false
	}
	return d.filtered[d.cursor], true
}

// healthText describes a device's live state: a glyph, a few words for the
// list row, and a sentence for the detail panel.
func healthText(h core.DeviceHealth) (glyph string, style lipgloss.Style, short, long string) {
	switch h.State {
	case core.HealthResponding:
		return IconTierFull, styleBadgeFull, "responding",
			fmt.Sprintf("Responding. It answered %d of %d diagnostic checks.", h.Answered, h.Total)
	case core.HealthSilent:
		return IconTierDetect, styleBadgeDetect, "not answering",
			fmt.Sprintf("Not answering. It opened, but none of the %d diagnostic checks got a reply.", h.Total)
	case core.HealthNoChannel:
		return IconTierDetect, styleBadgeDetect, "unreachable",
			"Connected, but it does not expose the interface OpenBitdo sends commands through. " +
				"OpenBitdo can identify it and nothing more in its current mode."
	case core.HealthNoPermission:
		return IconFail, styleDanger, "no permission",
			"Your user account is not allowed to open this device."
	case core.HealthDisconnected:
		return IconTierDetect, styleBadgeDetect, "unplugged",
			"Disconnected. Plug it back in and press r."
	case core.HealthError:
		return IconWarn, styleWarning, "check failed", "The last check could not run."
	default:
		return IconTierCandidate, styleBadgeCandidate, "checking…", "Checking what this device answers…"
	}
}

// permissionFixLines is the fix for HealthNoPermission, kept as separate
// lines so each command can be read (and copied) whole.
func permissionFixLines() []string {
	return []string{
		"To fix it, install the udev rule and replug the device:",
		"  sudo cp packaging/linux/70-openbitdo.rules /etc/udev/rules.d/",
		"  sudo udevadm control --reload-rules && sudo udevadm trigger",
		"Packaged installs already ship the rule; replugging is enough.",
	}
}

func (m Model) actionsForSelectedDevice() []actionItem {
	device, ok := m.devices.selected()
	if !ok {
		return nil
	}
	health := m.core.Health(device)
	unreachable := ""
	switch health.State {
	case core.HealthNoChannel:
		unreachable = "this device has no configuration interface"
	case core.HealthNoPermission:
		unreachable = "your user cannot open this device"
	case core.HealthSilent:
		unreachable = "the device is not answering"
	case core.HealthDisconnected:
		unreachable = "the device is disconnected"
	}

	diagnose := actionItem{label: "Run diagnostics", kind: actionDiagnose}
	if health.State == core.HealthNoChannel {
		// Nothing to send diagnostics through. Opening the screen would only
		// repeat what the status line above already says.
		diagnose.reason = unreachable
	}
	items := []actionItem{diagnose}

	mapping := actionItem{
		label: "Mapping editor", kind: actionMapping,
		reason: mappingDisabledReason(device, m.mockMode, m.writeLockUntilRestart),
	}
	if device.Capability.SupportsU2ButtonMap && m.mockMode {
		mapping.label = "Mapping preview (mock only)"
	}
	if mapping.reason == "" && unreachable != "" {
		mapping.reason = unreachable
	}
	items = append(items, mapping)

	// Firmware is listed as an action only in builds where it can run. In a
	// release it is unavailable for every device, which the detail panel
	// says once rather than offering a row that can never work.
	if m.core.FirmwareEnabled() {
		items = append(items, actionItem{
			label: "Firmware update", kind: actionFirmware,
			// Risk acknowledgement is collected interactively via modal on
			// trigger, not treated as a static precondition here.
			reason: firmwareDisabledReason(device, true, true, m.writeLockUntilRestart),
		})
	}
	if device.SupportTier == protocol.TierCandidateReadOnly {
		// The risk acknowledgement is collected by a dialog when the action
		// is triggered, so it is not a precondition here.
		reason := candidateUnlockDisabledReason(device, m.advancedMode, true, m.writeLockUntilRestart)
		switch {
		case reason == "Enable advanced mode first":
			reason = "turn on Advanced mode in Settings first"
		case reason == "" && !candidateUnlockFilePresent(m.settingsPath, device.VidPid):
			reason = "needs an unlock file: " + candidateUnlockFilePath(m.settingsPath, device.VidPid) +
				" containing pid = \"" + fmt.Sprintf("%#04x", device.VidPid.PID) + "\" and candidate_write_unlock = true"
		}
		items = append(items, actionItem{label: "Guarded write probe", kind: actionGuardedProbe, reason: reason})
	}
	return items
}

func (m Model) updateDevices(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.devices.filtering {
			return m.updateDeviceFilterInput(msg)
		}
		switch msg.String() {
		case "/":
			m.devices.filtering = true
			m.devices.pane = paneDeviceList
			return m, nil
		case "r":
			return m.rescanDevices()
		case "s":
			m.screen = screenSettings
			m.settingsCursor = 0
			return m, nil
		case "up", "k":
			if m.devices.pane == paneDeviceList {
				if m.devices.cursor > 0 {
					m.devices.cursor--
					m.ensureDeviceCursorVisible()
				}
			} else if m.devices.actionIdx > 0 {
				m.devices.actionIdx--
			}
			return m, nil
		case "down", "j":
			if m.devices.pane == paneDeviceList {
				if m.devices.cursor < len(m.devices.filtered)-1 {
					m.devices.cursor++
					m.ensureDeviceCursorVisible()
				}
			} else if items := m.actionsForSelectedDevice(); m.devices.actionIdx < len(items)-1 {
				m.devices.actionIdx++
			}
			return m, nil
		case "left":
			m.devices.pane = paneDeviceList
			return m, nil
		case "right", "tab":
			if _, ok := m.devices.selected(); ok {
				m.devices.pane = paneActions
				m.devices.actionIdx = 0
			}
			return m, nil
		case "esc":
			if m.devices.pane == paneDeviceList && m.devices.filterText != "" {
				m.devices.filterText = ""
				m.devices.applyFilter()
				m.devices.cursor = clampInt(m.devices.cursor, 0, len(m.devices.filtered)-1)
				return m, nil
			}
			m.devices.pane = paneDeviceList
			return m, nil
		case "enter":
			return m.triggerDevicesEnter()
		}
	}
	return m, nil
}

// rescanDevices looks for devices again on the user's request. Rescanning is
// always available, including with nothing connected: "plug in a controller
// and see it appear" is the core workflow. A probe that failed earlier is
// retried, since whatever caused it (permissions, a loose cable) is exactly
// what the user may have just fixed.
func (m Model) rescanDevices() (tea.Model, tea.Cmd) {
	m.core.ForgetFailedDiags()
	m.devices.loading = true
	m.devices.announceScan = true
	return m, cmdLoadDevices(m.ctx, m.core)
}

func (m Model) updateDeviceFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.devices.filtering = false
		return m, nil
	case tea.KeyEsc:
		m.devices.filtering = false
		m.devices.filterText = ""
	case tea.KeyBackspace:
		if len(m.devices.filterText) > 0 {
			runes := []rune(m.devices.filterText)
			m.devices.filterText = string(runes[:len(runes)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.devices.filterText += string(msg.Runes)
	default:
		return m, nil
	}
	m.devices.applyFilter()
	if m.devices.cursor >= len(m.devices.filtered) {
		m.devices.cursor = 0
	}
	m.devices.listOffset = 0
	return m, nil
}

func (m *Model) ensureDeviceCursorVisible() {
	start, _, _ := viewportWindow(len(m.devices.filtered), m.devices.cursor, m.devices.listOffset, m.deviceVisibleRows())
	m.devices.listOffset = start
}

// deviceVisibleRows is how many device rows the list panel shows. The wide
// layout gives the list the full body height; the compact one stacks the
// list above the detail panel and keeps it short.
func (m Model) deviceVisibleRows() int {
	layout := calculateLayout(m.width, m.height)
	if layout.mode == layoutCompact {
		return compactListRows(len(m.devices.filtered))
	}
	return max(1, layout.bodyHeight-4)
}

// compactListRows caps the stacked list so the detail panel below it keeps
// most of the screen.
func compactListRows(devices int) int {
	return clampInt(devices, 1, 4)
}

func (m Model) triggerDevicesEnter() (tea.Model, tea.Cmd) {
	if m.devices.pane == paneDeviceList {
		if _, ok := m.devices.selected(); ok {
			m.devices.pane = paneActions
			m.devices.actionIdx = 0
		}
		return m, nil
	}

	items := m.actionsForSelectedDevice()
	if m.devices.actionIdx >= len(items) {
		return m, nil
	}
	item := items[m.devices.actionIdx]
	if item.reason != "" {
		// The reason is already written beside the row; this only confirms
		// the keypress registered, and clears itself.
		return m.setNotice(noticeWarning, item.label+" is not available: "+item.reason, true)
	}
	device, _ := m.devices.selected()

	switch item.kind {
	case actionDiagnose:
		m.screen = screenDiagnostics
		m.diag = newDiagnosticsState()
		m.diag.device = device
		// A cache hit renders instantly with no loading flash — a plain
		// mutex-protected map read, not I/O, safe to do synchronously here
		// rather than round-tripping through a tea.Cmd just to look up what
		// core.HasDiagnosed would immediately confirm is already there.
		if entry, ok := m.core.CachedDiag(device); ok {
			m.diag.result = entry.Result
			m.diag.ranAt = entry.RanAt
			m.diag.err = entry.Err
			return m, nil
		}
		m.diag.loading = true
		return m, cmdDiagProbeCached(m.ctx, m.core, device)

	case actionMapping:
		m.screen = screenMapping
		m.mapping = newMappingState()
		m.mapping.device = device
		m.mapping.loading = true
		if device.Capability.SupportsJP108DedicatedMap {
			m.mapping.kind = core.KindJP108
			return m, cmdJP108ReadMapping(m.ctx, m.core, device.VidPid)
		}
		m.mapping.kind = core.KindUltimate2
		return m, cmdU2ReadProfile(m.ctx, m.core, device.VidPid, core.U2Slot1)

	case actionFirmware:
		startFirmware := firmwareBeginMsg{device: device}
		if !m.acknowledgedRisk {
			m.modal = riskAckModal("update this device's firmware", firmwareRisk, startFirmware)
			return m, nil
		}
		return m.Update(startFirmware)

	case actionGuardedProbe:
		probe := candidateProbeBeginMsg{device: device}
		if !m.acknowledgedRisk {
			m.modal = riskAckModal("run the guarded write probe", writeProbeRisk, probe)
			return m, nil
		}
		return m.Update(probe)

	}
	return m, nil
}

func (m Model) viewDevices(height int) string {
	list, detail := m.devicePanels(height)
	if calculateLayout(m.width, m.height).mode == layoutCompact {
		return list.render() + "\n" + detail.render()
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, list.render(), " ", detail.render())
}

// devicePanel is one of the two Devices panels, laid out but not yet
// rendered: its lines, and for each line which row (device or action) a
// click on it means, or -1. Rendering and mouse hit-testing both read this,
// so a click can never land on a different row than the one drawn there.
type devicePanel struct {
	area   rect // where the panel's first content line sits, in body coordinates
	width  int
	active bool
	lines  []string
	owners []int
	// keep is the row that must stay on screen when the panel is too short
	// for all its lines (the selected action), or -1.
	keep int
}

func (p *devicePanel) add(owner int, lines ...string) {
	for _, line := range lines {
		p.lines = append(p.lines, line)
		p.owners = append(p.owners, owner)
	}
}

// window returns the slice of lines shown and the index of the first one.
// A panel taller than its lines shows them all. Otherwise it scrolls just
// far enough to keep the kept row visible, and marks each cut edge.
func (p devicePanel) window() (lines []string, first int) {
	height := max(1, p.area.h)
	if len(p.lines) <= height {
		return p.lines, 0
	}
	last := -1
	for i, owner := range p.owners {
		if p.keep >= 0 && owner == p.keep {
			last = i
		}
	}
	// Leave the bottom line for the "more below" marker.
	if last >= 0 && last > height-2 {
		first = min(last-(height-2), len(p.lines)-height)
	}
	lines = append([]string(nil), p.lines[first:first+height]...)
	if first > 0 {
		lines[0] = styleFaint.Render("⋯ more above")
	}
	if first+height < len(p.lines) {
		lines[height-1] = styleFaint.Render("⋯ more below; enlarge the window")
	}
	return lines, first
}

func (p devicePanel) render() string {
	style := stylePanel
	if p.active {
		style = stylePanelActive
	}
	lines, _ := p.window()
	return renderBoundedPanelWithStyle(style, p.width, max(1, p.area.h), strings.Join(lines, "\n"))
}

// ownerAt maps a click at body coordinates to the row drawn there.
func (p devicePanel) ownerAt(x, y int) (int, bool) {
	if !p.area.contains(x, y) {
		return 0, false
	}
	shown, first := p.window()
	line := y - p.area.y
	if line >= len(shown) {
		return 0, false
	}
	// A cut edge shows a marker, not the row it replaced.
	if (line == 0 && first > 0) || (line == len(shown)-1 && first+len(shown) < len(p.lines)) {
		return 0, false
	}
	if owner := p.owners[first+line]; owner >= 0 {
		return owner, true
	}
	return 0, false
}

// devicePanels lays out the device list and the selected device's detail:
// side by side when there is room, stacked (list on top, kept short) when
// there is not. The same two panels in both layouts, so nothing a wide
// terminal shows is missing from a narrow one.
func (m Model) devicePanels(height int) (list, detail devicePanel) {
	panelHeight := max(1, height-2)
	if calculateLayout(m.width, m.height).mode == layoutCompact {
		width := max(1, m.width-2)
		// Title, the rows, and a blank line separating it from the detail.
		listHeight := min(panelHeight-4, compactListRows(len(m.devices.filtered))+2)
		if len(m.devices.filtered) == 0 {
			listHeight = min(panelHeight-4, 5)
		}
		listHeight = max(1, listHeight)
		list = m.deviceListPanel(width, listHeight)
		list.area = rect{x: 0, y: 0, w: m.width, h: listHeight}
		detailHeight := max(1, panelHeight-listHeight)
		detail = m.deviceDetailPanel(width, detailHeight)
		detail.area = rect{x: 0, y: listHeight, w: m.width, h: detailHeight}
		return list, detail
	}

	listWidth := max(24, m.width*2/5)
	detailWidth := m.width - listWidth - 5
	list = m.deviceListPanel(listWidth, panelHeight)
	list.area = rect{x: 0, y: 0, w: listWidth + 1, h: panelHeight}
	detail = m.deviceDetailPanel(detailWidth, panelHeight)
	detail.area = rect{x: listWidth + 2, y: 0, w: detailWidth + 1, h: panelHeight}
	return list, detail
}

func (m Model) deviceListPanel(width, height int) devicePanel {
	panel := devicePanel{width: width, active: m.devices.pane == paneDeviceList, keep: -1, area: rect{h: height}}
	text := max(1, width-2)

	title := stylePanelTitle.Render("Devices")
	switch {
	case m.devices.filtering:
		title += "  " + styleAccent.Render("/"+m.devices.filterText+"▏")
	case m.devices.filterText != "":
		title += "  " + styleFaint.Render("/"+m.devices.filterText)
	case m.devices.loading && m.devices.scanned:
		title += "  " + styleFaint.Render("scanning…")
	}
	panel.add(-1, title)
	if height > compactListRows(len(m.devices.filtered))+2 {
		panel.add(-1, "") // room to breathe when the panel is tall
	}

	if len(m.devices.filtered) == 0 {
		switch {
		case !m.devices.scanned:
			panel.add(-1, styleFaint.Render("Looking for devices…"))
		case m.devices.filterText != "":
			panel.add(-1, wrapStyled(styleFaint, fmt.Sprintf("No device matches %q. Press esc to clear the filter.", m.devices.filterText), text))
		default:
			panel.add(-1, styleBody.Render("No 8BitDo device found."))
			panel.add(-1, strings.Split(wrapStyled(styleFaint,
				"Plug one in over USB and it appears here by itself, or press r to look again. "+
					"No hardware? Run openbitdo --mock to look around.", text), "\n")...)
		}
		return panel
	}

	start, end, _ := viewportWindow(len(m.devices.filtered), m.devices.cursor, m.devices.listOffset, m.deviceVisibleRows())
	for i := start; i < end; i++ {
		device := m.devices.filtered[i]
		glyph, style, short, _ := healthText(m.core.Health(device))
		// Scroll markers share the first and last visible rows' line rather
		// than taking a row each, so a short list loses nothing to them.
		marker := ""
		if i == start && start > 0 {
			marker = " ↑"
		}
		if i == end-1 && end < len(m.devices.filtered) {
			marker = " ↓"
		}
		if m.core.Health(device).State == core.HealthResponding {
			short = "" // the glyph says it; the row's room goes to the name
		}
		name := truncate(device.DisplayName, max(4, text-ansi.StringWidth(short)-6-len([]rune(marker))))
		if i == m.devices.cursor {
			// One Render call over plain text: a nested style's reset would
			// cut the highlight short. The › keeps the row findable when
			// colour is off.
			panel.add(i, styleSelectedRow.Render(truncate("› "+glyph+" "+name+"  "+short+marker, text)))
		} else {
			panel.add(i, "  "+style.Render(glyph)+" "+styleBody.Render(name)+"  "+styleFaint.Render(short+marker))
		}
	}
	return panel
}

func (m Model) deviceDetailPanel(width, height int) devicePanel {
	panel := devicePanel{width: width, active: m.devices.pane == paneActions, keep: -1, area: rect{h: height}}
	if panel.active {
		panel.keep = m.devices.actionIdx
	}
	text := max(1, width-2)
	device, ok := m.devices.selected()
	if !ok {
		if m.devices.scanned && len(m.devices.devices) == 0 {
			panel.add(-1, strings.Split(wrapStyled(styleFaint,
				"Once a device is connected, this panel shows what it answers and what you can do with it.", text), "\n")...)
		}
		return panel
	}

	health := m.core.Health(device)
	glyph, glyphStyle, _, long := healthText(health)

	panel.add(-1, stylePanelTitle.Render(truncate(device.DisplayName, text)))
	identity := pidLabel(device.VidPid)
	if catalog := protocol.DeviceProfileFor(device.VidPid).DisplayName; catalog != "" && !strings.EqualFold(catalog, device.DisplayName) {
		identity = catalog + " · " + identity
	}
	panel.add(-1, styleFaint.Render(truncate(identity, text)), "")

	statusLines := wrapText(glyph+" "+long, text)
	for i, line := range statusLines {
		if i == 0 {
			// Colour the glyph only; the sentence stays body text.
			line = glyphStyle.Render(glyph) + styleBody.Render(strings.TrimPrefix(line, glyph))
		} else {
			line = styleBody.Render(line)
		}
		panel.add(-1, line)
	}
	switch health.State {
	case core.HealthNoPermission:
		for _, line := range permissionFixLines() {
			panel.add(-1, strings.Split(wrapStyled(styleFaint, line, text), "\n")...)
		}
	case core.HealthError:
		if health.Err != nil {
			panel.add(-1, strings.Split(wrapStyled(styleFaint, health.Err.Error(), text), "\n")...)
		}
	}

	panel.add(-1, "", stylePanelTitle.Render("Actions"))
	for i, item := range m.actionsForSelectedDevice() {
		selected := m.devices.pane == paneActions && i == m.devices.actionIdx
		switch {
		case selected:
			panel.add(i, styleSelectedRow.Render(truncate("› "+item.label, text)))
		case item.reason != "":
			panel.add(i, "  "+styleFaint.Render(truncate(item.label, text-2)))
		default:
			panel.add(i, "  "+styleBody.Render(truncate(item.label, text-2)))
		}
		if item.reason != "" {
			// The reason gets its own wrapped, indented lines under the row
			// instead of being cut off at the panel edge beside it.
			for _, line := range wrapText("not available: "+item.reason, max(1, text-4)) {
				panel.add(i, "    "+styleFaint.Render(line))
			}
		}
	}

	panel.add(-1, "")
	panel.add(-1, strings.Split(wrapStyled(styleFaint, "Next: "+m.nextStepFor(device, health), text), "\n")...)
	if !m.core.FirmwareEnabled() {
		// Said once, with the release's own label, instead of a disabled
		// action row on every device.
		note := "Firmware update: " + firmwareDisabledReason(device, false, true, m.writeLockUntilRestart) + ". Not available in this release."
		panel.add(-1, strings.Split(wrapStyled(styleFaint, note, text), "\n")...)
	}
	if note := tierNote(device); note != "" && health.Reachable() {
		panel.add(-1, "")
		panel.add(-1, strings.Split(wrapStyled(styleFaint, note, text), "\n")...)
	}
	return panel
}

// nextStepFor is the one thing worth doing next for device, in a sentence.
func (m Model) nextStepFor(device core.AppDevice, health core.DeviceHealth) string {
	switch health.State {
	case core.HealthNoChannel:
		return "nothing to do here. If this device has another connection mode (a wireless adapter, Bluetooth), try that."
	case core.HealthNoPermission:
		return "fix the permission as shown above, replug the device, then press r."
	case core.HealthDisconnected:
		return "reconnect the device and press r."
	case core.HealthSilent:
		return "open diagnostics and save the report; it shows exactly what was sent."
	case core.HealthError:
		return "press r to try again."
	case core.HealthUnknown:
		return "wait for the first check to finish."
	}
	if m.devices.pane == paneDeviceList {
		return "press enter to choose an action."
	}
	if device.SupportTier != protocol.TierFull {
		return "run diagnostics and save the report. That report is what moves this device toward full support."
	}
	return "run diagnostics to see which checks it answers."
}

// tierNote explains, for a device that is not fully supported, why most
// actions are held back. It replaces the tier jargon with what it means.
func tierNote(device core.AppDevice) string {
	switch device.SupportTier {
	case protocol.TierCandidateReadOnly:
		return "This model is recognised but has not been confirmed on real hardware yet, so OpenBitdo only reads from it. " +
			"Some checks may fail for that reason alone; it is not a fault in the device."
	case protocol.TierDetectOnly:
		return "This model can be identified, but nothing is known yet about how to talk to it."
	}
	return ""
}
