//go:build !darwin

package input

import "github.com/karalabe/hid"

// openNavDevice opens info the normal karalabe/hid way. The Path-resolution
// bug that forces internal/machid's existence on darwin (see
// navdevice_darwin.go) is specific to hidapi's mac backend; other platforms'
// backends populate Path correctly, so hid.DeviceInfo.Open() works as-is.
func openNavDevice(info hid.DeviceInfo) (navDevice, error) {
	return info.Open()
}
