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

	// Keys that work on every tab, listed after the tab's own.
	shell := []keyHint{{key: "tab", label: "section", help: "next section (also ← →, or 1-4 to jump)"}}
	if len(m.devices.filtered) > 1 {
		shell = append(shell, keyHint{key: "d", label: "next device", help: "next device (D for the previous one)"})
	}
	shell = append(shell, keyHint{key: "s", label: "settings"})

	switch m.screen {
	case screenDevices:
		if m.devices.filtering {
			return []keyHint{
				{key: "type", label: "to filter", help: "narrow the device list by name"},
				{key: "enter", label: "keep filter"},
				{key: "esc", label: "clear filter"},
			}
		}
		hints := []keyHint{}
		if len(m.availableActions()) > 0 {
			hints = append(hints,
				keyHint{key: move, label: "choose", help: "move through the You can list (j/k too)"},
				keyHint{key: choose, label: "do it", help: "do the selected thing"})
		}
		hints = append(hints, shell...)
		return append(hints,
			keyHint{key: "r", label: "rescan", help: "look for devices again"},
			keyHint{key: "/", label: "filter", help: "filter the device list by name"})

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
			return append([]keyHint{{key: "r", label: "try again", help: "run the checks again"}}, shell...)
		}
		return append([]keyHint{
			{key: move, label: "move", help: "move through the checks (j/k too)"},
			{key: choose, label: "details", help: "show or hide the raw details of the selected check"},
			{key: "r", label: "rerun", help: "run the checks again"},
			{key: "v", label: "report", help: "view the full report, to copy or save"},
			{key: "f", label: "issues only", help: "show only the checks that got no answer"},
		}, shell...)

	case screenMapping:
		if m.mapping.unavailable != "" || m.mapping.loading || m.mapping.err != nil {
			return shell
		}
		if m.mapping.kind == core.KindJP108 {
			if m.mapping.kb.picking {
				return []keyHint{
					{key: "type", label: "to search", help: "narrow the list by name"},
					{key: move, label: "move"},
					{key: "tab", label: "modifier", help: "hold a modifier with the key (Ctrl, Shift, Alt, Win)"},
					{key: "enter", label: "assign"},
					{key: "esc", label: "cancel"},
				}
			}
			return append([]keyHint{
				{key: move, label: "move", help: "move through the keys (j/k, pgup/pgdn too)"},
				{key: choose, label: "assign", help: "choose what the key does; on a setting, change it"},
				{key: "←→", label: "step", help: "step through the choices without opening the list"},
				{key: "del", label: "default", help: "put the key back to its normal behaviour"},
			}, shell...)
		}
		if m.mapping.previewing() {
			return []keyHint{
				{key: "p", label: "next slot", help: "preview the next slot"},
				{key: choose, label: "load slot", help: "load this slot into the editor"},
				{key: back, label: "back", help: "back to the editor"},
			}
		}
		hints := []keyHint{
			{key: move, label: "move", help: "move through the rows (j/k too)"},
			{key: "←→", label: "change", help: "change the selected row's target"},
			{key: choose, label: "run row", help: "run Apply, Undo or Reset"},
		}
		if m.mapping.kind == core.KindUltimate2 {
			hints = append(hints, keyHint{key: "p", label: "preview slot", help: "look at another slot"})
		}
		return append(hints, shell...)

	case screenButtons:
		return append([]keyHint{{key: "c", label: "clear", help: "forget what has been pressed so far"}}, shell...)

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
			{key: back, label: "back", help: "back to where you were"},
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
	if m.modal.active {
		return false
	}
	return (m.screen == screenDevices && m.devices.filtering) || (m.keyboardEditing() && m.mapping.kb.picking)
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
		return "Overview"
	case screenDiagnostics:
		if m.diag.showSupportRequest {
			return "Report"
		}
		return "Checks"
	case screenMapping:
		return "Mapping"
	case screenButtons:
		return "Buttons"
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
			styleBadgeFull.Render(IconTierFull)+"  working: settings can be changed",
			styleBadgeCandidate.Render(IconTierCandidate)+"  limited (read only), or still checking",
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
