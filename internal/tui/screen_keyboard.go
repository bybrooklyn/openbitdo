package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// The keyboard editor is the Mapping tab for a keyboard: every assignable
// key with what it is set to, the lock options and the volume level. Edits
// collect in a draft and nothing is written until Apply.

type keyboardLoadedMsg struct {
	profile core.KeyboardProfile
	err     error
}

type keyboardApplyResultMsg struct {
	report core.WriteRecoveryReport
	err    error
}

func cmdKeyboardRead(ctx context.Context, c *core.OpenBitdoCore, target protocol.VidPid) tea.Cmd {
	return func() tea.Msg {
		profile, err := c.KeyboardReadProfile(ctx, target)
		return keyboardLoadedMsg{profile: profile, err: err}
	}
}

func cmdKeyboardApply(ctx context.Context, c *core.OpenBitdoCore, target protocol.VidPid, changes core.KeyboardChanges) tea.Cmd {
	return func() tea.Msg {
		report, err := c.KeyboardApply(ctx, target, changes)
		return keyboardApplyResultMsg{report: report, err: err}
	}
}

// kbRowKind is what one row of the editor edits.
type kbRowKind int

const (
	kbRowKey kbRowKind = iota
	kbRowLockWin
	kbRowLockAltTab
	kbRowLockAltF4
	kbRowVolume
)

type kbRow struct {
	kind kbRowKind
	key  core.KeyboardKey
}

// keyboardRows is the editor's rows in order: the ten dedicated buttons,
// then the keyboard-wide settings, then every ordinary key.
var keyboardRows = buildKeyboardRows()

func buildKeyboardRows() []kbRow {
	var rows []kbRow
	for _, key := range core.Retro108Keys {
		if key.Dedicated {
			rows = append(rows, kbRow{kind: kbRowKey, key: key})
		}
	}
	rows = append(rows, kbRow{kind: kbRowLockWin}, kbRow{kind: kbRowLockAltTab}, kbRow{kind: kbRowLockAltF4}, kbRow{kind: kbRowVolume})
	for _, key := range core.Retro108Keys {
		if !key.Dedicated {
			rows = append(rows, kbRow{kind: kbRowKey, key: key})
		}
	}
	return rows
}

type keyboardState struct {
	loaded core.KeyboardProfile
	draft  core.KeyboardProfile
	undo   []core.KeyboardProfile

	// The target picker, open over the editor while a key is being assigned.
	picking      bool
	pickKey      core.KeyboardKey
	pickFilter   string
	pickCursor   int
	pickModifier int // index into core.ModifierChoices
}

func cloneKeyboardProfile(p core.KeyboardProfile) core.KeyboardProfile {
	out := p
	out.Mappings = make(map[byte]core.KeyTarget, len(p.Mappings))
	for id, target := range p.Mappings {
		out.Mappings[id] = target
	}
	return out
}

// changes is the difference between the draft and what the keyboard holds.
func (s keyboardState) changes() core.KeyboardChanges {
	changes := core.KeyboardChanges{Mappings: map[byte]core.KeyTarget{}}
	for _, key := range core.Retro108Keys {
		was, _ := s.loaded.Target(key)
		now, _ := s.draft.Target(key)
		if was != now {
			changes.Mappings[key.ID] = now
		}
	}
	if s.draft.Locks != s.loaded.Locks {
		locks := s.draft.Locks
		changes.Locks = &locks
	}
	if s.draft.Volume != s.loaded.Volume {
		volume := s.draft.Volume
		changes.Volume = &volume
	}
	return changes
}

func (s keyboardState) dirty() bool { return !s.changes().Empty() }

// keyboardEditing reports whether the Mapping tab is showing the keyboard
// editor (as opposed to a controller's editor, or the "not available" page).
func (m Model) keyboardEditing() bool {
	return m.screen == screenMapping && m.mapping.kind == core.KindJP108 && m.mapping.unavailable == ""
}

func (m *Model) kbSnapshot() {
	m.mapping.kb.undo = append(m.mapping.kb.undo, cloneKeyboardProfile(m.mapping.kb.draft))
}

// kbSetTarget assigns a key in the draft. Assigning a key its own default
// removes the entry, so "back to normal" is not counted as a change.
func (m *Model) kbSetTarget(key core.KeyboardKey, target core.KeyTarget) {
	m.kbSnapshot()
	if target == key.Default() {
		if was, explicit := m.mapping.kb.loaded.Target(key); explicit && was != target {
			m.mapping.kb.draft.Mappings[key.ID] = target
		} else {
			delete(m.mapping.kb.draft.Mappings, key.ID)
		}
		return
	}
	m.mapping.kb.draft.Mappings[key.ID] = target
}

func (m Model) updateKeyboard(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case keyboardLoadedMsg:
		m.mapping.loading = false
		m.mapping.err = msg.err
		if msg.err == nil {
			m.mapping.kb.loaded = cloneKeyboardProfile(msg.profile)
			m.mapping.kb.draft = cloneKeyboardProfile(msg.profile)
			m.mapping.kb.undo = nil
		}
		return m, nil

	case keyboardApplyResultMsg:
		return m.handleMappingApplyResult(msg.report, msg.err)

	case tea.KeyMsg:
		if m.mapping.loading || m.mapping.err != nil {
			if msg.String() == "esc" {
				m.screen = screenDevices
			}
			return m, nil
		}
		if m.mapping.kb.picking {
			return m.updateKeyboardPicker(msg)
		}
		rows := len(keyboardRows)
		switch msg.String() {
		case "esc":
			if m.mapping.dirty() {
				m.modal = discardMappingModal(discardMappingMsg{action: discardActionBack})
				return m, nil
			}
			m.screen = screenDevices
		case "up", "k":
			if m.mapping.cursor > 0 {
				m.mapping.cursor--
				m.ensureKeyboardCursorVisible()
			}
		case "down", "j":
			if m.mapping.cursor < rows+3-1 {
				m.mapping.cursor++
				m.ensureKeyboardCursorVisible()
			}
		case "pgdown":
			m.mapping.cursor = clampInt(m.mapping.cursor+m.keyboardVisibleRows(), 0, rows+2)
			m.ensureKeyboardCursorVisible()
		case "pgup":
			m.mapping.cursor = clampInt(m.mapping.cursor-m.keyboardVisibleRows(), 0, rows+2)
			m.ensureKeyboardCursorVisible()
		case "left", "right":
			if m.mapping.cursor < rows {
				delta := 1
				if msg.String() == "left" {
					delta = -1
				}
				m.kbAdjustRow(keyboardRows[m.mapping.cursor], delta)
			}
		case "backspace", "delete":
			if m.mapping.cursor < rows && keyboardRows[m.mapping.cursor].kind == kbRowKey {
				key := keyboardRows[m.mapping.cursor].key
				m.kbSetTarget(key, key.Default())
			}
		case "enter":
			return m.triggerKeyboardRow()
		}
	}
	return m, nil
}

// kbAdjustRow changes a row's value with left/right: a setting steps or
// toggles; a key steps through the target list.
func (m *Model) kbAdjustRow(row kbRow, delta int) {
	draft := &m.mapping.kb.draft
	switch row.kind {
	case kbRowLockWin:
		m.kbSnapshot()
		draft.Locks.WinKey = !draft.Locks.WinKey
	case kbRowLockAltTab:
		m.kbSnapshot()
		draft.Locks.AltTab = !draft.Locks.AltTab
	case kbRowLockAltF4:
		m.kbSnapshot()
		draft.Locks.AltF4 = !draft.Locks.AltF4
	case kbRowVolume:
		if next := clampInt(draft.Volume+delta, 1, 5); next != draft.Volume {
			m.kbSnapshot()
			draft.Volume = next
		}
	case kbRowKey:
		choices := core.KeyTargetChoices()
		current, _ := draft.Target(row.key)
		index := 0
		for i, choice := range choices {
			if choice.Target == current {
				index = i
				break
			}
		}
		index = ((index+delta)%len(choices) + len(choices)) % len(choices)
		m.kbSetTarget(row.key, choices[index].Target)
	}
}

func (m Model) triggerKeyboardRow() (tea.Model, tea.Cmd) {
	rows := len(keyboardRows)
	switch {
	case m.mapping.cursor < rows:
		row := keyboardRows[m.mapping.cursor]
		if row.kind != kbRowKey {
			m.kbAdjustRow(row, 1)
			return m, nil
		}
		m.mapping.kb.picking = true
		m.mapping.kb.pickKey = row.key
		m.mapping.kb.pickFilter = ""
		m.mapping.kb.pickCursor = 0
		m.mapping.kb.pickModifier = 0
	case m.mapping.cursor == rows: // Apply
		if !m.mapping.dirty() || m.mapping.applying {
			return m, nil
		}
		m.mapping.applying = true
		return m, cmdKeyboardApply(m.ctx, m.core, m.mapping.device.VidPid, m.mapping.kb.changes())
	case m.mapping.cursor == rows+1: // Undo
		if n := len(m.mapping.kb.undo); n > 0 {
			m.mapping.kb.draft = m.mapping.kb.undo[n-1]
			m.mapping.kb.undo = m.mapping.kb.undo[:n-1]
			m.mapping.statusMsg = "Last edit undone."
		}
	case m.mapping.cursor == rows+2: // Reset
		// A reset is itself undoable.
		m.kbSnapshot()
		m.mapping.kb.draft = cloneKeyboardProfile(m.mapping.kb.loaded)
		m.mapping.statusMsg = "Draft reset."
	}
	return m, nil
}

// pickerChoices is the target list filtered by what has been typed.
func (m Model) pickerChoices() []core.KeyTargetChoice {
	all := core.KeyTargetChoices()
	filter := strings.ToLower(strings.TrimSpace(m.mapping.kb.pickFilter))
	if filter == "" {
		return all
	}
	var exact, prefix, contains []core.KeyTargetChoice
	for _, choice := range all {
		name := strings.ToLower(choice.Target.String())
		switch {
		case name == filter:
			exact = append(exact, choice)
		case strings.HasPrefix(name, filter):
			prefix = append(prefix, choice)
		case strings.Contains(name, filter):
			contains = append(contains, choice)
		}
	}
	// Typing "a" should offer the A key before "Backspace".
	return append(append(exact, prefix...), contains...)
}

// pickedTarget applies the chosen modifier to a key choice. Media keys and
// mouse actions take no modifier.
func (m Model) pickedTarget(choice core.KeyTargetChoice) core.KeyTarget {
	target := choice.Target
	modifier := core.ModifierChoices[m.mapping.kb.pickModifier]
	if modifier != 0 && target.Kind == core.TargetKey && target.Key != 0 {
		target.Modifier = modifier
	}
	return target
}

func (m Model) updateKeyboardPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	kb := &m.mapping.kb
	choices := m.pickerChoices()
	switch msg.Type {
	case tea.KeyEsc:
		kb.picking = false
	case tea.KeyEnter:
		if kb.pickCursor < len(choices) {
			m.kbSetTarget(kb.pickKey, m.pickedTarget(choices[kb.pickCursor]))
		}
		kb.picking = false
	case tea.KeyUp:
		kb.pickCursor = clampInt(kb.pickCursor-1, 0, len(choices)-1)
	case tea.KeyDown:
		kb.pickCursor = clampInt(kb.pickCursor+1, 0, len(choices)-1)
	case tea.KeyPgUp:
		kb.pickCursor = clampInt(kb.pickCursor-m.pickerVisibleRows(), 0, len(choices)-1)
	case tea.KeyPgDown:
		kb.pickCursor = clampInt(kb.pickCursor+m.pickerVisibleRows(), 0, len(choices)-1)
	case tea.KeyTab:
		kb.pickModifier = (kb.pickModifier + 1) % len(core.ModifierChoices)
	case tea.KeyBackspace:
		if runes := []rune(kb.pickFilter); len(runes) > 0 {
			kb.pickFilter = string(runes[:len(runes)-1])
			kb.pickCursor = 0
		}
	case tea.KeyRunes, tea.KeySpace:
		kb.pickFilter += string(msg.Runes)
		kb.pickCursor = 0
	}
	return m, nil
}

// Lines the editor draws above its rows (title, profile line, blank) and
// below them (blank, three actions, blank, status).
const (
	keyboardHeaderLines = 3
	keyboardFooterLines = 6
)

func (m Model) keyboardVisibleRows() int {
	panel := max(1, calculateLayout(m.width, m.height).bodyHeight-2)
	return max(1, panel-keyboardHeaderLines-keyboardFooterLines)
}

func (m Model) pickerVisibleRows() int {
	panel := max(1, calculateLayout(m.width, m.height).bodyHeight-2)
	return max(1, panel-6)
}

func (m *Model) ensureKeyboardCursorVisible() {
	if m.mapping.cursor >= len(keyboardRows) {
		return
	}
	start, _, _ := viewportWindow(len(keyboardRows), m.mapping.cursor, m.mapping.rowOffset, m.keyboardVisibleRows())
	m.mapping.rowOffset = start
}

// keyboardRowText is a row's label and value as plain text.
func (m Model) keyboardRowText(row kbRow) (label, value string, changed bool) {
	kb := m.mapping.kb
	onOff := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	switch row.kind {
	case kbRowLockWin:
		return "Lock Win key", onOff(kb.draft.Locks.WinKey), kb.draft.Locks.WinKey != kb.loaded.Locks.WinKey
	case kbRowLockAltTab:
		return "Lock Alt+Tab", onOff(kb.draft.Locks.AltTab), kb.draft.Locks.AltTab != kb.loaded.Locks.AltTab
	case kbRowLockAltF4:
		return "Lock Alt+F4", onOff(kb.draft.Locks.AltF4), kb.draft.Locks.AltF4 != kb.loaded.Locks.AltF4
	case kbRowVolume:
		return "Volume", fmt.Sprintf("%d of 5", kb.draft.Volume), kb.draft.Volume != kb.loaded.Volume
	}
	now, explicit := kb.draft.Target(row.key)
	was, _ := kb.loaded.Target(row.key)
	value = now.String()
	if !explicit && !row.key.Dedicated {
		value = "itself"
	}
	return row.key.Name, value, now != was
}

// keyboardPanel lays the editor out as lines with the row each one belongs
// to, for drawing and for mouse hit-testing alike.
func (m Model) keyboardPanel(height int) devicePanel {
	panel := devicePanel{width: m.width - 2, keep: -1}
	panel.area = rect{x: 0, y: calculateLayout(m.width, m.height).headerHeight, w: m.width, h: max(1, height-2)}
	text := max(1, m.width-4)
	kb := m.mapping.kb

	if kb.picking {
		return m.keyboardPickerPanel(panel, text)
	}

	profile := "no profile yet (applying creates one)"
	if kb.loaded.Name != "" {
		profile = "profile \"" + kb.loaded.Name + "\" · in use while the profile light is on"
	}
	panel.add(-1,
		stylePanelTitle.Render("Key mapping"),
		styleFaint.Render(truncate(profile, text)),
		"")

	rows := len(keyboardRows)
	start, end, _ := viewportWindow(rows, min(m.mapping.cursor, rows-1), m.mapping.rowOffset, m.keyboardVisibleRows())
	for i := start; i < end; i++ {
		label, value, changed := m.keyboardRowText(keyboardRows[i])
		mark := " "
		if changed {
			mark = "*"
		}
		line := fmt.Sprintf("%s%-13s → %s", mark, truncate(label, 13), value)
		// Scroll markers ride on the first and last visible rows.
		if i == start && start > 0 {
			line += "  ↑"
		}
		if i == end-1 && end < rows {
			line += "  ↓"
		}
		if i == m.mapping.cursor {
			panel.add(i, styleSelectedRow.Render(truncate("›"+line+" ", text)))
		} else if value == "itself" {
			panel.add(i, " "+styleFaint.Render(truncate(line, text-1)))
		} else {
			panel.add(i, " "+styleBody.Render(truncate(line, text-1)))
		}
	}

	panel.add(-1, "")
	actions := []string{"Apply Changes", "Undo Last Edit", "Reset Draft"}
	notes := []string{"", "", ""}
	if count := len(kb.changes().Mappings); !kb.dirty() {
		notes[0] = "  (no changes)"
	} else if count > 0 {
		notes[0] = fmt.Sprintf("  (%d keys)", count)
	}
	if m.mapping.applying {
		actions[0], notes[0] = "Applying…", ""
	}
	if len(kb.undo) == 0 {
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

func (m Model) keyboardPickerPanel(panel devicePanel, text int) devicePanel {
	kb := m.mapping.kb
	choices := m.pickerChoices()
	panel.add(-1, stylePanelTitle.Render(truncate("Assign "+kb.pickKey.Name, text)))
	filter := kb.pickFilter
	if filter == "" {
		filter = styleFaint.Render("type to search")
	} else {
		filter = styleAccent.Render(filter + "▏")
	}
	modifier := "none"
	if usage := core.ModifierChoices[kb.pickModifier]; usage != 0 {
		modifier = core.KeyTargetKeyOf(usage).String()
	}
	panel.add(-1, styleBody.Render("Find: ")+filter, styleBody.Render("Held with: ")+styleAccent.Render(modifier), "")

	if len(choices) == 0 {
		panel.add(-1, styleFaint.Render("Nothing matches."))
		return panel
	}
	cursor := clampInt(kb.pickCursor, 0, len(choices)-1)
	start, end, _ := viewportWindow(len(choices), cursor, max(0, cursor-m.pickerVisibleRows()/2), m.pickerVisibleRows())
	for i := start; i < end; i++ {
		name := m.pickedTarget(choices[i]).String()
		line := fmt.Sprintf("%-22s %s", truncate(name, 22), choices[i].Group)
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

func (m Model) viewKeyboard(height int) string {
	text := max(1, m.width-4)
	if m.mapping.loading {
		return renderBoundedPanel(m.width-2, height-2,
			stylePanelTitle.Render("Key mapping")+"\n\n"+styleFaint.Render("Reading the keyboard's profile…"))
	}
	if m.mapping.err != nil {
		var b strings.Builder
		b.WriteString(stylePanelTitle.Render("Key mapping") + "\n\n")
		b.WriteString(styleDanger.Render("The profile could not be read.") + "\n")
		b.WriteString(wrapStyled(styleFaint, m.mapping.err.Error(), text) + "\n\n")
		b.WriteString(styleFaint.Render("Nothing was changed on the keyboard."))
		return renderBoundedPanel(m.width-2, height-2, b.String())
	}
	return m.keyboardPanel(height).render()
}

func (m Model) clickKeyboard(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mapping.loading || m.mapping.err != nil {
		return m, nil
	}
	owner, ok := m.keyboardPanel(m.height-3).ownerAt(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	if m.mapping.kb.picking {
		choices := m.pickerChoices()
		if owner < len(choices) {
			m.kbSetTarget(m.mapping.kb.pickKey, m.pickedTarget(choices[owner]))
			m.mapping.kb.picking = false
		}
		return m, nil
	}
	m.mapping.cursor = owner
	if owner >= len(keyboardRows) {
		return m.triggerKeyboardRow()
	}
	return m, nil
}
