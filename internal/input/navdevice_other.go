//go:build !darwin && !linux

package input

import "github.com/karalabe/hid"

func enumerateNavDevices() []hid.DeviceInfo { return hid.Enumerate(bitdoVID, 0) }

// openNavDevice opens info the normal karalabe/hid way.
func openNavDevice(info hid.DeviceInfo) (navDevice, error) {
	return info.Open()
}
