package tui

import (
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// jp108Presets is the exact remap-target cycle from the prior Rust editor
// (reducer.rs JP108_PRESETS) — raw HID keyboard-usage IDs.
var jp108Presets = []uint16{
	0x0004, 0x0005, 0x0006, 0x0007, 0x0008, 0x0009, 0x000a, 0x000b, 0x0028, 0x0029, 0x002c, 0x003a,
	0x003b, 0x003c, 0x00e0, 0x00e1,
}

// jp108TargetLabel mirrors Rust's mapping_editor.rs, which shows JP108
// targets as raw hex only (no friendly-name table exists for JP108).
func jp108TargetLabel(usage uint16) string {
	return fmt.Sprintf("0x%04x", usage)
}

func cycleFromTable(table []uint16, current uint16, delta int) uint16 {
	idx := 0
	for i, u := range table {
		if u == current {
			idx = i
			break
		}
	}
	idx = ((idx+delta)%len(table) + len(table)) % len(table)
	return table[idx]
}

// u2FunctionLabels names every catalog value core.U2Function defines — see
// paddles.go. Ultimate2 targets are single-bit function-catalog bitmasks
// (confirmed wire encoding), a completely different value space from
// JP108's raw HID usage IDs, so this and jp108Presets/jp108TargetLabel stay
// separate rather than sharing one generic table.
var u2FunctionLabels = map[core.U2Function]string{
	core.U2FuncNone: "(none)",
	core.U2FuncA:    "A", core.U2FuncB: "B", core.U2FuncX: "X", core.U2FuncY: "Y",
	core.U2FuncL1: "L1", core.U2FuncR1: "R1", core.U2FuncL2: "L2", core.U2FuncR2: "R2",
	core.U2FuncL3: "L3", core.U2FuncR3: "R3", core.U2FuncSelect: "Select", core.U2FuncStart: "Start",
	core.U2FuncDPadUp: "DPad Up", core.U2FuncDPadDown: "DPad Down",
	core.U2FuncDPadLeft: "DPad Left", core.U2FuncDPadRight: "DPad Right",
	core.U2FuncStickUp: "Stick Up", core.U2FuncStickDown: "Stick Down",
	core.U2FuncStickLeft: "Stick Left", core.U2FuncStickRight: "Stick Right",
	core.U2FuncStickUpLeft: "Stick Up-Left", core.U2FuncStickUpRight: "Stick Up-Right",
	core.U2FuncStickDownLeft: "Stick Down-Left", core.U2FuncStickDownRight: "Stick Down-Right",
	core.U2FuncHome: "Home", core.U2FuncMenu: "Menu", core.U2FuncScreenshot: "Screenshot",
	core.U2FuncTurboA: "Turbo A", core.U2FuncTurboB: "Turbo B", core.U2FuncButtonSwap: "Button Swap",
	core.U2FuncActAsPaddle1: "Act as Paddle 1", core.U2FuncActAsPaddle2: "Act as Paddle 2",
}

// u2FunctionCycle is every catalog value in declaration order, used to cycle
// a button or paddle row's assigned function with left/right. It contains
// only the values core.U2Function actually defines — there is no "act as
// paddle 3/4" value to cycle into by construction, which is how this UI
// respects U2Function.AssignableAsPaddleTarget's documented restriction
// without needing to filter anything explicitly.
var u2FunctionCycle = []core.U2Function{
	core.U2FuncNone,
	core.U2FuncA, core.U2FuncB, core.U2FuncX, core.U2FuncY,
	core.U2FuncL1, core.U2FuncR1, core.U2FuncL2, core.U2FuncR2, core.U2FuncL3, core.U2FuncR3,
	core.U2FuncSelect, core.U2FuncStart,
	core.U2FuncDPadUp, core.U2FuncDPadDown, core.U2FuncDPadLeft, core.U2FuncDPadRight,
	core.U2FuncStickUp, core.U2FuncStickDown, core.U2FuncStickLeft, core.U2FuncStickRight,
	core.U2FuncStickUpLeft, core.U2FuncStickUpRight, core.U2FuncStickDownLeft, core.U2FuncStickDownRight,
	core.U2FuncHome, core.U2FuncMenu, core.U2FuncScreenshot,
	core.U2FuncTurboA, core.U2FuncTurboB, core.U2FuncButtonSwap,
	core.U2FuncActAsPaddle1, core.U2FuncActAsPaddle2,
}

// u2FunctionLabel renders f's display name, falling back to raw hex for any
// value outside the known catalog (defensive only — every value this UI can
// ever set comes from u2FunctionCycle).
func u2FunctionLabel(f core.U2Function) string {
	if label, ok := u2FunctionLabels[f]; ok {
		return label
	}
	return fmt.Sprintf("0x%08x", uint32(f))
}

func cycleU2Function(current core.U2Function, delta int) core.U2Function {
	idx := 0
	for i, f := range u2FunctionCycle {
		if f == current {
			idx = i
			break
		}
	}
	idx = ((idx+delta)%len(u2FunctionCycle) + len(u2FunctionCycle)) % len(u2FunctionCycle)
	return u2FunctionCycle[idx]
}

type mappingState struct {
	device  core.AppDevice
	kind    core.DeviceKind
	loading bool
	err     error

	jp108Loaded []core.DedicatedButtonMapping
	jp108Draft  []core.DedicatedButtonMapping
	jp108Undo   [][]core.DedicatedButtonMapping

	u2Loaded core.U2CoreProfile
	u2Draft  core.U2CoreProfile
	u2Undo   []core.U2CoreProfile

	// u2Preview holds a read-only look at a slot other than whatever's
	// currently loaded into the draft, so a user can see what's actually
	// stored in each of the Ultimate2's 3 slots before deciding which one to
	// load into the editor — there is no protocol command to change which
	// slot is active on the device itself (that only happens on the
	// controller), so "switching" here means loading a previewed slot's data
	// into the draft, not writing anything, until Apply is pressed.
	u2PreviewSlot    core.U2SlotID
	u2PreviewLoading bool
	u2PreviewResult  *core.U2CoreProfile
	u2PreviewErr     error

	cursor    int
	rowOffset int
	applying  bool
	statusMsg string
}

func newMappingState() mappingState { return mappingState{} }

// rowCount is the number of button rows (plus, for Ultimate2, 4 paddle
// rows) plus the three virtual action rows (Apply, Undo, Reset) appended at
// the end for unified up/down navigation. Naturally 3 (no button/paddle
// rows at all) when u2Draft.MappingsUnavailable is set — see viewMapping.
func (s mappingState) rowCount() int {
	switch s.kind {
	case core.KindJP108:
		return len(s.jp108Draft) + 3
	default:
		return len(s.u2Draft.Mappings) + len(s.u2Draft.PaddleMappings) + 3
	}
}

// canUndo mirrors Rust's mapping_can_undo: true whenever there's a pushed
// snapshot to pop, independent of mapping_has_changes/dirty (a Reset also
// pushes a snapshot, so a Reset itself is undoable).
func (s mappingState) canUndo() bool {
	if s.kind == core.KindJP108 {
		return len(s.jp108Undo) > 0
	}
	return len(s.u2Undo) > 0
}

func (s mappingState) dirty() bool {
	if s.kind == core.KindJP108 {
		return !equalJP108(s.jp108Loaded, s.jp108Draft)
	}
	return !equalU2(s.u2Loaded.Mappings, s.u2Draft.Mappings) ||
		!equalU2Paddles(s.u2Loaded.PaddleMappings, s.u2Draft.PaddleMappings)
}

func equalJP108(a, b []core.DedicatedButtonMapping) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// cloneU2Profile makes a deep copy of a U2CoreProfile's Mappings and
// PaddleMappings slices so undo snapshots aren't aliased to the live draft.
func cloneU2Profile(p core.U2CoreProfile) core.U2CoreProfile {
	p.Mappings = append([]core.U2ButtonMapping(nil), p.Mappings...)
	p.PaddleMappings = append([]core.U2PaddleMapping(nil), p.PaddleMappings...)
	return p
}

func equalU2(a, b []core.U2ButtonMapping) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalU2Paddles(a, b []core.U2PaddleMapping) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m Model) updateMapping(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case jp108MappingLoadedMsg:
		m.mapping.loading = false
		m.mapping.err = msg.err
		m.mapping.jp108Loaded = msg.mappings
		m.mapping.jp108Draft = append([]core.DedicatedButtonMapping(nil), msg.mappings...)
		return m, nil

	case u2ProfileLoadedMsg:
		m.mapping.loading = false
		m.mapping.err = msg.err
		m.mapping.u2Loaded = msg.profile
		m.mapping.u2Draft = msg.profile
		m.mapping.u2Draft.Mappings = append([]core.U2ButtonMapping(nil), msg.profile.Mappings...)
		m.mapping.u2Draft.PaddleMappings = append([]core.U2PaddleMapping(nil), msg.profile.PaddleMappings...)
		return m, nil

	case jp108ApplyResultMsg:
		return m.handleMappingApplyResult(msg.report, msg.err)

	case u2ApplyResultMsg:
		return m.handleMappingApplyResult(msg.report, msg.err)

	case u2SlotPreviewMsg:
		m.mapping.u2PreviewLoading = false
		m.mapping.u2PreviewErr = msg.err
		if msg.err == nil {
			profile := msg.profile
			m.mapping.u2PreviewResult = &profile
		}
		return m, nil

	case tea.KeyMsg:
		if m.mapping.u2PreviewResult != nil || m.mapping.u2PreviewLoading {
			switch msg.String() {
			case "esc":
				m.mapping.u2PreviewResult = nil
				m.mapping.u2PreviewErr = nil
			case "enter":
				if m.mapping.dirty() {
					m.modal = discardMappingModal(discardActionLoadSlot)
					return m, nil
				}
				return m.loadPreviewedSlotIntoDraft()
			}
			return m, nil
		}
		switch msg.String() {
		case "esc":
			if m.mapping.dirty() {
				m.modal = discardMappingModal(discardActionBack)
				return m, nil
			}
			m.screen = screenDevices
			return m, nil
		case "p":
			if m.mapping.kind == core.KindUltimate2 {
				return m.previewNextU2Slot()
			}
		case "up", "k":
			if m.mapping.cursor > 0 {
				m.mapping.cursor--
				m.ensureMappingCursorVisible()
			}
		case "down", "j":
			if m.mapping.cursor < m.mapping.rowCount()-1 {
				m.mapping.cursor++
				m.ensureMappingCursorVisible()
			}
		case "left":
			m.cycleMappingCursor(-1)
		case "right":
			m.cycleMappingCursor(1)
		case "enter":
			return m.triggerMappingRow()
		}
	}
	return m, nil
}

// previewNextU2Slot cycles to the next of the 3 Ultimate2 slots and starts a
// read-only preview read against it.
func (m Model) previewNextU2Slot() (tea.Model, tea.Cmd) {
	next := m.mapping.u2PreviewSlot + 1
	if next > core.U2Slot3 {
		next = core.U2Slot1
	}
	m.mapping.u2PreviewSlot = next
	m.mapping.u2PreviewLoading = true
	m.mapping.u2PreviewResult = nil
	m.mapping.u2PreviewErr = nil
	return m, cmdU2PreviewSlot(m.ctx, m.core, m.mapping.device.VidPid, next)
}

// loadPreviewedSlotIntoDraft is what "switching" to a previewed slot
// actually means in this tool: the previewed slot's mappings replace the
// draft (and become the new Loaded baseline for undo/dirty comparisons), but
// nothing is written to the device until Apply is pressed — same
// write-on-confirm discipline as every other mapping change.
func (m Model) loadPreviewedSlotIntoDraft() (tea.Model, tea.Cmd) {
	if m.mapping.u2PreviewResult == nil {
		return m, nil
	}
	profile := *m.mapping.u2PreviewResult
	m.mapping.u2Loaded = profile
	m.mapping.u2Draft = profile
	m.mapping.u2Draft.Mappings = append([]core.U2ButtonMapping(nil), profile.Mappings...)
	m.mapping.u2Draft.PaddleMappings = append([]core.U2PaddleMapping(nil), profile.PaddleMappings...)
	m.mapping.u2PreviewResult = nil
	m.mapping.u2PreviewErr = nil
	m.mapping.cursor = 0
	m.mapping.rowOffset = 0
	return m, nil
}

func (m *Model) ensureMappingCursorVisible() {
	editableRows := m.mapping.rowCount() - 3
	if m.mapping.cursor >= editableRows {
		return
	}
	start, _, _ := viewportWindow(editableRows, m.mapping.cursor, m.mapping.rowOffset, m.mappingVisibleRows())
	m.mapping.rowOffset = start
}

func (m Model) mappingVisibleRows() int {
	reserved := 13
	if m.mapping.kind == core.KindUltimate2 && calculateLayout(m.width, m.height).mode == layoutWide && m.height >= 32 {
		reserved = 21
	}
	return min(12, max(1, m.height-reserved))
}

// mappingRowText is the single plain-text representation used for both
// rendering and mouse hit resolution. Keeping this in one place prevents a
// click from targeting a different logical row than the one visible on screen.
func (m Model) mappingRowText(i int) string {
	buttonCount := len(m.mapping.u2Draft.Mappings)
	var label, value string
	switch {
	case m.mapping.kind == core.KindJP108:
		row := m.mapping.jp108Draft[i]
		label = fmt.Sprintf("%v", row.Button)
		value = jp108TargetLabel(row.TargetHIDUsage)
	case i < buttonCount:
		row := m.mapping.u2Draft.Mappings[i]
		label = fmt.Sprintf("%v", row.Button)
		value = u2FunctionLabel(row.Target)
	default:
		row := m.mapping.u2Draft.PaddleMappings[i-buttonCount]
		label = fmt.Sprintf("%v", row.Paddle)
		value = u2FunctionLabel(row.Target)
	}
	return fmt.Sprintf("%-14s → %s", label, value)
}

func (m *Model) cycleMappingCursor(delta int) {
	editableRows := m.mapping.rowCount() - 3
	if m.mapping.cursor >= editableRows {
		return
	}
	// Push a snapshot before mutating, mirroring Rust's adjust_mapping
	// (reducer.rs) — every single-row edit is individually undoable.
	if m.mapping.kind == core.KindJP108 {
		m.mapping.jp108Undo = append(m.mapping.jp108Undo, append([]core.DedicatedButtonMapping(nil), m.mapping.jp108Draft...))
		mapping := &m.mapping.jp108Draft[m.mapping.cursor]
		mapping.TargetHIDUsage = cycleFromTable(jp108Presets, mapping.TargetHIDUsage, delta)
		return
	}
	m.mapping.u2Undo = append(m.mapping.u2Undo, cloneU2Profile(m.mapping.u2Draft))
	buttonCount := len(m.mapping.u2Draft.Mappings)
	if m.mapping.cursor < buttonCount {
		mapping := &m.mapping.u2Draft.Mappings[m.mapping.cursor]
		mapping.Target = cycleU2Function(mapping.Target, delta)
		return
	}
	paddle := &m.mapping.u2Draft.PaddleMappings[m.mapping.cursor-buttonCount]
	paddle.Target = cycleU2Function(paddle.Target, delta)
}

func (m *Model) undoMapping() {
	// Mirrors Rust's mapping_undo: pop the last snapshot and restore it as
	// the draft. A no-op when the undo stack is empty.
	if m.mapping.kind == core.KindJP108 {
		n := len(m.mapping.jp108Undo)
		if n == 0 {
			return
		}
		m.mapping.jp108Draft = m.mapping.jp108Undo[n-1]
		m.mapping.jp108Undo = m.mapping.jp108Undo[:n-1]
		return
	}
	n := len(m.mapping.u2Undo)
	if n == 0 {
		return
	}
	m.mapping.u2Draft = m.mapping.u2Undo[n-1]
	m.mapping.u2Undo = m.mapping.u2Undo[:n-1]
}

func (m Model) triggerMappingRow() (tea.Model, tea.Cmd) {
	editableRows := m.mapping.rowCount() - 3
	switch m.mapping.cursor {
	case editableRows: // Apply
		if !m.mapping.dirty() || m.mapping.applying {
			return m, nil
		}
		m.mapping.applying = true
		if m.mapping.kind == core.KindJP108 {
			return m, cmdJP108Apply(m.ctx, m.core, m.mapping.device.VidPid, m.mapping.jp108Draft)
		}
		p := m.mapping.u2Draft
		// cmdU2Apply only forwards p.Mappings (button targets), not
		// p.PaddleMappings — internal/core's real (non-mock) button-map
		// write is currently hard-blocked entirely regardless of content
		// (see U2WriteButtonMap's doc comment), so this has no effect on
		// real hardware today. In mock mode the apply always succeeds
		// without inspecting content either way, and
		// handleMappingApplyResult below sets u2Loaded from the full
		// u2Draft (paddles included) on success, so mock-mode paddle
		// drafting/apply works end to end despite this.
		return m, cmdU2Apply(m.ctx, m.core, m.mapping.device.VidPid, p.Slot, p.Mode, p.Mappings, p.L2Analog, p.R2Analog)
	case editableRows + 1: // Undo
		if !m.mapping.canUndo() {
			return m, nil
		}
		m.undoMapping()
		m.mapping.statusMsg = "Last edit undone."
	case editableRows + 2: // Reset
		// Rust's mapping_reset pushes the current draft onto the undo stack
		// before resetting, so a Reset is itself undoable — match that.
		if m.mapping.kind == core.KindJP108 {
			m.mapping.jp108Undo = append(m.mapping.jp108Undo, append([]core.DedicatedButtonMapping(nil), m.mapping.jp108Draft...))
			m.mapping.jp108Draft = append([]core.DedicatedButtonMapping(nil), m.mapping.jp108Loaded...)
		} else {
			m.mapping.u2Undo = append(m.mapping.u2Undo, cloneU2Profile(m.mapping.u2Draft))
			m.mapping.u2Draft = m.mapping.u2Loaded
			m.mapping.u2Draft.Mappings = append([]core.U2ButtonMapping(nil), m.mapping.u2Loaded.Mappings...)
			m.mapping.u2Draft.PaddleMappings = append([]core.U2PaddleMapping(nil), m.mapping.u2Loaded.PaddleMappings...)
		}
		m.mapping.statusMsg = "Draft reset."
	}
	return m, nil
}

func (m Model) handleMappingApplyResult(report core.WriteRecoveryReport, err error) (tea.Model, tea.Cmd) {
	m.mapping.applying = false
	if err != nil {
		m.mapping.statusMsg = "Apply failed: " + err.Error()
		return m, nil
	}
	device := m.mapping.device
	status := "ok"
	message := "Mapping applied."
	switch {
	case report.WriteApplied:
		if m.mapping.kind == core.KindJP108 {
			m.mapping.jp108Loaded = append([]core.DedicatedButtonMapping(nil), m.mapping.jp108Draft...)
		} else {
			m.mapping.u2Loaded = m.mapping.u2Draft
		}
		m.mapping.statusMsg = "Applied and verified."
	case report.RollbackFailed():
		status = "attention"
		message = "Write failed and rollback also failed — device state is uncertain."
		m.writeLockUntilRestart = true
		m.recoveryReason = "A mapping write to " + device.Name + " failed, and the automatic rollback to the previous mapping also failed."
		m.recoveryHasBackup = report.HasBackupID
		m.recoveryBackupID = report.BackupID
		m.mapping.statusMsg = message
	default:
		status = "attention"
		message = "Write failed; previous mapping was restored from backup."
		m.mapping.statusMsg = message
	}
	return m, cmdSaveReport(m.settings.ReportSaveMode, m.settingsPath, "mapping-apply", &device, status, message, nil, nil, nil)
}

func (m Model) viewMapping(height int) string {
	var b strings.Builder
	kindLabel := "JP108 Dedicated Mapping"
	if m.mapping.kind == core.KindUltimate2 {
		kindLabel = "Ultimate2 Core Mapping Preview (mock-only)"
	}
	b.WriteString(stylePanelTitle.Render(kindLabel+": "+m.mapping.device.Name) + "\n\n")

	if m.mapping.loading {
		b.WriteString(styleFaint.Render("Loading mapping…"))
		return renderBoundedPanel(m.width-2, height-2, b.String())
	}
	if m.mapping.err != nil {
		b.WriteString(styleDanger.Render("Error: " + m.mapping.err.Error()))
		return renderBoundedPanel(m.width-2, height-2, b.String())
	}
	if m.mapping.statusMsg != "" {
		b.WriteString(styleFaint.Render(m.mapping.statusMsg) + "\n\n")
	}

	if m.mapping.u2PreviewLoading || m.mapping.u2PreviewResult != nil || m.mapping.u2PreviewErr != nil {
		fmt.Fprintf(&b, "%s\n\n", stylePanelTitle.Render(fmt.Sprintf("Preview: Slot %d", m.mapping.u2PreviewSlot)))
		switch {
		case m.mapping.u2PreviewLoading:
			b.WriteString(styleFaint.Render("Reading slot…"))
		case m.mapping.u2PreviewErr != nil:
			b.WriteString(styleDanger.Render("Error: " + m.mapping.u2PreviewErr.Error()))
		default:
			for _, row := range m.mapping.u2PreviewResult.Mappings {
				line := fmt.Sprintf("%-14s → %s", fmt.Sprintf("%v", row.Button), u2FunctionLabel(row.Target))
				b.WriteString(styleBody.Render(line) + "\n")
			}
			for _, row := range m.mapping.u2PreviewResult.PaddleMappings {
				line := fmt.Sprintf("%-14s → %s", fmt.Sprintf("%v", row.Paddle), u2FunctionLabel(row.Target))
				b.WriteString(styleBody.Render(line) + "\n")
			}
			if m.mapping.u2PreviewResult.MappingsUnavailable != "" {
				b.WriteString(styleWarning.Render("Button/paddle map unavailable: " + m.mapping.u2PreviewResult.MappingsUnavailable))
			}
			b.WriteString("\n" + styleWarning.Render("This is a read-only preview — nothing has changed yet."))
			b.WriteString("\n" + styleFaint.Render("enter to load this slot into the editor · esc to dismiss · p for the next slot"))
		}
		return renderBoundedPanel(m.width-2, height-2, b.String())
	}

	editableRows := m.mapping.rowCount() - 3
	buttonCount := len(m.mapping.u2Draft.Mappings)

	diagramSelectedIdx := -1
	if m.mapping.cursor < editableRows {
		if m.mapping.kind == core.KindJP108 {
			diagramSelectedIdx = int(m.mapping.jp108Draft[m.mapping.cursor].Button.WireIndex())
		} else if m.mapping.cursor < buttonCount {
			// Paddle rows (cursor >= buttonCount) have no diagram position —
			// leave nothing highlighted rather than guess one.
			diagramSelectedIdx = int(m.mapping.u2Draft.Mappings[m.mapping.cursor].Button.WireIndex())
		}
	}
	if calculateLayout(m.width, m.height).mode == layoutWide && height >= 24 {
		b.WriteString(renderControllerDiagram(m.mapping.kind, diagramSelectedIdx) + "\n\n")
	}

	if m.mapping.kind == core.KindUltimate2 && m.mapping.u2Draft.MappingsUnavailable != "" {
		b.WriteString(styleWarningBlock.Render(styleWarning.Render("Button/paddle remapping isn't available yet: ")+m.mapping.u2Draft.MappingsUnavailable) + "\n\n")
	}

	start, end, more := viewportWindow(editableRows, m.mapping.cursor, m.mapping.rowOffset, m.mappingVisibleRows())
	if more != "" {
		b.WriteString(styleFaint.Render(more) + "\n")
	}
	for i := start; i < end; i++ {
		line := m.mappingRowText(i)
		if i == m.mapping.cursor {
			// One Render call over the whole plain-text row (no embedded
			// styling within line/value to clash with) so styleSelectedRow's
			// inverted background isn't cut short by an inner reset code —
			// see styleSelectedRow's doc comment in theme.go.
			b.WriteString(styleSelectedRow.Render("› "+line+"  (←/→ to change)") + "\n")
		} else {
			b.WriteString("  " + styleBody.Render(line) + "\n")
		}
	}

	if more != "" {
		b.WriteString(styleFaint.Render(more) + "\n")
	}
	b.WriteString("\n")
	applyText := "Apply Changes"
	applySuffix := ""
	if !m.mapping.dirty() {
		applySuffix = "  (no changes)"
	}
	if m.mapping.applying {
		applyText = "Applying…"
		applySuffix = ""
	}
	if m.mapping.cursor == editableRows {
		b.WriteString(styleSelectedRow.Render("› "+applyText+applySuffix) + "\n")
	} else {
		line := styleBody.Render(applyText)
		if applySuffix != "" {
			line += styleFaint.Render(applySuffix)
		}
		b.WriteString("  " + line + "\n")
	}

	undoText := "Undo Last Edit"
	undoSuffix := ""
	if !m.mapping.canUndo() {
		undoSuffix = "  (nothing to undo)"
	}
	if m.mapping.cursor == editableRows+1 {
		b.WriteString(styleSelectedRow.Render("› "+undoText+undoSuffix) + "\n")
	} else {
		line := styleBody.Render(undoText)
		if undoSuffix != "" {
			line += styleFaint.Render(undoSuffix)
		}
		b.WriteString("  " + line + "\n")
	}

	resetText := "Reset Draft"
	if m.mapping.cursor == editableRows+2 {
		b.WriteString(styleSelectedRow.Render("› "+resetText) + "\n")
	} else {
		b.WriteString("  " + styleBody.Render(resetText) + "\n")
	}

	if m.mapping.kind == core.KindUltimate2 {
		b.WriteString("\n" + styleHelp.Render("p to preview another slot before loading it into the editor"))
	}

	return renderBoundedPanel(m.width-2, height-2, b.String())
}
