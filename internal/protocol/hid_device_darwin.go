//go:build darwin

package protocol

import (
	"github.com/bybrooklyn/openbitdo/internal/machid"
	"github.com/karalabe/hid"
)

// openHidDevice opens info via internal/machid rather than karalabe/hid's
// own Open() -- see machid's package doc for why: hidapi's mac backend
// resolves every device's Path to empty on this SDK/OS, and Open() (which
// opens purely by Path) fails for every real device as a result. machid
// re-matches by vendor/product/usage-page/usage instead.
func openHidDevice(info hid.DeviceInfo) (hidDevice, error) {
	return machid.Open(int(info.VendorID), int(info.ProductID), int(info.UsagePage), int(info.Usage))
}
