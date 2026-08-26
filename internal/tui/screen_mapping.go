package tui

import (
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// usagePresets is a small curated cycle of common HID keyboard usage IDs
// (letters, enter/esc/backspace/tab/space) for Left/Right to step through
// when remapping a button — the same "cycle a preset list" interaction the
// prior Rust editor used, rather than free-form hex entry.
var usagePresets = func() []uint16 {
	presets := make([]uint16, 0, 32)
	for u := uint16(0x04); u <= 0x1D; u++ { // a-z
		presets = append(presets, u)
	}
	presets = append(presets, 0x28, 0x29, 0x2A, 0x2B, 0x2C) // enter, esc, backspace, tab, space
	return presets
}()

func presetLabel(usage uint16) string {
	switch {
	case usage >= 0x04 && usage <= 0x1D:
		return string(rune('a' + (usage - 0x04)))
	case usage == 0x28:
		return "enter"
	case usage == 0x29:
		return "esc"
	case usage == 0x2A:
		return "backspace"
	case usage == 0x2B:
		return "tab"
	case usage == 0x2C:
		return "space"
	default:
		return fmt.Sprintf("0x%02x", usage)
	}
}

func cyclePreset(current uint16, delta int) uint16 {
	idx := 0
	for i, u := range usagePresets {
		if u == current {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(usagePresets)) % len(usagePresets)
	return usagePresets[idx]
}

type mappingState struct {
	device  core.AppDevice
	kind    core.DeviceKind
	loading bool
	err     error

	jp108Loaded []core.DedicatedButtonMapping
	jp108Draft  []core.DedicatedButtonMapping

	u2Loaded core.U2CoreProfile
	u2Draft  core.U2CoreProfile

	cursor    int
	applying  bool
	statusMsg string
}

func newMappingState() mappingState { return mappingState{} }

// rowCount is the number of button rows plus the two virtual action rows
// (Apply, Reset) appended at the end for unified up/down navigation.
func (s mappingState) rowCount() int {
	switch s.kind {
	case core.KindJP108:
		return len(s.jp108Draft) + 2
	default:
		return len(s.u2Draft.Mappings) + 2
	}
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
	buttonRows := m.mapping.rowCount() - 2
	if m.mapping.cursor >= buttonRows {
		return
	}
	if m.mapping.kind == core.KindJP108 {
		mapping := &m.mapping.jp108Draft[m.mapping.cursor]
		mapping.TargetHIDUsage = cyclePreset(mapping.TargetHIDUsage, delta)
		return
	}
	mapping := &m.mapping.u2Draft.Mappings[m.mapping.cursor]
	mapping.TargetHIDUsage = cyclePreset(mapping.TargetHIDUsage, delta)
}

func (m Model) triggerMappingRow() (tea.Model, tea.Cmd) {
	buttonRows := m.mapping.rowCount() - 2
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
	case m.mapping.cursor == buttonRows+1: // Reset
		if m.mapping.kind == core.KindJP108 {
			m.mapping.jp108Draft = append([]core.DedicatedButtonMapping(nil), m.mapping.jp108Loaded...)
		} else {
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
	return m, cmdSaveReport(m.settings.ReportSaveMode, "mapping-apply", &device, status, message, nil, nil, nil)
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

	buttonRows := m.mapping.rowCount() - 2
	for i := 0; i < buttonRows; i++ {
		var label, value string
		if m.mapping.kind == core.KindJP108 {
			row := m.mapping.jp108Draft[i]
			label = fmt.Sprintf("%v", row.Button)
			value = presetLabel(row.TargetHIDUsage)
		} else {
			row := m.mapping.u2Draft.Mappings[i]
			label = fmt.Sprintf("%v", row.Button)
			value = presetLabel(row.TargetHIDUsage)
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

	resetLine := "Reset Draft"
	if m.mapping.cursor == buttonRows+1 {
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
