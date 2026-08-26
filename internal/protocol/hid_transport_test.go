package protocol

import (
	"errors"
	"strings"
	"testing"
)

func TestWithOpenHintForGOOSAddsHintOnlyOnLinux(t *testing.T) {
	base := errors.New("open failed")

	got := withOpenHintForGOOS(base, "linux")
	if !strings.Contains(got.Error(), "udev rule") {
		t.Fatalf("expected the linux udev hint in the error, got %q", got.Error())
	}
	if !strings.Contains(got.Error(), base.Error()) {
		t.Fatalf("expected the original error message to be preserved, got %q", got.Error())
	}

	for _, goos := range []string{"darwin", "windows", "freebsd"} {
		got := withOpenHintForGOOS(base, goos)
		if got.Error() != base.Error() {
			t.Fatalf("goos=%s: expected the error to pass through unchanged, got %q", goos, got.Error())
		}
	}
}

func TestWithOpenHintForGOOSPassesThroughNil(t *testing.T) {
	if withOpenHintForGOOS(nil, "linux") != nil {
		t.Fatal("expected a nil error to stay nil regardless of GOOS")
	}
}

// TestIsDevicePresentFalseForAnUnconnectedVidPid exercises the real (not
// injected) enumeration path. 0xFFFF isn't a real assigned USB vendor ID,
// so no device with it can genuinely be connected to any test machine --
// this is a real negative case, not a mock.
func TestIsDevicePresentFalseForAnUnconnectedVidPid(t *testing.T) {
	if IsDevicePresent(VidPid{VID: 0xFFFF, PID: 0xFFFF}) {
		t.Fatal("expected no device to be present for an unassigned vendor ID")
	}
}
