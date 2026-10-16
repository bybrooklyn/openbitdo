package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

type actionKind int

const (
	actionDiagnose actionKind = iota
	actionSaveReport
	actionMapping
	actionFirmware
	actionGuardedProbe
)

// actionItem is one thing that can be done with the selected device. An item
// with a reason cannot be done right now; the reason says why, in a few
// words that fit beside the label.
type actionItem struct {
	label  string
	kind   actionKind
	note   string // shown beside an available action (e.g. "5 of 12 answered")
	reason string // "" means available
}

type devicesState struct {
	devices    []core.AppDevice
	filtered   []core.AppDevice
	cursor     int
	listOffset int
	filterText string
	filtering  bool
	// actionIdx is the cursor in the Overview tab's "You can" list.
	actionIdx int

	// loading is true while a scan is in flight; scanned once any scan has
	// finished, so "no devices" is only claimed after actually looking.
	loading bool
	scanned bool
	// announceScan makes the next finished scan report what it found: set
	// when the user asked for it, not for background reloads.
	announceScan bool
}

func newDevicesState() devicesState {
	return devicesState{loading: true}
}

// sameDevice reports whether a and b are the same physical device.
func sameDevice(a, b core.AppDevice) bool {
	return a.VidPid == b.VidPid && a.Serial == b.Serial
}

// reselect moves the cursor back onto want after the list changed, so a
// reload never silently retargets the selection to a different device. It
// reports whether the selected device is still the one that was selected.
func (d *devicesState) reselect(want core.AppDevice, had bool) bool {
	if had {
		for i, dev := range d.filtered {
			if sameDevice(dev, want) {
				d.cursor = i
				return true
			}
		}
	}
	d.cursor = clampInt(d.cursor, 0, len(d.filtered)-1)
	d.actionIdx = 0
	return !had
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

// sortDevicesByTier groups the list by support tier — a stable sort so
// devices within the same tier keep their enumeration order.
func sortDevicesByTier(devices []core.AppDevice) []core.AppDevice {
	out := append([]core.AppDevice(nil), devices...)
	sort.SliceStable(out, func(i, j int) bool {
		// A device OpenBitdo cannot talk to goes below the ones it can,
		// whatever its tier: the top of the list is what is selected first.
		if a, b := out[i].ConfigChannel == core.ChannelAbsent, out[j].ConfigChannel == core.ChannelAbsent; a != b {
			return b
		}
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

// verdict is the one-glance answer to "is this device OK?": a word, a
// sentence, and a glyph and colour that agree with them.
type verdict struct {
	glyph    string
	style    lipgloss.Style
	word     string // "Working", "Limited", "Can't connect", ...
	sentence string
}

// verdictFor turns what is known about a device into a verdict. canChange
// says whether any setting on it can be changed right now; a device that
// answers but can only be read is "Limited", not "Working".
func verdictFor(h core.DeviceHealth, canChange bool, worksAs core.DeviceRole) verdict {
	if h.State == core.HealthNoChannel {
		// No way to configure it in this mode, but it is very likely doing
		// its job. Lead with that; "can't connect" would be wrong.
		switch worksAs {
		case core.RoleGamepad:
			return verdict{IconTierCandidate, styleBadgeCandidate, "Playing",
				"Working as a controller. Its settings can't be reached in this mode."}
		case core.RoleKeyboard:
			return verdict{IconTierCandidate, styleBadgeCandidate, "Typing",
				"Working as a keyboard. OpenBitdo doesn't know how to reach its settings yet."}
		}
	}
	switch h.State {
	case core.HealthResponding:
		if canChange {
			return verdict{IconTierFull, styleBadgeFull, "Working", "Connected and ready."}
		}
		return verdict{IconTierCandidate, styleBadgeCandidate, "Limited",
			"Connected. OpenBitdo can read from it, but can't change its settings yet."}
	case core.HealthSilent:
		return verdict{IconTierDetect, styleBadgeDetect, "Not answering",
			"Connected, but it isn't replying to OpenBitdo."}
	case core.HealthControllerOff:
		return verdict{IconTierDetect, styleBadgeDetect, "Controller off",
			"The receiver is plugged in, but the controller is off or asleep. Turn it on, then press s."}
	case core.HealthNoChannel:
		return verdict{IconTierDetect, styleBadgeDetect, "Can't connect",
			"Plugged in, but not in a mode OpenBitdo can talk to."}
	case core.HealthNoPermission:
		return verdict{IconFail, styleDanger, "No access",
			"Your user account isn't allowed to open this device."}
	case core.HealthDisconnected:
		return verdict{IconTierDetect, styleBadgeDetect, "Unplugged", "It was disconnected."}
	case core.HealthError:
		return verdict{IconWarn, styleWarning, "Check failed", "The last check could not run."}
	default:
		return verdict{IconTierCandidate, styleBadgeCandidate, "Checking", "Checking what this device answers…"}
	}
}

// deviceVerdict is the verdict for one listed device.
func (m Model) deviceVerdict(device core.AppDevice) verdict {
	health := m.core.Health(device)
	canChange := false
	for _, item := range m.actionsFor(device) {
		if item.reason == "" && (item.kind == actionMapping || item.kind == actionFirmware) {
			canChange = true
		}
	}
	return verdictFor(health, canChange, device.WorksAs)
}

// noChannelAdvice says what would let OpenBitdo configure a device that has
// no configuration interface right now.
func noChannelAdvice(device core.AppDevice) string {
	switch device.WorksAs {
	case core.RoleGamepad:
		return "Controllers keep their settings behind a separate mode. If yours has a mode switch or a wireless adapter, try the other position or connection and it will show up here again."
	case core.RoleKeyboard:
		return "Its buttons and keys work as normal. Changing what they do needs commands OpenBitdo hasn't learned for this keyboard yet."
	}
	return "If it has another connection (a wireless adapter, Bluetooth), try that one."
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

// unreachableReason says, for a device OpenBitdo cannot talk to, why nothing
// can be done with it; "" when it can be reached (or nothing rules that out).
func unreachableReason(health core.DeviceHealth) string {
	switch health.State {
	case core.HealthNoChannel:
		return "it isn't in a mode OpenBitdo can talk to"
	case core.HealthNoPermission:
		return "your account can't open the device"
	case core.HealthSilent:
		return "the device isn't answering"
	case core.HealthControllerOff:
		return "the controller is off"
	case core.HealthDisconnected:
		return "the device is unplugged"
	}
	return ""
}

// friendlyReason shortens a gating reason for the Overview list. The full
// reason is shown on the tab the action belongs to.
func friendlyReason(reason string) string {
	switch reason {
	case "button-map framing not hardware-confirmed":
		return "waiting on hardware testing"
	case "Deferred in 0.0.3":
		return "Deferred in 0.0.3: not part of this release"
	case "Write locked until restart":
		return "writes are locked until you restart"
	}
	return reason
}

func (m Model) actionsForSelectedDevice() []actionItem {
	device, ok := m.devices.selected()
	if !ok {
		return nil
	}
	return m.actionsFor(device)
}

// actionsFor lists everything that could be done with device, available or
// not. The Overview tab splits it into "You can" and "Not yet".
func (m Model) actionsFor(device core.AppDevice) []actionItem {
	health := m.core.Health(device)
	unreachable := unreachableReason(health)

	check := actionItem{label: "Check the connection", kind: actionDiagnose}
	switch health.State {
	case core.HealthNoChannel:
		// Nothing to send a check through.
		check.reason = unreachable
	case core.HealthResponding, core.HealthSilent:
		check.note = fmt.Sprintf("%d of %d answered", health.Answered, health.Total)
	case core.HealthControllerOff:
		check.note = "the receiver answers; the controller doesn't"
	}
	items := []actionItem{check}

	if health.State == core.HealthResponding || health.State == core.HealthSilent {
		items = append(items, actionItem{label: "Save a report", kind: actionSaveReport, note: "for a bug report"})
	}

	mapping := actionItem{
		label: "Remap buttons", kind: actionMapping,
		reason: mappingDisabledReason(device, m.mockMode, m.advancedMode, m.writeLockUntilRestart),
	}
	if device.Capability.SupportsJP108DedicatedMap {
		mapping.label = "Remap keys"
	}
	if mapping.reason == "" && unreachable != "" {
		mapping.reason = unreachable
	}
	items = append(items, mapping)

	// Risk acknowledgement is collected interactively via modal on trigger,
	// not treated as a static precondition here.
	items = append(items, actionItem{
		label: "Update firmware", kind: actionFirmware,
		reason: firmwareDisabledReason(device, m.core.FirmwareEnabled(), true, m.writeLockUntilRestart),
	})

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
		items = append(items, actionItem{label: "Test a write", kind: actionGuardedProbe, note: "guarded probe", reason: reason})
	}
	return items
}

// availableActions is the "You can" list: what the Overview cursor moves over.
func (m Model) availableActions() []actionItem {
	var out []actionItem
	for _, item := range m.actionsForSelectedDevice() {
		if item.reason == "" {
			out = append(out, item)
		}
	}
	return out
}

func (m Model) updateDevices(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.devices.filtering {
		return m.updateDeviceFilterInput(key)
	}
	switch key.String() {
	case "r":
		return m.rescanDevices()
	case "up", "k":
		if m.devices.actionIdx > 0 {
			m.devices.actionIdx--
		}
	case "down", "j":
		if m.devices.actionIdx < len(m.availableActions())-1 {
			m.devices.actionIdx++
		}
	case "esc":
		if m.devices.filterText != "" {
			m.devices.filterText = ""
			m.devices.applyFilter()
			m.devices.cursor = clampInt(m.devices.cursor, 0, len(m.devices.filtered)-1)
		}
	case "enter":
		return m.triggerDevicesEnter()
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
	m.core.ForgetSharedProducts()
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
	m.devices.actionIdx = 0
	return m, nil
}

// triggerDevicesEnter runs the Overview action under the cursor.
func (m Model) triggerDevicesEnter() (tea.Model, tea.Cmd) {
	items := m.availableActions()
	if m.devices.actionIdx >= len(items) {
		return m, nil
	}
	device, _ := m.devices.selected()

	switch items[m.devices.actionIdx].kind {
	case actionDiagnose:
		return m.navigate(screenDiagnostics, m.devices.cursor)

	case actionSaveReport:
		entry, ok := m.core.CachedDiag(device)
		if !ok {
			return m, nil
		}
		message := m.core.BeginnerDiagSummary(device, entry.Result)
		return m, cmdSaveReport(ReportSaveAlways, m.settingsPath, "diag-probe", &device, "saved-on-request", message, &entry.Result, nil, nil)

	case actionMapping:
		return m.navigate(screenMapping, m.devices.cursor)

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

// devicePanel is laid-out content: its lines, and for each line which row a
// click on it means, or -1. Rendering and mouse hit-testing both read this,
// so a click can never land on a different row than the one drawn there.
type devicePanel struct {
	area   rect // where the panel's first content line sits
	width  int
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

func (p *devicePanel) addWrapped(owner int, style lipgloss.Style, text string, width int) {
	p.add(owner, strings.Split(wrapStyled(style, text, width), "\n")...)
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
	lines, _ := p.window()
	return renderBoundedPanel(p.width, max(1, p.area.h), strings.Join(lines, "\n"))
}

// ownerAt maps a click to the row drawn there.
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

// viewDevices is the Overview tab: the selected device's verdict, what can
// be done with it now, and what cannot be done yet.
func (m Model) viewDevices(height int) string {
	return m.overviewPanel(height).render()
}

func (m Model) overviewPanel(height int) devicePanel {
	panel := devicePanel{width: m.width - 2, keep: -1}
	panel.area = rect{x: 0, y: calculateLayout(m.width, m.height).headerHeight, w: m.width, h: max(1, height-2)}
	text := max(1, m.width-4)

	device, ok := m.devices.selected()
	if !ok {
		switch {
		case !m.devices.scanned:
			panel.add(-1, styleBody.Render("Looking for devices…"))
		case m.devices.filterText != "":
			panel.addWrapped(-1, styleBody, fmt.Sprintf("No device matches %q.", m.devices.filterText), text)
			panel.addWrapped(-1, styleFaint, "Press esc to clear the filter.", text)
		default:
			panel.add(-1, stylePanelTitle.Render("No 8BitDo device found"), "")
			panel.addWrapped(-1, styleBody, "Plug one in over USB and it shows up here by itself.", text)
			panel.add(-1, "")
			panel.addWrapped(-1, styleFaint, "Already plugged in? Press r to look again.", text)
			panel.addWrapped(-1, styleFaint, "No hardware? Run openbitdo --mock to look around.", text)
		}
		return panel
	}

	health := m.core.Health(device)
	v := m.deviceVerdict(device)

	// Name on the left, verdict on the right of the same line.
	badge := v.glyph + " " + strings.ToUpper(v.word)
	name := truncate(device.DisplayName, max(4, text-lipgloss.Width(badge)-2))
	gap := strings.Repeat(" ", max(2, text-lipgloss.Width(name)-lipgloss.Width(badge)))
	panel.add(-1, stylePanelTitle.Render(name)+gap+v.style.Render(badge))
	panel.addWrapped(-1, styleBody, v.sentence, text)

	switch health.State {
	case core.HealthNoPermission:
		panel.add(-1, "")
		for _, line := range permissionFixLines() {
			panel.addWrapped(-1, styleFaint, line, text)
		}
	case core.HealthNoChannel:
		panel.add(-1, "")
		panel.addWrapped(-1, styleFaint, noChannelAdvice(device), text)
	case core.HealthError:
		if health.Err != nil {
			panel.addWrapped(-1, styleFaint, health.Err.Error(), text)
		}
	}

	var can, notYet []actionItem
	for _, item := range m.actionsFor(device) {
		if item.reason == "" {
			can = append(can, item)
		} else {
			notYet = append(notYet, item)
		}
	}

	// Labels share one column so the notes beside them line up. A note is
	// only an aside: where the pane is too narrow for it, it is left out
	// rather than squeezing the label.
	labelWidth := 0
	for _, item := range can {
		labelWidth = max(labelWidth, lipgloss.Width(item.label))
	}
	labelWidth = min(labelWidth, max(8, text-2))
	noteFits := func(note string) bool {
		return note != "" && 2+labelWidth+3+lipgloss.Width(note) <= text
	}

	if len(can) > 0 {
		panel.keep = m.devices.actionIdx
		panel.add(-1, "", styleSection.Render("You can"))
		for i, item := range can {
			label := fmt.Sprintf("%-*s", labelWidth, truncate(item.label, labelWidth))
			if i == m.devices.actionIdx {
				// One Render call over plain text: a nested style's reset
				// would cut the highlight short.
				line := "› " + label
				if noteFits(item.note) {
					line += "   " + item.note
				}
				panel.add(i, styleSelectedRow.Render(line+" "))
				continue
			}
			line := "  " + styleBody.Render(label)
			if noteFits(item.note) {
				line += "   " + styleFaint.Render(item.note)
			}
			panel.add(i, line)
		}
	}

	if unreachableReason(health) != "" {
		// Every action is out for the same reason, already given above.
		// Listing each one again would only bury it.
		if len(can) == 0 {
			panel.add(-1, "")
			panel.addWrapped(-1, styleFaint, "Until then OpenBitdo can show it, but not change it.", text)
		}
		notYet = nil
	}

	if len(notYet) > 0 {
		panel.add(-1, "", styleSection.Render("Not yet"))
		for _, item := range notYet {
			panel.add(-1, "  "+styleFaint.Render(truncate(item.label, text-2)))
			for _, line := range wrapText(friendlyReason(item.reason), max(1, text-6)) {
				panel.add(-1, "      "+styleFaint.Render(line))
			}
		}
	}

	if note := tierNote(device); note != "" && health.Reachable() {
		panel.add(-1, "")
		panel.addWrapped(-1, styleFaint, note, text)
	}
	return panel
}

// tierNote explains, for a device that is not fully supported, why most
// actions are held back. It replaces the tier jargon with what it means.
func tierNote(device core.AppDevice) string {
	switch device.SupportTier {
	case protocol.TierCandidateReadOnly:
		return "This model is recognised but hasn't been confirmed on real hardware yet, so OpenBitdo only reads from it. " +
			"Some checks may fail for that reason alone; it is not a fault in the device."
	case protocol.TierDetectOnly:
		return "This model can be identified, but nothing is known yet about how to talk to it."
	}
	return ""
}
