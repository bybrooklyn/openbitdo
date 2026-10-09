package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

var (
	retroMouseTarget    = protocol.VidPid{VID: 0x2dc8, PID: 0x5205}
	retroReceiverTarget = protocol.VidPid{VID: 0x2dc8, PID: 0x5206}
	rivieraMouseTarget  = protocol.VidPid{VID: 0x2dc8, PID: 0x205d}
)

// mouseCore is a core that reaches a simulated mouse, with the mice made
// candidate-readonly for the test. In docs/spec/pid_matrix.csv the Retro R8
// and its receiver are detect-only and the Riviera mouse has no row, so as
// shipped nothing is exchanged with any of them (see
// TestMouseIsNotTouchedAsShipped); the other tests show what happens the
// day that changes.
func mouseCore(t *testing.T, mouse protocol.Transport) *OpenBitdoCore {
	t.Helper()
	shipped := protocol.PIDRegistry
	t.Cleanup(func() { protocol.PIDRegistry = shipped })
	rows := append([]protocol.PidRow(nil), shipped...)
	riviera := false
	for i := range rows {
		if _, isMouse := mouseLayoutFor(protocol.VidPid{PID: rows[i].Pid}); isMouse {
			rows[i].SupportTier = protocol.TierCandidateReadOnly
			riviera = riviera || rows[i].Pid == rivieraMouseTarget.PID
		}
	}
	if !riviera {
		rows = append(rows, protocol.PidRow{
			Name: "PID_RivieraMouse", Pid: rivieraMouseTarget.PID,
			SupportTier: protocol.TierCandidateReadOnly, ProtocolFamily: protocol.Standard64,
		})
	}
	protocol.PIDRegistry = rows
	c := New(Config{})
	if mouse != nil {
		c.transportOverride = mouse
	}
	return c
}

// retroMouseSim is a simulated Retro R8 that holds a profile named "Desk"
// with everything at its defaults.
func retroMouseSim(receiver bool) *protocol.MouseSimulator {
	mouse := protocol.NewMouseSimulator(receiver)
	copy(mouse.Name[:], "\x00D\x00e\x00s\x00k")
	return mouse
}

// retroWrites lists the command bytes of the write requests sent since
// from, in order.
func retroWrites(mouse *protocol.MouseSimulator, from int) string {
	var cmds []byte
	for _, frame := range mouse.Frames[from:] {
		if frame[1] == 0x01 && frame[4] == 0x04 {
			cmds = append(cmds, frame[3])
		}
	}
	return hex.EncodeToString(cmds)
}

// rivieraMouseSim is a simulated Riviera mouse that holds a profile named
// "Desk" with everything at its defaults.
func rivieraMouseSim() *protocol.KbRecordSimulator {
	mouse := protocol.NewRivieraMouseSimulator()
	record := defaultMouseRecord()
	binary.LittleEndian.PutUint32(record, kbRecInUse)
	copy(record[mouseRecOffName:], "\x00D\x00e\x00s\x00k")
	copy(mouse.Record(), record)
	return mouse
}

func rivieraWrites(mouse *protocol.KbRecordSimulator, from int) int {
	count := 0
	for _, frame := range mouse.Frames[from:] {
		if frame[2] == 0x01 {
			count++
		}
	}
	return count
}

func mustReadMouse(t *testing.T, c *OpenBitdoCore, target protocol.VidPid) MouseProfile {
	t.Helper()
	profile, err := c.MouseReadProfile(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func mustApplyMouse(t *testing.T, c *OpenBitdoCore, target protocol.VidPid, edited MouseProfile) WriteRecoveryReport {
	t.Helper()
	report, err := c.MouseApply(context.Background(), target, edited, RuntimeUnlockPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.WriteApplied || report.RollbackAttempted || !report.HasBackupID {
		t.Fatalf("apply: %+v", report)
	}
	return report
}

func TestRetroMouseWithoutAProfileReadsAsItsDefaults(t *testing.T) {
	mouse := protocol.NewMouseSimulator(false)
	// What the mouse holds is not asked for when it has no profile, as the
	// vendor application does not ask.
	mouse.LeftHanded, mouse.WheelSpeed = 1, 9
	profile := mustReadMouse(t, mouseCore(t, mouse), retroMouseTarget)
	if len(mouse.Frames) != 2 {
		t.Fatalf("%d requests were sent; only the name's two should be", len(mouse.Frames))
	}
	if profile.InUse || profile.Name != "" || profile.LeftHanded || profile.WheelSpeed != 0 || profile.LiftOff != 1 || profile.PollingRate != 500 {
		t.Fatalf("defaults: %+v", profile)
	}
	want := []MouseAssignment{{Function: 2}, {Function: 3}, {Function: 5}, {Function: 4}, {}, {}}
	if !reflect.DeepEqual(profile.Assignments, want) {
		t.Fatalf("assignments: %+v", profile.Assignments)
	}
	for i, dpi := range []int{800, 1200, 1600, 2400, 3200, 6400} {
		if profile.Dpi[i] != (MouseDpiStage{Enabled: true, X: dpi, Y: dpi}) {
			t.Fatalf("stage %d: %+v", i+1, profile.Dpi[i])
		}
	}
	buttons := profile.Buttons()
	if len(buttons) != 6 || buttons[0].Name != "Right button" || buttons[1].MacroSlot != -1 || buttons[2].MacroSlot != 0 || buttons[5].MacroSlot != 3 {
		t.Fatalf("buttons: %+v", buttons)
	}
	if profile.NameLimit != 10 || profile.DpiMax != 26000 || !profile.HasSplitXY || !profile.HasLiftOff || !profile.HasMacros ||
		!reflect.DeepEqual(profile.Rates, []int{250, 500, 1000, 2000, 4000, 8000}) {
		t.Fatalf("what the model offers: %+v", profile)
	}
}

func TestRetroMouseProfileIsReadInTheVendorsOrder(t *testing.T) {
	mouse := retroMouseSim(false)
	mouse.LeftHanded, mouse.LiftOff, mouse.WheelSpeed, mouse.WheelDirection = 1, 2, 200, 1
	mouse.Buttons[0].Function = 128
	mouse.Buttons[3] = protocol.MouseButtonEntry{Macro: 1, Function: 4}
	mouse.Dpi[protocol.MouseAxisX] = protocol.MouseDpi{Mask: 0x15, Values: [6]uint16{400, 1200, 1650, 2400, 3200, 26000}}
	mouse.Dpi[protocol.MouseAxisY] = protocol.MouseDpi{Mask: 0x04, Values: [6]uint16{400, 1200, 900, 2400, 3200, 26000}}
	mouse.Stage = 4
	mouse.Rate = protocol.MousePollingRate{Count: 5, Current: 3, Rates: [6]uint16{125, 250, 500, 1000, 2000, 0}}
	mouse.Macros[1] = protocol.MouseMacro{
		Cycles: 0xffff, IntervalMs: 250, Name: []byte("\x00G\x00o"),
		Records: []protocol.MouseMacroRecord{{State: 10, Code: 0x20e1, Timer: 10}, {State: 2, Code: 0x20e1, Timer: 40}, {State: 15, Code: 2, Timer: 10}},
	}

	profile := mustReadMouse(t, mouseCore(t, mouse), retroMouseTarget)
	// Name (two blocks), left-handed, buttons in pairs, lift-off, wheel
	// speed, wheel direction, X and Y stages, polling rate, the macro of
	// the one button that plays one (header, name, records), the stage.
	var order []string
	for _, frame := range mouse.Frames {
		order = append(order, hex.EncodeToString(frame[3:4]))
	}
	if got := strings.Join(order, " "); got != "1c 1c 27 14 14 14 26 24 23 28 28 0d 0f 0f 0f 0f 0f 12" {
		t.Fatalf("read order: %s", got)
	}
	if !profile.InUse || profile.Name != "Desk" || !profile.LeftHanded || profile.LiftOff != 2 || !profile.WheelNatural {
		t.Fatalf("profile: %+v", profile)
	}
	// A wheel speed above 14 shows as 14, as in the vendor application.
	if profile.WheelSpeed != 14 {
		t.Fatalf("wheel speed %d", profile.WheelSpeed)
	}
	if profile.Assignments[0] != (MouseAssignment{Function: 128}) || profile.Assignments[3] != (MouseAssignment{Function: 4, Macro: true}) {
		t.Fatalf("assignments: %+v", profile.Assignments)
	}
	if profile.Dpi[0] != (MouseDpiStage{Enabled: true, X: 400, Y: 400}) || profile.Dpi[1].Enabled ||
		profile.Dpi[2] != (MouseDpiStage{Enabled: true, X: 1650, SplitXY: true, Y: 900}) || profile.ActiveStage != 4 {
		t.Fatalf("stages: %+v in use %d", profile.Dpi, profile.ActiveStage)
	}
	// The rate is named by its place, whatever the mouse's own table says,
	// and a mouse that reports five rates is offered five.
	if profile.PollingRate != 2000 || !reflect.DeepEqual(profile.Rates, []int{250, 500, 1000, 2000, 4000}) {
		t.Fatalf("polling rate %d of %v", profile.PollingRate, profile.Rates)
	}
	want := MouseMacro{Name: "Go", Repeat: MouseMacroUntilReleased, IntervalMs: 250, Steps: []MouseMacroStep{
		{Kind: MouseStepKeyDown, Code: 0x20e1, DelayMs: 10}, {Kind: MouseStepKeyUp, Code: 0x20e1, DelayMs: 40}, {Kind: MouseStepButtonDown, Code: 2, DelayMs: 10},
	}}
	if !reflect.DeepEqual(profile.Macros[1], want) || len(profile.Macros[0].Steps) != 0 {
		t.Fatalf("macros: %+v", profile.Macros)
	}
}

func TestRetroMouseApplyWritesOnlyWhatChanged(t *testing.T) {
	mouse := retroMouseSim(false)
	// Things this program does not model: the mask's two spare bits, and
	// the mouse's own table of rates.
	mouse.Dpi[protocol.MouseAxisX].Mask = 0xff
	mouse.Rate.Rates = [6]uint16{125, 250, 500, 1000, 2000, 4000}
	c := mouseCore(t, mouse)
	profile := mustReadMouse(t, c, retroMouseTarget)

	// Applying what was read sends no write at all.
	from := len(mouse.Frames)
	mustApplyMouse(t, c, retroMouseTarget, profile)
	if got := retroWrites(mouse, from); got != "" {
		t.Fatalf("an unedited profile wrote %s", got)
	}

	edited := profile
	edited.Assignments = append([]MouseAssignment(nil), profile.Assignments...)
	edited.LeftHanded = true
	edited.Assignments[2].Function = 128
	edited.Dpi[2].X = 1000
	edited.Dpi[5].Enabled = false
	edited.Dpi[0].SplitXY, edited.Dpi[0].Y = true, 400
	edited.ActiveStage = 2
	edited.PollingRate = 1000
	edited.LiftOff = 2
	edited.WheelSpeed = 9
	edited.WheelNatural = true
	from = len(mouse.Frames)
	mustApplyMouse(t, c, retroMouseTarget, edited)
	// One command a setting, in the order the vendor application applies a
	// profile: left-handed, the button, lift-off, wheel speed and
	// direction, X stages, Y stages, polling rate, the stage in use.
	if got := retroWrites(mouse, from); got != "271026242328280d12" {
		t.Fatalf("writes: %s", got)
	}
	if mouse.LeftHanded != 1 || mouse.LiftOff != 2 || mouse.WheelSpeed != 9 || mouse.WheelDirection != 1 {
		t.Fatalf("settings: %d %d %d %d", mouse.LeftHanded, mouse.LiftOff, mouse.WheelSpeed, mouse.WheelDirection)
	}
	if mouse.Buttons[2] != (protocol.MouseButtonEntry{Function: 128}) || mouse.Buttons[3] != (protocol.MouseButtonEntry{Function: 4}) {
		t.Fatalf("buttons: %+v", mouse.Buttons)
	}
	if want := (protocol.MouseDpi{Mask: 0xdf, Values: [6]uint16{800, 1200, 1000, 2400, 3200, 6400}}); mouse.Dpi[protocol.MouseAxisX] != want {
		t.Fatalf("X stages: %+v", mouse.Dpi[protocol.MouseAxisX])
	}
	if want := (protocol.MouseDpi{Mask: 0x01, Values: [6]uint16{400, 1200, 1600, 2400, 3200, 6400}}); mouse.Dpi[protocol.MouseAxisY] != want {
		t.Fatalf("Y stages: %+v", mouse.Dpi[protocol.MouseAxisY])
	}
	// The stage in use goes with its new X value.
	if mouse.Stage != 2 || mouse.StageDpi != 1000 {
		t.Fatalf("stage %d at %d DPI", mouse.Stage, mouse.StageDpi)
	}
	// The rate in use moved; the table and the count went back as read.
	if want := (protocol.MousePollingRate{Count: 6, Current: 2, Rates: [6]uint16{125, 250, 500, 1000, 2000, 4000}}); mouse.Rate != want {
		t.Fatalf("polling rate: %+v", mouse.Rate)
	}
	if !reflect.DeepEqual(mustReadMouse(t, c, retroMouseTarget).Dpi, edited.Dpi) {
		t.Fatal("the stages read back differ from what was applied")
	}

	// A name is renamed on its own: two reports.
	renamed := mustReadMouse(t, c, retroMouseTarget)
	renamed.Name = "Ten chars!"
	from = len(mouse.Frames)
	mustApplyMouse(t, c, retroMouseTarget, renamed)
	if got := retroWrites(mouse, from); got != "1c1c" || !bytes.Equal(mouse.Name[:], []byte("\x00T\x00e\x00n\x00 \x00c\x00h\x00a\x00r\x00s\x00!")) {
		t.Fatalf("rename wrote %s, name % x", got, mouse.Name)
	}

	// Changing only the X value of the stage in use sends the stage again,
	// since it goes with that value; another stage's does not.
	again := mustReadMouse(t, c, retroMouseTarget)
	again.Dpi[2].X = 1050
	from = len(mouse.Frames)
	mustApplyMouse(t, c, retroMouseTarget, again)
	if got := retroWrites(mouse, from); got != "2812" || mouse.StageDpi != 1050 {
		t.Fatalf("writes %s, stage at %d DPI", got, mouse.StageDpi)
	}
	again = mustReadMouse(t, c, retroMouseTarget)
	again.Dpi[3].X = 26000
	from = len(mouse.Frames)
	mustApplyMouse(t, c, retroMouseTarget, again)
	if got := retroWrites(mouse, from); got != "28" || mouse.Dpi[protocol.MouseAxisX].Values[3] != 26000 {
		t.Fatalf("writes %s", got)
	}
}

func TestRetroMouseRefusesWhatItCannotHoldBeforeSendingAnything(t *testing.T) {
	steps := []MouseMacroStep{{Kind: MouseStepKeyDown, Code: 4, DelayMs: 10}, {Kind: MouseStepKeyUp, Code: 4, DelayMs: 10}}
	for name, edit := range map[string]func(*MouseProfile){
		"DPI below 50":             func(p *MouseProfile) { p.Dpi[1].X = 0 },
		"DPI above 26000":          func(p *MouseProfile) { p.Dpi[1].X = 26050 },
		"DPI off the 50 grid":      func(p *MouseProfile) { p.Dpi[1].X = 875 },
		"vertical DPI out of step": func(p *MouseProfile) { p.Dpi[1].Y = 30000 },
		"no stage enabled": func(p *MouseProfile) {
			for i := range p.Dpi {
				p.Dpi[i].Enabled = false
			}
		},
		"stage in use switched off":    func(p *MouseProfile) { p.Dpi[0].Enabled = false },
		"a disabled stage put in use":  func(p *MouseProfile) { p.Dpi[3].Enabled, p.ActiveStage = false, 3 },
		"a seventh stage":              func(p *MouseProfile) { p.ActiveStage = 6 },
		"a rate that is not offered":   func(p *MouseProfile) { p.PollingRate = 3000 },
		"a rate the mouse lacks":       func(p *MouseProfile) { p.PollingRate = 8000 },
		"lift-off of 3 mm":             func(p *MouseProfile) { p.LiftOff = 3 },
		"wheel speed 15":               func(p *MouseProfile) { p.WheelSpeed = 15 },
		"a function not offered":       func(p *MouseProfile) { p.Assignments[2].Function = 24 }, // DPI up
		"a function off the table":     func(p *MouseProfile) { p.Assignments[2].Function = 253 },
		"a macro on the right button":  func(p *MouseProfile) { p.Assignments[0].Macro = true },
		"a macro button with no macro": func(p *MouseProfile) { p.Assignments[4].Macro = true },
		"a macro nothing plays":        func(p *MouseProfile) { p.Macros[2] = MouseMacro{Steps: steps, Repeat: 1} },
		"a Riviera target":             func(p *MouseProfile) { p.Assignments[1].Target = RecordKeyTarget{Kind: RecordTargetOff} },
		"an empty name":                func(p *MouseProfile) { p.Name = "" },
		"a name of eleven characters":  func(p *MouseProfile) { p.Name = "Eleven char" },
		"too few buttons":              func(p *MouseProfile) { p.Assignments = p.Assignments[:5] },
		"a macro repeated 100 times": func(p *MouseProfile) {
			p.Assignments[2].Macro, p.Macros[0] = true, MouseMacro{Steps: steps, Repeat: 100}
		},
		"a macro of 80 steps": func(p *MouseProfile) {
			p.Assignments[2].Macro, p.Macros[0] = true, MouseMacro{Steps: make([]MouseMacroStep, 80), Repeat: 1}
		},
		"a macro step that is no step": func(p *MouseProfile) {
			p.Assignments[2].Macro, p.Macros[0] = true, MouseMacro{Steps: []MouseMacroStep{{Kind: 3, Code: 4}}, Repeat: 1}
		},
		"a macro key that is no key": func(p *MouseProfile) {
			p.Assignments[2].Macro, p.Macros[0] = true, MouseMacro{Steps: []MouseMacroStep{{Kind: MouseStepKeyDown, Code: 3}}, Repeat: 1}
		},
		"a macro mouse button 5": func(p *MouseProfile) {
			p.Assignments[2].Macro, p.Macros[0] = true, MouseMacro{Steps: []MouseMacroStep{{Kind: MouseStepButtonDown, Code: 5}}, Repeat: 1}
		},
		"a macro wait of 60001 ms": func(p *MouseProfile) {
			p.Assignments[2].Macro, p.Macros[0] = true, MouseMacro{Steps: []MouseMacroStep{{Kind: MouseStepKeyDown, Code: 4, DelayMs: 60001}}, Repeat: 1}
		},
		"a macro name of eleven characters": func(p *MouseProfile) {
			p.Assignments[2].Macro, p.Macros[0] = true, MouseMacro{Name: "Eleven char", Steps: steps, Repeat: 1}
		},
	} {
		mouse := retroMouseSim(false)
		mouse.Rate.Count = 5 // five rates: 8000 Hz is not among them
		c := mouseCore(t, mouse)
		edited := mustReadMouse(t, c, retroMouseTarget)
		edited.Assignments = append([]MouseAssignment(nil), edited.Assignments...)
		edit(&edited)
		from := len(mouse.Frames)
		_, err := c.MouseApply(context.Background(), retroMouseTarget, edited, RuntimeUnlockPolicy{})
		var coreErr *Error
		if !errors.As(err, &coreErr) || coreErr.Kind != KindInvalidState {
			t.Errorf("%s: expected a refusal, got %v", name, err)
		}
		if got := retroWrites(mouse, from); got != "" {
			t.Errorf("%s: wrote %s before refusing", name, got)
		}
	}
}

func TestRetroMouseMacroIsAssignedChangedAndTakenOff(t *testing.T) {
	mouse := retroMouseSim(false)
	c := mouseCore(t, mouse)
	macro := MouseMacro{Name: "Burst", Repeat: 3, IntervalMs: 200, Steps: []MouseMacroStep{
		{Kind: MouseStepKeyDown, Code: 4, DelayMs: 10}, {Kind: MouseStepKeyUp, Code: 4, DelayMs: 50},
		{Kind: MouseStepButtonDown, Code: 0, DelayMs: 10}, {Kind: MouseStepButtonUp, Code: 0, DelayMs: 10},
	}}

	edited := mustReadMouse(t, c, retroMouseTarget)
	edited.Assignments[3].Macro = true
	edited.Macros[1] = macro
	from := len(mouse.Frames)
	mustApplyMouse(t, c, retroMouseTarget, edited)
	// The button first, then the macro: its header, three reports of name,
	// two of records.
	if got := retroWrites(mouse, from); got != "10"+"0f0f0f0f0f0f" {
		t.Fatalf("writes: %s", got)
	}
	// A button that plays its macro keeps the function a new profile gives
	// it; the macro goes in as the second of the four.
	if mouse.Buttons[3] != (protocol.MouseButtonEntry{Macro: 1, Function: 4}) {
		t.Fatalf("button entry: %+v", mouse.Buttons[3])
	}
	stored := mouse.Macros[1]
	wantRecords := []protocol.MouseMacroRecord{{State: 10, Code: 4, Timer: 10}, {State: 2, Code: 4, Timer: 50}, {State: 15, Timer: 10}, {State: 7, Timer: 10}}
	if stored.Cycles != 3 || stored.IntervalMs != 200 || !reflect.DeepEqual(stored.Records, wantRecords) ||
		!bytes.HasPrefix(stored.Name, []byte("\x00B\x00u\x00r\x00s\x00t\x00\x00")) {
		t.Fatalf("stored macro: %+v", stored)
	}
	read := mustReadMouse(t, c, retroMouseTarget)
	if read.Assignments[3] != (MouseAssignment{Function: 4, Macro: true}) || !reflect.DeepEqual(read.Macros[1], macro) {
		t.Fatalf("read back: %+v %+v", read.Assignments[3], read.Macros[1])
	}

	// Changing the macro rewrites it and leaves the button alone.
	read.Macros[1].Repeat = MouseMacroUntilReleased
	from = len(mouse.Frames)
	mustApplyMouse(t, c, retroMouseTarget, read)
	if got := retroWrites(mouse, from); got != "0f0f0f0f0f0f" || mouse.Macros[1].Cycles != 0xffff {
		t.Fatalf("writes %s, cycles %#x", got, mouse.Macros[1].Cycles)
	}

	// Taking it off rewrites only the button; there is no macro erase.
	read = mustReadMouse(t, c, retroMouseTarget)
	read.Assignments[3] = MouseAssignment{Function: MouseFunctionDoubleClick}
	from = len(mouse.Frames)
	mustApplyMouse(t, c, retroMouseTarget, read)
	if got := retroWrites(mouse, from); got != "10" || mouse.Buttons[3] != (protocol.MouseButtonEntry{Function: 36}) || len(mouse.Macros[1].Records) != 4 {
		t.Fatalf("writes %s, button %+v", got, mouse.Buttons[3])
	}
	if after := mustReadMouse(t, c, retroMouseTarget); len(after.Macros[1].Steps) != 0 {
		t.Fatal("a macro no button plays should read as none")
	}
}

func TestRetroMouseRollsBackWhatTheMouseDidNotKeep(t *testing.T) {
	ctx := context.Background()

	// A mouse that acknowledges and drops every write: the first readback
	// catches it, and what was there is written back and checked.
	mouse := retroMouseSim(false)
	c := mouseCore(t, mouse)
	edited := mustReadMouse(t, c, retroMouseTarget)
	edited.LeftHanded, edited.WheelSpeed = true, 7
	mouse.IgnoreWrites = true
	from := len(mouse.Frames)
	report, err := c.MouseApply(ctx, retroMouseTarget, edited, RuntimeUnlockPolicy{})
	if err != nil || report.WriteApplied || !report.RollbackAttempted || !report.RollbackSucceeded || !strings.Contains(report.WriteError, "did not keep") {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	// The failed write, then the rollback's; the wheel speed was never
	// reached.
	if got := retroWrites(mouse, from); got != "2727" {
		t.Fatalf("writes: %s", got)
	}

	// A write that goes unanswered part of the way through: what was
	// already written is put back.
	mouse = retroMouseSim(false)
	c = mouseCore(t, mouse)
	edited = mustReadMouse(t, c, retroMouseTarget)
	edited.LeftHanded, edited.PollingRate, edited.WheelSpeed = true, 4000, 7
	mouse.Mute, mouse.MuteOnce = 0x0d, true
	from = len(mouse.Frames)
	report, err = c.MouseApply(ctx, retroMouseTarget, edited, RuntimeUnlockPolicy{})
	if err != nil || report.WriteApplied || !report.RollbackSucceeded || report.WriteError == "" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if got := retroWrites(mouse, from); got != "27240d"+"27240d" {
		t.Fatalf("writes: %s", got)
	}
	if mouse.LeftHanded != 0 || mouse.WheelSpeed != 0 || mouse.Rate.Current != 1 {
		t.Fatalf("after rollback: %d %d %d", mouse.LeftHanded, mouse.WheelSpeed, mouse.Rate.Current)
	}

	// When the rollback fails too, the report says so.
	mouse = retroMouseSim(false)
	c = mouseCore(t, mouse)
	edited = mustReadMouse(t, c, retroMouseTarget)
	edited.LeftHanded, edited.PollingRate = true, 4000
	mouse.Mute = 0x0d
	report, err = c.MouseApply(ctx, retroMouseTarget, edited, RuntimeUnlockPolicy{})
	if err != nil || report.WriteApplied || report.RollbackSucceeded || report.RollbackError == "" || !report.RollbackFailed() {
		t.Fatalf("report=%+v err=%v", report, err)
	}

	// A macro that fails on the way in: the one the button played before
	// is written back.
	mouse = retroMouseSim(false)
	mouse.Buttons[2] = protocol.MouseButtonEntry{Macro: 1, Function: 5}
	old := protocol.MouseMacro{Cycles: 1, Name: make([]byte, protocol.MouseMacroNameLen), Records: []protocol.MouseMacroRecord{{State: 10, Code: 5, Timer: 10}}}
	mouse.Macros[0] = old
	c = mouseCore(t, mouse)
	edited = mustReadMouse(t, c, retroMouseTarget)
	edited.Macros[0].Steps = []MouseMacroStep{{Kind: MouseStepKeyDown, Code: 6, DelayMs: 10}, {Kind: MouseStepKeyUp, Code: 6, DelayMs: 10}}
	mouse.Mute, mouse.MuteOnce = 0x0f, true
	report, err = c.MouseApply(ctx, retroMouseTarget, edited, RuntimeUnlockPolicy{})
	if err != nil || report.WriteApplied || !report.RollbackSucceeded {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if !reflect.DeepEqual(mouse.Macros[0].Records, old.Records) {
		t.Fatalf("macro after rollback: %+v", mouse.Macros[0])
	}
}

func TestRetroMouseProfileIsCreatedWholeAndClearedOnRollback(t *testing.T) {
	ctx := context.Background()
	mouse := protocol.NewMouseSimulator(false)
	c := mouseCore(t, mouse)
	edited := mustReadMouse(t, c, retroMouseTarget)

	// Without a name there is nothing to create.
	edited.WheelSpeed = 5
	from := len(mouse.Frames)
	if _, err := c.MouseApply(ctx, retroMouseTarget, edited, RuntimeUnlockPolicy{}); err == nil || retroWrites(mouse, from) != "" {
		t.Fatalf("a profile without a name: err=%v writes=%s", err, retroWrites(mouse, from))
	}

	// With one, the whole profile is written as the vendor application
	// writes it: name, left-handed, six buttons, lift-off, wheel speed and
	// direction, both axes of stages, polling rate.
	edited.Name = "New"
	from = len(mouse.Frames)
	report := mustApplyMouse(t, c, retroMouseTarget, edited)
	if got := retroWrites(mouse, from); got != "1c1c"+"27"+"101010101010"+"26"+"24"+"23"+"2828"+"0d" {
		t.Fatalf("writes: %s", got)
	}
	created := mustReadMouse(t, c, retroMouseTarget)
	if !created.InUse || created.Name != "New" || created.WheelSpeed != 5 || mouse.WheelSpeed != 5 {
		t.Fatalf("created: %+v", created)
	}
	// Restoring the backup of a mouse that had no profile clears it again.
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatal(err)
	}
	if mouse.Cleared != 1 || mustReadMouse(t, c, retroMouseTarget).InUse {
		t.Fatalf("cleared %d times", mouse.Cleared)
	}

	// A creation that fails part of the way is cleared, since there are no
	// earlier settings to write back.
	mouse = protocol.NewMouseSimulator(false)
	c = mouseCore(t, mouse)
	edited = mustReadMouse(t, c, retroMouseTarget)
	edited.Name = "New"
	mouse.Mute, mouse.MuteOnce = 0x0d, true
	rolled, err := c.MouseApply(ctx, retroMouseTarget, edited, RuntimeUnlockPolicy{})
	if err != nil || rolled.WriteApplied || !rolled.RollbackSucceeded || mouse.Cleared != 1 || mouse.Name != [protocol.MouseNameLen]byte{} {
		t.Fatalf("report=%+v err=%v cleared=%d", rolled, err, mouse.Cleared)
	}
}

func TestRetroMouseBackupIsRestored(t *testing.T) {
	ctx := context.Background()
	mouse := retroMouseSim(false)
	mouse.Buttons[4] = protocol.MouseButtonEntry{Macro: 1}
	mouse.Macros[2] = protocol.MouseMacro{Cycles: 1, Name: make([]byte, protocol.MouseMacroNameLen), Records: []protocol.MouseMacroRecord{{State: 10, Code: 5, Timer: 10}}}
	c := mouseCore(t, mouse)
	edited := mustReadMouse(t, c, retroMouseTarget)
	edited.LeftHanded, edited.PollingRate, edited.ActiveStage = true, 8000, 5
	edited.Assignments[2].Function = MouseFunctionNone
	edited.Assignments[4] = MouseAssignment{Function: 130}
	edited.Dpi[1].X = 5000
	report := mustApplyMouse(t, c, retroMouseTarget, edited)
	if mouse.LeftHanded != 1 || mouse.Rate.Current != 5 || mouse.Stage != 5 || mouse.Buttons[4].Macro != 0 {
		t.Fatal("the edit was not applied")
	}
	// Something else overwrites the macro before the backup is restored.
	mouse.Macros[2].Records = []protocol.MouseMacroRecord{{State: 10, Code: 9, Timer: 1}}

	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatal(err)
	}
	if mouse.LeftHanded != 0 || mouse.Rate.Current != 1 || mouse.Stage != 0 || mouse.StageDpi != 800 ||
		mouse.Buttons[2].Function != 5 || mouse.Buttons[4] != (protocol.MouseButtonEntry{Macro: 1}) ||
		mouse.Dpi[protocol.MouseAxisX].Values[1] != 1200 || mouse.Macros[2].Records[0].Code != 5 {
		t.Fatalf("after restore: %+v", mouse)
	}
	if err := c.RestoreBackup(ctx, "no-such-backup"); err == nil {
		t.Fatal("an unknown backup must be refused")
	}
}

func TestRetroMouseThroughItsReceiver(t *testing.T) {
	ctx := context.Background()
	receiver := retroMouseSim(true)
	c := mouseCore(t, receiver)

	// The receiver is asked whether its mouse is there before anything
	// else.
	profile := mustReadMouse(t, c, retroReceiverTarget)
	if want, _ := hex.DecodeString("0006030a01"); !bytes.Equal(receiver.Frames[0][:5], want) || profile.Name != "Desk" {
		t.Fatalf("first frame % x, name %q", receiver.Frames[0][:5], profile.Name)
	}
	profile.WheelNatural = true
	mustApplyMouse(t, c, retroReceiverTarget, profile)
	if receiver.WheelDirection != 1 {
		t.Fatal("the edit did not reach the mouse behind the receiver")
	}
	if linked, err := c.MouseReceiverLinked(ctx, retroReceiverTarget); err != nil || !linked {
		t.Fatalf("linked=%v err=%v", linked, err)
	}

	// With the mouse off the receiver says so, and the mouse is asked
	// nothing.
	receiver.Off = true
	from := len(receiver.Frames)
	_, err := c.MouseReadProfile(ctx, retroReceiverTarget)
	var coreErr *Error
	if !errors.As(err, &coreErr) || coreErr.Kind != KindInvalidState || len(receiver.Frames) != from+1 {
		t.Fatalf("err=%v frames=%d", err, len(receiver.Frames)-from)
	}
	if _, err := c.MouseApply(ctx, retroReceiverTarget, profile, RuntimeUnlockPolicy{}); err == nil {
		t.Fatal("a mouse that is off cannot be written")
	}
	if linked, err := c.MouseReceiverLinked(ctx, retroReceiverTarget); err != nil || linked {
		t.Fatalf("linked=%v err=%v with the mouse off", linked, err)
	}
	// The question is the receiver's alone.
	if _, err := c.MouseReceiverLinked(ctx, retroMouseTarget); !errors.As(err, &coreErr) || coreErr.Reason != ReasonUnsupportedPid {
		t.Fatalf("link question to a mouse by cable: %v", err)
	}
}

func TestRivieraMouseProfileIsDecodedFromItsRecord(t *testing.T) {
	// Without a profile the defaults show, whatever the record holds.
	blank := protocol.NewRivieraMouseSimulator()
	blank.Record()[mouseRecOffWheelSpeed] = 9
	profile := mustReadMouse(t, mouseCore(t, blank), rivieraMouseTarget)
	if profile.InUse || profile.Name != "" || profile.PollingRate != 500 || profile.WheelSpeed != 0 || profile.LiftOff != 1 {
		t.Fatalf("defaults: %+v", profile)
	}
	for i, dpi := range []int{800, 1200, 1600, 2400, 3200, 6400} {
		if profile.Dpi[i] != (MouseDpiStage{Enabled: true, X: dpi, Y: dpi}) {
			t.Fatalf("stage %d: %+v", i+1, profile.Dpi[i])
		}
	}
	if profile.NameLimit != 16 || profile.DpiMax != 12000 || profile.HasSplitXY || profile.HasLiftOff || profile.HasMacros ||
		!reflect.DeepEqual(profile.Rates, []int{250, 500, 1000}) {
		t.Fatalf("what the model offers: %+v", profile)
	}
	buttons := profile.Buttons()
	if len(buttons) != 5 || !buttons[0].Fixed || buttons[1].Fixed || buttons[2].Fixed || !buttons[3].Fixed || !buttons[4].Fixed {
		t.Fatalf("buttons: %+v", buttons)
	}

	mouse := rivieraMouseSim()
	record := mouse.Record()
	record[mouseRecOffStage] = 3
	copy(record[mouseRecOffDpi+5:], []byte{0, 0x10, 0x27, 0x10, 0x27}) // stage 2 off, 10000
	record[mouseRecOffWheelDir], record[mouseRecOffWheelSpeed] = 1, 14
	binary.LittleEndian.PutUint16(record[mouseRecOffRate:], 1000)
	record[mouseRecOffLeftHanded] = 1
	// Right button sends Win+E, the wheel button plays or pauses, button 4
	// holds something this program does not understand.
	copy(record[mouseRecOffButtons+12:], []byte{0xea, 0, 0, 0, 0xe3, 0x08, 0, 0, 1, 0, 0, 0})
	copy(record[mouseRecOffButtons+24:], []byte{0xe9, 0, 0, 0, 0xcd, 0, 0, 0, 2, 0, 0, 0})
	copy(record[mouseRecOffButtons+36:], []byte{0xec, 0, 0, 0, 0x09, 0, 0, 0, 6, 0, 0, 0})
	profile = mustReadMouse(t, mouseCore(t, mouse), rivieraMouseTarget)
	if !profile.InUse || profile.Name != "Desk" || !profile.LeftHanded || !profile.WheelNatural || profile.WheelSpeed != 14 ||
		profile.PollingRate != 1000 || profile.ActiveStage != 3 || profile.Dpi[1] != (MouseDpiStage{X: 10000, Y: 10000}) {
		t.Fatalf("profile: %+v", profile)
	}
	if got := profile.Assignments[0].Target; got != (RecordKeyTarget{}) {
		t.Fatalf("left button: %+v", got)
	}
	if got := profile.Assignments[1].Target; got.Kind != RecordTargetKey || got.Key != 0x08 || got.Modifier != 0xe3 {
		t.Fatalf("right button: %+v", got)
	}
	if got := profile.Assignments[2].Target; got.Kind != RecordTargetMedia || got.Media != 205 {
		t.Fatalf("wheel button: %+v", got)
	}
	if got := profile.Assignments[3].Target; got.Kind != RecordTargetUnknown {
		t.Fatalf("button 4: %+v", got)
	}
}

func TestRivieraMouseApplyStoresExactBytesAndKeepsTheRest(t *testing.T) {
	mouse := rivieraMouseSim()
	record := mouse.Record()
	// Bytes this program does not model: the one after the name, the six
	// spare ones, the padding of a button's entry, and a fixed button
	// holding something unknown.
	record[0x24] = 0x7e
	copy(record[0x4a:0x50], []byte{0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6})
	copy(record[mouseRecOffButtons+1:], []byte{0x11, 0x22, 0x33})
	copy(record[mouseRecOffButtons+36:], []byte{0xec, 0x44, 0x55, 0x66, 0x09, 0, 0, 0, 6, 0, 0, 0})
	record[mouseRecOffLiftOff] = 2
	before := append([]byte(nil), record...)
	c := mouseCore(t, mouse)
	profile := mustReadMouse(t, c, rivieraMouseTarget)

	from := len(mouse.Frames)
	mustApplyMouse(t, c, rivieraMouseTarget, profile)
	if rivieraWrites(mouse, from) != 0 {
		t.Fatal("an unedited profile was written")
	}

	edited := profile
	edited.Assignments = append([]MouseAssignment(nil), profile.Assignments...)
	edited.ActiveStage = 2
	edited.Dpi[2].X = 1000
	edited.Dpi[5].Enabled = false
	edited.WheelNatural, edited.WheelSpeed = true, 9
	edited.PollingRate = 1000
	edited.LeftHanded = true
	edited.Assignments[1].Target = RecordKeyTarget{Kind: RecordTargetKey, Key: 0x04}
	edited.Assignments[2].Target = RecordKeyTarget{Kind: RecordTargetOff}
	from = len(mouse.Frames)
	mustApplyMouse(t, c, rivieraMouseTarget, edited)
	// Nine ranges, each one the vendor application also writes alone.
	if got := rivieraWrites(mouse, from); got != 9 {
		t.Fatalf("%d record writes", got)
	}
	want := append([]byte(nil), before...)
	for offset, text := range map[int]string{
		mouseRecOffStage:        "02",
		mouseRecOffDpi + 2*5:    "01e803e803", // on, 1000, and Y following X
		mouseRecOffDpi + 5*5:    "0000190019", // off, 6400 kept
		mouseRecOffWheelDir:     "01",
		mouseRecOffWheelSpeed:   "09",
		mouseRecOffRate:         "e803",
		mouseRecOffLeftHanded:   "01",
		mouseRecOffButtons + 12: "ea0000000400000001000000", // own code, key A, keyboard
		mouseRecOffButtons + 24: "e9000000f300000001000000", // does nothing
	} {
		raw, _ := hex.DecodeString(text)
		copy(want[offset:], raw)
	}
	if got := mouse.Record(); !bytes.Equal(got, want) {
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("record differs at %#x: % x, want % x", i, got[i:min(i+12, len(got))], want[i:min(i+12, len(want))])
			}
		}
	}

	// A button given its own click, or one more assignment, and a rename.
	read := mustReadMouse(t, c, rivieraMouseTarget)
	read.Assignments[1].Target = RecordKeyTarget{Kind: RecordTargetMouse, Mouse: RecordMouseRight}
	read.Assignments[2].Target = RecordKeyTarget{Kind: RecordTargetMedia, Media: 205}
	read.Name = "Sixteen letters!"
	mustApplyMouse(t, c, rivieraMouseTarget, read)
	if got := hex.EncodeToString(mouse.Record()[mouseRecOffButtons+12 : mouseRecOffButtons+36]); got != "ea000000ea00000000000000"+"e9000000cd00000002000000" {
		t.Fatalf("button entries: %s", got)
	}
	if got := decodePadName(mouse.Record()[mouseRecOffName : mouseRecOffName+padNameLen]); got != "Sixteen letters!" {
		t.Fatalf("name %q", got)
	}
	if mouse.Record()[0x24] != 0x7e || mouse.Record()[mouseRecOffLiftOff] != 2 || !bytes.Equal(mouse.Record()[0x4a:0x50], before[0x4a:0x50]) {
		t.Fatal("bytes this program does not model were changed")
	}
}

func TestRivieraMouseRefusesWhatItCannotHoldBeforeSendingAnything(t *testing.T) {
	for name, edit := range map[string]func(*MouseProfile){
		"DPI above 12000":         func(p *MouseProfile) { p.Dpi[1].X = 12050 },
		"DPI off the 50 grid":     func(p *MouseProfile) { p.Dpi[1].X = 825 },
		"a vertical DPI":          func(p *MouseProfile) { p.Dpi[1].Y = 400 },
		"a split stage":           func(p *MouseProfile) { p.Dpi[1].SplitXY = true },
		"stage in use off":        func(p *MouseProfile) { p.Dpi[0].Enabled = false },
		"2000 Hz":                 func(p *MouseProfile) { p.PollingRate = 2000 },
		"a lift-off distance":     func(p *MouseProfile) { p.LiftOff = 2 },
		"wheel speed 15":          func(p *MouseProfile) { p.WheelSpeed = 15 },
		"the left button":         func(p *MouseProfile) { p.Assignments[0].Target = RecordKeyTarget{Kind: RecordTargetOff} },
		"a side button":           func(p *MouseProfile) { p.Assignments[3].Target = RecordKeyTarget{Kind: RecordTargetKey, Key: 4} },
		"a macro":                 func(p *MouseProfile) { p.Assignments[1].Target = RecordKeyTarget{Kind: RecordTargetMacro} },
		"the Fn key":              func(p *MouseProfile) { p.Assignments[1].Target = RecordKeyTarget{Kind: RecordTargetFn} },
		"a key that is no key":    func(p *MouseProfile) { p.Assignments[1].Target = RecordKeyTarget{Kind: RecordTargetKey, Key: 0x03} },
		"a media key not offered": func(p *MouseProfile) { p.Assignments[1].Target = RecordKeyTarget{Kind: RecordTargetMedia, Media: 1} },
		"a Retro R8 function":     func(p *MouseProfile) { p.Assignments[1].Function = 128 },
		"a Retro R8 macro": func(p *MouseProfile) {
			p.Macros[0] = MouseMacro{Steps: []MouseMacroStep{{Kind: MouseStepKeyDown, Code: 4}}, Repeat: 1}
		},
		"a name of 17 characters": func(p *MouseProfile) { p.Name = "Seventeen letters" },
		"an empty name":           func(p *MouseProfile) { p.Name = "" },
	} {
		mouse := rivieraMouseSim()
		c := mouseCore(t, mouse)
		edited := mustReadMouse(t, c, rivieraMouseTarget)
		edited.Assignments = append([]MouseAssignment(nil), edited.Assignments...)
		edit(&edited)
		from := len(mouse.Frames)
		_, err := c.MouseApply(context.Background(), rivieraMouseTarget, edited, RuntimeUnlockPolicy{})
		var coreErr *Error
		if !errors.As(err, &coreErr) || coreErr.Kind != KindInvalidState {
			t.Errorf("%s: expected a refusal, got %v", name, err)
		}
		if len(mouse.Frames) != from+3 { // the three reads of the record
			t.Errorf("%s: %d frames were sent", name, len(mouse.Frames)-from)
		}
	}
}

func TestRivieraMouseRollsBackAndCreatesAProfile(t *testing.T) {
	ctx := context.Background()

	// A mouse that acknowledges and drops writes: caught by the readback.
	mouse := rivieraMouseSim()
	before := append([]byte(nil), mouse.Record()...)
	c := mouseCore(t, mouse)
	edited := mustReadMouse(t, c, rivieraMouseTarget)
	edited.LeftHanded, edited.PollingRate = true, 250
	mouse.IgnoreWrites = true
	report, err := c.MouseApply(ctx, rivieraMouseTarget, edited, RuntimeUnlockPolicy{})
	if err != nil || report.WriteApplied || !report.RollbackSucceeded || !strings.Contains(report.WriteError, "did not keep") || !bytes.Equal(mouse.Record(), before) {
		t.Fatalf("report=%+v err=%v", report, err)
	}

	// One that takes a byte less than it was sent: an error, and the
	// rollback's writes fare no better.
	mouse = rivieraMouseSim()
	c = mouseCore(t, mouse)
	edited = mustReadMouse(t, c, rivieraMouseTarget)
	edited.PollingRate = 250
	mouse.ShortAccept = true
	report, err = c.MouseApply(ctx, rivieraMouseTarget, edited, RuntimeUnlockPolicy{})
	if err != nil || report.WriteApplied || !report.RollbackAttempted || !report.RollbackFailed() || !strings.Contains(report.WriteError, "accepted 1 of 2") {
		t.Fatalf("report=%+v err=%v", report, err)
	}

	// A mouse without a profile is given a whole one, and it needs a name.
	mouse = protocol.NewRivieraMouseSimulator()
	mouse.Record()[0x24] = 0x7e // not carried into a new profile
	c = mouseCore(t, mouse)
	edited = mustReadMouse(t, c, rivieraMouseTarget)
	edited.PollingRate = 1000
	from := len(mouse.Frames)
	if _, err := c.MouseApply(ctx, rivieraMouseTarget, edited, RuntimeUnlockPolicy{}); err == nil || rivieraWrites(mouse, from) != 0 {
		t.Fatalf("a profile without a name: err=%v", err)
	}
	edited.Name = "New"
	from = len(mouse.Frames)
	created := mustApplyMouse(t, c, rivieraMouseTarget, edited)
	// 140 bytes in three reports.
	if got := rivieraWrites(mouse, from); got != 3 {
		t.Fatalf("%d record writes", got)
	}
	want := defaultMouseRecord()
	binary.LittleEndian.PutUint32(want, kbRecInUse)
	copy(want[mouseRecOffName:], "\x00N\x00e\x00w")
	binary.LittleEndian.PutUint16(want[mouseRecOffRate:], 1000)
	if !bytes.Equal(mouse.Record(), want) {
		t.Fatalf("created record:\n% x\nwant\n% x", mouse.Record(), want)
	}
	if got := hex.EncodeToString(want[mouseRecOffButtons : mouseRecOffButtons+24]); got != "e8000000e800000000000000"+"ea000000ea00000000000000" {
		t.Fatalf("default button entries: %s", got)
	}
	// Restoring the backup puts back what the mouse held before.
	if err := c.RestoreBackup(ctx, created.BackupID); err != nil {
		t.Fatal(err)
	}
	blank := make([]byte, protocol.MouseRecordSize)
	blank[0x24] = 0x7e
	if !bytes.Equal(mouse.Record(), blank) {
		t.Fatal("the record was not restored")
	}
}

func TestMouseIsNotTouchedAsShipped(t *testing.T) {
	ctx := context.Background()
	var coreErr *Error
	// As shipped, with every gate the caller has open, a mouse is refused
	// and nothing is sent: its tier is detect-only.
	for _, target := range []protocol.VidPid{retroMouseTarget, retroReceiverTarget, rivieraMouseTarget} {
		mouse := protocol.NewMouseSimulator(target == retroReceiverTarget)
		c := New(Config{})
		c.transportOverride = mouse
		c.SetAdvancedMode(true)
		if _, err := c.MouseReadProfile(ctx, target); !errors.As(err, &coreErr) || coreErr.Reason != ReasonUnsupportedPid {
			t.Fatalf("%s read: %v", target, err)
		}
		unlocked := RuntimeUnlockPolicy{AdvancedMode: true, AcknowledgedRisk: true, UnlockFilePresent: true}
		if _, err := c.MouseApply(ctx, target, MouseProfile{}, unlocked); !errors.As(err, &coreErr) || coreErr.Reason != ReasonUnsupportedPid {
			t.Fatalf("%s apply: %v", target, err)
		}
		if len(mouse.Frames) != 0 {
			t.Fatalf("%s: %d frames were sent to a detect-only mouse", target, len(mouse.Frames))
		}
	}
	if supportsMouse(retroMouseTarget) || supportsMouse(rivieraMouseTarget) {
		t.Fatal("no mouse is supported until its tier is raised")
	}
}

func TestMouseIsNotTouchedWithoutTheUnlock(t *testing.T) {
	ctx := context.Background()
	// With the tier raised and no simulator in place a real mouse would be
	// opened: reading needs advanced mode, writing the whole unlock
	// ceremony.
	c := mouseCore(t, nil)
	var coreErr *Error
	if _, err := c.MouseReadProfile(ctx, retroMouseTarget); !errors.As(err, &coreErr) || coreErr.Reason != ReasonExperimentalRequired {
		t.Fatalf("read without advanced mode: %v", err)
	}
	if _, err := c.MouseReceiverLinked(ctx, retroReceiverTarget); !errors.As(err, &coreErr) || coreErr.Reason != ReasonExperimentalRequired {
		t.Fatalf("link question without advanced mode: %v", err)
	}
	unlocked := RuntimeUnlockPolicy{AdvancedMode: true, AcknowledgedRisk: true, UnlockFilePresent: true}
	if _, err := c.MouseApply(ctx, retroMouseTarget, MouseProfile{}, unlocked); !errors.As(err, &coreErr) || coreErr.Reason != ReasonNotHardwareConfirmed {
		t.Fatalf("apply without advanced mode: %v", err)
	}
	c.SetAdvancedMode(true)
	for _, policy := range []RuntimeUnlockPolicy{
		{}, {AdvancedMode: true, AcknowledgedRisk: true}, {AdvancedMode: true, UnlockFilePresent: true}, {AcknowledgedRisk: true, UnlockFilePresent: true},
	} {
		if _, err := c.MouseApply(ctx, rivieraMouseTarget, MouseProfile{}, policy); !errors.As(err, &coreErr) || coreErr.Reason != ReasonNotHardwareConfirmed {
			t.Fatalf("apply with %+v: %v", policy, err)
		}
	}
	// Mock mode's simulated devices are not mice.
	mock := New(Config{MockMode: true})
	if _, err := mock.MouseReadProfile(ctx, retroMouseTarget); !errors.As(err, &coreErr) || coreErr.Reason != ReasonFeatureUnavailable {
		t.Fatalf("read in mock mode: %v", err)
	}
	// And a device of another kind is refused outright.
	if _, err := mouseCore(t, retroMouseSim(false)).MouseReadProfile(ctx, padTarget); !errors.As(err, &coreErr) || coreErr.Reason != ReasonUnsupportedPid {
		t.Fatalf("read of a controller: %v", err)
	}
}
