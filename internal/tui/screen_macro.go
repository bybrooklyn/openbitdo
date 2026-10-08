package tui

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// The macro editor opens over the keyboard editor for one key. It edits a
// copy; Save puts the copy into the keyboard draft, and nothing reaches the
// keyboard until the draft is applied.

// Rows above the step list.
const (
	macroRowName = iota
	macroRowRepeat
	macroRowInterval
	macroHeadRows
)

// macroAdd is what the key picker is choosing a key for.
type macroAdd int

const (
	macroAddTap macroAdd = iota
	macroAddPress
	macroAddRelease
)

type macroEditor struct {
	open  bool
	key   core.KeyboardKey
	macro core.KeyMacro
	// existed is whether the key had a macro in the draft when this opened.
	existed bool
	// cursor: the head rows, then one per step, then Save, Remove.
	cursor int
	naming bool
	// picking is true while a key is being chosen for a new step.
	picking    bool
	pickFor    macroAdd
	pickFilter string
	pickCursor int
	problem    string
}

func macrosEqual(a, b map[byte]core.KeyMacro) bool {
	if len(a) != len(b) {
		return false
	}
	for id, macro := range a {
		if other, ok := b[id]; !ok || !reflect.DeepEqual(macro, other) {
			return false
		}
	}
	return true
}

func cloneMacro(m core.KeyMacro) core.KeyMacro {
	m.Steps = append([]core.KeyMacroStep(nil), m.Steps...)
	return m
}

// openMacroEditor starts editing the macro on key, or a new one.
func (m *Model) openMacroEditor(key core.KeyboardKey) {
	editor := macroEditor{open: true, key: key}
	if macro, ok := m.mapping.kb.draft.Macros[key.ID]; ok {
		editor.macro, editor.existed = cloneMacro(macro), true
	} else {
		editor.macro = core.KeyMacro{Key: key.ID, Repeat: 1}
	}
	m.mapping.kb.macro = editor
}

func (e macroEditor) rows() int { return macroHeadRows + len(e.macro.Steps) + 2 }

// macroKeyChoices is the keys a step can press, filtered by what was typed.
func (e macroEditor) keyChoices() []byte {
	filter := strings.ToLower(strings.TrimSpace(e.pickFilter))
	var exact, prefix, contains []byte
	for _, choice := range core.KeyTargetChoices() {
		target := choice.Target
		if target.Kind != core.TargetKey {
			continue
		}
		usage := target.Key
		if usage == 0 {
			usage = target.Modifier
		}
		name := strings.ToLower(target.String())
		switch {
		case filter == "" || name == filter:
			exact = append(exact, usage)
		case strings.HasPrefix(name, filter):
			prefix = append(prefix, usage)
		case strings.Contains(name, filter):
			contains = append(contains, usage)
		}
	}
	return append(append(exact, prefix...), contains...)
}

func (m Model) updateMacroEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &m.mapping.kb.macro
	e.problem = ""
	switch {
	case e.picking:
		choices := e.keyChoices()
		switch msg.Type {
		case tea.KeyEsc:
			e.picking = false
		case tea.KeyEnter:
			if e.pickCursor < len(choices) {
				e.addSteps(choices[e.pickCursor])
			}
			e.picking = false
		case tea.KeyUp:
			e.pickCursor = clampInt(e.pickCursor-1, 0, len(choices)-1)
		case tea.KeyDown:
			e.pickCursor = clampInt(e.pickCursor+1, 0, len(choices)-1)
		case tea.KeyBackspace:
			if runes := []rune(e.pickFilter); len(runes) > 0 {
				e.pickFilter, e.pickCursor = string(runes[:len(runes)-1]), 0
			}
		case tea.KeyRunes, tea.KeySpace:
			e.pickFilter, e.pickCursor = e.pickFilter+string(msg.Runes), 0
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
			if runes := []rune(e.macro.Name + string(msg.Runes)); len(runes) <= kbNameMax {
				e.macro.Name = string(runes)
			}
		}
		return m, nil
	}

	steps := len(e.macro.Steps)
	stepIndex := e.cursor - macroHeadRows
	switch msg.String() {
	case "esc":
		e.open = false
	case "up", "k":
		e.cursor = max(0, e.cursor-1)
	case "down", "j":
		e.cursor = min(e.rows()-1, e.cursor+1)
	case "left", "right", "[", "]":
		delta := map[string]int{"left": -1, "right": 1, "[": -10, "]": 10}[msg.String()]
		switch {
		case e.cursor == macroRowRepeat:
			// 1..99, then "until pressed again".
			switch {
			case e.macro.Repeat == core.KeyMacroForever && delta < 0:
				e.macro.Repeat = 99
			case e.macro.Repeat != core.KeyMacroForever && e.macro.Repeat+delta > 99:
				e.macro.Repeat = core.KeyMacroForever
			case e.macro.Repeat != core.KeyMacroForever:
				e.macro.Repeat = max(1, e.macro.Repeat+delta)
			}
		case e.cursor == macroRowInterval:
			e.macro.IntervalMillis = clampInt(e.macro.IntervalMillis+delta*50, 0, core.KeyMacroMaxDelay)
		case stepIndex >= 0 && stepIndex < steps && e.macro.Steps[stepIndex].Kind == core.StepWait:
			step := &e.macro.Steps[stepIndex]
			step.Millis = clampInt(step.Millis+delta*10, 10, core.KeyMacroMaxDelay)
		}
	case "a":
		e.startPick(macroAddTap)
	case "p":
		e.startPick(macroAddPress)
	case "r":
		e.startPick(macroAddRelease)
	case "w":
		e.insert(core.KeyMacroStep{Kind: core.StepWait, Millis: 50})
	case "backspace", "delete":
		if stepIndex >= 0 && stepIndex < steps {
			e.macro.Steps = append(e.macro.Steps[:stepIndex], e.macro.Steps[stepIndex+1:]...)
		}
	case "enter":
		switch e.cursor {
		case macroRowName:
			e.naming = true
		case macroHeadRows + steps: // Save
			if e.macro.Name == "" {
				e.problem = "Give the macro a name first."
				break
			}
			if err := e.macro.Validate(); err != nil {
				e.problem = "Can't save yet: " + err.Error() + "."
				break
			}
			m.kbSnapshot()
			m.mapping.kb.draft.Macros[e.key.ID] = cloneMacro(e.macro)
			e.open = false
		case macroHeadRows + steps + 1: // Remove
			if e.existed {
				m.kbSnapshot()
				delete(m.mapping.kb.draft.Macros, e.key.ID)
			}
			e.open = false
		}
	}
	return m, nil
}

func (e *macroEditor) startPick(kind macroAdd) {
	e.picking, e.pickFor, e.pickFilter, e.pickCursor = true, kind, "", 0
}

// insert adds steps after the selected step, or at the end.
func (e *macroEditor) insert(steps ...core.KeyMacroStep) {
	at := len(e.macro.Steps)
	if i := e.cursor - macroHeadRows; i >= 0 && i < len(e.macro.Steps) {
		at = i + 1
	}
	if len(e.macro.Steps)+len(steps) > core.KeyMacroMaxSteps {
		e.problem = fmt.Sprintf("A macro holds at most %d steps.", core.KeyMacroMaxSteps)
		return
	}
	e.macro.Steps = append(e.macro.Steps[:at], append(steps, e.macro.Steps[at:]...)...)
	e.cursor = macroHeadRows + at + len(steps) - 1
}

func (e *macroEditor) addSteps(usage byte) {
	switch e.pickFor {
	case macroAddTap:
		e.insert(core.KeyMacroStep{Kind: core.StepPress, Usage: usage}, core.KeyMacroStep{Kind: core.StepRelease, Usage: usage})
	case macroAddPress:
		e.insert(core.KeyMacroStep{Kind: core.StepPress, Usage: usage})
	case macroAddRelease:
		e.insert(core.KeyMacroStep{Kind: core.StepRelease, Usage: usage})
	}
}

func (m Model) macroPanel(panel devicePanel, text int) devicePanel {
	e := m.mapping.kb.macro
	if e.picking {
		verb := map[macroAdd]string{macroAddTap: "Tap", macroAddPress: "Hold down", macroAddRelease: "Let go of"}[e.pickFor]
		panel.add(-1, stylePanelTitle.Render(truncate(verb+" which key?", text)))
		filter := styleFaint.Render("type to search")
		if e.pickFilter != "" {
			filter = styleAccent.Render(e.pickFilter + "▏")
		}
		panel.add(-1, styleBody.Render("Find: ")+filter, "")
		choices := e.keyChoices()
		if len(choices) == 0 {
			panel.add(-1, styleFaint.Render("Nothing matches."))
			return panel
		}
		cursor := clampInt(e.pickCursor, 0, len(choices)-1)
		visible := m.pickerVisibleRows() + 1
		start, end, _ := viewportWindow(len(choices), cursor, max(0, cursor-visible/2), visible)
		for i := start; i < end; i++ {
			line := core.KeyTargetKeyOf(choices[i]).String()
			if i == cursor {
				panel.add(i, styleSelectedRow.Render(truncate("› "+line+" ", text)))
			} else {
				panel.add(i, "  "+styleBody.Render(truncate(line, text-2)))
			}
		}
		return panel
	}

	panel.add(-1, stylePanelTitle.Render(truncate("Macro on "+e.key.Name, text)))
	summary := e.macro.Summary()
	if len(e.macro.Steps) == 0 {
		summary = "empty: a adds a key tap, w a pause"
	}
	panel.add(-1, styleFaint.Render(truncate(summary, text)), "")

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
	row(macroRowName, "Name", name)
	repeat := "once"
	switch {
	case e.macro.Repeat == core.KeyMacroForever:
		repeat = "until the key is pressed again"
	case e.macro.Repeat > 1:
		repeat = fmt.Sprintf("%d times", e.macro.Repeat)
	}
	row(macroRowRepeat, "Plays", repeat)
	interval := "no pause"
	if e.macro.IntervalMillis > 0 {
		interval = fmt.Sprintf("%d ms", e.macro.IntervalMillis)
	}
	row(macroRowInterval, "Between plays", interval)

	steps := len(e.macro.Steps)
	visible := max(1, m.keyboardVisibleRows()-macroHeadRows-1)
	stepCursor := clampInt(e.cursor-macroHeadRows, 0, max(0, steps-1))
	start, end, _ := viewportWindow(steps, stepCursor, max(0, stepCursor-visible+1), visible)
	for i := start; i < end; i++ {
		line := fmt.Sprintf("%3d. %s", i+1, e.macro.Steps[i])
		if i == start && start > 0 {
			line += "  ↑"
		}
		if i == end-1 && end < steps {
			line += "  ↓"
		}
		if macroHeadRows+i == e.cursor {
			panel.add(macroHeadRows+i, styleSelectedRow.Render(truncate("› "+line+" ", text)))
		} else {
			panel.add(macroHeadRows+i, "  "+styleBody.Render(truncate(line, text-2)))
		}
	}

	panel.add(-1, "")
	remove := "Discard"
	if e.existed {
		remove = "Remove This Macro"
	}
	for i, action := range []string{"Save Macro", remove} {
		at := macroHeadRows + steps + i
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
