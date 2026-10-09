//go:build linux

package hidraw

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	sysClassDir = "/sys/class/hidraw"
	devDir      = "/dev"
)

// Info describes one hidraw node.
type Info struct {
	VendorID  uint16
	ProductID uint16
	Product   string
	Serial    string
	// Path is the device node to open, e.g. /dev/hidraw3.
	Path string
	// Interface is the USB interface number, or 0 when the device is not USB.
	Interface int
	// Usages are the top-level collections of the report descriptor.
	Usages []Usage
	// Descriptor is the raw HID report descriptor.
	Descriptor []byte
}

// Enumerate lists hidraw nodes. A zero vendorID or productID matches any.
func Enumerate(vendorID, productID uint16) []Info {
	return enumerateAt(sysClassDir, devDir, vendorID, productID)
}

func enumerateAt(sysDir, devDir string, vendorID, productID uint16) []Info {
	entries, err := os.ReadDir(sysDir)
	if err != nil {
		return nil
	}
	infos := make([]Info, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		device := filepath.Join(sysDir, name, "device")
		uevent, err := os.ReadFile(filepath.Join(device, "uevent"))
		if err != nil {
			continue
		}
		info, ok := parseUevent(string(uevent))
		if !ok {
			continue
		}
		if vendorID != 0 && info.VendorID != vendorID {
			continue
		}
		if productID != 0 && info.ProductID != productID {
			continue
		}
		info.Path = filepath.Join(devDir, name)
		info.Interface = usbInterfaceNumber(device)
		// A node whose descriptor cannot be read is still listed, with no
		// usages, so the caller can report it rather than silently lose it.
		if descriptor, err := os.ReadFile(filepath.Join(device, "report_descriptor")); err == nil {
			info.Descriptor = descriptor
			info.Usages = TopLevelUsages(descriptor)
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Path < infos[j].Path })
	return infos
}

// parseUevent reads the HID_ID, HID_NAME and HID_UNIQ keys of a HID device's
// uevent file. HID_ID is "bus:vendor:product" in hex.
func parseUevent(uevent string) (Info, bool) {
	var info Info
	found := false
	for _, line := range strings.Split(uevent, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "HID_ID":
			parts := strings.Split(value, ":")
			if len(parts) != 3 {
				return Info{}, false
			}
			vendor, err := strconv.ParseUint(parts[1], 16, 32)
			if err != nil {
				return Info{}, false
			}
			product, err := strconv.ParseUint(parts[2], 16, 32)
			if err != nil {
				return Info{}, false
			}
			info.VendorID, info.ProductID = uint16(vendor), uint16(product)
			found = true
		case "HID_NAME":
			info.Product = value
		case "HID_UNIQ":
			info.Serial = value
		}
	}
	return info, found
}

// usbInterfaceNumber returns the bInterfaceNumber of the USB interface a HID
// device sits on. The HID device's parent directory is named like
// "1-2.3:1.0", where the final number is the interface.
func usbInterfaceNumber(deviceLink string) int {
	resolved, err := filepath.EvalSymlinks(deviceLink)
	if err != nil {
		return 0
	}
	parent := filepath.Base(filepath.Dir(resolved))
	_, config, ok := strings.Cut(parent, ":")
	if !ok {
		return 0
	}
	_, iface, ok := strings.Cut(config, ".")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(iface)
	if err != nil {
		return 0
	}
	return n
}

// ErrTimeout is returned by ReadTimeout when no report arrives in time.
var ErrTimeout = errors.New("hidraw: read timed out")

// Device is an open hidraw node.
type Device struct {
	file *os.File
}

// Open opens a hidraw node for reading and writing. The returned error wraps
// the OS error, so errors.Is(err, fs.ErrPermission) and friends work.
func Open(path string) (*Device, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	// A non-blocking descriptor is registered with the runtime poller, which
	// is what makes read deadlines and Close-unblocks-Read work.
	file := os.NewFile(uintptr(fd), path)
	if err := file.SetReadDeadline(time.Time{}); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("hidraw: %s does not support timed reads: %w", path, err)
	}
	return &Device{file: file}, nil
}

// Read blocks until a report arrives or the device is closed.
func (d *Device) Read(buf []byte) (int, error) {
	if err := d.file.SetReadDeadline(time.Time{}); err != nil {
		return 0, err
	}
	return d.file.Read(buf)
}

// ReadTimeout waits up to timeout for a report, returning ErrTimeout if none
// arrives. A report with a numbered ID includes that ID as its first byte.
func (d *Device) ReadTimeout(buf []byte, timeout time.Duration) (int, error) {
	if err := d.file.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}
	n, err := d.file.Read(buf)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return 0, ErrTimeout
	}
	return n, err
}

// Write sends one output report. The first byte is the report ID, or 0 for a
// device that does not number its reports.
func (d *Device) Write(data []byte) (int, error) {
	return d.file.Write(data)
}

// Close releases the node and unblocks any pending Read.
func (d *Device) Close() error {
	return d.file.Close()
}
