package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
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

// The device list used to disappear at 80x24. It is part of the frame now,
// at every size.
func TestDeviceListIsAlwaysOnScreen(t *testing.T) {
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 40}} {
		m, _ := loadedModel(t, size[0], size[1])
		for _, target := range []screen{screenDevices, screenDiagnostics, screenMapping, screenSettings} {
			m, _ = m.navigate(target, 0)
			plain := ansi.Strip(m.View())
			for _, name := range []string{"Retro 108", "Ultimate 2", "Xcloud"} {
				if !strings.Contains(plain, name) {
					t.Fatalf("%dx%d screen %v: expected %q in the device list:\n%s", size[0], size[1], target, name, plain)
				}
			}
		}
	}
}

// The selected device and the pane's cursor are marked with glyphs, not
// only colour, and with different ones so they are not mistaken.
func TestSelectionsCarryMarkersNotJustColour(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m, _ = m.navigate(screenDevices, 1)
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "┃ Ultimate 2 Wireless") {
		t.Fatalf("expected a bar beside the selected device:\n%s", plain)
	}
	if !strings.Contains(plain, "› Check the connection") {
		t.Fatalf("expected a cursor on the selected action:\n%s", plain)
	}
}

// A reload used to keep the cursor's index, so the selection could jump to
// whichever device now sat at that index.
func TestReloadKeepsTheSameDeviceSelected(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m, _ = m.navigate(screenDiagnostics, 2)
	selected, _ := m.devices.selected()

	reordered := []core.AppDevice{m.devices.devices[2], m.devices.devices[0], m.devices.devices[1]}
	next, _ := m.Update(devicesLoadedMsg{devices: reordered})
	m = next.(Model)
	if got, _ := m.devices.selected(); !sameDevice(got, selected) {
		t.Fatalf("selection moved from %s to %s", selected.DisplayName, got.DisplayName)
	}
	if m.screen != screenDiagnostics {
		t.Fatal("a reload that keeps the device must keep its tab")
	}

	// When the selected device is gone, the tab showing its checks goes too.
	next, _ = m.Update(devicesLoadedMsg{devices: []core.AppDevice{m.devices.devices[0]}})
	m = next.(Model)
	if m.screen != screenDevices {
		t.Fatal("a tab must not stay open on a device that was unplugged")
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
		ConfigChannel: core.ChannelAbsent, WorksAs: core.RoleKeyboard,
	}
	next, cmd := m.Update(devicesLoadedMsg{devices: []core.AppDevice{keyboard}})
	m = next.(Model)
	if cmd != nil {
		if batch, ok := cmd().(tea.BatchMsg); ok && len(batch) > 0 {
			t.Fatal("a device with nothing to probe through must not be auto-diagnosed")
		}
	}

	plain := ansi.Strip(m.View())
	for _, want := range []string{"TYPING", "Working as a keyboard", "work as normal", "can show it, but not change it"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q:\n%s", want, plain)
		}
	}
	if got := m.availableActions(); len(got) != 0 {
		t.Fatalf("actions offered for a device that cannot be reached: %+v", got)
	}
	// Its Checks tab explains instead of trying, and sends nothing.
	next2, cmd2 := m.navigate(screenDiagnostics, 0)
	if cmd2 != nil || next2.diag.loading {
		t.Fatal("opening Checks for an unreachable device must not start a probe")
	}
	if plain := ansi.Strip(next2.View()); !strings.Contains(plain, "can't talk to this device") {
		t.Fatalf("expected the Checks tab to say why:\n%s", plain)
	}
}

func TestDiagnosticsExplainsWhyItCouldNotRun(t *testing.T) {
	cases := map[core.ErrorKind][]string{
		core.KindPermissionDenied:   {"not allowed to open", "70-openbitdo.rules", "udevadm"},
		core.KindNoConfigChannel:    {"can't talk to this device"},
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
		if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "r try again") || strings.Contains(footer, "details") {
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
	for _, want := range []string{"Keys: Overview", "look for devices again", "next section", "working: settings can be changed", "[ Close ]"} {
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

func TestLongTextWrapsInsteadOfBeingCut(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m, _ = m.navigate(screenDevices, 2) // the candidate device has the longest text
	plain := ansi.Strip(m.View())
	// The tier note runs over several lines and ends intact.
	for _, want := range []string{"This model is recognised", "it is not a fault in the device."} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q to be readable in full:\n%s", want, plain)
		}
	}
}

// One cursor, always in the pane: device and section are switched by their
// own keys from any tab, and esc is never needed to "get out" of a region.
func TestGettingAroundNeedsNoFocusSwitching(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	press := func(keys ...string) {
		t.Helper()
		for _, key := range keys {
			next, cmd := m.Update(*keyMsg(key))
			m = drainCmds(t, next.(Model), cmd)
		}
	}

	press("tab")
	if m.screen != screenDiagnostics {
		t.Fatalf("tab should move to Checks, got %v", m.screen)
	}
	first := m.diag.device
	press("d")
	if m.screen != screenDiagnostics || sameDevice(m.diag.device, first) {
		t.Fatal("d should show the next device's checks without leaving the tab")
	}
	press("3")
	if m.screen != screenMapping {
		t.Fatalf("3 should jump to Mapping, got %v", m.screen)
	}
	press("tab")
	if m.screen != screenButtons {
		t.Fatalf("tab should move on to Buttons, got %v", m.screen)
	}
	press("tab")
	if m.screen != screenDevices {
		t.Fatalf("tab should wrap round to Overview, got %v", m.screen)
	}
	press("2", "s")
	if m.screen != screenSettings {
		t.Fatalf("s should open settings, got %v", m.screen)
	}
	press("esc")
	if m.screen != screenDiagnostics {
		t.Fatalf("leaving settings should return to the tab it was opened from, got %v", m.screen)
	}
	press("esc")
	if m.screen != screenDevices {
		t.Fatalf("esc on a tab should go back to Overview, got %v", m.screen)
	}
}

// Leaving the mapping editor with unapplied changes asks first, whichever
// way the user is leaving, and then carries on to where they were going.
func TestLeavingADirtyMappingDraftAsksThenContinues(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	next, cmd := m.navigate(screenMapping, 0)
	m = drainCmds(t, next, cmd)
	nextModel, _ := m.Update(*keyMsg("right")) // change the first key's target
	m = nextModel.(Model)
	if !m.mapping.dirty() {
		t.Fatal("expected a changed target to make the draft dirty")
	}

	nextModel, _ = m.Update(*keyMsg("d")) // off to the next device
	m = nextModel.(Model)
	if !m.modal.active || m.screen != screenMapping || m.devices.cursor != 0 {
		t.Fatal("expected a confirmation before the draft is dropped")
	}
	nextModel, cmd = m.Update(*keyMsg("enter")) // Discard is the default button here
	m = drainCmds(t, nextModel.(Model), cmd)
	if m.devices.cursor != 1 || m.screen != screenMapping || m.mapping.dirty() {
		t.Fatalf("expected to arrive at the next device's Mapping tab, got device %d screen %v", m.devices.cursor, m.screen)
	}
}

func TestVerdictSeparatesWorkingFromLimited(t *testing.T) {
	responding := core.DeviceHealth{State: core.HealthResponding, Answered: 5, Total: 12}
	if got := verdictFor(responding, true, core.RoleUnknown).word; got != "Working" {
		t.Fatalf("a device whose settings can be changed is Working, got %q", got)
	}
	// Answering some checks is not the same as being usable.
	if got := verdictFor(responding, false, core.RoleUnknown).word; got != "Limited" {
		t.Fatalf("a device that can only be read is Limited, got %q", got)
	}
	noChannel := core.DeviceHealth{State: core.HealthNoChannel}
	if got := verdictFor(noChannel, false, core.RoleUnknown).word; got != "Can't connect" {
		t.Fatalf("unexpected verdict for an unreachable device: %q", got)
	}
	// A device that is plainly doing its job is not "can't connect", even
	// though its settings are out of reach.
	if got := verdictFor(noChannel, false, core.RoleGamepad); got.word != "Playing" || !strings.Contains(got.sentence, "Working as a controller") {
		t.Fatalf("unexpected verdict for a gamepad-mode controller: %+v", got)
	}
	if got := verdictFor(noChannel, false, core.RoleKeyboard); got.word != "Typing" || !strings.Contains(got.sentence, "Working as a keyboard") {
		t.Fatalf("unexpected verdict for a keyboard: %+v", got)
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

// The Buttons tab shows what a controller sends. While it is open a press
// is only shown: it must not also act as enter, or jump to another device.
func TestButtonsTabShowsPressesWithoutActingOnThem(t *testing.T) {
	m, _ := loadedModel(t, 100, 30)
	m, _ = m.navigate(screenButtons, 1) // the Ultimate 2
	m.devices.filtered[1].WorksAs = core.RoleGamepad
	send := func(e input.NavEvent) {
		t.Helper()
		e.SourcePID = 0x6012
		next, _ := m.Update(navEventMsg{event: e})
		m = next.(Model)
	}

	send(input.NavEvent{Kind: input.EventButtonDown, Button: 3})
	send(input.NavEvent{Kind: input.EventButtonUp, Button: 3})
	send(input.NavEvent{Kind: input.EventButtonDown, Button: 3})
	send(input.NavEvent{Kind: input.EventButtonDown, Button: 18})
	send(input.NavEvent{Kind: input.EventDPadChanged, DPad: input.DirUpLeft})
	send(input.NavEvent{Kind: input.EventButtonDown, Button: 1}) // "enter" elsewhere

	if m.screen != screenButtons || m.devices.cursor != 1 {
		t.Fatalf("a press on the Buttons tab moved the app: screen=%v device=%d", m.screen, m.devices.cursor)
	}
	state := m.pads[0x6012]
	if state.presses[3] != 2 || !state.held[3] || !state.held[18] || state.last != 1 {
		t.Fatalf("unexpected recorded state: %+v", state)
	}
	plain := ansi.Strip(m.View())
	for _, want := range []string{"Last pressed: button 1", "Seen so far: 1 3 18", "D-pad: up-left", " 18 "} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q:\n%s", want, plain)
		}
	}

	// c forgets this controller's presses.
	next, _ := m.Update(*keyMsg("c"))
	m = next.(Model)
	if got := m.pads[0x6012]; got.events != 0 {
		t.Fatalf("expected c to clear the recorded presses, got %+v", got)
	}

	// On any other tab the same press drives the menus again.
	m, _ = m.navigate(screenDevices, 1)
	send(input.NavEvent{Kind: input.EventButtonDown, Button: 3}) // next device
	if m.devices.cursor != 2 {
		t.Fatalf("expected button 3 to move to the next device off the Buttons tab, got device %d", m.devices.cursor)
	}
}

func TestButtonsTabExplainsADeviceThatSendsNothing(t *testing.T) {
	m, _ := loadedModel(t, 80, 24)
	m, _ = m.navigate(screenButtons, 1) // a controller in a mode with no gamepad interface
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "No button presses to show") || !strings.Contains(plain, "works as a gamepad") {
		t.Fatalf("expected an explanation, not an empty grid:\n%s", plain)
	}

	m, _ = m.navigate(screenButtons, 0)
	m.devices.filtered[0].WorksAs = core.RoleKeyboard
	if plain := ansi.Strip(m.View()); !strings.Contains(plain, "This is a keyboard") {
		t.Fatalf("expected the keyboard explanation:\n%s", plain)
	}
}

// Every check a diagnostics run of an Ultimate 2 reports is named in plain
// words; a check shown by its command ID is one nobody wrote a label for.
func TestUltimate2DiagnosticChecksHavePlainLabels(t *testing.T) {
	c := core.New(core.Config{MockMode: true})
	diag, err := c.DiagProbe(context.Background(), protocol.VidPid{VID: 0x2dc8, PID: 0x6012})
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	seen := map[protocol.CommandID]bool{}
	for _, check := range diag.CommandChecks {
		seen[check.Command] = true
		if !check.OK {
			t.Errorf("%s: a mock controller should answer every check: %s", check.Command, check.Detail)
		}
		if label := checkLabel(check.Command); label == string(check.Command) {
			t.Errorf("%s has no plain-language label", check.Command)
		}
	}
	for _, want := range []protocol.CommandID{protocol.CommandU2GetConnected, protocol.CommandU2GetPhysicalMode} {
		if !seen[want] {
			t.Errorf("diagnostics did not run %s", want)
		}
	}
}
