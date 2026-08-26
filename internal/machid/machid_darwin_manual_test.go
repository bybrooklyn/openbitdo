//go:build darwin

package machid

import (
	"fmt"
	"testing"
	"time"
)

// TestManualGetModeRoundTrip sends CommandGetMode (Report ID 0x81, opcode
// 0x04 0x05 0x01) -- the registry's only Confidence:"confirmed" command with
// AppliesTo:nil (applies to every PID, not just a documented subset). It
// documents two confirmed, separate results rather than one:
//
//  1. The transport itself works: Write reports IOReturn success every
//     time, and a synchronous IOHIDDeviceGetReport for the Input report
//     returns a real USB STALL (not silence, not an OS-level error) --
//     proof the device is live and answering requests at the USB layer.
//  2. No command gets an actual response: this was true for GetPid (ruled
//     out separately: no AppliesTo entry at all for PID 0x6012/0x6013),
//     CommandU2GetCurrentSlot (Confidence:"inferred", so inconclusive on its
//     own), and now GetMode despite being the strongest possible candidate.
//     Both write-byte conventions (report ID stripped vs. kept in the
//     payload) and repeated attempts with settle delays made no difference.
//
// See the package doc for why: the registry's "confirmed" reflects static
// analysis of the vendor binary, not a runtime-verified hardware response,
// and no dossier exists yet for this PID. This test intentionally does not
// assert success -- it's a live probe for whoever picks up the protocol
// reverse-engineering work, not a pass/fail gate on the transport fix.
func TestManualGetModeRoundTrip(t *testing.T) {
	dev, err := Open(0x2dc8, 0x6013, 0xffa0, 0x0001)
	if err != nil {
		t.Skipf("no real device to test against (expected in CI): %v", err)
	}
	defer func() { _ = dev.Close() }()

	request := make([]byte, 64)
	request[0] = 0x81
	request[1] = 0x04
	request[2] = 0x05
	request[3] = 0x01

	for attempt := 1; attempt <= 3; attempt++ {
		time.Sleep(500 * time.Millisecond)
		n, err := dev.Write(request)
		if err != nil {
			t.Fatalf("attempt %d: write failed: %v", attempt, err)
		}
		t.Logf("attempt %d: wrote %d bytes", attempt, n)

		buf := make([]byte, 128)
		readDone := make(chan struct{})
		var readN int
		var readErr error
		go func() {
			readN, readErr = dev.Read(buf)
			close(readDone)
		}()
		select {
		case <-readDone:
			if readErr != nil {
				t.Logf("attempt %d: read failed: %v", attempt, readErr)
				continue
			}
			t.Logf("attempt %d: RESPONSE (%d bytes): % x", attempt, readN, buf[:readN])
			return
		case <-time.After(2 * time.Second):
			t.Logf("attempt %d: no response within 2s", attempt)
		}
	}

	syncBuf, syncRes, syncErr := dev.ReadInputSync(0x02, 64)
	t.Logf("sync GetReport(Input, 0x02): IOReturn=%#x err=%v buf=% x", syncRes, syncErr, syncBuf)
	t.Log("no response to GetMode after 3 attempts -- see package doc: this is a protocol-RE gap, not a transport bug")
}

func TestManualDumpDescriptor(t *testing.T) {
	dev, err := Open(0x2dc8, 0x6013, 0xffa0, 0x0001)
	if err != nil {
		t.Skipf("no real device to test against (expected in CI): %v", err)
	}
	defer func() { _ = dev.Close() }()

	desc, err := dev.DumpDescriptor()
	if err != nil {
		t.Fatalf("dump descriptor failed: %v", err)
	}
	t.Logf("descriptor (%d bytes): % x", len(desc), desc)

	// Manually walk the HID report-descriptor bytecode looking for Input
	// (0x81/0x82 short items), Output (0x91/0x92), Feature (0xB1/0xB2),
	// and Report ID (0x85) main/global items, to see what this device
	// actually declares without needing full semantic parsing.
	i := 0
	for i < len(desc) {
		b := desc[i]
		tag := b & 0xFC
		size := int(b & 0x03)
		if size == 3 {
			size = 4
		}
		if i+1+size > len(desc) {
			break
		}
		var val uint32
		for j := 0; j < size; j++ {
			val |= uint32(desc[i+1+j]) << (8 * j)
		}
		label := ""
		switch tag {
		case 0x80:
			label = "Input"
		case 0x90:
			label = "Output"
		case 0xB0:
			label = "Feature"
		case 0x84:
			label = "ReportID"
		case 0x94:
			label = "ReportCount"
		case 0x74:
			label = "ReportSize"
		case 0xA0:
			label = "Collection"
		case 0xC0:
			label = "EndCollection"
		}
		if label != "" {
			fmt.Printf("offset %d: %s = %#x (raw byte %#02x)\n", i, label, val, b)
		}
		i += 1 + size
	}
}
