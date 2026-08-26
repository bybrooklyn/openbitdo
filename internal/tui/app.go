// Package tui is OpenBitdo's Bubbletea terminal UI. It is a genuine
// redesign, not a port of the prior Rust TUI's six-screen layout — the hard
// constraints carried over are the safety semantics (support-tier gating,
// unsafe/experimental gating, write-lock/recovery, candidate write-probe
// unlock ceremony) in gatekeeping.go, not any particular screen shape.
package tui

import (
	"context"
	"fmt"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	devices        devicesState
	diag           diagnosticsState
	mapping        mappingState
	fw             firmwareState
	settingsCursor int
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

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancel()
			return m, tea.Quit
		}
		if m.modal.active {
			return m.updateModalKey(msg)
		}

	case navEventMsg:
		cmd := cmdListenNav(m.navEvents)
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
			m.statusLine = "Report saved: " + msg.path
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
		m.statusLine = "Running guarded write probe…"
		return m, cmdCandidateProbe(m.ctx, m.core, device, policy)

	case candidateProbeResultMsg:
		m.err = msg.err
		if msg.err != nil {
			return m, nil
		}
		m.statusLine = msg.report.Message
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
		return m, cmdSaveReport(m.settings.ReportSaveMode, m.settingsPath, "candidate-write-probe", &device, status, msg.report.Message, nil, nil, &msg.report)

	case restoreBackupResultMsg:
		m.recoveryRestoreDone = msg.err == nil
		m.recoveryRestoreErr = msg.err
		return m, nil
	}

	return m.route(msg)
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

	header := m.viewHeader()
	footer := m.viewFooter()
	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
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

	page := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	if m.modal.active {
		return m.modal.viewOverlaid(page, m.width, m.height)
	}
	return page
}

func (m Model) viewHeader() string {
	title := styleTitle.Render("OpenBitdo")
	if m.mockMode {
		title += "  " + styleWarning.Render("[mock]")
	}
	crumbs := screenLabel(m.screen)
	right := styleFaint.Render(crumbs)
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	line := title + lipgloss.NewStyle().Width(gap).Render("") + right
	return lipgloss.NewStyle().Padding(0, 1).BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).BorderForeground(theme.BorderDim).Width(m.width - 2).Render(line)
}

func screenLabel(s screen) string {
	switch s {
	case screenDevices:
		return "Devices"
	case screenDiagnostics:
		return "Diagnostics"
	case screenMapping:
		return "Mapping"
	case screenFirmware:
		return "Firmware"
	case screenSettings:
		return "Settings"
	case screenRecovery:
		return "Recovery"
	}
	return ""
}

func (m Model) viewFooter() string {
	help := m.screenHelp()
	line := styleHelp.Render(help)
	if m.statusLine != "" {
		line = stylePositive.Render(m.statusLine) + "   " + line
	}
	if m.err != nil {
		line = styleDanger.Render(fmt.Sprintf("error: %v", m.err)) + "   " + line
	}
	return lipgloss.NewStyle().Padding(0, 1).Width(m.width - 2).Render(line)
}

// hint renders one "key label" pair for the footer — the small building
// block every screen's contextual hints are assembled from, matching
// opencode's per-context footer (e.g. "tab agents  ctrl+p commands") rather
// than the same generic line everywhere.
func hint(key, label string) string {
	return styleKey.Render(key) + " " + label
}

func (m Model) screenHelp() string {
	nav := hint("↑↓/dpad", "move") + "  " + hint("enter/A", "select") + "  " + hint("esc/B", "back") + "  " + hint("ctrl+c", "quit")
	switch m.screen {
	case screenDevices:
		return hint("/", "filter") + "  " + hint("r", "rescan") + "  " + nav
	case screenDiagnostics:
		extra := hint("tab", "toggle filter") + "  " + hint("r", "rerun")
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
			return hint("enter", "confirm — begin transfer") + "  " + hint("esc", "back")
		case fwStageRunning:
			return hint("c", "cancel") + "  " + styleFaint.Render("(transfer continues if you leave this screen)")
		default:
			return hint("esc", "back")
		}
	case screenSettings:
		return hint("←→/enter", "toggle") + "  " + nav
	case screenRecovery:
		extra := hint("q", "quit")
		if m.recoveryHasBackup && !m.recoveryRestoreDone {
			extra = hint("r", "restore backup") + "  " + extra
		}
		return extra
	default:
		return nav
	}
}

func (m Model) handleDevicesLoaded(msg devicesLoadedMsg) (tea.Model, tea.Cmd) {
	m.err = msg.err
	m.devices.devices = sortDevicesByTier(msg.devices)
	m.devices.applyFilter()
	if m.devices.cursor >= len(m.devices.filtered) {
		m.devices.cursor = 0
	}
	return m, nil
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
