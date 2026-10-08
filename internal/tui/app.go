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
	screenButtons
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
	// pads is what each controller has sent this session, by PID, for the
	// Buttons tab.
	pads map[uint16]padState

	width, height int
	// paned is set on the copy of the model a screen works on, whose width
	// and height are the pane's rather than the terminal's (see inPane).
	paned      bool
	screen     screen
	prevScreen screen // the tab to go back to from Settings or Recovery

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
	activity   []activityEntry

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

// activityEntry is one past notice, kept so a message that has left the
// footer can still be read (on the Settings screen).
type activityEntry struct {
	at      time.Time
	level   noticeLevel
	message string
}

const activityLogSize = 20

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
		// Once writes are locked, Recovery is the only screen; a click must
		// not reach the screen that was showing when the lock tripped.
		if m.writeLockUntilRestart {
			if m.screen != screenRecovery {
				m.prevScreen = m.screen
				m.screen = screenRecovery
			}
			return m, nil
		}
		return m.routeMouse(msg)

	case discardMappingMsg:
		m.modal = modal{}
		switch msg.action {
		case discardActionBack:
			m.mapping = newMappingState()
			m.screen = screenDevices
			return m, nil
		case discardActionNavigate:
			m.mapping = newMappingState()
			return m.navigateNow(msg.screen, msg.deviceIdx)
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
		if m.modal.active {
			return m.updateModalKey(msg)
		}
		// A text field gets its keys before any shortcut does: q, ? and x
		// are letters there, not commands.
		if m.capturingText() {
			return m.route(msg)
		}
		if msg.String() == "x" && m.notice.level >= noticeWarning {
			m.notice = noticeState{}
			m.err = nil
			m.statusLine = ""
			return m, nil
		}
		if msg.String() == "q" {
			if m.screen == screenMapping && m.mapping.dirty() {
				m.modal = discardMappingModal(discardMappingMsg{action: discardActionQuit})
				return m, nil
			}
			m.cancel()
			return m, tea.Quit
		}
		if msg.String() == "?" {
			m.modal = helpModal(m.screenTitle(), m.helpLines())
			return m, nil
		}
		if !m.writeLockUntilRestart {
			if next, cmd, handled := m.shellKey(msg); handled {
				return next, cmd
			}
		}

	case navEventMsg:
		cmd := cmdListenNav(m.navEvents)
		if msg.event.Kind == input.EventDeviceConnected || msg.event.Kind == input.EventDeviceDisconnected {
			return m.handleHotplugEvent(msg.event, cmd)
		}
		m = m.recordInput(msg.event)
		if m.screen == screenButtons && !m.modal.active {
			// The Buttons tab is for watching the controller. A press must
			// not also act as enter or jump to another device.
			return m, cmd
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
			// The same path a key press takes, so the controller reaches
			// the shell's keys (section, device) as well as the screen's.
			updated, screenCmd := m.Update(*navMsg)
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

// routeMouse sends a click or wheel turn to whichever region of the shell
// it landed in. A click in the pane is handed to the screen in the pane's
// own coordinates, which is what its click code is written against.
func (m Model) routeMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	g := m.shellGeom()
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.routeMouseWheel(-3)
	case tea.MouseButtonWheelDown:
		return m.routeMouseWheel(3)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if g.sidebar.contains(msg.X, msg.Y) && !m.inSubView() {
			if i, ok := m.deviceAt(msg.Y); ok {
				target := m.screen
				if !isTab(target) {
					target = screenDevices
				}
				m.devices.filtering = false
				return m.navigate(target, i)
			}
			return m, nil
		}
		if msg.Y == g.tabRow && isTab(m.screen) && !m.inSubView() {
			if target, ok := m.tabAt(msg.X); ok {
				return m.navigate(target, m.devices.cursor)
			}
			return m, nil
		}
		if !g.pane.contains(msg.X, msg.Y) {
			return m, nil
		}
		// Screens lay their panel out below a two-row header.
		local := msg
		local.X = msg.X - g.pane.x
		local.Y = msg.Y - g.pane.y + calculateLayout(m.width, m.height).headerHeight
		inner := m.inPane()
		var next tea.Model
		var cmd tea.Cmd
		switch m.screen {
		case screenDevices:
			next, cmd = inner.clickDevices(local)
		case screenDiagnostics:
			next, cmd = inner.clickDiagnostics(local)
		case screenMapping:
			next, cmd = inner.clickMapping(local)
		case screenSettings:
			next, cmd = inner.clickSettings(local)
		default:
			return m, nil
		}
		return m.outOfPane(next.(Model)), cmd
	}
	return m, nil
}

func (m Model) routeMouseWheel(delta int) (tea.Model, tea.Cmd) {
	inner := m.inPane()
	switch inner.screen {
	case screenDevices:
		inner.devices.actionIdx = clampInt(inner.devices.actionIdx+delta, 0, len(inner.availableActions())-1)
	case screenDiagnostics:
		if inner.diag.showSupportRequest {
			body := inner.diagnosticsReportLines(supportRequestBody(inner.diag.device, inner.diag.result))
			inner.diag.supportOffset = clampInt(inner.diag.supportOffset+delta, 0, max(0, len(body)-inner.diagnosticsReportRows()))
		} else {
			inner.diag.cursor = clampInt(inner.diag.cursor+delta, 0, len(inner.diag.visibleChecks())-1)
			inner.ensureDiagnosticsCursorVisible()
		}
	case screenMapping:
		inner.mapping.cursor = clampInt(inner.mapping.cursor+delta, 0, inner.mapping.rowCount()-1)
		inner.ensureMappingCursorVisible()
	case screenSettings:
		inner.settingsInfoOffset = clampInt(inner.settingsInfoOffset+delta, 0, inner.settingsInfoMaxOffset())
	}
	return m.outOfPane(inner), nil
}

// clickDevices handles a click in the Overview tab: on a "You can" row it
// selects that row and runs it.
func (m Model) clickDevices(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	panel := m.overviewPanel(m.height - 3)
	if i, ok := panel.ownerAt(msg.X, msg.Y); ok {
		m.devices.actionIdx = i
		return m.triggerDevicesEnter()
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
	if m.diag.showSupportRequest || m.diag.loading || m.diag.err != nil {
		return m, nil
	}
	layout := calculateLayout(m.width, m.height)
	if msg.X < 0 || msg.X >= m.width {
		return m, nil
	}
	// Check rows start right under the fixed header lines, in list order.
	checks := m.diag.visibleChecks()
	start, end, _ := viewportWindow(len(checks), m.diag.cursor, m.diag.rowOffset, m.diagnosticsVisibleRows())
	row := msg.Y - layout.headerHeight - diagHeaderLines
	if row >= 0 && start+row < end {
		m.diag.cursor = start + row
	}
	return m, nil
}

func (m Model) clickSettings(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	layout := calculateLayout(m.width, m.height)
	if msg.X < 0 || msg.X >= m.width {
		return m, nil
	}
	// The setting rows sit right under the title and its blank line.
	row := msg.Y - layout.headerHeight - settingsFirstRow
	if row < 0 || row >= settingsRowCount {
		return m, nil
	}
	m.settingsCursor = row
	return m.triggerSettingsRow()
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
	m.activity = append(m.activity, activityEntry{at: time.Now(), level: level, message: message})
	if len(m.activity) > activityLogSize {
		m.activity = append([]activityEntry(nil), m.activity[len(m.activity)-activityLogSize:]...)
	}
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
	inner := m.inPane()
	var next tea.Model = inner
	var cmd tea.Cmd
	switch m.screen {
	case screenDevices:
		next, cmd = inner.updateDevices(msg)
	case screenDiagnostics:
		next, cmd = inner.updateDiagnostics(msg)
	case screenMapping:
		next, cmd = inner.updateMapping(msg)
	case screenFirmware:
		next, cmd = inner.updateFirmware(msg)
	case screenSettings:
		next, cmd = inner.updateSettings(msg)
	case screenRecovery:
		next, cmd = inner.updateRecovery(msg)
	case screenButtons:
		next, cmd = inner.updateButtons(msg)
	}
	return m.outOfPane(next.(Model)), cmd
}

func (m Model) updateModalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "left", "right", "tab", "shift+tab", "h", "l":
		if m.modal.cancelLabel != "" {
			m.modal.focusCancel = !m.modal.focusCancel
		}
		return m, nil
	case "enter", " ":
		if m.modal.focusCancel && m.modal.cancelLabel != "" {
			m.modal = modal{}
			return m, nil
		}
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
	if calculateLayout(m.width, m.height).mode == layoutTooSmall {
		return clampRendered(m.viewTooSmall(), m.width, m.height)
	}
	page := m.viewShell()
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

// viewFooter is one line: a notice (if any) on the left, then the keys that
// work right now. When both do not fit, hints are dropped before the notice
// is shortened, and the notice is shortened with an ellipsis rather than
// replaced by a generic word, so what went wrong stays readable.
func (m Model) viewFooter() string {
	width := max(1, m.width-2)
	notice, noticeStyle := m.footerNotice()
	if notice == "" {
		return lipgloss.NewStyle().Padding(0, 1).Render(styleHelp.Render(m.footerHints(width)))
	}

	const gap = "   "
	minHints := lipgloss.Width(m.footerHints(0))
	room := width - minHints - len(gap)
	if room < 8 {
		return lipgloss.NewStyle().Padding(0, 1).Render(noticeStyle.Render(truncate(notice, width)))
	}
	notice = truncate(notice, room)
	hints := m.footerHints(width - lipgloss.Width(notice) - len(gap))
	return lipgloss.NewStyle().Padding(0, 1).Render(noticeStyle.Render(notice) + gap + styleHelp.Render(hints))
}

func (m Model) footerNotice() (string, lipgloss.Style) {
	if m.notice.message != "" {
		switch m.notice.level {
		case noticeSuccess:
			return m.notice.message, stylePositive
		case noticeWarning:
			return m.notice.message, styleWarning
		case noticeError:
			return "error: " + m.notice.message, styleDanger
		default:
			return m.notice.message, styleAccent
		}
	}
	if m.err != nil {
		return fmt.Sprintf("error: %v", m.err), styleDanger
	}
	return m.statusLine, stylePositive
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

func (m Model) handleDevicesLoaded(msg devicesLoadedMsg) (tea.Model, tea.Cmd) {
	var noticeCmd tea.Cmd
	announce := m.devices.announceScan
	m.devices.loading, m.devices.scanned, m.devices.announceScan = false, true, false

	selected, hadSelection := m.devices.selected()
	m.devices.devices = sortDevicesByTier(msg.devices)
	m.devices.applyFilter()
	if !hadSelection {
		m.devices.cursor = 0 // the list is sorted with reachable devices first
	}
	if !m.devices.reselect(selected, hadSelection) && isTab(m.screen) && m.screen != screenDevices {
		// The device a tab was showing is gone. Its checks or its mapping
		// draft now describe nothing on screen, so fall back to Overview.
		m.screen = screenDevices
		m.mapping = newMappingState()
	}

	switch {
	case msg.err != nil:
		m, noticeCmd = m.setNotice(noticeError, msg.err.Error(), false)
	case announce:
		m.err = nil
		m, noticeCmd = m.setNotice(noticeInfo, scanSummary(len(msg.devices)), true)
	default:
		m.err = nil
	}

	// Every load (startup, manual "r" rescan, or a hotplug-triggered reload
	// from handleHotplugEvent) auto-diagnoses any device this session hasn't
	// probed yet — core.HasDiagnosed makes this naturally idempotent, so an
	// already-cached device is skipped rather than re-probed on every
	// reload. This is what makes a freshly-connected controller have a
	// "Last run: Xs ago" diagnostic result already waiting by the time the
	// user navigates to it. A device with no configuration interface is
	// skipped: there is nothing to send a check through.
	cmds := make([]tea.Cmd, 0, len(msg.devices)+1)
	for _, d := range msg.devices {
		if d.ConfigChannel == core.ChannelAbsent {
			continue
		}
		if !m.core.HasDiagnosed(d) {
			cmds = append(cmds, cmdAutoDiagnose(m.ctx, m.core, d))
		}
	}
	if noticeCmd != nil {
		cmds = append(cmds, noticeCmd)
	}
	return m, tea.Batch(cmds...)
}

func scanSummary(found int) string {
	switch found {
	case 0:
		return "No 8BitDo device found."
	case 1:
		return "Found 1 device."
	default:
		return fmt.Sprintf("Found %d devices.", found)
	}
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
	summary := "Device connected."
	if e.Kind == input.EventDeviceDisconnected {
		summary = "Device disconnected."
	}
	m, noticeCmd := m.setNotice(noticeInfo, summary, true)
	// Whatever a failed probe was caused by, the device set just changed.
	m.core.ForgetFailedDiags()

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
		case 3:
			// With no keyboard to hand, a controller still needs a way to
			// reach the other devices.
			return keyMsg("d")
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
