package input

import "testing"

// sampleJoystickDescriptor is a minimal, standard-compliant HID report
// descriptor for a 2-axis/hat-switch/8-button joystick, hand-encoded byte by
// byte against the USB HID 1.11 item-encoding rules (each byte's derivation
// is checked in the corresponding review). This is a self-contained public
// USB-HID-standard example, not vendor material — used purely to validate
// the parser/decoder against a known-correct layout, since no hardware is
// available to capture a real descriptor from.
var sampleJoystickDescriptor = []byte{
	0x05, 0x01, // Usage Page (Generic Desktop)
	0x09, 0x04, // Usage (Joystick)
	0xA1, 0x01, // Collection (Application)
	0x09, 0x30, //   Usage (X)
	0x09, 0x31, //   Usage (Y)
	0x15, 0x81, //   Logical Minimum (-127)
	0x25, 0x7F, //   Logical Maximum (127)
	0x75, 0x08, //   Report Size (8)
	0x95, 0x02, //   Report Count (2)
	0x81, 0x02, //   Input (Data,Var,Abs)
	0x09, 0x39, //   Usage (Hat switch)
	0x15, 0x00, //   Logical Minimum (0)
	0x25, 0x07, //   Logical Maximum (7)
	0x75, 0x04, //   Report Size (4)
	0x95, 0x01, //   Report Count (1)
	0x81, 0x42, //   Input (Data,Var,Abs,Null)
	0x05, 0x09, //   Usage Page (Button)
	0x19, 0x01, //   Usage Minimum (1)
	0x29, 0x08, //   Usage Maximum (8)
	0x15, 0x00, //   Logical Minimum (0)
	0x25, 0x01, //   Logical Maximum (1)
	0x75, 0x01, //   Report Size (1)
	0x95, 0x08, //   Report Count (8)
	0x81, 0x02, //   Input (Data,Var,Abs)
	0xC0, // End Collection
}

func TestParseReportDescriptorExtractsAllFields(t *testing.T) {
	fields, err := ParseReportDescriptor(sampleJoystickDescriptor)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 1 X + 1 Y + 1 Hat + 8 Buttons = 11 fields.
	if len(fields) != 11 {
		t.Fatalf("expected 11 fields, got %d: %+v", len(fields), fields)
	}

	byUsage := func(page, usage uint16) (Field, bool) {
		for _, f := range fields {
			if f.UsagePage == page && f.Usage == usage {
				return f, true
			}
		}
		return Field{}, false
	}

	x, ok := byUsage(UsagePageGenericDesktop, UsageX)
	if !ok || x.BitOffset != 0 || x.BitSize != 8 {
		t.Fatalf("X field wrong: %+v ok=%v", x, ok)
	}
	y, ok := byUsage(UsagePageGenericDesktop, UsageY)
	if !ok || y.BitOffset != 8 || y.BitSize != 8 {
		t.Fatalf("Y field wrong: %+v ok=%v", y, ok)
	}
	hat, ok := byUsage(UsagePageGenericDesktop, UsageHatSwitch)
	if !ok || hat.BitOffset != 16 || hat.BitSize != 4 || hat.LogicalMax != 7 {
		t.Fatalf("Hat field wrong: %+v ok=%v", hat, ok)
	}
	button1, ok := byUsage(UsagePageButton, 1)
	if !ok || button1.BitOffset != 20 || button1.BitSize != 1 {
		t.Fatalf("Button 1 field wrong: %+v ok=%v", button1, ok)
	}
	button8, ok := byUsage(UsagePageButton, 8)
	if !ok || button8.BitOffset != 27 || button8.BitSize != 1 {
		t.Fatalf("Button 8 field wrong: %+v ok=%v", button8, ok)
	}
}
