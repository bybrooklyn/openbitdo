//go:build manual

package protocol

import "testing"

// TestIsDevicePresentFalseForAnUnconnectedVidPid exercises the real (not
// injected) enumeration path. 0xFFFF isn't a real assigned USB vendor ID,
// so no device with it can genuinely be connected to any test machine.
func TestIsDevicePresentFalseForAnUnconnectedVidPid(t *testing.T) {
	if IsDevicePresent(VidPid{VID: 0xFFFF, PID: 0xFFFF}) {
		t.Fatal("expected no device to be present for an unassigned vendor ID")
	}
}
