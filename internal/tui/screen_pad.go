package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// The controller editor is the Mapping tab for a controller: one of its
// three profile slots at a time, with the slot's name, button map, stick and
// trigger ranges, vibration strength and option switches. Edits collect in
// a draft and nothing is written until Apply.

type padLoadedMsg struct {
	profile core.PadProfile
	err     error
}

type padApplyResultMsg struct {
	report core.WriteRecoveryReport
	err    error
}

func cmdPadRead(ctx context.Context, c *core.OpenBitdoCore, target protocol.VidPid) tea.Cmd {
	return func() tea.Msg {
		profile, err := c.PadReadProfile(ctx, target)
		return padLoadedMsg{profile: profile, err: err}
	}
}

func cmdPadApply(ctx context.Context, c *core.OpenBitdoCore, target protocol.VidPid, profile core.PadProfile) tea.Cmd {
	return func() tea.Msg {
		report, err := c.PadApply(ctx, target, profile)
		return padApplyResultMsg{report: report, err: err}
	}
}

// padRowKind is what one row of the editor edits.
type padRowKind int

const (
	padRowSlot padRowKind = iota
	padRowName
	padRowButton
	padRowRange
	padRowVibration
	padRowOption
)

type padRow struct {
	kind  padRowKind
	label string
	index int    // button index; range or vibration selector
	end   bool   // for a range row: the End value rather than Start
	max   int    // for a range row: the largest End
	bit   uint32 // for an option row
}

// Range and vibration selectors.
const (
	padLeftStick = iota
	padRightStick
	padLeftTrigger
	padRightTrigger
)

// padRows is the editor's rows in order.
var padRows = buildPadRows()

func buildPadRows() []padRow {
	rows := []padRow{{kind: padRowSlot, label: "Slot"}, {kind: padRowName, label: "Profile name"}}
	// The back paddles and extra buttons come first: they do nothing until
	// assigned, so they are what most people come to set.
	for _, i := range []int{18, 19, 20, 21} {
		rows = append(rows, padRow{kind: padRowButton, label: core.PadInputs[i].Name, index: i})
	}
	for i := 0; i < 18; i++ {
		rows = append(rows, padRow{kind: padRowButton, label: core.PadInputs[i].Name, index: i})
	}
	for _, r := range []struct {
		name  string
		index int
		max   int
	}{{"Left stick", padLeftStick, 128}, {"Right stick", padRightStick, 128},
		{"Left trigger", padLeftTrigger, 255}, {"Right trigger", padRightTrigger, 255}} {
		rows = append(rows,
			padRow{kind: padRowRange, label: r.name + " starts at", index: r.index, max: r.max},
			padRow{kind: padRowRange, label: r.name + " full at", index: r.index, max: r.max, end: true})
	}
	rows = append(rows,
		padRow{kind: padRowVibration, label: "Left vibration", index: 0},
		padRow{kind: padRowVibration, label: "Right vibration", index: 1})
	for _, option := range []struct {
		name string
		bit  uint32
	}{
		{"Invert left stick X", core.PadInvertLeftX}, {"Invert left stick Y", core.PadInvertLeftY},
		{"Invert right stick X", core.PadInvertRightX}, {"Invert right stick Y", core.PadInvertRightY},
		{"Swap the sticks", core.PadSwapSticks}, {"Swap the triggers", core.PadSwapTriggers},
		{"Swap d-pad and left stick", core.PadSwapDpadStick},
	} {
		rows = append(rows, padRow{kind: padRowOption, label: option.name, bit: option.bit})
	}
	return rows
}

type padEditor struct {
	loaded core.PadProfile
	draft  core.PadProfile
	undo   []core.PadProfile
	// slot is the slot being edited, 0-2.
	slot int

	// The function picker, open over the editor while a button is assigned.
	picking    bool
	pickButton int
	pickFilter string
	pickCursor int

	naming    bool
	nameInput string
}

// padNameMax is the longest profile name a slot holds, in characters.
const padNameMax = 16

func (s padEditor) dirty() bool { return s.draft.Slots != s.loaded.Slots }

// padEditing reports whether the Mapping tab is showing the controller
// editor.
func (m Model) padEditing() bool {
	return m.screen == screenMapping && m.mapping.kind == core.KindUltimate2 && m.mapping.unavailable == ""
}

func (m *Model) padSnapshot() { m.mapping.pad.undo = append(m.mapping.pad.undo, m.mapping.pad.draft) }

func (m *Model) padSlot() *core.PadSlot { return &m.mapping.pad.draft.Slots[m.mapping.pad.slot] }

func padRangeOf(slot *core.PadSlot, index int) *core.PadRange {
	switch index {
	case padLeftStick:
		return &slot.LeftStick
	case padRightStick:
		return &slot.RightStick
	case padLeftTrigger:
		return &slot.LeftTrigger
	}
	return &slot.RightTrigger
}

func (m Model) updatePad(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case padLoadedMsg:
		m.mapping.loading = false
		m.mapping.err = msg.err
		if msg.err == nil {
			m.mapping.pad = padEditor{loaded: msg.profile, draft: msg.profile, slot: msg.profile.ActiveSlot}
		}
		return m, nil

	case padApplyResultMsg:
		return m.handleMappingApplyResult(msg.report, msg.err)

	case tea.KeyMsg:
		if m.mapping.loading || m.mapping.err != nil {
			if msg.String() == "esc" {
				m.screen = screenDevices
			}
			return m, nil
		}
		if m.mapping.pad.picking {
			return m.updatePadPicker(msg)
		}
		if m.mapping.pad.naming {
			return m.updatePadName(msg)
		}
		if m.mapping.files.open {
			return m.updateProfileFiles(msg)
		}
		switch msg.String() {
		case "E":
			m.saveProfileFile()
			return m, nil
		case "I":
			m.openProfileFiles()
			return m, nil
		}
		rows := len(padRows)
		switch msg.String() {
		case "esc":
			if m.mapping.dirty() {
				m.modal = discardMappingModal(discardMappingMsg{action: discardActionBack})
				return m, nil
			}
			m.screen = screenDevices
		case "up", "k":
			m.mapping.cursor = max(0, m.mapping.cursor-1)
			m.ensurePadCursorVisible()
		case "down", "j":
			m.mapping.cursor = min(rows+2, m.mapping.cursor+1)
			m.ensurePadCursorVisible()
		case "pgdown":
			m.mapping.cursor = min(rows+2, m.mapping.cursor+m.padVisibleRows())
			m.ensurePadCursorVisible()
		case "pgup":
			m.mapping.cursor = max(0, m.mapping.cursor-m.padVisibleRows())
			m.ensurePadCursorVisible()
		case "left", "right", "[", "]":
			if m.mapping.cursor < rows {
				delta := map[string]int{"left": -1, "right": 1, "[": -10, "]": 10}[msg.String()]
				m.padAdjustRow(padRows[m.mapping.cursor], delta)
			}
		case "backspace", "delete":
			if m.mapping.cursor < rows && padRows[m.mapping.cursor].kind == padRowButton {
				row := padRows[m.mapping.cursor]
				m.padSetButton(row.index, core.PadProfile{Platform: m.mapping.pad.draft.Platform}.DefaultTarget(row.index))
			}
		case "enter":
			return m.triggerPadRow()
		}
	}
	return m, nil
}

func (m *Model) padSetButton(index int, target core.PadTarget) {
	if m.padSlot().Buttons[index] == target {
		return
	}
	m.padSnapshot()
	m.padSlot().Buttons[index] = target
}

// padAdjustRow changes a row's value by delta steps.
func (m *Model) padAdjustRow(row padRow, delta int) {
	pad := &m.mapping.pad
	switch row.kind {
	case padRowSlot:
		// Which slot is shown is not an edit.
		pad.slot = ((pad.slot+delta)%core.PadSlots + core.PadSlots) % core.PadSlots
	case padRowButton:
		targets := core.PadTargets()
		index := 0
		for i, target := range targets {
			if target == m.padSlot().Buttons[row.index] {
				index = i
				break
			}
		}
		step := 1
		if delta < 0 {
			step = -1
		}
		m.padSetButton(row.index, targets[((index+step)%len(targets)+len(targets))%len(targets)])
	case padRowRange:
		current := *padRangeOf(m.padSlot(), row.index)
		next := current
		if row.end {
			next.End = byte(clampInt(int(current.End)+delta, int(current.Start)+1, row.max))
		} else {
			next.Start = byte(clampInt(int(current.Start)+delta, 0, int(current.End)-1))
		}
		if next != current {
			m.padSnapshot()
			*padRangeOf(m.padSlot(), row.index) = next
		}
	case padRowVibration:
		level := &m.padSlot().VibrationLeft
		if row.index == 1 {
			level = &m.padSlot().VibrationRight
		}
		step := 1
		if delta < 0 {
			step = -1
		}
		if next := clampInt(*level+step, 0, 5); next != *level {
			m.padSnapshot()
			if row.index == 1 {
				m.padSlot().VibrationRight = next
			} else {
				m.padSlot().VibrationLeft = next
			}
		}
	case padRowOption:
		m.padSnapshot()
		slot := m.padSlot()
		slot.Options ^= row.bit
		// The controller cannot swap the d-pad with a left stick that is
		// itself swapped or inverted; turning one on turns the other off.
		const stickEdits = core.PadSwapSticks | core.PadInvertLeftX | core.PadInvertLeftY
		if slot.Options&row.bit != 0 {
			if row.bit == core.PadSwapDpadStick {
				slot.Options &^= stickEdits
			} else if row.bit&stickEdits != 0 {
				slot.Options &^= core.PadSwapDpadStick
			}
		}
	}
}

func (m Model) triggerPadRow() (tea.Model, tea.Cmd) {
	rows := len(padRows)
	pad := &m.mapping.pad
	switch {
	case m.mapping.cursor < rows:
		row := padRows[m.mapping.cursor]
		switch row.kind {
		case padRowButton:
			pad.picking, pad.pickButton, pad.pickFilter, pad.pickCursor = true, row.index, "", 0
		case padRowName:
			pad.naming, pad.nameInput = true, m.padSlot().Name
		default:
			m.padAdjustRow(row, 1)
		}
	case m.mapping.cursor == rows: // Apply
		if !pad.dirty() || m.mapping.applying {
			return m, nil
		}
		m.mapping.applying = true
		return m, cmdPadApply(m.ctx, m.core, m.mapping.device.VidPid, pad.draft)
	case m.mapping.cursor == rows+1: // Undo
		if n := len(pad.undo); n > 0 {
			pad.draft, pad.undo = pad.undo[n-1], pad.undo[:n-1]
			m.mapping.statusMsg = "Last edit undone."
		}
	case m.mapping.cursor == rows+2: // Reset
		m.padSnapshot()
		pad.draft.Slots = pad.loaded.Slots
		m.mapping.statusMsg = "Draft reset."
	}
	return m, nil
}

func (m Model) updatePadName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pad := &m.mapping.pad
	switch msg.Type {
	case tea.KeyEsc:
		pad.naming = false
	case tea.KeyEnter:
		pad.naming = false
		if name := strings.TrimSpace(pad.nameInput); name != m.padSlot().Name {
			m.padSnapshot()
			m.padSlot().Name = name
		}
	case tea.KeyBackspace:
		if runes := []rune(pad.nameInput); len(runes) > 0 {
			pad.nameInput = string(runes[:len(runes)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		if runes := []rune(pad.nameInput + string(msg.Runes)); len(runes) <= padNameMax {
			pad.nameInput = string(runes)
		}
	}
	return m, nil
}

// padPickerChoices is the function list filtered by what has been typed.
func (m Model) padPickerChoices() []core.PadTarget {
	all := core.PadTargets()
	filter := strings.ToLower(strings.TrimSpace(m.mapping.pad.pickFilter))
	if filter == "" {
		return all
	}
	var exact, prefix, contains []core.PadTarget
	for _, target := range all {
		name := strings.ToLower(target.String())
		switch {
		case name == filter:
			exact = append(exact, target)
		case strings.HasPrefix(name, filter):
			prefix = append(prefix, target)
		case strings.Contains(name, filter):
			contains = append(contains, target)
		}
	}
	return append(append(exact, prefix...), contains...)
}

func (m Model) updatePadPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pad := &m.mapping.pad
	choices := m.padPickerChoices()
	switch msg.Type {
	case tea.KeyEsc:
		pad.picking = false
	case tea.KeyEnter:
		if pad.pickCursor < len(choices) {
			m.padSetButton(pad.pickButton, choices[pad.pickCursor])
		}
		pad.picking = false
	case tea.KeyUp:
		pad.pickCursor = clampInt(pad.pickCursor-1, 0, len(choices)-1)
	case tea.KeyDown:
		pad.pickCursor = clampInt(pad.pickCursor+1, 0, len(choices)-1)
	case tea.KeyPgUp:
		pad.pickCursor = clampInt(pad.pickCursor-m.pickerVisibleRows(), 0, len(choices)-1)
	case tea.KeyPgDown:
		pad.pickCursor = clampInt(pad.pickCursor+m.pickerVisibleRows(), 0, len(choices)-1)
	case tea.KeyBackspace:
		if runes := []rune(pad.pickFilter); len(runes) > 0 {
			pad.pickFilter, pad.pickCursor = string(runes[:len(runes)-1]), 0
		}
	case tea.KeyRunes, tea.KeySpace:
		pad.pickFilter, pad.pickCursor = pad.pickFilter+string(msg.Runes), 0
	}
	return m, nil
}

func (m Model) padVisibleRows() int { return m.keyboardVisibleRows() }

func (m *Model) ensurePadCursorVisible() {
	if m.mapping.cursor >= len(padRows) {
		return
	}
	start, _, _ := viewportWindow(len(padRows), m.mapping.cursor, m.mapping.rowOffset, m.padVisibleRows())
	m.mapping.rowOffset = start
}

// padRowText is a row's value as plain text, and whether it differs from
// what the controller holds.
func (m Model) padRowText(row padRow) (value string, changed bool) {
	pad := m.mapping.pad
	now, was := pad.draft.Slots[pad.slot], pad.loaded.Slots[pad.slot]
	switch row.kind {
	case padRowSlot:
		value = fmt.Sprintf("◂ %d of %d ▸", pad.slot+1, core.PadSlots)
		if pad.slot == pad.draft.ActiveSlot {
			value += "  (the one in use)"
		}
		return value, false
	case padRowName:
		if pad.naming {
			return pad.nameInput + "▏", true
		}
		if now.Name == "" {
			return "(unnamed)", now.Name != was.Name
		}
		return now.Name, now.Name != was.Name
	case padRowButton:
		return now.Buttons[row.index].String(), now.Buttons[row.index] != was.Buttons[row.index]
	case padRowRange:
		a, b := *padRangeOf(&now, row.index), *padRangeOf(&was, row.index)
		if row.end {
			return fmt.Sprintf("%d%%", int(a.End)*100/row.max), a.End != b.End
		}
		return fmt.Sprintf("%d%%", int(a.Start)*100/row.max), a.Start != b.Start
	case padRowVibration:
		a, b := now.VibrationLeft, was.VibrationLeft
		if row.index == 1 {
			a, b = now.VibrationRight, was.VibrationRight
		}
		if a == 0 {
			return "off", a != b
		}
		return fmt.Sprintf("%d of 5", a), a != b
	}
	on := "off"
	if now.Options&row.bit != 0 {
		on = "on"
	}
	return on, now.Options&row.bit != was.Options&row.bit
}

func (m Model) padPanel(height int) devicePanel {
	panel := devicePanel{width: m.width - 2, keep: -1}
	panel.area = rect{x: 0, y: calculateLayout(m.width, m.height).headerHeight, w: m.width, h: max(1, height-2)}
	text := max(1, m.width-4)
	pad := m.mapping.pad

	if pad.picking {
		return m.padPickerPanel(panel, text)
	}
	if m.mapping.files.open {
		return m.profileFilesPanel(panel, text)
	}

	platform := "DInput"
	switch pad.draft.Platform {
	case protocol.U2PlatformXInput:
		platform = "XInput"
	case protocol.U2PlatformSwitch:
		platform = "Switch"
	}
	panel.add(-1,
		stylePanelTitle.Render("Controller profile"),
		styleFaint.Render(truncate("for the "+platform+" position of the mode switch", text)),
		"")

	rows := len(padRows)
	start, end, _ := viewportWindow(rows, min(m.mapping.cursor, rows-1), m.mapping.rowOffset, m.padVisibleRows())
	for i := start; i < end; i++ {
		value, changed := m.padRowText(padRows[i])
		mark := " "
		if changed {
			mark = "*"
		}
		line := fmt.Sprintf("%s%-25s → %s", mark, truncate(padRows[i].label, 25), value)
		if i == start && start > 0 {
			line += "  ↑"
		}
		if i == end-1 && end < rows {
			line += "  ↓"
		}
		if i == m.mapping.cursor {
			panel.add(i, styleSelectedRow.Render(truncate("›"+line+" ", text)))
		} else {
			panel.add(i, " "+styleBody.Render(truncate(line, text-1)))
		}
	}

	panel.add(-1, "")
	actions := []string{"Apply Changes", "Undo Last Edit", "Reset Draft"}
	notes := []string{"", "", ""}
	if !pad.dirty() {
		notes[0] = "  (no changes)"
	}
	if m.mapping.applying {
		actions[0], notes[0] = "Applying…", ""
	}
	if len(pad.undo) == 0 {
		notes[1] = "  (nothing to undo)"
	}
	for i, action := range actions {
		if m.mapping.cursor == rows+i {
			panel.add(rows+i, styleSelectedRow.Render("› "+action+notes[i]+" "))
		} else {
			panel.add(rows+i, "  "+styleBody.Render(action)+styleFaint.Render(notes[i]))
		}
	}
	if m.mapping.statusMsg != "" {
		panel.add(-1, "")
		panel.addWrapped(-1, styleFaint, m.mapping.statusMsg, text)
	}
	return panel
}

func (m Model) padPickerPanel(panel devicePanel, text int) devicePanel {
	pad := m.mapping.pad
	choices := m.padPickerChoices()
	panel.add(-1, stylePanelTitle.Render(truncate("Assign "+core.PadInputs[pad.pickButton].Name, text)))
	filter := styleFaint.Render("type to search")
	if pad.pickFilter != "" {
		filter = styleAccent.Render(pad.pickFilter + "▏")
	}
	panel.add(-1, styleBody.Render("Find: ")+filter, "")
	if len(choices) == 0 {
		panel.add(-1, styleFaint.Render("Nothing matches."))
		return panel
	}
	cursor := clampInt(pad.pickCursor, 0, len(choices)-1)
	visible := m.pickerVisibleRows() + 1
	start, end, _ := viewportWindow(len(choices), cursor, max(0, cursor-visible/2), visible)
	for i := start; i < end; i++ {
		line := choices[i].String()
		if i == start && start > 0 {
			line += "  ↑"
		}
		if i == end-1 && end < len(choices) {
			line += "  ↓"
		}
		if i == cursor {
			panel.add(i, styleSelectedRow.Render(truncate("› "+line+" ", text)))
		} else {
			panel.add(i, "  "+styleBody.Render(truncate(line, text-2)))
		}
	}
	return panel
}

func (m Model) viewPad(height int) string {
	text := max(1, m.width-4)
	if m.mapping.loading {
		return renderBoundedPanel(m.width-2, height-2,
			stylePanelTitle.Render("Controller profile")+"\n\n"+styleFaint.Render("Reading the controller's profile…"))
	}
	if m.mapping.err != nil {
		var b strings.Builder
		b.WriteString(stylePanelTitle.Render("Controller profile") + "\n\n")
		b.WriteString(styleDanger.Render("The profile could not be read.") + "\n")
		b.WriteString(wrapStyled(styleFaint, m.mapping.err.Error(), text) + "\n\n")
		b.WriteString(styleFaint.Render("Nothing was changed on the controller."))
		return renderBoundedPanel(m.width-2, height-2, b.String())
	}
	return m.padPanel(height).render()
}

func (m Model) clickPad(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mapping.loading || m.mapping.err != nil || m.mapping.files.open {
		return m, nil
	}
	owner, ok := m.padPanel(m.height-3).ownerAt(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	if m.mapping.pad.picking {
		if choices := m.padPickerChoices(); owner < len(choices) {
			m.padSetButton(m.mapping.pad.pickButton, choices[owner])
			m.mapping.pad.picking = false
		}
		return m, nil
	}
	m.mapping.cursor = owner
	if owner >= len(padRows) {
		return m.triggerPadRow()
	}
	return m, nil
}
