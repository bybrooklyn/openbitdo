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
	footer := ansi.Strip(lines[len(lines)-1])
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
	m.mapping = mappingState{
		device: core.AppDevice{Name: "JP108", VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x5203}},
		kind:   core.KindJP108,
	}
	for i := 0; i < 18; i++ {
		m.mapping.jp108Loaded = append(m.mapping.jp108Loaded, core.DedicatedButtonMapping{Button: core.DedicatedButtonID(i), TargetHIDUsage: 0x0004})
		m.mapping.jp108Draft = append(m.mapping.jp108Draft, core.DedicatedButtonMapping{Button: core.DedicatedButtonID(i), TargetHIDUsage: 0x0004})
	}
	return m
}

func responsiveU2MappingModel(t *testing.T, width, height int) Model {
	t.Helper()
	m, _ := responsiveModel(t, width, height)
	m.screen = screenMapping
	profile := core.U2CoreProfile{Slot: core.U2Slot1}
	for _, button := range core.AllU2Buttons {
		profile.Mappings = append(profile.Mappings, core.U2ButtonMapping{Button: button, Target: core.U2FuncA})
	}
	for _, paddle := range core.AllU2Paddles {
		profile.PaddleMappings = append(profile.PaddleMappings, core.U2PaddleMapping{Paddle: paddle, Target: core.U2FuncNone})
	}
	m.mapping = mappingState{
		device:   core.AppDevice{Name: "Ultimate2", VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012}},
		kind:     core.KindUltimate2,
		u2Loaded: profile,
		u2Draft:  cloneU2Profile(profile),
	}
	m.mapping.cursor = len(profile.Mappings) + len(profile.PaddleMappings) - 1
	m.ensureMappingCursorVisible()
	return m
}

func TestResponsiveEveryScreenCriticalContentAndFooter(t *testing.T) {
	for _, size := range responsiveSizes {
		t.Run(size.name+"/diagnostics-detail", func(t *testing.T) {
			m := responsiveDiagModel(t, size.width, size.height, false)
			assertResponsiveFrame(t, m, "Diagnostics:", "Checks:", "Detail", "command=")
			next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
			m = next.(Model)
			if m.diag.cursor != 11 {
				t.Fatalf("diagnostics wheel should keep the selected detail reachable, got cursor=%d", m.diag.cursor)
			}
		})

		t.Run(size.name+"/diagnostics-support-request", func(t *testing.T) {
			m := responsiveDiagModel(t, size.width, size.height, true)
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
			m = next.(Model)
			assertResponsiveFrame(t, m, "Support request", "esc to go back")
		})

		t.Run(size.name+"/jp108-mapping-actions", func(t *testing.T) {
			m := responsiveJP108MappingModel(t, size.width, size.height)
			assertResponsiveFrame(t, m, "JP108", "Apply", "Undo", "Reset")
			for _, cursor := range []int{m.mapping.rowCount() - 3, m.mapping.rowCount() - 2, m.mapping.rowCount() - 1} {
				m.mapping.cursor = cursor
				next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				m = next.(Model)
				if m.screen != screenMapping {
					t.Fatalf("mapping action cursor %d left mapping screen", cursor)
				}
			}
		})

		t.Run(size.name+"/u2-long-mapping-actions", func(t *testing.T) {
			m := responsiveU2MappingModel(t, size.width, size.height)
			assertResponsiveFrame(t, m, "Ultimate2", "Apply", "Undo", "Reset", "more above")
			m.mapping.cursor = m.mapping.rowCount() - 1
			m.ensureMappingCursorVisible()
			assertResponsiveFrame(t, m, "Reset")
		})

		t.Run(size.name+"/settings-long-nav-notes", func(t *testing.T) {
			m, _ := responsiveModel(t, size.width, size.height)
			m.screen = screenSettings
			for i := 0; i < 16; i++ {
				m.navNotes = append(m.navNotes, fmt.Sprintf("pid=0x%04x: gamepad nav active with a deliberately long note", 0x6000+i))
			}
			assertResponsiveFrame(t, m, "Settings", "Advanced Mode", "Report Save Mode", "Build")
			if m.settingsInfoMaxOffset() > 0 {
				next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
				m = next.(Model)
				if m.settingsInfoOffset == 0 {
					t.Fatal("settings wheel must scroll secondary paths/navigation information")
				}
				assertResponsiveFrame(t, m, "more above", "Gamepad Navigation")
			}
			settingsRow := renderedRowContaining(t, m.View(), "Report Save Mode:")
			next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 4, Y: settingsRow})
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
			assertResponsiveFrame(t, m, "Write lock active", "restore", "quit")
		})

		t.Run(size.name+"/deferred-firmware-dashboard", func(t *testing.T) {
			m, c := responsiveModel(t, size.width, size.height)
			m = loadDevices(t, m, c)
			m.devices.pane = paneActions
			m.devices.actionIdx = 2
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(Model)
			if calculateLayout(m.width, m.height).mode == layoutCompact {
				assertResponsiveFrame(t, m, "Actions:", "Firmware Update (Deferred in 0.0.3)")
			} else {
				assertResponsiveFrame(t, m, "Firmware update: Deferred in 0.0.3")
			}
			if m.screen != screenDevices {
				t.Fatalf("deferred firmware action should not leave dashboard, got screen=%v", m.screen)
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
			m.modal = discardMappingModal(discardActionBack)
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
			m.modal = discardMappingModal(discardActionBack)
			next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: confirm.x, Y: confirm.y})
			m = next.(Model)
			if m.screen != screenDevices {
				t.Fatalf("confirm should dispatch discard action back to devices, got screen=%v", m.screen)
			}
		})
	}
}

func TestCompactActionsEveryRowVisibleAndClickableAt60x18(t *testing.T) {
	m, c := responsiveModel(t, 60, 18)
	m = loadDevices(t, m, c)
	m.devices.pane = paneActions
	items := m.actionsForSelectedDevice()
	if len(items) < 5 {
		t.Fatalf("expected compact action pane to expose at least five actions, got %d", len(items))
	}

	for i, item := range items {
		m.devices.actionIdx = i
		view := ansi.Strip(m.View())
		label := item.label
		if !strings.Contains(view, "› "+label) {
			t.Fatalf("selected compact action %d %q not visible:\n%s", i, label, view)
		}
		if item.reason != "" && !strings.Contains(view, item.reason) {
			t.Fatalf("disabled compact action %q should show reason %q:\n%s", label, item.reason, view)
		}

		row := renderedRowContaining(t, m.View(), "› "+label)
		next, _ := m.Update(tea.MouseMsg{
			Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
			X: 3, Y: row,
		})
		clicked := next.(Model)
		if item.reason != "" {
			if clicked.screen != screenDevices || !strings.Contains(clicked.statusLine, item.reason) {
				t.Fatalf("disabled compact click should stay on devices and expose reason, screen=%v status=%q", clicked.screen, clicked.statusLine)
			}
		}
	}
}
