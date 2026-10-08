package tui

import (
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/charmbracelet/lipgloss"
)

// keyHint is one key binding as shown to the user: in the footer (key and
// short label) and in the help overlay (key and what it does).
type keyHint struct {
	key   string
	label string // footer wording, a word or two
	help  string // help-overlay wording; label is used when empty
}

func (h keyHint) helpText() string {
	if h.help != "" {
		return h.help
	}
	return h.label
}

var (
	hintHelp = keyHint{key: "?", label: "help", help: "show this help"}
	hintQuit = keyHint{key: "q", label: "quit", help: "quit (ctrl+c too)"}
)

// viewHints lists the bindings that do something in the view currently on
// screen, most useful first. The footer shows as many as fit, from the
// front; the help overlay shows them all. A key is only listed where it
// works, so a sub-view (the report, the slot preview, a text filter) has its
// own short list rather than inheriting its parent's.
func (m Model) viewHints() []keyHint {
	move, choose, back := "↑↓", "enter", "esc"
	if m.gamepadConnected() {
		move, choose, back = "↑↓/dpad", "enter/A", "esc/B"
	}
	moveHint := keyHint{key: move, label: "move", help: "move the selection (j/k too)"}

	switch m.screen {
	case screenDevices:
		if m.devices.filtering {
			return []keyHint{
				{key: "type", label: "to filter", help: "narrow the device list by name"},
				{key: "enter", label: "keep filter"},
				{key: "esc", label: "clear filter"},
			}
		}
		if m.devices.pane == paneActions {
			return []keyHint{
				moveHint,
				{key: choose, label: "run", help: "run the selected action"},
				{key: back, label: "devices", help: "back to the device list (← too)"},
				{key: "r", label: "rescan", help: "look for devices again"},
				{key: "s", label: "settings"},
			}
		}
		return []keyHint{
			moveHint,
			{key: choose, label: "actions", help: "open its actions (→ or tab too)"},
			{key: "r", label: "rescan", help: "look for devices again"},
			{key: "/", label: "filter", help: "filter the device list by name"},
			{key: "s", label: "settings"},
		}

	case screenDiagnostics:
		if m.diag.showSupportRequest {
			return []keyHint{
				{key: move, label: "scroll"},
				{key: "c", label: "copy", help: "copy the report to the clipboard"},
				{key: "w", label: "save", help: "save the report to a file"},
				{key: back, label: "back", help: "back to the check list"},
			}
		}
		if m.diag.loading || m.diag.err != nil {
			return []keyHint{
				{key: "r", label: "retry", help: "run diagnostics again"},
				{key: back, label: "back", help: "back to the device list"},
			}
		}
		return []keyHint{
			moveHint,
			{key: "r", label: "rerun", help: "run diagnostics again"},
			{key: "d", label: "details", help: "raw details of the selected check"},
			{key: "f", label: "issues only", help: "show only the checks that did not pass"},
			{key: "v", label: "report", help: "view the full report, to copy or save"},
			{key: back, label: "back", help: "back to the device list"},
		}

	case screenMapping:
		if m.mapping.previewing() {
			return []keyHint{
				{key: "p", label: "next slot", help: "preview the next slot"},
				{key: choose, label: "load slot", help: "load this slot into the editor"},
				{key: back, label: "back", help: "back to the editor"},
			}
		}
		hints := []keyHint{
			moveHint,
			{key: "←→", label: "change", help: "change the selected row's target"},
			{key: choose, label: "run row", help: "run Apply, Undo or Reset"},
		}
		if m.mapping.kind == core.KindUltimate2 {
			hints = append(hints, keyHint{key: "p", label: "preview slot", help: "look at another slot"})
		}
		return append(hints, keyHint{key: back, label: "back", help: "back to the device list"})

	case screenFirmware:
		switch m.fw.stage {
		case fwStageReadyToConfirm:
			return []keyHint{{key: "enter", label: "confirm"}, {key: "esc", label: "back"}}
		case fwStageRunning:
			return []keyHint{{key: "c", label: "cancel", help: "cancel the transfer"}}
		default:
			return []keyHint{{key: "esc", label: "back"}}
		}

	case screenSettings:
		return []keyHint{
			moveHint,
			{key: choose, label: "change", help: "change the selected setting"},
			{key: "pg↑↓", label: "scroll", help: "scroll the information below"},
			{key: back, label: "back", help: "back to the device list"},
		}

	case screenRecovery:
		if m.recoveryHasBackup && !m.recoveryRestoreDone {
			return []keyHint{{key: "r", label: "restore backup", help: "write the backup back to the device"}}
		}
		return nil
	}
	return nil
}

// capturingText reports whether keystrokes are being typed into a text field,
// in which case they are text first and shortcuts never.
func (m Model) capturingText() bool {
	return m.screen == screenDevices && m.devices.filtering && !m.modal.active
}

func renderHint(h keyHint) string {
	return styleKey.Render(h.key) + " " + h.label
}

// footerHints fits as many hints as the width allows. Help and quit are
// always kept (they are how a lost user recovers); the view's own hints are
// dropped from the least useful end first, never reordered.
func (m Model) footerHints(width int) string {
	const gap = "  "
	tail := []keyHint{hintHelp, hintQuit}
	if m.capturingText() {
		tail = nil // ? and q are text while typing
	}
	if m.notice.level >= noticeWarning {
		tail = append([]keyHint{{key: "x", label: "dismiss"}}, tail...)
	}
	join := func(hints []keyHint) string {
		parts := make([]string, len(hints))
		for i, h := range hints {
			parts[i] = renderHint(h)
		}
		return strings.Join(parts, gap)
	}

	own := m.viewHints()
	for keep := len(own); keep >= 0; keep-- {
		line := join(append(append([]keyHint{}, own[:keep]...), tail...))
		if lipgloss.Width(line) <= width || keep == 0 {
			return line
		}
	}
	return join(tail)
}

// screenTitle names the current view for the help overlay.
func (m Model) screenTitle() string {
	switch m.screen {
	case screenDevices:
		return "Devices"
	case screenDiagnostics:
		if m.diag.showSupportRequest {
			return "Diagnostics report"
		}
		return "Diagnostics"
	case screenMapping:
		return "Mapping editor"
	case screenFirmware:
		return "Firmware"
	case screenSettings:
		return "Settings"
	case screenRecovery:
		return "Recovery"
	}
	return ""
}

// helpLines is the body of the help overlay: every key that works in the
// current view, then what the status symbols mean.
func (m Model) helpLines() []string {
	hints := append(m.viewHints(), hintHelp, hintQuit)
	keyWidth := 0
	for _, h := range hints {
		keyWidth = max(keyWidth, lipgloss.Width(h.key))
	}
	lines := make([]string, 0, len(hints)+8)
	for _, h := range hints {
		pad := strings.Repeat(" ", keyWidth-lipgloss.Width(h.key))
		lines = append(lines, styleKey.Render(h.key)+pad+"  "+h.helpText())
	}
	if m.screen == screenDevices {
		lines = append(lines, "",
			styleBadgeFull.Render(IconTierFull)+"  answering diagnostics",
			styleBadgeCandidate.Render(IconTierCandidate)+"  not checked yet, or checking now",
			styleBadgeDetect.Render(IconTierDetect)+"  connected, but OpenBitdo can't talk to it",
		)
	}
	if m.screen == screenDiagnostics && !m.diag.showSupportRequest {
		lines = append(lines, "",
			stylePositive.Render(IconPass)+"  the device answered as expected",
			styleWarning.Render(IconWarn)+"  no answer, or not the expected one",
			styleDanger.Render(IconFail)+"  an answer that contradicts what is known",
		)
	}
	return lines
}
