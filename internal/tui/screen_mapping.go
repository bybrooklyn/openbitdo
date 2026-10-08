package tui

import (
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// The Mapping tab holds one of two editors, by what the device is: the
// keyboard editor (screen_keyboard.go) or the controller editor
// (screen_pad.go). This file is what they share: the tab's state, the page
// shown when a device has no editor, and what happens after an apply.

type mappingState struct {
	device  core.AppDevice
	kind    core.DeviceKind
	loading bool
	err     error

	// kb is the keyboard editor's state, used when kind is KindJP108; pad
	// the controller editor's, used when kind is KindUltimate2.
	kb  keyboardState
	pad padEditor

	// cursor is the selected row: an editor row, then Apply, Undo, Reset.
	cursor    int
	rowOffset int
	applying  bool
	statusMsg string

	// unavailable is why this device has no mapping editor right now. When
	// set, the tab explains that instead of loading anything.
	unavailable string
}

func newMappingState() mappingState { return mappingState{} }

// rowCount is the editor's rows plus the three action rows after them.
func (s mappingState) rowCount() int {
	if s.kind == core.KindJP108 {
		return len(keyboardRows) + 3
	}
	return len(padRows) + 3
}

func (s mappingState) canUndo() bool {
	if s.kind == core.KindJP108 {
		return len(s.kb.undo) > 0
	}
	return len(s.pad.undo) > 0
}

func (s mappingState) dirty() bool {
	if s.kind == core.KindJP108 {
		return s.kb.dirty()
	}
	return s.pad.dirty()
}

// typing reports whether keys are going into a text field or search box.
func (s mappingState) typing() bool {
	return s.kb.picking || s.kb.naming || s.pad.picking || s.pad.naming
}

func (m Model) updateMapping(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.mapping.unavailable != "" {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.screen = screenDevices
		}
		return m, nil
	}
	if m.mapping.kind == core.KindJP108 {
		return m.updateKeyboard(msg)
	}
	return m.updatePad(msg)
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
		m.mapping.statusMsg = "Applied and verified."
		if m.mapping.kind == core.KindJP108 {
			m.mapping.kb.loaded = cloneKeyboardProfile(m.mapping.kb.draft)
			if m.mapping.kb.loaded.Name == "" {
				m.mapping.kb.loaded.Name = "OpenBitdo" // created by the first write
				m.mapping.kb.draft.Name = m.mapping.kb.loaded.Name
			}
			m.mapping.kb.undo = nil
			// The keyboard stores a mapping whether or not it is using it.
			m.mapping.statusMsg += " The buttons use it while the profile is on: that's the third small button at the keyboard's top left, lit when on."
		} else {
			// Anything written to a slot makes it a slot in use.
			for i := range m.mapping.pad.draft.Slots {
				if m.mapping.pad.draft.Slots[i] != m.mapping.pad.loaded.Slots[i] {
					m.mapping.pad.draft.Slots[i].InUse = true
				}
			}
			m.mapping.pad.loaded.Slots = m.mapping.pad.draft.Slots
			m.mapping.pad.undo = nil
		}
	case report.RollbackFailed():
		status = "attention"
		message = "Write failed and rollback also failed — device state is uncertain."
		m.writeLockUntilRestart = true
		m.recoveryReason = "A mapping write to " + device.DisplayName + " failed, and the automatic rollback to the previous mapping also failed."
		m.recoveryHasBackup = report.HasBackupID
		m.recoveryBackupID = report.BackupID
		m.mapping.statusMsg = message
		// Take over now, not on the next key press: the mapping screen must
		// not stay up, clickable, after a failed rollback.
		m.prevScreen = m.screen
		m.screen = screenRecovery
	default:
		status = "attention"
		message = "Write failed; previous mapping was restored from backup."
		m.mapping.statusMsg = message
	}
	return m, cmdSaveReport(m.settings.ReportSaveMode, m.settingsPath, "mapping-apply", &device, status, message, nil, nil, nil)
}

// mappingUnavailableAdvice says what, if anything, would change the answer.
func mappingUnavailableAdvice(reason string) string {
	switch reason {
	case "button-map framing not hardware-confirmed":
		return "OpenBitdo knows how this controller's profile is read and written, but that exchange has not been confirmed on a real controller yet. Until it is, the editor stays off unless you turn on advanced mode in Settings. You can explore it safely with openbitdo --mock."
	case "the controller is off":
		return "The receiver is plugged in, but the controller is not connected to it. Turn the controller on, then press s to scan again."
	case "Write locked until restart":
		return "An earlier write failed, so all writes are off until you restart OpenBitdo."
	}
	return "Nothing was read from or written to the device. The Overview tab lists what you can do with it today."
}

func (m Model) viewMapping(height int) string {
	if m.mapping.unavailable != "" {
		text := max(1, m.width-4)
		var u strings.Builder
		u.WriteString(stylePanelTitle.Render(truncate("Remapping isn't available yet", text)) + "\n\n")
		u.WriteString(wrapStyled(styleBody, "Why: "+m.mapping.unavailable+".", text) + "\n\n")
		u.WriteString(wrapStyled(styleFaint, mappingUnavailableAdvice(m.mapping.unavailable), text))
		return renderBoundedPanel(m.width-2, height-2, u.String())
	}
	if m.mapping.kind == core.KindJP108 {
		return m.viewKeyboard(height)
	}
	return m.viewPad(height)
}
