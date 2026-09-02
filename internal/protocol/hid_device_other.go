//go:build !darwin

package protocol

import "github.com/karalabe/hid"

// openHidDevice opens info the normal karalabe/hid way. The Path-resolution
// bug that forces internal/machid's existence on darwin (see
// hid_device_darwin.go) is specific to hidapi's mac backend; other
// platforms' backends populate Path correctly, so hid.DeviceInfo.Open()
// works as-is.
func openHidDevice(info hid.DeviceInfo) (hidDevice, error) {
	return info.Open()
}
