package input

import (
	"encoding/hex"
	"testing"
)

// The report descriptor and input reports below were captured from a real
// 8BitDo Ultimate 2 in its gamepad mode (2dc8:6012) over USB.
const ultimate2GamepadDescriptor = "05010905a1018501050115002507463b0195017504651409398142750195048101150026ff00" +
	"09300931093209359504750881020502150026ff0009c409c5950275088102050919012918150025017501951881020600ff" +
	"0920750895178102050f0970850515002564750895049102c0"

// ultimate2Report builds a 34-byte input report: ID, hat, four stick axes at
// rest, two triggers, three button bytes, then 23 vendor bytes.
func ultimate2Report(hat byte, buttons [3]byte) []byte {
	report := make([]byte, 34)
	report[0], report[1] = 0x01, hat
	copy(report[2:6], []byte{0x7f, 0x7f, 0x7f, 0x7f})
	copy(report[8:11], buttons[:])
	return report
}

func TestDecodeUltimate2GamepadReports(t *testing.T) {
	descriptor, err := hex.DecodeString(ultimate2GamepadDescriptor)
	if err != nil {
		t.Fatal(err)
	}
	if !isGamepadDescriptor(descriptor) {
		t.Fatal("expected the descriptor to be recognised as a gamepad")
	}
	fields, err := ParseReportDescriptor(descriptor)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	cases := []struct {
		name    string
		hat     byte
		buttons [3]byte
		dpad    Direction
		pressed []uint16
	}{
		{"at rest", 0x0f, [3]byte{}, DirNone, nil},
		// btn=200000 and btn=040000 are the two extra buttons seen most.
		{"button 6", 0x0f, [3]byte{0x20, 0, 0}, DirNone, []uint16{6}},
		{"buttons 3 and 6 together", 0x0f, [3]byte{0x24, 0, 0}, DirNone, []uint16{3, 6}},
		{"button 10", 0x0f, [3]byte{0, 0x02, 0}, DirNone, []uint16{10}},
		{"buttons 17 and 18", 0x0f, [3]byte{0, 0, 0x03}, DirNone, []uint16{17, 18}},
		{"d-pad up", 0x00, [3]byte{}, DirUp, nil},
		{"d-pad right", 0x02, [3]byte{}, DirRight, nil},
		{"d-pad down", 0x04, [3]byte{}, DirDown, nil},
		{"d-pad left", 0x06, [3]byte{}, DirLeft, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := DecodeReport(fields, 0x01, ultimate2Report(tc.hat, tc.buttons))
			if state.DPad != tc.dpad {
				t.Errorf("d-pad = %v, want %v", state.DPad, tc.dpad)
			}
			if len(state.Buttons) != len(tc.pressed) {
				t.Errorf("buttons = %v, want %v", state.Buttons, tc.pressed)
			}
			for _, button := range tc.pressed {
				if !state.Buttons[button] {
					t.Errorf("button %d not reported pressed: %v", button, state.Buttons)
				}
			}
		})
	}
}
