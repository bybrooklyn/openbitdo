package tui

import (
	"sort"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	actionSettings
	actionQuit
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
}

func newDevicesState() devicesState {
	return devicesState{pane: paneDeviceList}
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
		names[i] = dev.Name
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

func (m Model) actionsForSelectedDevice() []actionItem {
	device, ok := m.devices.selected()
	if !ok {
		return nil
	}
	items := []actionItem{{label: "Diagnose", kind: actionDiagnose}}

	items = append(items, actionItem{
		label: "Mapping Editor", kind: actionMapping,
		reason: mappingDisabledReason(device, m.mockMode, m.writeLockUntilRestart),
	})
	if device.Capability.SupportsU2ButtonMap && m.mockMode {
		items[len(items)-1].label = "Mapping Preview (mock-only)"
	}
	items = append(items, actionItem{
		label: "Firmware Update", kind: actionFirmware,
		// Risk acknowledgement is collected interactively via modal on
		// trigger, not treated as a static precondition here.
		reason: firmwareDisabledReason(device, m.core.FirmwareEnabled(), true, m.writeLockUntilRestart),
	})
	if device.SupportTier == protocol.TierCandidateReadOnly {
		reason := ""
		switch {
		case m.writeLockUntilRestart:
			reason = "Write locked until restart"
		case !m.advancedMode:
			reason = "Enable advanced mode first"
		case !candidateUnlockFilePresent(m.settingsPath, device.VidPid):
			reason = "Create " + candidateUnlockFilePath(m.settingsPath, device.VidPid) + " with candidate_write_unlock = true first"
		}
		items = append(items, actionItem{label: "Guarded Write Probe", kind: actionGuardedProbe, reason: reason})
	}
	items = append(items, actionItem{label: "Settings", kind: actionSettings})
	items = append(items, actionItem{label: "Quit", kind: actionQuit})
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
			return m, nil
		case "r":
			// Rescanning is always available — including with zero devices
			// connected, since "plug in a controller and see it appear" is
			// the core workflow and there was otherwise no way back into
			// the device list after the one-shot load at startup.
			return m, cmdLoadDevices(m.ctx, m.core)
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
			m.devices.pane = paneDeviceList
			return m, nil
		case "enter":
			return m.triggerDevicesEnter()
		}
	}
	return m, nil
}

func (m Model) updateDeviceFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter, tea.KeyEsc:
		m.devices.filtering = false
		return m, nil
	case tea.KeyBackspace:
		if len(m.devices.filterText) > 0 {
			m.devices.filterText = m.devices.filterText[:len(m.devices.filterText)-1]
		}
	case tea.KeyRunes:
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

func (m Model) deviceVisibleRows() int {
	return max(1, m.height-7)
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
		return m.setNotice(noticeWarning, item.label+": "+item.reason, false)
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
			m.modal = riskAckModal("update this device's firmware", startFirmware)
			return m, nil
		}
		return m.Update(startFirmware)

	case actionGuardedProbe:
		probe := candidateProbeBeginMsg{device: device}
		if !m.acknowledgedRisk {
			m.modal = riskAckModal("run the guarded write/readback probe", probe)
			return m, nil
		}
		return m.Update(probe)

	case actionSettings:
		m.screen = screenSettings
		return m, nil

	case actionQuit:
		m.cancel()
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewDevices(height int) string {
	if calculateLayout(m.width, m.height).mode == layoutCompact {
		return m.viewDevicesCompact(m.width-2, height)
	}
	listWidth := m.width * 2 / 5
	if listWidth < 24 {
		listWidth = 24
	}
	detailWidth := m.width - listWidth - 5

	list := m.viewDeviceList(listWidth, height)
	detail := m.viewDeviceDetail(detailWidth, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, " ", detail)
}

func (m Model) viewDevicesCompact(width, height int) string {
	if m.devices.pane == paneActions {
		return m.viewDevicesCompactActions(width, height)
	}
	var b strings.Builder
	b.WriteString(stylePanelTitle.Render("Status") + "\n")
	if len(m.devices.filtered) == 0 {
		b.WriteString(styleFaint.Render("No controller detected. Press r to rescan, s for settings, or run --mock to preview.") + "\n")
	} else if device, ok := m.devices.selected(); ok {
		b.WriteString(deviceRowLabel(device) + "\n")
		b.WriteString(styleFaint.Render(pidLabel(device.VidPid)+" · "+string(device.ProtocolFamily)) + "\n")
	}

	b.WriteString("\n" + stylePanelTitle.Render("Works now") + "\n")
	if device, ok := m.devices.selected(); ok {
		b.WriteString(styleBody.Render("Safe diagnostics and support reports") + "\n")
		if device.Capability.SupportsJP108DedicatedMap {
			b.WriteString(styleBody.Render("JP108 mapping editor") + "\n")
		}
		if m.mockMode && device.Capability.SupportsU2ButtonMap {
			b.WriteString(styleBody.Render("Mock-only Ultimate2 mapping preview") + "\n")
		}
	} else {
		b.WriteString(styleFaint.Render("Settings and device rescan") + "\n")
	}

	if device, ok := m.devices.selected(); ok {
		blocked := blockedLinesForDevice(device, m.core.FirmwareEnabled(), m.mockMode, m.acknowledgedRisk, m.advancedMode, m.acknowledgedRisk, m.writeLockUntilRestart)
		if len(blocked) > 0 {
			b.WriteString("\n" + stylePanelTitle.Render("Blocked") + "\n")
			for _, line := range blocked {
				b.WriteString(styleFaint.Render(line) + "\n")
			}
		}
	}

	b.WriteString("\n" + stylePanelTitle.Render("Next step") + "\n")
	if len(m.devices.filtered) == 0 {
		b.WriteString(styleBody.Render("Connect an 8BitDo controller, then press r.") + "\n")
	} else if m.devices.pane == paneActions {
		for i, item := range m.actionsForSelectedDevice() {
			label := item.label
			if item.reason != "" {
				label += " (" + item.reason + ")"
			}
			if i == m.devices.actionIdx {
				b.WriteString(styleSelectedRow.Render("› "+label) + "\n")
			} else {
				b.WriteString("  " + styleBody.Render(label) + "\n")
			}
		}
	} else {
		b.WriteString(styleBody.Render("Choose a controller, then enter/right for actions.") + "\n")
	}

	return renderBoundedPanelWithStyle(stylePanelActive, width, height-2, b.String())
}

func (m Model) viewDevicesCompactActions(width, height int) string {
	var b strings.Builder
	device, ok := m.devices.selected()
	if !ok {
		b.WriteString(stylePanelTitle.Render("Actions") + "\n")
		b.WriteString(styleFaint.Render("No selected device."))
		return renderBoundedPanelWithStyle(stylePanelActive, width, height-2, b.String())
	}

	b.WriteString(stylePanelTitle.Render("Actions: "+device.Name) + "\n")
	b.WriteString(styleFaint.Render(pidLabel(device.VidPid)+" · "+string(device.ProtocolFamily)) + "\n")
	items := m.actionsForSelectedDevice()
	for i, item := range items {
		label := item.label
		if item.reason != "" {
			label += " (" + item.reason + ")"
		}
		if i == m.devices.actionIdx {
			b.WriteString(styleSelectedRow.Render("› "+label) + "\n")
			if item.reason != "" {
				b.WriteString(styleWarning.Render("  "+item.reason) + "\n")
			}
		} else {
			b.WriteString("  " + styleBody.Render(label) + "\n")
		}
	}
	b.WriteString("\n" + styleFaint.Render("left/esc returns to dashboard summary"))
	return renderBoundedPanelWithStyle(stylePanelActive, width, height-2, b.String())
}

func (m Model) viewDeviceList(width, height int) string {
	title := "Devices"
	if m.devices.filtering || m.devices.filterText != "" {
		title = "Devices  " + styleFaint.Render("/"+m.devices.filterText)
	}

	var b strings.Builder
	if len(m.devices.filtered) == 0 {
		b.WriteString(styleFaint.Render("No devices found."))
		if m.mockMode {
			b.WriteString("\n" + styleFaint.Render("(mock mode should list 3 devices — check core.ListDevices)"))
		} else {
			b.WriteString("\n" + styleFaint.Render("Connect an 8BitDo device, or run with --mock to preview."))
			b.WriteString("\n" + styleFaint.Render("Press r to rescan once it's plugged in."))
		}
	}
	start, end, more := viewportWindow(len(m.devices.filtered), m.devices.cursor, m.devices.listOffset, m.deviceVisibleRows())
	if more != "" {
		b.WriteString(styleFaint.Render(more) + "\n")
	}
	for i := start; i < end; i++ {
		d := m.devices.filtered[i]
		row := deviceRowLabel(d)
		if i == m.devices.cursor {
			row = styleSelectedRow.Render(" " + row + " ")
		} else {
			row = " " + row
		}
		b.WriteString(row + "\n")
	}
	if more != "" {
		b.WriteString(styleFaint.Render(more) + "\n")
	}

	content := stylePanelTitle.Render(title) + "\n\n" + b.String()
	if m.devices.pane == paneDeviceList {
		return renderBoundedPanelWithStyle(stylePanelActive, width, height-2, content)
	}
	return renderBoundedPanel(width, height-2, content)
}

func deviceRowLabel(d core.AppDevice) string {
	kind := "detect"
	switch d.SupportTier {
	case protocol.TierFull:
		kind = "full"
	case protocol.TierCandidateReadOnly:
		kind = "candidate"
	}
	return supportTierBadge(d.Name, kind) + "  " + styleFaint.Render(pidLabel(d.VidPid))
}

func (m Model) viewDeviceDetail(width, height int) string {
	device, ok := m.devices.selected()
	if !ok {
		return renderBoundedPanel(width, height-2, styleFaint.Render("Select a device to see details."))
	}

	var b strings.Builder
	b.WriteString(stylePanelTitle.Render(device.Name) + "\n")
	b.WriteString(styleFaint.Render(pidLabel(device.VidPid)+" · "+string(device.ProtocolFamily)) + "\n\n")

	// Blocks joined by exactly one blank line each, rather than each piece
	// manually tracking its own leading/trailing newlines — the latter
	// previously produced a double blank line before "Actions" whenever the
	// Blocked/candidate-tier sections had already ended with their own
	// trailing blank (both applied on top of Actions' own leading blank).
	var blocks []string
	blocks = append(blocks, styleBody.Render("Status: "+string(device.SupportStatus()))+"\n"+styleBody.Render("Evidence: "+string(device.Evidence)))

	if blocked := blockedLinesForDevice(device, m.core.FirmwareEnabled(), m.mockMode, m.acknowledgedRisk, m.advancedMode, m.acknowledgedRisk, m.writeLockUntilRestart); len(blocked) > 0 {
		var blockedText strings.Builder
		blockedText.WriteString(styleWarning.Render("Blocked:"))
		for _, line := range blocked {
			blockedText.WriteString("\n" + styleFaint.Render("  · "+line))
		}
		blocks = append(blocks, blockedText.String())
	}

	var actions strings.Builder
	actions.WriteString(stylePanelTitle.Render("Actions"))
	items := m.actionsForSelectedDevice()
	for i, item := range items {
		label := item.label
		style := styleBody
		if item.reason != "" {
			label += "  (" + item.reason + ")"
		}
		if m.devices.pane == paneActions && i == m.devices.actionIdx {
			style = styleSelectedRow
			label = "› " + label
		} else if item.reason != "" {
			style = styleFaint
		} else {
			label = "  " + label
		}
		actions.WriteString("\n" + style.Render(label))
	}
	blocks = append(blocks, actions.String())

	if device.SupportTier != protocol.TierFull {
		blocks = append(blocks, candidateTierExplanation(device))
	}

	b.WriteString(strings.Join(blocks, "\n\n"))

	if m.devices.pane == paneActions {
		return renderBoundedPanelWithStyle(stylePanelActive, width, height-2, b.String())
	}
	return renderBoundedPanel(width, height-2, b.String())
}

// candidateTierExplanation is the GitHub-issue-#15 fix: candidate-readonly
// and detect-only devices get a plain-language explanation of what their
// tier means and how to help, instead of leaving users staring at a wall of
// failed diagnostic checks wondering whether the tool is broken.
func candidateTierExplanation(device core.AppDevice) string {
	var b strings.Builder
	switch device.SupportTier {
	case protocol.TierCandidateReadOnly:
		b.WriteString(styleWarning.Render("Not hardware-confirmed yet.") + "\n")
		b.WriteString(styleFaint.Render(
			"This PID is recognized from static analysis only — nobody has run\n" +
				"confirmed request/response traces against real hardware for it yet.\n" +
				"That's why some \"Confirmed\" diagnostic checks below may show as\n" +
				"failed: their validators are tuned to hardware-confirmed devices,\n" +
				"and this one hasn't gone through that process. It's not a bug in\n" +
				"the tool — it's exactly what \"candidate-readonly\" means.",
		))
		return styleWarningBlock.Render(b.String())
	case protocol.TierDetectOnly:
		b.WriteString(styleFaint.Render("This PID is only known well enough to identify it — no diagnostic\nevidence exists yet."))
		return styleAccentBlock.Render(b.String())
	}
	return b.String()
}
