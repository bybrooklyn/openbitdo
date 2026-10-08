//go:build linux

package protocol

import (
	"errors"

	"github.com/bybrooklyn/openbitdo/internal/hidraw"
	"github.com/karalabe/hid"
)

// enumerateHID lists HID interfaces from the kernel's hidraw nodes rather
// than karalabe/hid's libusb backend. hidraw reports each interface's real
// usage page/usage (libusb gives 0/0 for all of them on Linux, which made a
// multi-interface device like the Retro 108 impossible to select from), and
// opening a hidraw node leaves the kernel's own HID driver attached.
func enumerateHID(vendorID, productID uint16) []hid.DeviceInfo {
	return hidInfosFromHidraw(hidraw.Enumerate(vendorID, productID))
}

func hidInfosFromHidraw(nodes []hidraw.Info) []hid.DeviceInfo {
	infos := make([]hid.DeviceInfo, 0, len(nodes))
	for _, node := range nodes {
		info := hid.DeviceInfo{
			Path:      node.Path,
			VendorID:  node.VendorID,
			ProductID: node.ProductID,
			Product:   node.Product,
			Serial:    node.Serial,
			Interface: node.Interface,
		}
		// The first top-level collection identifies the interface.
		if len(node.Usages) > 0 {
			info.UsagePage, info.Usage = node.Usages[0].Page, node.Usages[0].Usage
		}
		infos = append(infos, info)
	}
	return infos
}

// openHidDevice opens the hidraw node enumerateHID reported for info.
func openHidDevice(info hid.DeviceInfo) (hidDevice, error) {
	return hidraw.Open(info.Path)
}

func isReadTimeout(err error) bool { return errors.Is(err, hidraw.ErrTimeout) }
