package input

import "testing"

func TestDecodeReportHatAndButtons(t *testing.T) {
	fields, err := ParseReportDescriptor(sampleJoystickDescriptor)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// X=-10, Y=50, hat=2 (East), buttons 1/3/6 pressed.
	report := []byte{0xF6, 0x32, 0x52, 0x02}

	state := DecodeReport(fields, 0, report)
	if state.DPad != DirRight {
		t.Fatalf("expected DirRight (hat=East), got %v", state.DPad)
	}
	wantPressed := map[uint16]bool{1: true, 3: true, 6: true}
	for usage := uint16(1); usage <= 8; usage++ {
		want := wantPressed[usage]
		got := state.Buttons[usage]
		if got != want {
			t.Errorf("button %d: want pressed=%v, got %v", usage, want, got)
		}
	}
}

func TestDecodeReportHatNeutral(t *testing.T) {
	fields, err := ParseReportDescriptor(sampleJoystickDescriptor)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// hat nibble = 8 (0x8), out of [0,7] logical range -> neutral.
	report := []byte{0x00, 0x00, 0x08, 0x00}
	state := DecodeReport(fields, 0, report)
	if state.DPad != DirNone {
		t.Fatalf("expected DirNone for out-of-range hat, got %v", state.DPad)
	}
}

func TestDecodeReportNoButtonsPressed(t *testing.T) {
	fields, err := ParseReportDescriptor(sampleJoystickDescriptor)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	report := []byte{0x00, 0x00, 0x00, 0x00} // hat=0 (North), no buttons
	state := DecodeReport(fields, 0, report)
	if state.DPad != DirUp {
		t.Fatalf("expected DirUp (hat=North), got %v", state.DPad)
	}
	if len(state.Buttons) != 0 {
		t.Fatalf("expected no buttons pressed, got %v", state.Buttons)
	}
}

func TestExtractBitsAcrossByteBoundary(t *testing.T) {
	// bit offset 4, size 8 spans byte0's high nibble and byte1's low nibble.
	report := []byte{0xF0, 0x0F}
	got := extractBits(report, 4, 8)
	if got != 0xFF {
		t.Fatalf("expected 0xFF, got %#x", got)
	}
}
