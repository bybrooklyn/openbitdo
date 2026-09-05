// Package tui is OpenBitdo's Bubbletea terminal UI. It is a genuine
// redesign, not a port of the prior Rust TUI's six-screen layout — the hard
// constraints carried over are the safety semantics (support-tier gating,
// unsafe/experimental gating, write-lock/recovery, candidate write-probe
// unlock ceremony) in gatekeeping.go, not any particular screen shape.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type screen int

const (
	screenDevices screen = iota
	screenDiagnostics
	screenMapping
	screenFirmware
	screenSettings
	screenRecovery
)

// BuildInfo is displayed on the Settings screen.
type BuildInfo struct {
	AppVersion string
	Commit     string
	BuildDate  string
	Platform   string
	Dirty      string
}

// Options configures a launched Model.
type Options struct {
	Build        BuildInfo
	Settings     Settings
	SettingsPath string
	MockMode     bool
	NavNotes     []string
}

// Model is the root Bubbletea model. Screen-specific state lives in the
// *State structs below, all embedded here — Bubbletea's single-model
// convention means one struct holds everything, but each screen's own file
// (screen_*.go) only ever touches its own slice of it.
type Model struct {
	ctx    context.Context
	cancel context.CancelFunc
	core   *core.OpenBitdoCore

	navEvents <-chan input.NavEvent
	navNotes  []string

	width, height int
	screen        screen
	prevScreen    screen // where Recovery returns focus once it can

	build        BuildInfo
	settings     Settings
	settingsPath string
	mockMode     bool

	advancedMode          bool
	acknowledgedRisk      bool // brick-risk ack: granted once per session
	writeLockUntilRestart bool
	recoveryReason        string
	recoveryHasBackup     bool
	recoveryBackupID      core.ConfigBackupID
	recoveryRestoreDone   bool
	recoveryRestoreErr    error

	modal modal

	statusLine string
	err        error
	notice     noticeState
	nextNotice int

	devices            devicesState
	diag               diagnosticsState
	mapping            mappingState
	fw                 firmwareState
	settingsCursor     int
	settingsInfoOffset int
}

type noticeLevel int

const (
	noticeNone noticeLevel = iota
	noticeInfo
	noticeSuccess
	noticeWarning
	noticeError
)

type noticeState struct {
	id        int
	level     noticeLevel
	message   string
	transient bool
}

// NewModel constructs the root model. ctx is the app's root context,
// cancelled by cancel on quit.
func NewModel(ctx context.Context, cancel context.CancelFunc, c *core.OpenBitdoCore, nav input.StartResult, opts Options) Model {
	return Model{
		ctx: ctx, cancel: cancel, core: c,
		navEvents: nav.Events, navNotes: nav.Notes,
		screen: screenDevices,
		build:  opts.Build, settings: opts.Settings, settingsPath: opts.SettingsPath, mockMode: opts.MockMode,
		advancedMode: opts.Settings.AdvancedMode,
		devices:      newDevicesState(),
		diag:         newDiagnosticsState(),
		mapping:      newMappingState(),
		fw:           newFirmwareState(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(cmdLoadDevices(m.ctx, m.core), cmdListenNav(m.navEvents), tea.EnterAltScreen)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case noticeExpiredMsg:
		if m.notice.transient && m.notice.id == msg.id {
			m.notice = noticeState{}
			m.statusLine = ""
		}
		return m, nil

	case tea.MouseMsg:
		if m.modal.active {
			return m.updateModalMouse(msg)
		}
		return m.routeMouse(msg)

	case discardMappingMsg:
		m.modal = modal{}
		switch msg.action {
		case discardActionBack:
			m.mapping = newMappingState()
			m.screen = screenDevices
			return m, nil
		case discardActionQuit:
			m.cancel()
			return m, tea.Quit
		case discardActionLoadSlot:
			return m.loadPreviewedSlotIntoDraft()
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancel()
			return m, tea.Quit
		}
		if msg.String() == "x" && m.notice.level >= noticeWarning {
			m.notice = noticeState{}
			m.err = nil
			m.statusLine = ""
			return m, nil
		}
		if m.modal.active {
			return m.updateModalKey(msg)
		}
		if msg.String() == "q" {
			if m.screen == screenMapping && m.mapping.dirty() {
				m.modal = discardMappingModal(discardActionQuit)
				return m, nil
			}
			m.cancel()
			return m, tea.Quit
		}
		if msg.String() == "?" {
			m.modal = helpModal(m.screenHelp())
			return m, nil
		}

	case navEventMsg:
		cmd := cmdListenNav(m.navEvents)
		if msg.event.Kind == input.EventDeviceConnected || msg.event.Kind == input.EventDeviceDisconnected {
			return m.handleHotplugEvent(msg.event, cmd)
		}
		if m.modal.active {
			navMsg := navToKeyMsg(msg.event)
			if navMsg != nil {
				updated, modalCmd := m.updateModalKey(*navMsg)
				return updated, tea.Batch(cmd, modalCmd)
			}
			return m, cmd
		}
		if navMsg := navToKeyMsg(msg.event); navMsg != nil {
			updated, screenCmd := m.route(*navMsg)
			return updated, tea.Batch(cmd, screenCmd)
		}
		return m, cmd

	case navClosedMsg:
		return m, nil

	case devicesLoadedMsg:
		return m.handleDevicesLoaded(msg)

	case reportSavedMsg:
		if msg.err == nil && msg.path != "" {
			return m.setNotice(noticeSuccess, "Report saved: "+msg.path, true)
		}
		if msg.err != nil {
			return m.setNotice(noticeError, "report save failed: "+msg.err.Error(), false)
		}
		return m, nil

	case firmwareBeginMsg:
		m.acknowledgedRisk = true
		m.screen = screenFirmware
		m.fw = newFirmwareState()
		m.fw.device = msg.device
		m.fw.stage = fwStageDownloading
		return m, cmdDownloadFirmware(m.ctx, m.core, msg.device.VidPid)

	case candidateProbeBeginMsg:
		m.acknowledgedRisk = true
		device := msg.device
		policy := core.RuntimeUnlockPolicy{
			AdvancedMode: m.advancedMode, AcknowledgedRisk: true,
			UnlockFilePresent: candidateUnlockFilePresent(m.settingsPath, device.VidPid), UnlockFilePath: candidateUnlockFilePath(m.settingsPath, device.VidPid),
		}
		m, noticeCmd := m.setNotice(noticeInfo, "Running guarded write probe…", true)
		return m, tea.Batch(noticeCmd, cmdCandidateProbe(m.ctx, m.core, device, policy))

	case candidateProbeResultMsg:
		if msg.err != nil {
			return m.setNotice(noticeError, msg.err.Error(), false)
		}
		if msg.report.WriteLockRequired {
			m.writeLockUntilRestart = true
			m.recoveryReason = "The guarded write probe failed: " + msg.report.Message
			m.recoveryHasBackup = false
		}
		status := "ok"
		if !msg.report.Allowed || !msg.report.ReadbackVerified {
			status = "attention"
		}
		device := msg.device
		level := noticeSuccess
		if status == "attention" {
			level = noticeWarning
		}
		m, noticeCmd := m.setNotice(level, msg.report.Message, level == noticeSuccess)
		return m, tea.Batch(noticeCmd, cmdSaveReport(m.settings.ReportSaveMode, m.settingsPath, "candidate-write-probe", &device, status, msg.report.Message, nil, nil, &msg.report))

	case restoreBackupResultMsg:
		m.recoveryRestoreDone = msg.err == nil
		m.recoveryRestoreErr = msg.err
		return m, nil

	case autoDiagResultMsg:
		// Only take over the live view if Diagnostics is actually still
		// waiting on a probe for this exact device — otherwise this is
		// either a different device's background result (silently cached
		// for later) or a duplicate of a result the user's own "r" rerun /
		// cache hit already displayed (m.diag.loading is already false by
		// then), which must not clobber it.
		if m.screen == screenDiagnostics && m.diag.loading &&
			m.diag.device.VidPid == msg.device.VidPid && m.diag.device.Serial == msg.device.Serial {
			m.diag.loading = false
			m.diag.err = msg.err
			m.diag.result = msg.result
			m.diag.ranAt = msg.ranAt
		}
		return m, nil
	}

	return m.route(msg)
}

func (m Model) routeMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.routeMouseWheel(-3)
	case tea.MouseButtonWheelDown:
		return m.routeMouseWheel(3)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		switch m.screen {
		case screenDevices:
			return m.clickDevices(msg)
		case screenDiagnostics:
			return m.clickDiagnostics(msg)
		case screenMapping:
			return m.clickMapping(msg)
		case screenSettings:
			return m.clickSettings(msg)
		}
	}
	return m, nil
}

func (m Model) routeMouseWheel(delta int) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenDevices:
		if m.devices.pane == paneActions {
			items := m.actionsForSelectedDevice()
			m.devices.actionIdx = clampInt(m.devices.actionIdx+delta, 0, len(items)-1)
		} else {
			m.devices.cursor = clampInt(m.devices.cursor+delta, 0, len(m.devices.filtered)-1)
			m.ensureDeviceCursorVisible()
		}
	case screenDiagnostics:
		if m.diag.showSupportRequest {
			bodyLines := strings.Split(supportRequestBody(m.diag.device, m.diag.result), "\n")
			m.diag.supportOffset = clampInt(m.diag.supportOffset+delta, 0, len(bodyLines)-1)
		} else {
			m.diag.cursor = clampInt(m.diag.cursor+delta, 0, len(m.diag.visibleChecks())-1)
			m.ensureDiagnosticsCursorVisible()
		}
	case screenMapping:
		m.mapping.cursor = clampInt(m.mapping.cursor+delta, 0, m.mapping.rowCount()-1)
		m.ensureMappingCursorVisible()
	case screenSettings:
		m.settingsInfoOffset = clampInt(
			m.settingsInfoOffset+delta,
			0,
			m.settingsInfoMaxOffset(),
		)
	}
	return m, nil
}

func (m Model) clickDevices(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	mode := calculateLayout(m.width, m.height).mode
	layout := calculateLayout(m.width, m.height)
	bodyRow := msg.Y - layout.headerHeight
	if bodyRow < 0 || bodyRow >= layout.bodyHeight {
		return m, nil
	}
	if mode == layoutCompact {
		if m.devices.pane != paneActions {
			return m, nil
		}
		line := renderedLine(m.viewDevicesCompactActions(m.width-2, layout.bodyHeight), bodyRow)
		for i, item := range m.actionsForSelectedDevice() {
			if strings.Contains(line, item.label) {
				m.devices.actionIdx = i
				return m.triggerDevicesEnter()
			}
		}
		return m, nil
	}

	listWidth := m.width * 2 / 5
	if listWidth < 24 {
		listWidth = 24
	}
	listRendered := m.viewDeviceList(listWidth, layout.bodyHeight)
	listActualWidth := lipgloss.Width(listRendered)
	if msg.X >= 0 && msg.X < listActualWidth {
		line := renderedLine(listRendered, bodyRow)
		start, end, _ := viewportWindow(len(m.devices.filtered), m.devices.cursor, m.devices.listOffset, m.deviceVisibleRows())
		for i := start; i < end; i++ {
			if strings.Contains(line, pidLabel(m.devices.filtered[i].VidPid)) {
				m.devices.cursor = i
				m.ensureDeviceCursorVisible()
				m.devices.pane = paneDeviceList
				return m, nil
			}
		}
		return m, nil
	}

	detailX := listActualWidth + 1
	detailWidth := m.width - listWidth - 5
	detailRendered := m.viewDeviceDetail(detailWidth, layout.bodyHeight)
	if msg.X < detailX || msg.X >= detailX+lipgloss.Width(detailRendered) {
		return m, nil
	}
	line := renderedLine(detailRendered, bodyRow)
	for i, item := range m.actionsForSelectedDevice() {
		if strings.Contains(line, item.label) {
			m.devices.pane = paneActions
			m.devices.actionIdx = i
			return m.triggerDevicesEnter()
		}
	}
	return m, nil
}

func (m Model) clickMapping(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	layout := calculateLayout(m.width, m.height)
	content := rect{x: 0, y: layout.headerHeight, w: m.width, h: layout.bodyHeight}
	if !content.contains(msg.X, msg.Y) {
		return m, nil
	}
	line := renderedLine(m.viewMapping(layout.bodyHeight), msg.Y-layout.headerHeight)
	editableRows := m.mapping.rowCount() - 3
	start, end, _ := viewportWindow(editableRows, m.mapping.cursor, m.mapping.rowOffset, m.mappingVisibleRows())
	for i := start; i < end; i++ {
		if strings.Contains(line, m.mappingRowText(i)) {
			m.mapping.cursor = i
			return m, nil
		}
	}
	actions := []string{"Apply Changes", "Undo Last Edit", "Reset Draft"}
	for i, label := range actions {
		if strings.Contains(line, label) {
			m.mapping.cursor = editableRows + i
			return m.triggerMappingRow()
		}
	}
	return m, nil
}

func renderedLine(rendered string, row int) string {
	lines := strings.Split(ansi.Strip(rendered), "\n")
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

func (m Model) clickDiagnostics(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.diag.showSupportRequest {
		return m, nil
	}
	layout := calculateLayout(m.width, m.height)
	if msg.X < 0 || msg.X >= m.width || msg.Y < layout.headerHeight || msg.Y >= m.height-layout.footerHeight {
		return m, nil
	}
	line := renderedLine(m.viewDiagnostics(layout.bodyHeight), msg.Y-layout.headerHeight)
	for i, check := range m.diag.visibleChecks() {
		if strings.Contains(line, string(check.Command)) {
			m.diag.cursor = i
			m.ensureDiagnosticsCursorVisible()
			return m, nil
		}
	}
	return m, nil
}

func (m Model) clickSettings(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	layout := calculateLayout(m.width, m.height)
	if msg.X < 0 || msg.X >= m.width || msg.Y < layout.headerHeight || msg.Y >= m.height-layout.footerHeight {
		return m, nil
	}
	line := renderedLine(m.viewSettings(layout.bodyHeight), msg.Y-layout.headerHeight)
	labels := []string{"Advanced Mode:", "Report Save Mode:", "Back"}
	for i, label := range labels {
		if strings.Contains(line, label) {
			m.settingsCursor = i
			return m.triggerSettingsRow()
		}
	}
	return m, nil
}

func (m Model) updateModalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	box, confirm, cancel := modalGeometry(m.modal, m.width, m.height)
	if !box.contains(msg.X, msg.Y) {
		return m, nil
	}
	if confirm.contains(msg.X, msg.Y) {
		confirmMsg := m.modal.onConfirm
		m.modal = modal{}
		if confirmMsg == nil {
			return m, nil
		}
		updated, cmd := m.Update(confirmMsg)
		return updated.(Model), cmd
	}
	if cancel.contains(msg.X, msg.Y) {
		m.modal = modal{}
	}
	return m, nil
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m Model) setNotice(level noticeLevel, message string, transient bool) (Model, tea.Cmd) {
	m.nextNotice++
	m.notice = noticeState{id: m.nextNotice, level: level, message: message, transient: transient}
	m.statusLine = ""
	m.err = nil
	switch level {
	case noticeError:
		m.err = fmt.Errorf("%s", message)
	default:
		m.statusLine = message
	}
	if !transient {
		return m, nil
	}
	id := m.notice.id
	return m, tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return noticeExpiredMsg{id: id}
	})
}

// route dispatches a message to the currently active screen's handler,
// after intercepting the write-lock takeover: once tripped, Recovery is the
// only reachable screen until the process restarts.
func (m Model) route(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.writeLockUntilRestart && m.screen != screenRecovery {
		m.prevScreen = m.screen
		m.screen = screenRecovery
	}
	switch m.screen {
	case screenDevices:
		return m.updateDevices(msg)
	case screenDiagnostics:
		return m.updateDiagnostics(msg)
	case screenMapping:
		return m.updateMapping(msg)
	case screenFirmware:
		return m.updateFirmware(msg)
	case screenSettings:
		return m.updateSettings(msg)
	case screenRecovery:
		return m.updateRecovery(msg)
	}
	return m, nil
}

func (m Model) updateModalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "enter", " ":
		confirmMsg := m.modal.onConfirm
		m.modal = modal{}
		if confirmMsg == nil {
			return m, nil
		}
		updated, cmd := m.Update(confirmMsg)
		return updated.(Model), cmd
	case "esc", "b":
		m.modal = modal{}
		return m, nil
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "starting…"
	}
	layout := calculateLayout(m.width, m.height)
	if layout.mode == layoutTooSmall {
		return clampRendered(m.viewTooSmall(), m.width, m.height)
	}

	header := clampRendered(m.viewHeader(), m.width, layout.headerHeight)
	footer := clampRendered(m.viewFooter(), m.width, layout.footerHeight)
	bodyHeight := m.height - layout.headerHeight - layout.footerHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	switch m.screen {
	case screenDevices:
		body = m.viewDevices(bodyHeight)
	case screenDiagnostics:
		body = m.viewDiagnostics(bodyHeight)
	case screenMapping:
		body = m.viewMapping(bodyHeight)
	case screenFirmware:
		body = m.viewFirmware(bodyHeight)
	case screenSettings:
		body = m.viewSettings(bodyHeight)
	case screenRecovery:
		body = m.viewRecovery(bodyHeight)
	}
	body = clampRendered(body, m.width, bodyHeight)

	page := header + "\n" + body + "\n" + footer
	if m.modal.active {
		return clampRendered(m.modal.viewOverlaid(page, m.width, m.height), m.width, m.height)
	}
	return clampRendered(page, m.width, m.height)
}

func (m Model) viewTooSmall() string {
	return strings.Join([]string{
		styleTitle.Render("OpenBitdo"),
		styleWarning.Render(fmt.Sprintf("Terminal too small: %dx%d", m.width, m.height)),
		styleFaint.Render("Required: at least 60x18"),
		styleHelp.Render("q / ctrl+c quit"),
	}, "\n")
}

func (m Model) viewHeader() string {
	title := styleTitle.Render("OpenBitdo")
	if m.mockMode {
		title += "  " + styleWarning.Render("[mock]")
	}
	return lipgloss.NewStyle().Padding(0, 1).BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).BorderForeground(theme.BorderDim).Width(m.width - 2).Render(title)
}

func (m Model) viewFooter() string {
	help := m.screenHelp()
	if calculateLayout(m.width, m.height).mode == layoutCompact {
		help = m.compactScreenHelp()
	}
	if help == "" {
		help = hint("?", "help")
	}
	if m.notice.level >= noticeWarning {
		help = hint("x", "dismiss") + "  " + help
	}
	line := m.composeFooterLine(help)
	if lipgloss.Width(line) > max(1, m.width-2) {
		help = m.compactScreenHelp()
		if m.notice.level >= noticeWarning {
			help = hint("x", "dismiss") + "  " + help
		}
		line = m.composeFooterLine(help)
	}
	if lipgloss.Width(line) > max(1, m.width-2) {
		help = hint("?", "help") + "  " + hint("q", "quit")
		if m.notice.level >= noticeWarning {
			help = hint("x", "dismiss") + "  " + help
		}
		line = m.composeFooterLine(help)
	}
	if lipgloss.Width(line) > max(1, m.width-2) && m.notice.message != "" {
		saved := m.notice.message
		m.notice.message = m.shortNoticeMessage()
		line = m.composeFooterLine(help)
		m.notice.message = saved
	}
	if lipgloss.Width(line) > max(1, m.width-2) {
		line = ansi.Cut(line, 0, max(1, m.width-2))
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(line)
}

func (m Model) composeFooterLine(help string) string {
	line := styleHelp.Render(help)
	if m.notice.message != "" {
		msg := m.notice.message
		switch m.notice.level {
		case noticeSuccess:
			line = stylePositive.Render(msg) + "   " + line
		case noticeWarning:
			line = styleWarning.Render(msg) + "   " + line
		case noticeError:
			line = styleDanger.Render("error: "+msg) + "   " + line
		default:
			line = styleAccent.Render(msg) + "   " + line
		}
	} else {
		if m.statusLine != "" {
			line = stylePositive.Render(m.statusLine) + "   " + line
		}
		if m.err != nil {
			line = styleDanger.Render(fmt.Sprintf("error: %v", m.err)) + "   " + line
		}
	}
	return line
}

func (m Model) shortNoticeMessage() string {
	if strings.Contains(m.notice.message, "Deferred in 0.0.3") {
		return "Deferred in 0.0.3"
	}
	if strings.Contains(m.notice.message, "button-map framing not hardware-confirmed") {
		return "U2 mapping not hardware-confirmed"
	}
	switch m.notice.level {
	case noticeError:
		return "error"
	case noticeWarning:
		return "warning"
	default:
		return "notice"
	}
}

// hint renders one "key label" pair for the footer — the small building
// block every screen's contextual hints are assembled from, matching
// opencode's per-context footer (e.g. "tab agents  ctrl+p commands") rather
// than the same generic line everywhere.
func hint(key, label string) string {
	return styleKey.Render(key) + " " + label
}

func (m Model) compactScreenHelp() string {
	must := hint("?", "help") + "  " + hint("q", "quit")
	switch m.screen {
	case screenDevices:
		return hint("r", "rescan") + "  " + hint("s", "settings") + "  " + must
	case screenDiagnostics:
		return hint("d", "details") + "  " + hint("r", "rerun") + "  " + must
	case screenMapping:
		return hint("←→", "edit") + "  " + hint("esc", "back") + "  " + must
	case screenFirmware:
		return hint("esc", "back") + "  " + must
	case screenSettings:
		return hint("enter", "toggle") + "  " + hint("pg↑↓", "info") + "  " + must
	case screenRecovery:
		if m.recoveryHasBackup && !m.recoveryRestoreDone {
			return hint("r", "restore") + "  " + must
		}
		return must
	default:
		return must
	}
}

// Switch-vs-Xbox button-layout awareness (physical A/B/X/Y swapped) was
// investigated and found not currently feasible: GetMode's response does
// carry a real, parsed "mode" byte (validation.go: parsed["mode"] =
// response[5]), but nothing in this codebase, docs/spec/*, or the dirty-room
// evidence dossiers documents what specific values mean, and
// docs/spec/device_name_catalog.md lists every known PID's ProtocolFamily
// as "DInput" uniformly — no separate Switch-layout protocol family exists
// in the evidence at all. Inventing a mode-value-to-layout mapping without
// hardware-confirmed evidence would be exactly the kind of guessed byte
// layout this project's own conventions deliberately avoid (see
// internal/input/descriptor_other.go's comment on the same principle) — so
// this scopes down to just the connected/not-connected label-hiding below,
// per gamepadConnected. Revisit if a future dossier documents this.

// gamepadConnected reports whether a gamepad's nav stream is currently
// wired up, from internal/input.Start's per-device Notes (the same data
// already surfaced verbatim on the Settings screen — see
// screen_settings.go's "Gamepad Navigation" section). Live, not just a
// startup snapshot: handleHotplugEvent keeps m.navNotes current via
// replacePIDNote as EventDeviceConnected/EventDeviceDisconnected events
// arrive, so a controller plugged in (or unplugged) after launch flips this
// without needing a restart. It's still the right, honest signal for
// "should the footer show controller-specific key hints," since a
// keyboard-only user should never see "A"/"B" glyphs that mean nothing to
// them.
func (m Model) gamepadConnected() bool {
	for _, note := range m.navNotes {
		if strings.Contains(note, "gamepad nav active") {
			return true
		}
	}
	return false
}

func (m Model) screenHelp() string {
	moveLabel, selectLabel, backLabel := "↑↓", "enter", "esc"
	if m.gamepadConnected() {
		moveLabel, selectLabel, backLabel = "↑↓/dpad", "enter/A", "esc/B"
	}
	nav := hint(moveLabel, "move") + "  " + hint(selectLabel, "select") + "  " + hint(backLabel, "back") + "  " + hint("?", "help") + "  " + hint("q", "quit")
	switch m.screen {
	case screenDevices:
		return hint("/", "filter") + "  " + hint("r", "rescan") + "  " + hint("s", "settings") + "  " + hint("right/tab", "actions") + "  " + nav
	case screenDiagnostics:
		extra := hint("tab", "toggle filter") + "  " + hint("d", "details") + "  " + hint("r", "rerun")
		if m.diag.device.SupportTier != protocol.TierFull {
			extra = hint("s", "file support request") + "  " + extra
		}
		return extra + "  " + nav
	case screenMapping:
		extra := hint("←→", "cycle target")
		if m.mapping.kind == core.KindUltimate2 {
			extra += "  " + hint("p", "preview slot")
		}
		return extra + "  " + nav
	case screenFirmware:
		switch m.fw.stage {
		case fwStageReadyToConfirm:
			return hint("enter", "confirm") + "  " + hint("esc", "back") + "  " + hint("?", "help") + "  " + hint("q", "quit")
		case fwStageRunning:
			return hint("c", "cancel") + "  " + hint("?", "help") + "  " + hint("q", "quit")
		default:
			return hint("esc", "back") + "  " + hint("?", "help") + "  " + hint("q", "quit")
		}
	case screenSettings:
		return hint("←→/enter", "toggle") + "  " + hint("pg↑↓/wheel", "info") + "  " + nav
	case screenRecovery:
		extra := hint("q", "quit")
		if m.recoveryHasBackup && !m.recoveryRestoreDone {
			extra = hint("r", "restore backup") + "  " + extra
		}
		return extra + "  " + hint("?", "help")
	default:
		return nav
	}
}

func (m Model) handleDevicesLoaded(msg devicesLoadedMsg) (tea.Model, tea.Cmd) {
	var noticeCmd tea.Cmd
	if msg.err != nil {
		m, noticeCmd = m.setNotice(noticeError, msg.err.Error(), false)
	} else {
		m.err = nil
	}
	m.devices.devices = sortDevicesByTier(msg.devices)
	m.devices.applyFilter()
	if m.devices.cursor >= len(m.devices.filtered) {
		m.devices.cursor = 0
	}
	// Every load (startup, manual "r" rescan, or a hotplug-triggered reload
	// from handleHotplugEvent) auto-diagnoses any device this session hasn't
	// probed yet — core.HasDiagnosed makes this naturally idempotent, so an
	// already-cached device is skipped rather than re-probed on every
	// reload. This is what makes a freshly-connected controller have a
	// "Last run: Xs ago" diagnostic result already waiting by the time the
	// user navigates to it.
	cmds := make([]tea.Cmd, 0, len(msg.devices)+1)
	for _, d := range msg.devices {
		if !m.core.HasDiagnosed(d) {
			cmds = append(cmds, cmdAutoDiagnose(m.ctx, m.core, d))
		}
	}
	if noticeCmd != nil {
		cmds = append(cmds, noticeCmd)
	}
	return m, tea.Batch(cmds...)
}

// handleHotplugEvent reacts to a live device connect/disconnect
// (input.EventDeviceConnected/EventDeviceDisconnected, emitted by
// internal/input's background hotplug poller). It refreshes the device list
// the same way a manual "r" rescan does — handleDevicesLoaded's own
// auto-diagnose sweep then picks up any newly-connected device — and, if
// the device that just disconnected is the one currently shown on
// Diagnostics, surfaces that using the same KindDeviceDisconnected shape
// screen_diagnostics.go already renders for an operation-level disconnect,
// rather than inventing a second disconnect story. listenCmd is
// cmdListenNav's already-issued re-arm, batched in so the nav channel keeps
// being read.
func (m Model) handleHotplugEvent(e input.NavEvent, listenCmd tea.Cmd) (Model, tea.Cmd) {
	m.navNotes = replacePIDNote(m.navNotes, e.SourcePID, e.Note)
	m, noticeCmd := m.setNotice(noticeInfo, e.Note, true)

	if e.Kind == input.EventDeviceDisconnected && m.screen == screenDiagnostics &&
		m.diag.device.VidPid.PID == e.SourcePID && m.diag.device.Serial == e.Serial {
		m.diag.loading = false
		m.diag.err = &core.Error{Kind: core.KindDeviceDisconnected, Message: fmt.Sprintf("%s is no longer connected", m.diag.device.VidPid)}
	}

	return m, tea.Batch(listenCmd, noticeCmd, cmdLoadDevices(m.ctx, m.core))
}

// replacePIDNote drops any existing note for pid and appends newNote,
// keeping gamepadConnected's substring scan and Settings' "Gamepad
// Navigation" list live-accurate across hotplug connect/disconnect events
// instead of only reflecting internal/input.Start's startup-time snapshot.
func replacePIDNote(notes []string, pid uint16, newNote string) []string {
	prefix := fmt.Sprintf("pid=%#04x:", pid)
	newActive := strings.Contains(newNote, "gamepad nav active")
	disconnected := strings.Contains(newNote, "disconnected")
	if !newActive && !disconnected {
		// A multi-interface controller can emit a usable gamepad note followed
		// by a lower-priority vendor-interface note. Preserve the active state
		// regardless of HID enumeration order. Any real disconnect still clears
		// it immediately below.
		for _, note := range notes {
			if strings.HasPrefix(note, prefix) && strings.Contains(note, "gamepad nav active") {
				return notes
			}
		}
	}
	out := make([]string, 0, len(notes)+1)
	for _, n := range notes {
		if !strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return append(out, newNote)
}

// navToKeyMsg translates a gamepad nav event into the same tea.KeyMsg every
// screen already handles from a keyboard, so controller and keyboard
// navigation share one code path end to end. Convention (undocumented by
// hardware, since no descriptor evidence exists yet — see
// spec/gamepad_input.md): the lowest-numbered HID button usage is Confirm,
// the second-lowest is Cancel.
func navToKeyMsg(e input.NavEvent) *tea.KeyMsg {
	switch e.Kind {
	case input.EventDPadChanged:
		switch e.DPad {
		case input.DirUp, input.DirUpLeft, input.DirUpRight:
			return keyMsg("up")
		case input.DirDown, input.DirDownLeft, input.DirDownRight:
			return keyMsg("down")
		case input.DirLeft:
			return keyMsg("left")
		case input.DirRight:
			return keyMsg("right")
		}
		return nil
	case input.EventButtonDown:
		switch e.Button {
		case 1:
			return keyMsg("enter")
		case 2:
			return keyMsg("esc")
		}
		return nil
	}
	return nil
}

func keyMsg(s string) *tea.KeyMsg {
	k := tea.KeyMsg{Type: keyTypeFor(s), Runes: []rune(s)}
	return &k
}

func keyTypeFor(s string) tea.KeyType {
	switch s {
	case "up":
		return tea.KeyUp
	case "down":
		return tea.KeyDown
	case "left":
		return tea.KeyLeft
	case "right":
		return tea.KeyRight
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEsc
	default:
		return tea.KeyRunes
	}
}

// pidLabel formats a VID:PID pair consistently across screens.
func pidLabel(v protocol.VidPid) string { return v.String() }
