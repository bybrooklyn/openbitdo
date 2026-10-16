package tui

import (
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// The controller macro editor opens over the controller editor for one of
// a slot's four macros. A macro is a list of moments: what is held, and
// for how long. It edits a copy; Save puts the copy into the draft.

const (
	padMacroRowName = iota
	padMacroRowTrigger
	padMacroRowRepeat
	padMacroRowInterval
	padMacroHeadRows
)

type padMacroEditor struct {
	open    bool
	index   int // which of the slot's macros
	macro   core.PadMacro
	existed bool
	cursor  int
	naming  bool
	// picking is true while a button is chosen: to toggle in the selected
	// step, or (tap) to add as a press-and-release pair of steps.
	picking    bool
	pickTap    bool
	pickCursor int
	problem    string
}

func clonePadMacro(m core.PadMacro) core.PadMacro {
	m.Steps = append([]core.PadMacroStep(nil), m.Steps...)
	return m
}

func (m *Model) openPadMacroEditor(index int) {
	pad := &m.mapping.pad
	existing := pad.draft.Macros[pad.slot][index]
	editor := padMacroEditor{open: true, index: index, macro: clonePadMacro(existing), existed: !existing.Empty()}
	if existing.Empty() {
		editor.macro = core.PadMacro{Trigger: core.PadMacroTriggers[0], Repeat: 1}
	}
	pad.macro = editor
}

func restStep(millis int) core.PadMacroStep {
	return core.PadMacroStep{Millis: millis, Left: core.PadStickCentre, Right: core.PadStickCentre}
}

func nextStick(current core.PadStick) core.PadStick {
	sticks := core.PadSticks()
	for i, stick := range sticks {
		if stick == current {
			return sticks[(i+1)%len(sticks)]
		}
	}
	return core.PadStickCentre
}

func (m Model) updatePadMacroEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &m.mapping.pad.macro
	e.problem = ""
	buttons := core.PadMacroStepButtons()
	stepIndex := e.cursor - padMacroHeadRows
	onStep := stepIndex >= 0 && stepIndex < len(e.macro.Steps)

	switch {
	case e.picking:
		switch msg.String() {
		case "esc":
			e.picking = false
		case "up", "k":
			e.pickCursor = max(0, e.pickCursor-1)
		case "down", "j":
			e.pickCursor = min(len(buttons)-1, e.pickCursor+1)
		case "enter":
			bit := uint16(buttons[e.pickCursor])
			e.picking = false
			if e.pickTap {
				hold := restStep(50)
				hold.Buttons = bit
				e.insert(hold, restStep(50))
			} else if onStep {
				e.macro.Steps[stepIndex].Buttons ^= bit
			}
		}
		return m, nil
	case e.naming:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyEnter:
			e.naming = false
			e.macro.Name = strings.TrimSpace(e.macro.Name)
		case tea.KeyBackspace:
			if runes := []rune(e.macro.Name); len(runes) > 0 {
				e.macro.Name = string(runes[:len(runes)-1])
			}
		case tea.KeyRunes, tea.KeySpace:
			if runes := []rune(e.macro.Name + string(msg.Runes)); len(runes) <= padNameMax {
				e.macro.Name = string(runes)
			}
		}
		return m, nil
	}

	steps := len(e.macro.Steps)
	switch msg.String() {
	case "esc":
		e.open = false
	case "up", "k":
		e.cursor = max(0, e.cursor-1)
	case "down", "j":
		e.cursor = min(padMacroHeadRows+steps+1, e.cursor+1)
	case "left", "right", "[", "]":
		delta := map[string]int{"left": -1, "right": 1, "[": -10, "]": 10}[msg.String()]
		switch {
		case e.cursor == padMacroRowTrigger:
			index := 0
			for i, trigger := range core.PadMacroTriggers {
				if trigger == e.macro.Trigger {
					index = i
				}
			}
			n := len(core.PadMacroTriggers)
			e.macro.Trigger = core.PadMacroTriggers[((index+sign(delta))%n+n)%n]
		case e.cursor == padMacroRowRepeat:
			e.macro.Repeat = clampInt(e.macro.Repeat+delta, 1, 999)
		case e.cursor == padMacroRowInterval:
			e.macro.IntervalMillis = clampInt(e.macro.IntervalMillis+delta*50, 0, 60000)
		case onStep:
			step := &e.macro.Steps[stepIndex]
			step.Millis = clampInt(step.Millis+delta*10, 10, 60000)
		}
	case "t":
		e.picking, e.pickTap, e.pickCursor = true, true, 0
	case "a":
		e.insert(restStep(50))
	case "b":
		if onStep {
			e.picking, e.pickTap, e.pickCursor = true, false, 0
		}
	case "l":
		if onStep {
			e.macro.Steps[stepIndex].Left = nextStick(e.macro.Steps[stepIndex].Left)
		}
	case "r":
		if onStep {
			e.macro.Steps[stepIndex].Right = nextStick(e.macro.Steps[stepIndex].Right)
		}
	case "backspace", "delete":
		if onStep {
			e.macro.Steps = append(e.macro.Steps[:stepIndex], e.macro.Steps[stepIndex+1:]...)
		}
	case "enter":
		pad := &m.mapping.pad
		switch e.cursor {
		case padMacroRowName:
			e.naming = true
		case padMacroHeadRows + steps: // Save
			if len(e.macro.Steps) == 0 {
				e.problem = "Add at least one step first: t taps a button."
				break
			}
			if err := e.macro.Validate(); err != nil {
				e.problem = "Can't save yet: " + err.Error() + "."
				break
			}
			m.padSnapshot()
			pad.draft.Macros[pad.slot][e.index] = clonePadMacro(e.macro)
			e.open = false
		case padMacroHeadRows + steps + 1: // Remove
			if e.existed {
				m.padSnapshot()
				pad.draft.Macros[pad.slot][e.index] = core.PadMacro{}
			}
			e.open = false
		}
	}
	return m, nil
}

// insert adds steps after the selected step, or at the end.
func (e *padMacroEditor) insert(steps ...core.PadMacroStep) {
	at := len(e.macro.Steps)
	if i := e.cursor - padMacroHeadRows; i >= 0 && i < len(e.macro.Steps) {
		at = i + 1
	}
	if len(e.macro.Steps)+len(steps) > core.PadMacroMaxSteps {
		e.problem = fmt.Sprintf("A macro holds at most %d steps.", core.PadMacroMaxSteps)
		return
	}
	e.macro.Steps = append(e.macro.Steps[:at], append(steps, e.macro.Steps[at:]...)...)
	e.cursor = padMacroHeadRows + at + len(steps) - 1
}

func (m Model) padMacroPanel(panel devicePanel, text int) devicePanel {
	e := m.mapping.pad.macro
	if e.picking {
		title := "Hold or let go of which button in this step?"
		if e.pickTap {
			title = "Tap which button?"
		}
		panel.add(-1, stylePanelTitle.Render(truncate(title, text)), "")
		for i, button := range core.PadMacroStepButtons() {
			if i == e.pickCursor {
				panel.add(i, styleSelectedRow.Render("› "+button.String()+" "))
			} else {
				panel.add(i, "  "+styleBody.Render(button.String()))
			}
		}
		return panel
	}

	panel.add(-1, stylePanelTitle.Render(truncate(fmt.Sprintf("Macro %d of slot %d", e.index+1, m.mapping.pad.slot+1), text)))
	panel.add(-1, styleFaint.Render(truncate("each step holds what it lists for its time; end with a step that holds nothing", text)), "")
	row := func(i int, label, value string) {
		line := fmt.Sprintf("%-13s → %s", label, value)
		if i == e.cursor {
			panel.add(i, styleSelectedRow.Render(truncate("› "+line+" ", text)))
		} else {
			panel.add(i, "  "+styleBody.Render(truncate(line, text-2)))
		}
	}
	name := e.macro.Name
	switch {
	case e.naming:
		name += "▏"
	case name == "":
		name = "(enter to name it)"
	}
	row(padMacroRowName, "Name", name)
	row(padMacroRowTrigger, "Played by", core.PadMotionButtonName(e.macro.Trigger))
	plays := "once"
	if e.macro.Repeat > 1 {
		plays = fmt.Sprintf("%d times", e.macro.Repeat)
	}
	row(padMacroRowRepeat, "Plays", plays)
	interval := "no pause"
	if e.macro.IntervalMillis > 0 {
		interval = fmt.Sprintf("%d ms", e.macro.IntervalMillis)
	}
	row(padMacroRowInterval, "Between plays", interval)

	steps := len(e.macro.Steps)
	visible := max(1, m.keyboardVisibleRows()-padMacroHeadRows-1)
	stepCursor := clampInt(e.cursor-padMacroHeadRows, 0, max(0, steps-1))
	start, end, _ := viewportWindow(steps, stepCursor, max(0, stepCursor-visible+1), visible)
	for i := start; i < end; i++ {
		line := fmt.Sprintf("%3d. %s", i+1, e.macro.Steps[i])
		if i == start && start > 0 {
			line += "  ↑"
		}
		if i == end-1 && end < steps {
			line += "  ↓"
		}
		if padMacroHeadRows+i == e.cursor {
			panel.add(padMacroHeadRows+i, styleSelectedRow.Render(truncate("› "+line+" ", text)))
		} else {
			panel.add(padMacroHeadRows+i, "  "+styleBody.Render(truncate(line, text-2)))
		}
	}
	panel.add(-1, "")
	remove := "Discard"
	if e.existed {
		remove = "Remove This Macro"
	}
	for i, action := range []string{"Save Macro", remove} {
		at := padMacroHeadRows + steps + i
		if at == e.cursor {
			panel.add(at, styleSelectedRow.Render("› "+action+" "))
		} else {
			panel.add(at, "  "+styleBody.Render(action))
		}
	}
	if e.problem != "" {
		panel.add(-1, "")
		panel.addWrapped(-1, styleWarning, e.problem, text)
	}
	return panel
}
