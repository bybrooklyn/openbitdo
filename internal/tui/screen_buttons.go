package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The Buttons tab shows what a controller is sending right now: press
// anything and its number lights up. It only reads the input reports the
// controller already sends to the computer, so it is safe on any device,
// and it is the quickest way to learn which number a given button is (the
// back buttons and extra shoulder buttons have no standard one).

// padState is what has been seen from one controller this session.
type padState struct {
	held    map[uint16]bool
	presses map[uint16]int
	last    uint16
	dpad    input.Direction
	events  int
}

// record returns s updated with one input event. It copies its maps: the
// model is passed by value, and an earlier copy must not change under a
// frame that is still being drawn.
func (s padState) record(e input.NavEvent) padState {
	next := padState{
		held: make(map[uint16]bool, len(s.held)+1), presses: make(map[uint16]int, len(s.presses)+1),
		last: s.last, dpad: s.dpad, events: s.events + 1,
	}
	for k, v := range s.held {
		next.held[k] = v
	}
	for k, v := range s.presses {
		next.presses[k] = v
	}
	switch e.Kind {
	case input.EventButtonDown:
		next.held[e.Button] = true
		next.presses[e.Button]++
		next.last = e.Button
	case input.EventButtonUp:
		delete(next.held, e.Button)
	case input.EventDPadChanged:
		next.dpad = e.DPad
	}
	return next
}

// recordInput notes a controller event against the device that sent it.
func (m Model) recordInput(e input.NavEvent) Model {
	pads := make(map[uint16]padState, len(m.pads)+1)
	for pid, state := range m.pads {
		pads[pid] = state
	}
	pads[e.SourcePID] = pads[e.SourcePID].record(e)
	m.pads = pads
	return m
}

func (m Model) updateButtons(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.screen = screenDevices
	case "c":
		if device, ok := m.devices.selected(); ok {
			pads := make(map[uint16]padState, len(m.pads))
			for pid, state := range m.pads {
				if pid != device.VidPid.PID {
					pads[pid] = state
				}
			}
			m.pads = pads
		}
	}
	return m, nil
}

func dpadLabel(d input.Direction) string {
	switch d {
	case input.DirUp:
		return "up"
	case input.DirUpRight:
		return "up-right"
	case input.DirRight:
		return "right"
	case input.DirDownRight:
		return "down-right"
	case input.DirDown:
		return "down"
	case input.DirDownLeft:
		return "down-left"
	case input.DirLeft:
		return "left"
	case input.DirUpLeft:
		return "up-left"
	}
	return "centre"
}

// buttonsOnShow is how many button numbers the grid shows before any press
// asks for more.
const buttonsOnShow = 16

func (m Model) viewButtons(height int) string {
	text := max(1, m.width-4)
	var lines []string
	add := func(style lipgloss.Style, s string) {
		lines = append(lines, strings.Split(wrapStyled(style, s, text), "\n")...)
	}

	device, ok := m.devices.selected()
	if !ok {
		add(styleFaint, "No device selected.")
		return renderBoundedPanel(m.width-2, height-2, strings.Join(lines, "\n"))
	}
	state := m.pads[device.VidPid.PID]

	if device.WorksAs != core.RoleGamepad && state.events == 0 {
		lines = append(lines, stylePanelTitle.Render(truncate("No button presses to show", text)), "")
		switch {
		case device.WorksAs == core.RoleKeyboard:
			add(styleBody, "This is a keyboard. Its keys go to whatever you are typing in, so they are not shown here.")
		case device.ProtocolFamily == protocol.JpHandshake:
			add(styleBody, "This device isn't sending button presses to the computer.")
		default:
			add(styleBody, "In its current mode this controller isn't sending button presses to the computer, so there is nothing to watch.")
			lines = append(lines, "")
			add(styleFaint, "Switch it to a mode where it works as a gamepad (its mode switch, or the other connection) and this tab will follow by itself.")
		}
		return renderBoundedPanel(m.width-2, height-2, strings.Join(lines, "\n"))
	}

	lines = append(lines, stylePanelTitle.Render(truncate("Press anything on the controller", text)))
	add(styleFaint, "Each button lights up under its number. While this tab is open the controller doesn't drive the menus.")
	lines = append(lines, "")

	highest := buttonsOnShow
	for button := range state.presses {
		highest = max(highest, int(button))
	}
	// Rows of eight where there is room, fewer where there is not.
	const cell = 5
	perRow := clampInt(text/cell, 4, 8)
	var row strings.Builder
	for button := 1; button <= highest; button++ {
		label := fmt.Sprintf("%3d ", button)
		switch {
		case state.held[uint16(button)]:
			row.WriteString(styleSelectedRow.Render(label) + " ")
		case state.presses[uint16(button)] > 0:
			row.WriteString(styleBody.Render(label) + " ")
		default:
			row.WriteString(styleFaint.Render(label) + " ")
		}
		if button%perRow == 0 || button == highest {
			lines = append(lines, row.String())
			row.Reset()
		}
	}

	lines = append(lines, "", styleBody.Render("D-pad: ")+styleAccent.Render(dpadLabel(state.dpad)))
	if state.last == 0 {
		lines = append(lines, styleFaint.Render("Nothing pressed yet."))
	} else {
		lines = append(lines, styleBody.Render(fmt.Sprintf("Last pressed: button %d", state.last))+
			styleFaint.Render(fmt.Sprintf("  (%d times)", state.presses[state.last])))
		seen := make([]int, 0, len(state.presses))
		for button := range state.presses {
			seen = append(seen, int(button))
		}
		sort.Ints(seen)
		parts := make([]string, len(seen))
		for i, button := range seen {
			parts[i] = fmt.Sprint(button)
		}
		add(styleFaint, "Seen so far: "+strings.Join(parts, " "))
	}
	return renderBoundedPanel(m.width-2, height-2, strings.Join(lines, "\n"))
}
