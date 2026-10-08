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
	// focusCancel is which button enter activates. A dangerous confirmation
	// starts on Cancel, so a stray enter (or a held gamepad button) backs
	// out instead of writing to a device.
	focusCancel bool
}

type discardAction int

const (
	discardActionBack discardAction = iota
	discardActionQuit
	discardActionLoadSlot
	// discardActionNavigate carries on to wherever the user was heading
	// when the unapplied draft stopped them.
	discardActionNavigate
)

type discardMappingMsg struct {
	action    discardAction
	screen    screen // for discardActionNavigate
	deviceIdx int
}

func newModal(title string, body []string, danger bool, confirmLabel string, onConfirm tea.Msg) modal {
	if confirmLabel == "" {
		confirmLabel = "Confirm"
	}
	return modal{
		active: true, danger: danger, title: title, body: body,
		confirmLabel: confirmLabel, cancelLabel: "Cancel", onConfirm: onConfirm,
		focusCancel: danger,
	}
}

// riskAckModal is the real one-time confirmation before anything is written
// to a device that could harm it (the Rust TUI hardcoded the acknowledgement
// flags true with a comment claiming a UI surface that didn't exist).
// consequence says what this particular action writes and what can go wrong;
// onConfirm is the action that was waiting on this acknowledgement.
func riskAckModal(action string, consequence []string, onConfirm tea.Msg) modal {
	body := append([]string{"You are about to " + action + ".", ""}, consequence...)
	body = append(body, "", "This acknowledgement applies for the rest of this session.")
	return newModal("Confirm a risky operation", body, true, "I understand the risk", onConfirm)
}

var (
	firmwareRisk = []string{
		"This writes to your controller's firmware or boot state.",
		"An interrupted or failed write can permanently brick the device.",
	}
	writeProbeRisk = []string{
		"This writes one setting to the device and reads it back, to learn",
		"whether writes work on this model. It does not touch firmware, but a",
		"write to an unconfirmed device can leave it in an unexpected state.",
	}
)

func discardMappingModal(onDiscard discardMappingMsg) modal {
	return newModal(
		"Discard mapping draft?",
		[]string{
			"You have unapplied mapping changes.",
			"",
			"Discarding leaves the connected device unchanged.",
		},
		false, "Discard", onDiscard,
	)
}

// helpModal lists the keys for the current view. It has nothing to confirm,
// so it shows a single Close button.
func helpModal(title string, lines []string) modal {
	m := newModal("Keys: "+title, lines, false, "Close", nil)
	m.cancelLabel = ""
	return m
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

	// The focused button is drawn inverted; the other one faint. Colour
	// alone never carries it: the focused button also gets the › marker.
	button := func(label string, focused bool, tone lipgloss.Style) string {
		if focused {
			return tone.Reverse(true).Render("›[ " + label + " ]")
		}
		return styleFaint.Render(" [ " + label + " ]")
	}
	tone := stylePositive
	if m.danger {
		tone = styleDanger
	}
	if m.cancelLabel == "" {
		b.WriteString(button(m.confirmLabel, true, tone))
		b.WriteString("\n")
		b.WriteString(styleHelp.Render("enter or esc to close"))
	} else {
		b.WriteString(button(m.confirmLabel, !m.focusCancel, tone) + "  " + button(m.cancelLabel, m.focusCancel, styleBody))
		b.WriteString("\n")
		b.WriteString(styleHelp.Render("←→ choose · enter/A select · esc/B cancel"))
	}

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
