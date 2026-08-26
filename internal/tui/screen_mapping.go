package tui

import (
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// jp108Presets and u2Presets are the exact remap-target cycles from the
// prior Rust editor (reducer.rs JP108_PRESETS/U2_PRESETS) — JP108 targets are
// raw HID keyboard-usage IDs, U2 targets are the device's own 17 logical
// button-target IDs (0x0100.."A" .. 0x0110.."DPadRight"), a completely
// different value space, so the two tables and their cycle/label functions
// must stay separate rather than sharing one generic preset list.
var jp108Presets = []uint16{
	0x0004, 0x0005, 0x0006, 0x0007, 0x0008, 0x0009, 0x000a, 0x000b, 0x0028, 0x0029, 0x002c, 0x003a,
	0x003b, 0x003c, 0x00e0, 0x00e1,
}

var u2Presets = []uint16{
	0x0100, 0x0101, 0x0102, 0x0103, 0x0104, 0x0105, 0x0106, 0x0107, 0x0108, 0x0109, 0x010a, 0x010b,
	0x010c, 0x010d, 0x010e, 0x010f, 0x0110,
}

// jp108TargetLabel mirrors Rust's mapping_editor.rs, which shows JP108
// targets as raw hex only (no friendly-name table exists for JP108).
func jp108TargetLabel(usage uint16) string {
	return fmt.Sprintf("0x%04x", usage)
}

// u2TargetLabel ports mapping_editor.rs's u2_target_label exactly.
func u2TargetLabel(target uint16) string {
	switch target {
	case 0x0100:
		return "A"
	case 0x0101:
		return "B"
	case 0x0102:
		return "X"
	case 0x0103:
		return "Y"
	case 0x0104:
		return "L1"
	case 0x0105:
		return "R1"
	case 0x0106:
		return "L2"
	case 0x0107:
		return "R2"
	case 0x0108:
		return "L3"
	case 0x0109:
		return "R3"
	case 0x010a:
		return "Select"
	case 0x010b:
		return "Start"
	case 0x010c:
		return "Home"
	case 0x010d:
		return "DPadUp"
	case 0x010e:
		return "DPadDown"
	case 0x010f:
		return "DPadLeft"
	case 0x0110:
		return "DPadRight"
	default:
		return "Unknown"
	}
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

	cursor    int
	applying  bool
	statusMsg string
}

func newMappingState() mappingState { return mappingState{} }

// rowCount is the number of button rows plus the three virtual action rows
// (Apply, Undo, Reset) appended at the end for unified up/down navigation.
func (s mappingState) rowCount() int {
	switch s.kind {
	case core.KindJP108:
		return len(s.jp108Draft) + 3
	default:
		return len(s.u2Draft.Mappings) + 3
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
	return !equalU2(s.u2Loaded.Mappings, s.u2Draft.Mappings)
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

// cloneU2Profile makes a deep copy of a U2CoreProfile's Mappings slice so
// undo snapshots aren't aliased to the live draft.
func cloneU2Profile(p core.U2CoreProfile) core.U2CoreProfile {
	p.Mappings = append([]core.U2ButtonMapping(nil), p.Mappings...)
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
		return m, nil

	case jp108ApplyResultMsg:
		return m.handleMappingApplyResult(msg.report, msg.err)

	case u2ApplyResultMsg:
		return m.handleMappingApplyResult(msg.report, msg.err)

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.screen = screenDevices
			return m, nil
		case "up", "k":
			if m.mapping.cursor > 0 {
				m.mapping.cursor--
			}
		case "down", "j":
			if m.mapping.cursor < m.mapping.rowCount()-1 {
				m.mapping.cursor++
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

func (m *Model) cycleMappingCursor(delta int) {
	buttonRows := m.mapping.rowCount() - 3
	if m.mapping.cursor >= buttonRows {
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
	mapping := &m.mapping.u2Draft.Mappings[m.mapping.cursor]
	mapping.TargetHIDUsage = cycleFromTable(u2Presets, mapping.TargetHIDUsage, delta)
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
	buttonRows := m.mapping.rowCount() - 3
	switch {
	case m.mapping.cursor == buttonRows: // Apply
		if !m.mapping.dirty() || m.mapping.applying {
			return m, nil
		}
		m.mapping.applying = true
		if m.mapping.kind == core.KindJP108 {
			return m, cmdJP108Apply(m.ctx, m.core, m.mapping.device.VidPid, m.mapping.jp108Draft)
		}
		p := m.mapping.u2Draft
		return m, cmdU2Apply(m.ctx, m.core, m.mapping.device.VidPid, p.Slot, p.Mode, p.Mappings, p.L2Analog, p.R2Analog)
	case m.mapping.cursor == buttonRows+1: // Undo
		if !m.mapping.canUndo() {
			return m, nil
		}
		m.undoMapping()
		m.mapping.statusMsg = "Last edit undone."
	case m.mapping.cursor == buttonRows+2: // Reset
		// Rust's mapping_reset pushes the current draft onto the undo stack
		// before resetting, so a Reset is itself undoable — match that.
		if m.mapping.kind == core.KindJP108 {
			m.mapping.jp108Undo = append(m.mapping.jp108Undo, append([]core.DedicatedButtonMapping(nil), m.mapping.jp108Draft...))
			m.mapping.jp108Draft = append([]core.DedicatedButtonMapping(nil), m.mapping.jp108Loaded...)
		} else {
			m.mapping.u2Undo = append(m.mapping.u2Undo, cloneU2Profile(m.mapping.u2Draft))
			m.mapping.u2Draft = m.mapping.u2Loaded
			m.mapping.u2Draft.Mappings = append([]core.U2ButtonMapping(nil), m.mapping.u2Loaded.Mappings...)
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
		kindLabel = "Ultimate2 Core Mapping"
	}
	b.WriteString(stylePanelTitle.Render(kindLabel+": "+m.mapping.device.Name) + "\n\n")

	if m.mapping.loading {
		b.WriteString(styleFaint.Render("Loading mapping…"))
		return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
	}
	if m.mapping.err != nil {
		b.WriteString(styleDanger.Render("Error: " + m.mapping.err.Error()))
		return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
	}

	buttonRows := m.mapping.rowCount() - 3
	for i := 0; i < buttonRows; i++ {
		var label, value string
		if m.mapping.kind == core.KindJP108 {
			row := m.mapping.jp108Draft[i]
			label = fmt.Sprintf("%v", row.Button)
			value = jp108TargetLabel(row.TargetHIDUsage)
		} else {
			row := m.mapping.u2Draft.Mappings[i]
			label = fmt.Sprintf("%v", row.Button)
			value = fmt.Sprintf("%s (0x%04x)", u2TargetLabel(row.TargetHIDUsage), row.TargetHIDUsage)
		}
		line := fmt.Sprintf("%-14s → %s", label, value)
		if i == m.mapping.cursor {
			line = styleAccent.Render("› " + line + "  (←/→ to change)")
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	applyLine := "Apply Changes"
	if !m.mapping.dirty() {
		applyLine += styleFaint.Render("  (no changes)")
	}
	if m.mapping.applying {
		applyLine = "Applying…"
	}
	if m.mapping.cursor == buttonRows {
		applyLine = styleAccent.Render("› " + applyLine)
	} else {
		applyLine = "  " + applyLine
	}
	b.WriteString(applyLine + "\n")

	undoLine := "Undo Last Edit"
	if !m.mapping.canUndo() {
		undoLine += styleFaint.Render("  (nothing to undo)")
	}
	if m.mapping.cursor == buttonRows+1 {
		undoLine = styleAccent.Render("› " + undoLine)
	} else {
		undoLine = "  " + undoLine
	}
	b.WriteString(undoLine + "\n")

	resetLine := "Reset Draft"
	if m.mapping.cursor == buttonRows+2 {
		resetLine = styleAccent.Render("› " + resetLine)
	} else {
		resetLine = "  " + resetLine
	}
	b.WriteString(resetLine + "\n")

	if m.mapping.statusMsg != "" {
		b.WriteString("\n" + styleFaint.Render(m.mapping.statusMsg))
	}

	return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
}
