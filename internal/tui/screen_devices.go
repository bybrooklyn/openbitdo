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
		reason: mappingDisabledReason(device, m.writeLockUntilRestart),
	})
	items = append(items, actionItem{
		label: "Firmware Update", kind: actionFirmware,
		// Risk acknowledgement is collected interactively via modal on
		// trigger, not treated as a static precondition here.
		reason: firmwareDisabledReason(device, true, m.writeLockUntilRestart),
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
		case "up", "k":
			if m.devices.pane == paneDeviceList {
				if m.devices.cursor > 0 {
					m.devices.cursor--
				}
			} else if m.devices.actionIdx > 0 {
				m.devices.actionIdx--
			}
			return m, nil
		case "down", "j":
			if m.devices.pane == paneDeviceList {
				if m.devices.cursor < len(m.devices.filtered)-1 {
					m.devices.cursor++
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
	return m, nil
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
		return m, nil
	}
	device, _ := m.devices.selected()

	switch item.kind {
	case actionDiagnose:
		m.screen = screenDiagnostics
		m.diag = newDiagnosticsState()
		m.diag.device = device
		m.diag.loading = true
		return m, cmdRunDiagnostics(m.ctx, m.core, device.VidPid)

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
	listWidth := m.width * 2 / 5
	if listWidth < 24 {
		listWidth = 24
	}
	detailWidth := m.width - listWidth - 5

	list := m.viewDeviceList(listWidth, height)
	detail := m.viewDeviceDetail(detailWidth, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, " ", detail)
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
	for i, d := range m.devices.filtered {
		row := deviceRowLabel(d)
		if i == m.devices.cursor {
			row = styleSelectedRow.Render(" " + row + " ")
		} else {
			row = " " + row
		}
		b.WriteString(row + "\n")
	}

	style := stylePanel
	if m.devices.pane == paneDeviceList {
		style = stylePanelActive
	}
	return style.Width(width).Height(height - 2).Render(stylePanelTitle.Render(title) + "\n\n" + b.String())
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
		return stylePanel.Width(width).Height(height - 2).Render(styleFaint.Render("Select a device to see details."))
	}

	var b strings.Builder
	b.WriteString(stylePanelTitle.Render(device.Name) + "\n")
	b.WriteString(styleFaint.Render(pidLabel(device.VidPid)+" · "+string(device.ProtocolFamily)) + "\n\n")
	b.WriteString("Status: " + string(device.SupportStatus()) + "\n")
	b.WriteString("Evidence: " + string(device.Evidence) + "\n\n")

	if blocked := blockedLinesForDevice(device, m.acknowledgedRisk, m.advancedMode, m.acknowledgedRisk, m.writeLockUntilRestart); len(blocked) > 0 {
		b.WriteString(styleWarning.Render("Blocked:") + "\n")
		for _, line := range blocked {
			b.WriteString(styleFaint.Render("  · "+line) + "\n")
		}
		b.WriteString("\n")
	}

	if device.SupportTier != protocol.TierFull {
		b.WriteString(candidateTierExplanation(device))
		b.WriteString("\n")
	}

	b.WriteString("\n" + stylePanelTitle.Render("Actions") + "\n")
	items := m.actionsForSelectedDevice()
	for i, item := range items {
		label := item.label
		style := styleBody
		if item.reason != "" {
			style = styleFaint
			label += "  (" + item.reason + ")"
		} else if m.devices.pane == paneActions && i == m.devices.actionIdx {
			style = styleAccent
			label = "› " + label
		} else {
			label = "  " + label
		}
		b.WriteString(style.Render(label) + "\n")
	}

	style := stylePanel
	if m.devices.pane == paneActions {
		style = stylePanelActive
	}
	return style.Width(width).Height(height - 2).Render(b.String())
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
