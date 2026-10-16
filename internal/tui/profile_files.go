package tui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bybrooklyn/openbitdo/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// Saving the draft to a profile file and loading one back, for both
// editors. A loaded profile only replaces the draft; Apply is still what
// writes to the device.

// profileFiles is the "load a profile" list, open over an editor.
type profileFiles struct {
	open   bool
	names  []string
	cursor int
	// replace is the file a second save would overwrite.
	replace string
}

// profilesDir is where this device's profile files live.
func (m Model) profilesDir() string {
	kind := "ultimate-2"
	if m.mapping.kind == core.KindJP108 {
		kind = "retro-108"
	}
	return filepath.Join(filepath.Dir(m.settingsPath), "profiles", kind)
}

// saveProfileFile writes the draft to a file named after the profile. A
// file already there is only replaced when save is pressed a second time.
func (m *Model) saveProfileFile() {
	var (
		data []byte
		name string
		err  error
	)
	if m.mapping.kind == core.KindJP108 {
		name = m.mapping.kb.draft.Name
		data, err = core.EncodeKeyboardProfile(m.mapping.kb.draft)
	} else {
		slot := m.mapping.pad.draft.Slots[m.mapping.pad.slot]
		name = slot.Name
		data, err = core.EncodePadSlot(m.mapping.pad.draft.Platform, slot)
	}
	if err != nil {
		m.mapping.statusMsg = "Could not save: " + err.Error()
		return
	}
	file := core.ProfileFileName(name)
	overwrite := m.mapping.files.replace == file
	path, err := core.WriteProfileFile(m.profilesDir(), name, data, overwrite)
	switch {
	case errors.Is(err, fs.ErrExist):
		m.mapping.files.replace = file
		m.mapping.statusMsg = path + " already exists. Press E again to replace it."
	case err != nil:
		m.mapping.statusMsg = "Could not save: " + err.Error()
	default:
		m.mapping.files.replace = ""
		m.mapping.statusMsg = "Saved to " + path
	}
}

func (m *Model) openProfileFiles() {
	names := core.ListProfileFiles(m.profilesDir())
	if len(names) == 0 {
		m.mapping.statusMsg = "No saved profiles yet in " + m.profilesDir() + ". Press E to save this one."
		return
	}
	m.mapping.files = profileFiles{open: true, names: names}
}

// loadProfileFile replaces the draft with a saved profile.
func (m *Model) loadProfileFile(name string) {
	data, err := os.ReadFile(filepath.Join(m.profilesDir(), name))
	if err != nil {
		m.mapping.statusMsg = "Could not load: " + err.Error()
		return
	}
	if m.mapping.kind == core.KindJP108 {
		profile, err := core.DecodeKeyboardProfile(data)
		if err != nil {
			m.mapping.statusMsg = "Could not load " + name + ": " + err.Error()
			return
		}
		m.kbSnapshot()
		m.mapping.kb.draft = cloneKeyboardProfile(profile)
	} else {
		slot, err := core.DecodePadSlot(data, m.mapping.pad.draft.Platform)
		if err != nil {
			m.mapping.statusMsg = "Could not load " + name + ": " + err.Error()
			return
		}
		m.padSnapshot()
		*m.padSlot() = slot
	}
	m.mapping.statusMsg = "Loaded " + name + " into the draft. Apply Changes writes it to the device."
}

func (m Model) updateProfileFiles(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	files := &m.mapping.files
	switch msg.String() {
	case "esc":
		files.open = false
	case "up", "k":
		files.cursor = max(0, files.cursor-1)
	case "down", "j":
		files.cursor = min(len(files.names)-1, files.cursor+1)
	case "enter":
		name := files.names[files.cursor]
		files.open = false
		m.loadProfileFile(name)
	}
	return m, nil
}

func (m Model) profileFilesPanel(panel devicePanel, text int) devicePanel {
	files := m.mapping.files
	panel.add(-1, stylePanelTitle.Render("Load a saved profile"),
		styleFaint.Render(truncate(m.profilesDir(), text)), "")
	visible := m.pickerVisibleRows() + 1
	start, end, _ := viewportWindow(len(files.names), files.cursor, max(0, files.cursor-visible/2), visible)
	for i := start; i < end; i++ {
		if i == files.cursor {
			panel.add(i, styleSelectedRow.Render(truncate("› "+files.names[i]+" ", text)))
		} else {
			panel.add(i, "  "+styleBody.Render(truncate(files.names[i], text-2)))
		}
	}
	return panel
}
