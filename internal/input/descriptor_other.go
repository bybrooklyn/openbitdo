//go:build !linux

package input

import "fmt"

// fetchReportDescriptor has no portable implementation on this platform:
// karalabe/hid doesn't expose report descriptors, and there's no
// cgo-free OS facility for it here the way Linux's hidraw sysfs export
// provides one. See spec/gamepad_input.md — this is a documented gap, not a
// guessed byte layout standing in for one.
func fetchReportDescriptor(devicePath string) ([]byte, error) {
	return nil, fmt.Errorf("report descriptor acquisition is not implemented on this platform (device %s)", devicePath)
}

const descriptorAcquisitionSupported = false
