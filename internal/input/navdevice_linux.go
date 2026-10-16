//go:build linux

package input

import (
	"github.com/bybrooklyn/openbitdo/internal/hidraw"
	"github.com/karalabe/hid"
)

// enumerateNavDevices lists 8BitDo HID interfaces from the kernel's hidraw
// nodes, so each Path is a /dev/hidrawN that fetchReportDescriptor can find
// in sysfs and openNavDevice can open without detaching the kernel driver.
func enumerateNavDevices() []hid.DeviceInfo {
	nodes := hidraw.Enumerate(bitdoVID, 0)
	infos := make([]hid.DeviceInfo, 0, len(nodes))
	for _, node := range nodes {
		info := hid.DeviceInfo{
			Path: node.Path, VendorID: node.VendorID, ProductID: node.ProductID,
			Product: node.Product, Serial: node.Serial, Interface: node.Interface,
		}
		if len(node.Usages) > 0 {
			info.UsagePage, info.Usage = node.Usages[0].Page, node.Usages[0].Usage
		}
		infos = append(infos, info)
	}
	return infos
}

func openNavDevice(info hid.DeviceInfo) (navDevice, error) {
	return hidraw.Open(info.Path)
}
