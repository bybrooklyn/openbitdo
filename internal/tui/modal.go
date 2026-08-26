package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func (m modal) view(width, height int) string {
	border := styleModalBorder
	if m.danger {
		border = styleModalBorderDanger
	}

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

	box := border.Width(min(60, width-6)).Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceForeground(theme.TextFaint))
}
