package input

import (
	"strings"
	"testing"
)

func TestOpenHintSuffixForGOOSOnlyOnLinux(t *testing.T) {
	if !strings.Contains(openHintSuffixForGOOS("linux"), "udev rule") {
		t.Fatalf("expected the linux udev hint, got %q", openHintSuffixForGOOS("linux"))
	}
	for _, goos := range []string{"darwin", "windows", "freebsd"} {
		if got := openHintSuffixForGOOS(goos); got != "" {
			t.Fatalf("goos=%s: expected an empty suffix, got %q", goos, got)
		}
	}
}
