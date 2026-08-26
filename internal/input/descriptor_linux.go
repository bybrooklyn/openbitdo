//go:build linux

package input

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/karalabe/hid"
)

// fetchReportDescriptor reads a HID device's report descriptor from the
// kernel's hidraw sysfs export. karalabe/hid's Linux backend gives device
// paths of the form "/dev/hidrawN"; the matching descriptor lives at
// /sys/class/hidraw/hidrawN/device/report_descriptor.
func fetchReportDescriptor(info hid.DeviceInfo) ([]byte, error) {
	name := filepath.Base(info.Path)
	sysfsPath := filepath.Join("/sys/class/hidraw", name, "device", "report_descriptor")
	data, err := os.ReadFile(sysfsPath)
	if err != nil {
		return nil, fmt.Errorf("read report descriptor from %s: %w", sysfsPath, err)
	}
	return data, nil
}
