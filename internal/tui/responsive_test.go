package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var responsiveSizes = []struct {
	name          string
	width, height int
}{
	{"60x18", 60, 18},
	{"80x24", 80, 24},
	{"100x30", 100, 30},
	{"120x40", 120, 40},
}

func assertResponsiveFrame(t *testing.T, m Model, want ...string) {
	t.Helper()
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("rendered %d lines, want exactly %d:\n%s", len(lines), m.height, ansi.Strip(view))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("line %d width=%d exceeds terminal width=%d: %q", i, w, m.width, line)
		}
	}
	plain := ansi.Strip(view)
	for _, s := range want {
		if !strings.Contains(plain, s) {
			t.Fatalf("expected %q in %dx%d frame:\n%s", s, m.width, m.height, plain)
		}
	}
	// The last row is the frame's bottom edge; the footer sits just above it.
	footer := ansi.Strip(lines[len(lines)-2])
	if !strings.Contains(footer, "? help") || !strings.Contains(footer, "q quit") {
		t.Fatalf("footer must stay one-line and retain ?/q hints, got %q in:\n%s", footer, plain)
	}
}

func responsiveModel(t *testing.T, width, height int) (Model, *core.OpenBitdoCore) {
	t.Helper()
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.width, m.height = width, height
	return m, c
}

func responsiveDiagModel(t *testing.T, width, height int, candidate bool) Model {
	t.Helper()
	m, _ := responsiveModel(t, width, height)
	tier := protocol.TierFull
	if candidate {
		tier = protocol.TierCandidateReadOnly
	}
	m.screen = screenDiagnostics
	m.diag = diagnosticsState{
		device: core.AppDevice{
			Name: "Ultimate2 Candidate", VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012},
			SupportTier: tier, ProtocolFamily: protocol.Standard64, Evidence: protocol.EvidenceConfirmed,
		},
		ranAt:      time.Now(),
		showDetail: true,
	}
	for i := 0; i < 12; i++ {
		ok := i%4 != 3
		severity := protocol.SeverityOK
		if !ok {
			severity = protocol.SeverityNeedsAttention
		}
		m.diag.result.CommandChecks = append(m.diag.result.CommandChecks, protocol.DiagCommandStatus{
			Command:      protocol.CommandID(fmt.Sprintf("Check%02d", i)),
			OK:           ok,
			Severity:     severity,
			Confidence:   protocol.EvidenceConfirmed,
			Attempts:     1,
			Validator:    "validator",
			BytesRead:    8,
			BytesWritten: 8,
			Detail:       "raw response bytes available",
		})
	}
	m.diag.cursor = 9
	m.ensureDiagnosticsCursorVisible()
	return m
}

func responsiveJP108MappingModel(t *testing.T, width, height int) Model {
	t.Helper()
	m, _ := responsiveModel(t, width, height)
	m.screen = screenMapping
	m.mapping = keyboardMapping()
	return m
}

func TestOverviewEveryActionReachableAndClickableAt60x18(t *testing.T) {
	m, c := responsiveModel(t, 60, 18)
	m = loadDevicesAndDrain(t, m, c)
	items := m.availableActions()
	if len(items) < 3 {
		t.Fatalf("expected the default device to offer at least three actions, got %d", len(items))
	}

	for i, item := range items {
		m.devices.actionIdx = i
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "› "+item.label) {
			t.Fatalf("selected action %d %q not visible at 60x18:\n%s", i, item.label, view)
		}
		row := renderedRowContaining(t, m.View(), "› "+item.label)
		probe := m
		probe.devices.actionIdx = 0
		next, _ := probe.Update(tea.MouseMsg{
			Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
			X: m.width - 20, Y: row,
		})
		if got := next.(Model); got.devices.actionIdx != i && got.screen == screenDevices {
			t.Fatalf("clicking %q selected action %d, want %d", item.label, got.devices.actionIdx, i)
		}
	}
}

func responsivePadModel(t *testing.T, width, height int) Model {
	t.Helper()
	m, _ := responsiveModel(t, width, height)
	m.screen = screenMapping
	m.mapping = padMapping()
	return m
}

func TestResponsiveEveryScreenCriticalContentAndFooter(t *testing.T) {
	for _, size := range responsiveSizes {
		t.Run(size.name+"/diagnostics-detail", func(t *testing.T) {
			m := responsiveDiagModel(t, size.width, size.height, false)
			assertResponsiveFrame(t, m, "checks answered", "All checks", "command ")
			next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
			m = next.(Model)
			if m.diag.cursor != 11 {
				t.Fatalf("diagnostics wheel should keep the selected detail reachable, got cursor=%d", m.diag.cursor)
			}
		})

		t.Run(size.name+"/diagnostics-support-request", func(t *testing.T) {
			m := responsiveDiagModel(t, size.width, size.height, true)
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
			m = next.(Model)
			assertResponsiveFrame(t, m, "Report:", "c copy", "w save", "esc back")
		})

		t.Run(size.name+"/jp108-mapping-actions", func(t *testing.T) {
			m := responsiveJP108MappingModel(t, size.width, size.height)
			assertResponsiveFrame(t, m, "Key mapping", "Apply", "Undo", "Reset")
			for _, cursor := range []int{m.mapping.rowCount() - 3, m.mapping.rowCount() - 2, m.mapping.rowCount() - 1} {
				m.mapping.cursor = cursor
				next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				m = next.(Model)
				if m.screen != screenMapping {
					t.Fatalf("mapping action cursor %d left mapping screen", cursor)
				}
			}
		})

		t.Run(size.name+"/pad-profile-actions", func(t *testing.T) {
			m := responsivePadModel(t, size.width, size.height)
			assertResponsiveFrame(t, m, "Controller profile", "Apply", "Undo", "Reset")
			m.mapping.cursor = m.mapping.rowCount() - 1
			m.ensurePadCursorVisible()
			assertResponsiveFrame(t, m, "Reset")
		})

		t.Run(size.name+"/settings-long-nav-notes", func(t *testing.T) {
			m, _ := responsiveModel(t, size.width, size.height)
			m.screen = screenSettings
			for i := 0; i < 16; i++ {
				m.navNotes = append(m.navNotes, fmt.Sprintf("pid=0x%04x: gamepad nav active with a deliberately long note", 0x6000+i))
			}
			assertResponsiveFrame(t, m, "Settings", "Advanced mode", "Save reports", "Recent activity")
			if m.settingsInfoMaxOffset() > 0 {
				next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
				m = next.(Model)
				if m.settingsInfoOffset == 0 {
					t.Fatal("settings wheel must scroll secondary paths/navigation information")
				}
				assertResponsiveFrame(t, m, "pgup/pgdn to scroll", "Controller navigation")
			}
			settingsRow := renderedRowContaining(t, m.View(), "Save reports")
			next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: m.width - 20, Y: settingsRow})
			m = next.(Model)
			if m.settingsCursor != 1 {
				t.Fatalf("settings report-save row must remain clickable, got cursor=%d", m.settingsCursor)
			}
		})

		t.Run(size.name+"/recovery", func(t *testing.T) {
			m, _ := responsiveModel(t, size.width, size.height)
			m.screen = screenRecovery
			m.recoveryReason = "write failed during guarded recovery"
			m.recoveryHasBackup = true
			assertResponsiveFrame(t, m, "Writes are locked", "restore", "quit")
		})

		t.Run(size.name+"/overview", func(t *testing.T) {
			m, c := responsiveModel(t, size.width, size.height)
			m = loadDevicesAndDrain(t, m, c)
			m, _ = m.navigate(screenDevices, 1) // Ultimate 2: remapping is blocked on real hardware
			// The same shell at every size: the device list, the tabs and
			// both halves of the answer to "what can I do?".
			assertResponsiveFrame(t, m, "DEVICES", "Overview", "Checks", "Mapping", "LIMITED", "You can", "Not yet")
		})

		t.Run(size.name+"/mapping-unavailable", func(t *testing.T) {
			m, c := responsiveModel(t, size.width, size.height)
			m = loadDevicesAndDrain(t, m, c)
			m, _ = m.navigate(screenMapping, 1)
			assertResponsiveFrame(t, m, "isn't available yet", "button-map framing not")
			if m.mapping.loading {
				t.Fatal("an unavailable editor must not read from the device")
			}
		})

		t.Run(size.name+"/firmware-denied-screen", func(t *testing.T) {
			m, _ := responsiveModel(t, size.width, size.height)
			m.screen = screenFirmware
			m.fw = firmwareState{device: core.AppDevice{Name: "JP108"}, stage: fwStageDenied, deniedMsg: "Firmware updates are deferred in 0.0.3."}
			assertResponsiveFrame(t, m, "Firmware", "deferred")
		})

		t.Run(size.name+"/modal", func(t *testing.T) {
			m, _ := responsiveModel(t, size.width, size.height)
			m.modal = discardMappingModal(discardMappingMsg{action: discardActionBack})
			assertResponsiveFrame(t, m, "Discard mapping draft?", "Discard", "Cancel")
			box, confirm, cancel := modalGeometry(m.modal, m.width, m.height)
			next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: box.x - 1, Y: box.y - 1})
			m = next.(Model)
			if !m.modal.active {
				t.Fatal("outside modal click must not dismiss")
			}
			next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: cancel.x, Y: cancel.y})
			m = next.(Model)
			if m.modal.active {
				t.Fatal("cancel button must dismiss modal")
			}
			m.modal = discardMappingModal(discardMappingMsg{action: discardActionBack})
			next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: confirm.x, Y: confirm.y})
			m = next.(Model)
			if m.screen != screenDevices {
				t.Fatalf("confirm should dispatch discard action back to devices, got screen=%v", m.screen)
			}
		})
	}
}
