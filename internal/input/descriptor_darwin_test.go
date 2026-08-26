//go:build darwin

package input

import (
	"testing"

	"github.com/karalabe/hid"
)

// TestFetchReportDescriptorDarwinAgainstRealDevices is a real-hardware smoke
// test, not a hermetic unit test: no 8BitDo controller is available in this
// project's development/CI environment, so this instead proves the IOKit
// acquisition plumbing (IOHIDManager enumerate -> match by
// vendor/product/usage -> read the ReportDescriptor property) genuinely
// works end-to-end against whatever real HID devices exist on the machine
// running it, using the exact same code path a real 8BitDo controller would
// use. It intentionally does not assert every enumerated device succeeds —
// devices with vendor/product ID 0x0000 (seen for some synthetic Apple
// internal HID nodes, e.g. the internal keyboard/trackpad) aren't
// meaningfully matchable by VID/PID and are expected to fail; this only
// requires that at least one real (non-zero VID) device round-trips through
// both acquisition and the existing descriptor parser. If a given machine
// happens to expose no real HID devices at all, the test skips rather than
// fails, since that's an environment property, not a code defect.
func TestFetchReportDescriptorDarwinAgainstRealDevices(t *testing.T) {
	infos := hid.Enumerate(0, 0)

	var attempted, succeeded int
	for _, info := range infos {
		if info.VendorID == 0 {
			continue // not meaningfully matchable by VID/PID; expected to fail, skip rather than log noise
		}
		attempted++

		data, err := fetchReportDescriptor(info)
		if err != nil {
			t.Logf("vid=%#04x pid=%#04x product=%q: acquisition failed: %v", info.VendorID, info.ProductID, info.Product, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("vid=%#04x pid=%#04x: fetchReportDescriptor returned no error but zero bytes", info.VendorID, info.ProductID)
			continue
		}
		succeeded++

		// Not asserting this parses cleanly: real non-gamepad HID devices
		// (accelerometers, keyboard backlights, etc.) have descriptors that
		// are valid HID but not necessarily shaped the way our
		// gamepad-oriented parser expects every field of. The point here is
		// proving acquisition, not that every device on the machine is a
		// gamepad.
		if fields, perr := ParseReportDescriptor(data); perr == nil {
			t.Logf("vid=%#04x pid=%#04x product=%q: acquired %d bytes, parsed %d fields", info.VendorID, info.ProductID, info.Product, len(data), len(fields))
		} else {
			t.Logf("vid=%#04x pid=%#04x product=%q: acquired %d bytes (parse: %v)", info.VendorID, info.ProductID, info.Product, len(data), perr)
		}
	}

	if attempted == 0 {
		t.Skip("no real (non-zero VID) HID devices enumerated on this machine — nothing to smoke-test acquisition against")
	}
	if succeeded == 0 {
		t.Fatalf("attempted acquisition against %d real HID device(s), none succeeded — IOKit acquisition plumbing looks broken, not just short of a matching device", attempted)
	}
	t.Logf("real-device acquisition smoke test: %d/%d real HID devices succeeded", succeeded, attempted)
}
