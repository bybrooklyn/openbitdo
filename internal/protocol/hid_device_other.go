//go:build !darwin && !linux

package protocol

import "github.com/karalabe/hid"

// enumerateHID lists HID interfaces the karalabe/hid way.
func enumerateHID(vendorID, productID uint16) []hid.DeviceInfo {
	return hid.Enumerate(vendorID, productID)
}

// openHidDevice opens info the normal karalabe/hid way.
func openHidDevice(info hid.DeviceInfo) (hidDevice, error) {
	return info.Open()
}

func isReadTimeout(error) bool { return false }
