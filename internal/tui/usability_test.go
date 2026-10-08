package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func typeRunes(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatalf("typing %q quit the app", string(r))
			}
		}
		m = next.(Model)
	}
	return m
}

func loadedModel(t *testing.T, width, height int) (Model, *core.OpenBitdoCore) {
	t.Helper()
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.width, m.height = width, height
	return loadDevicesAndDrain(t, m, c), c
}

// q used to quit, ? used to open help, and x used to dismiss a notice, all
// while the user was typing a device name into the filter.
func TestFilterTypingIsTextNotShortcuts(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m, _ = m.setNotice(noticeWarning, "something to dismiss", false)

	m = typeRunes(t, m, "/")
	if !m.devices.filtering {
		t.Fatal("expected / to start filtering")
	}
	m = typeRunes(t, m, "xq?")
	if m.devices.filterText != "xq?" {
		t.Fatalf("expected every key to be typed into the filter, got %q", m.devices.filterText)
	}
	if m.modal.active {
		t.Fatal("? must not open help while typing")
	}
	if m.notice.message == "" {
		t.Fatal("x must not dismiss a notice while typing")
	}
	footer := ansi.Strip(m.viewFooter())
	if strings.Contains(footer, "q quit") || strings.Contains(footer, "? help") {
		t.Fatalf("the footer must not offer shortcuts that are text right now: %q", footer)
	}

	// esc clears the filter and gives the list back.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.devices.filtering || m.devices.filterText != "" || len(m.devices.filtered) != 3 {
		t.Fatalf("expected esc to clear the filter, got filtering=%v text=%q rows=%d", m.devices.filtering, m.devices.filterText, len(m.devices.filtered))
	}
}

// The filter title nested one style inside another, so the inner style's
// reset leaked out as literal "[38;5;240m" text.
func TestFilterTitleRendersWithoutLeakedEscapes(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m = typeRunes(t, m, "/ult")

	plain := ansi.Strip(m.View())
	if strings.Contains(plain, "[38;") || strings.Contains(plain, "[0m") {
		t.Fatalf("escape sequence leaked into the frame:\n%s", plain)
	}
	if !strings.Contains(plain, "/ult") {
		t.Fatalf("expected the filter text in the title:\n%s", plain)
	}
	if len(m.devices.filtered) != 1 || m.devices.filtered[0].VidPid.PID != 0x6012 {
		t.Fatalf("expected the filter to match the Ultimate 2 by its shown name, got %+v", m.devices.filtered)
	}
}

// At 80x24 the compact layout used to show one device and tell the user to
// "choose a controller" from a list that was not on screen.
func TestCompactLayoutKeepsTheDeviceList(t *testing.T) {
	for _, size := range [][2]int{{60, 18}, {80, 24}} {
		m, _ := loadedModel(t, size[0], size[1])
		plain := ansi.Strip(m.View())
		for _, name := range []string{"Retro 108 Mechanical Keyboard", "Ultimate 2 Wireless Controller", "Xcloud"} {
			if !strings.Contains(plain, name) {
				t.Fatalf("%dx%d: expected %q in the device list:\n%s", size[0], size[1], name, plain)
			}
		}
		if !strings.Contains(plain, "Actions") {
			t.Fatalf("%dx%d: expected the selected device's actions too:\n%s", size[0], size[1], plain)
		}
	}
}

// Without colour, the selected device row was indistinguishable.
func TestSelectedRowsCarryAMarkerNotJustColour(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m.devices.cursor = 1
	if plain := ansi.Strip(m.View()); !strings.Contains(plain, "› ● Ultimate 2 Wireless Controller") {
		t.Fatalf("expected a › marker on the selected device row:\n%s", plain)
	}
}

// A reload used to keep the cursor's index, so the selection (and an open
// actions pane) could jump to whichever device now sat at that index.
func TestReloadKeepsTheSameDeviceSelected(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m.devices.cursor = 2
	m.devices.pane = paneActions
	selected, _ := m.devices.selected()

	reordered := []core.AppDevice{m.devices.devices[2], m.devices.devices[0], m.devices.devices[1]}
	next, _ := m.Update(devicesLoadedMsg{devices: reordered})
	m = next.(Model)
	if got, _ := m.devices.selected(); !sameDevice(got, selected) {
		t.Fatalf("selection moved from %s to %s", selected.DisplayName, got.DisplayName)
	}

	// When the selected device is gone, its actions pane goes with it.
	next, _ = m.Update(devicesLoadedMsg{devices: []core.AppDevice{m.devices.devices[0]}})
	m = next.(Model)
	if m.devices.pane != paneDeviceList {
		t.Fatal("the actions pane must not stay open on a device that was unplugged")
	}
}

func TestRescanReportsWhatItFound(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(Model)
	if !m.devices.loading {
		t.Fatal("expected a rescan to be in flight")
	}
	m = drainCmds(t, m, cmd)
	if !strings.Contains(m.statusLine, "Found 3 devices") {
		t.Fatalf("expected the rescan to say what it found, got %q", m.statusLine)
	}
}

func TestEmptyStatesSayWhatIsHappening(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	if plain := ansi.Strip(m.View()); !strings.Contains(plain, "Looking for devices") || strings.Contains(plain, "No 8BitDo device found") {
		t.Fatalf("before the first scan finishes, nothing has been ruled out:\n%s", plain)
	}
	next, _ := m.Update(devicesLoadedMsg{})
	m = next.(Model)
	if plain := ansi.Strip(m.View()); !strings.Contains(plain, "No 8BitDo device found") || !strings.Contains(plain, "--mock") {
		t.Fatalf("expected the no-device state with what to do next:\n%s", plain)
	}
}

// A device with no configuration interface (a Retro 108 over USB) used to
// be listed as "Supported" with every action offered.
func TestUnreachableDeviceIsShownAsSuchAndOffersNothing(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	keyboard := core.AppDevice{
		VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x5209}, Name: "PID_108JP", DisplayName: "Retro 108 Keyboard",
		SupportTier: protocol.TierFull, Capability: protocol.DeviceProfileFor(protocol.VidPid{VID: 0x2dc8, PID: 0x5209}).Capability,
		ConfigChannel: core.ChannelAbsent,
	}
	next, cmd := m.Update(devicesLoadedMsg{devices: []core.AppDevice{keyboard}})
	m = next.(Model)
	if cmd != nil {
		if batch, ok := cmd().(tea.BatchMsg); ok && len(batch) > 0 {
			t.Fatal("a device with nothing to probe through must not be auto-diagnosed")
		}
	}

	plain := ansi.Strip(m.View())
	for _, want := range []string{"unreachable", "does not expose the interface", "connection mode"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q:\n%s", want, plain)
		}
	}
	for _, item := range m.actionsForSelectedDevice() {
		if item.reason == "" {
			t.Fatalf("action %q is offered for a device that cannot be reached", item.label)
		}
	}
}

func TestDiagnosticsExplainsWhyItCouldNotRun(t *testing.T) {
	cases := map[core.ErrorKind][]string{
		core.KindPermissionDenied:   {"not allowed to open", "70-openbitdo.rules", "udevadm"},
		core.KindNoConfigChannel:    {"no configuration interface"},
		core.KindDeviceDisconnected: {"disconnected", "press r"},
	}
	for kind, wants := range cases {
		m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
		m.width, m.height = 80, 24
		m.screen = screenDiagnostics
		m.diag.device = core.AppDevice{DisplayName: "Test Pad"}
		m.diag.err = &core.Error{Kind: kind, Message: "transport error: open failed for 2dc8:6013: a very long underlying message that used to be printed on a single clipped line"}

		plain := ansi.Strip(m.View())
		for _, want := range wants {
			if !strings.Contains(plain, want) {
				t.Fatalf("%s: expected %q:\n%s", kind, want, plain)
			}
		}
		if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "r retry") || strings.Contains(footer, "details") {
			t.Fatalf("%s: the footer should offer retry and nothing that needs results: %q", kind, footer)
		}
	}
}

// Enter (and a gamepad's A button) used to confirm a brick-risk dialog the
// moment it appeared.
func TestRiskDialogDefaultsToCancel(t *testing.T) {
	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	type confirmed struct{}
	m.modal = riskAckModal("run the guarded write probe", writeProbeRisk, confirmed{})

	if body := strings.Join(m.modal.body, " "); strings.Contains(body, "firmware or boot state") || !strings.Contains(body, "does not touch firmware") {
		t.Fatalf("the write probe's dialog must describe the write probe: %q", body)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if next.(Model).modal.active {
		t.Fatal("enter on the default button should cancel and close the dialog")
	}

	m.modal = riskAckModal("run the guarded write probe", writeProbeRisk, confirmed{})
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(Model)
	if m.modal.focusCancel {
		t.Fatal("expected left to move focus to the confirm button")
	}
	if !strings.Contains(ansi.Strip(m.modal.view(100)), "›[ I understand the risk ]") {
		t.Fatalf("the focused button needs a marker, not just colour:\n%s", ansi.Strip(m.modal.view(100)))
	}
}

func TestHelpListsEveryKeyOfTheCurrentView(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = next.(Model)
	plain := ansi.Strip(m.View())
	for _, want := range []string{"Keys: Devices", "look for devices again", "filter the device list", "answering diagnostics", "[ Close ]"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q in help:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "[ Cancel ]") {
		t.Fatal("help has nothing to cancel")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if next.(Model).modal.active {
		t.Fatal("enter should close help")
	}
}

// Sub-views used to keep their parent's footer, advertising keys that did
// nothing there.
func TestFooterOnlyOffersKeysThatWork(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m.screen = screenDiagnostics
	m.diag.device, _ = m.devices.selected()
	m.diag.showSupportRequest = true
	footer := ansi.Strip(m.viewFooter())
	if strings.Contains(footer, "rerun") || strings.Contains(footer, "details") {
		t.Fatalf("the report view cannot rerun or toggle details: %q", footer)
	}
	if !strings.Contains(footer, "c copy") || !strings.Contains(footer, "w save") {
		t.Fatalf("the report view should offer copy and save: %q", footer)
	}

	// However narrow, help and quit survive and nothing is cut mid-hint.
	m.diag.showSupportRequest = false
	for width := 20; width <= 120; width += 7 {
		m.width = width
		line := ansi.Strip(m.footerHints(width - 2))
		if !strings.HasSuffix(line, "? help  q quit") {
			t.Fatalf("width %d: expected help and quit to survive, got %q", width, line)
		}
	}
}

func TestLongNoticeIsShortenedNotReplaced(t *testing.T) {
	m, _ := loadedModel(t, 80, 24)
	m, _ = m.setNotice(noticeError, "open failed for 2dc8:6013 because of something long enough that it cannot possibly fit beside the key hints", false)
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "error: open failed for 2dc8:6013") || !strings.Contains(footer, "…") {
		t.Fatalf("expected the start of the real message with an ellipsis, got %q", footer)
	}
	if !strings.Contains(footer, "x dismiss") {
		t.Fatalf("a sticky error needs its dismiss key on screen: %q", footer)
	}
	// And the whole message stays readable afterwards.
	m.screen = screenSettings
	if plain := ansi.Strip(m.View()); !strings.Contains(plain, "Recent activity") || !strings.Contains(plain, "open failed for 2dc8:6013") {
		t.Fatalf("expected the message in Settings' recent activity:\n%s", plain)
	}
}

// After a failed rollback the mapping screen stayed up until the next key
// press, and a click on Apply issued another write.
func TestWriteLockBlocksMouseToo(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m.screen = screenMapping
	m.mapping.kind = core.KindJP108
	m.mapping.device, _ = m.devices.selected()

	next, _ := m.handleMappingApplyResult(core.WriteRecoveryReport{
		RollbackAttempted: true, RollbackSucceeded: false, WriteError: "boom", RollbackError: "also boom",
	}, nil)
	m = next.(Model)
	if m.screen != screenRecovery {
		t.Fatalf("a failed rollback must switch to recovery at once, got screen %v", m.screen)
	}

	m.screen = screenMapping // even if something left it showing
	next, cmd := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 5, Y: 10})
	if next.(Model).screen != screenRecovery || cmd != nil {
		t.Fatal("a click while writes are locked must land on recovery and do nothing else")
	}
}

func TestSlotPreviewStartsAtTheNextSlotAndCycles(t *testing.T) {
	m, c := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	_ = c
	m.screen = screenMapping
	m.mapping.kind = core.KindUltimate2
	m.mapping.device = core.AppDevice{VidPid: protocol.VidPid{VID: 0x2dc8, PID: 0x6012}}
	m.mapping.u2Loaded.Slot = core.U2Slot1

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = drainCmds(t, next.(Model), cmd)
	if m.mapping.u2PreviewSlot != core.U2Slot2 {
		t.Fatalf("the first preview should be the slot after the loaded one, got slot %d", m.mapping.u2PreviewSlot)
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = drainCmds(t, next.(Model), cmd)
	if m.mapping.u2PreviewSlot != core.U2Slot3 {
		t.Fatalf("p inside the preview should move to the next slot, got slot %d", m.mapping.u2PreviewSlot)
	}
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "p next slot") || strings.Contains(footer, "←→") {
		t.Fatalf("the preview's footer should list the preview's keys: %q", footer)
	}
}

func TestNothingIsCutMidWordInTheDetailPanel(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m.devices.cursor = 2 // the candidate device has the longest text
	for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(line, "Blocked until runtime and hardware confirmat") && !strings.Contains(line, "confirmation") {
			t.Fatalf("line cut mid-word: %q", line)
		}
	}
	plain := ansi.Strip(m.View())
	// The reason and the tier note each wrap onto further lines in full.
	for _, want := range []string{"not available: this model is not confirmed for", "not a fault in the device."} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q to be readable in full:\n%s", want, plain)
		}
	}
}

func TestWrapTextBreaksOnlyAtSpaces(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  []string
	}{
		{"run openbitdo --mock to look around", 20, []string{"run openbitdo --mock", "to look around"}},
		{"see 70-openbitdo.rules now", 18, []string{"see", "70-openbitdo.rules", "now"}},
		{"/a/very/long/path/that/does/not/fit", 10, []string{"/a/very/lo", "ng/path/th", "at/does/no", "t/fit"}},
		{"  sudo udevadm control --reload-rules", 24, []string{"  sudo udevadm control", "--reload-rules"}},
		{"first\n\nsecond", 10, []string{"first", "", "second"}},
		{"", 10, []string{""}},
	}
	for _, tc := range cases {
		got := wrapText(tc.in, tc.width)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("wrapText(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
		for _, line := range got {
			if ansi.StringWidth(line) > tc.width {
				t.Errorf("wrapText(%q, %d) produced an over-long line %q", tc.in, tc.width, line)
			}
		}
	}
}
