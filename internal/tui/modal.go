package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// modal is a real overlay confirmation rendered on top of the current
// screen, replacing the prior Rust TUI's pattern of dedicating a whole
// screen (Task/Preflight) to confirmations. onConfirm is re-dispatched
// through Update as if it had arrived from the outside, so confirming a
// modal is just "deliver this message now" — every screen already knows how
// to handle its own completion/result messages.
type modal struct {
	active       bool
	danger       bool
	title        string
	body         []string
	confirmLabel string
	cancelLabel  string
	onConfirm    tea.Msg
}

type discardAction int

const (
	discardActionBack discardAction = iota
	discardActionQuit
	discardActionLoadSlot
)

type discardMappingMsg struct {
	action discardAction
}

func newModal(title string, body []string, danger bool, confirmLabel string, onConfirm tea.Msg) modal {
	if confirmLabel == "" {
		confirmLabel = "Confirm"
	}
	return modal{
		active: true, danger: danger, title: title, body: body,
		confirmLabel: confirmLabel, cancelLabel: "Cancel", onConfirm: onConfirm,
	}
}

// riskAckModal is the real one-time "this may brick your device"
// confirmation the Rust TUI never actually had (it hardcoded the
// acknowledgement flags true with a comment claiming a UI surface that
// didn't exist). onConfirm is the action that was waiting on this
// acknowledgement.
func riskAckModal(action string, onConfirm tea.Msg) modal {
	return newModal(
		"Unsafe operation acknowledgement",
		[]string{
			"You are about to " + action + ".",
			"",
			"This writes to your controller's firmware or boot state.",
			"An interrupted or failed write can permanently brick the device.",
			"",
			"This acknowledgement applies for the rest of this session.",
		},
		true, "I understand the risk", onConfirm,
	)
}

func discardMappingModal(action discardAction) modal {
	return newModal(
		"Discard mapping draft?",
		[]string{
			"You have unapplied mapping changes.",
			"",
			"Discarding leaves the connected device unchanged.",
		},
		false, "Discard", discardMappingMsg{action: action},
	)
}

func helpModal(help string) modal {
	return newModal("Help", []string{help}, false, "OK", nil)
}

// view renders the modal box itself (no positioning/backdrop) — see
// viewOverlaid for how it gets composited onto the dimmed screen behind it.
func (m modal) view(width int) string {
	titleStyle := styleAccent
	if m.danger {
		titleStyle = styleDanger
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")
	b.WriteString(strings.Join(m.body, "\n"))
	b.WriteString("\n\n")

	confirmBtn := stylePositive.Render("[ " + m.confirmLabel + " ]")
	if m.danger {
		confirmBtn = styleDanger.Render("[ " + m.confirmLabel + " ]")
	}
	cancelBtn := styleFaint.Render("[ " + m.cancelLabel + " ]")
	b.WriteString(confirmBtn + "   " + cancelBtn)
	b.WriteString("\n")
	b.WriteString(styleHelp.Render("enter/A confirm · esc/B cancel"))

	return styleModal.Width(min(60, width-6)).Render(b.String())
}

// viewOverlaid composites the modal on top of page (the already-rendered
// screen behind it), dimmed, so the confirmation still shows real context
// (which device, which screen) instead of replacing it entirely.
//
// Lipgloss/Bubbletea compose styled character cells, not RGBA layers, so
// there's no built-in alpha-blend equivalent to a real overlay. This
// approximates it in two steps that are each individually simple and
// correct: strip every existing color from the rendered page (ansi.Strip),
// then re-apply one single faint foreground uniformly — real UI dimming
// desaturates/flattens rather than preserving full color richness anyway,
// which is exactly what this produces. The modal itself is then spliced
// into the dimmed lines using ansi.Cut, which is escape-code- and
// display-width-aware so it can't corrupt an SGR sequence mid-cut.
func (m modal) viewOverlaid(page string, width, height int) string {
	box := m.view(width)
	boxLines := strings.Split(box, "\n")
	boxWidth := lipgloss.Width(box)
	boxHeight := len(boxLines)

	dimmed := lipgloss.NewStyle().Foreground(theme.TextFaint).Render(ansi.Strip(page))
	bgLines := strings.Split(dimmed, "\n")
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}

	startRow := max(0, (height-boxHeight)/2)
	startCol := max(0, (width-boxWidth)/2)

	for i, boxLine := range boxLines {
		row := startRow + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		left := ansi.Cut(bgLines[row], 0, startCol)
		right := ansi.Cut(bgLines[row], startCol+boxWidth, width)
		bgLines[row] = left + boxLine + right
	}
	return strings.Join(bgLines, "\n")
}
