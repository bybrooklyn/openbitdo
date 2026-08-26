//go:build !linux && !darwin

package input

import (
	"fmt"

	"github.com/karalabe/hid"
)

// fetchReportDescriptor has no implementation on this platform: karalabe/hid
// doesn't expose report descriptors at the Go API level, and there's no
// portable OS facility for it here the way Linux's hidraw sysfs export and
// macOS's IOHIDManager property lookup (see descriptor_darwin.go) provide.
// See spec/gamepad_input.md — this is a documented gap, not a guessed byte
// layout standing in for one.
func fetchReportDescriptor(info hid.DeviceInfo) ([]byte, error) {
	return nil, fmt.Errorf("report descriptor acquisition is not implemented on this platform (device pid=%#04x)", info.ProductID)
}
