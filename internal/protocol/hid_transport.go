package protocol

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/karalabe/hid"
)

const (
	vendorConfigUsagePage uint16 = 0xffa0
	vendorConfigUsage     uint16 = 0x0001
)

// linuxUdevHint is appended to a device-open failure on Linux. karalabe/hid
// (see its Open() in hid_enabled.go) returns a hardcoded generic error on
// any failure -- the underlying C library's errno is never surfaced to Go,
// on any platform -- so this can't reliably distinguish "permission denied"
// from "device busy" or any other cause. Rather than overclaim a precise
// diagnosis it can't actually deliver, this is phrased as a likely cause and
// a thing to try, not a certainty.
const linuxUdevHint = ` (if this is a permission error: openbitdo talks to devices via libusb, ` +
	`so a udev rule targeting the "usb" subsystem is usually the fix -- see ` +
	`packaging/linux/99-openbitdo.rules, or add ` +
	`'SUBSYSTEM=="usb", ATTR{idVendor}=="2dc8", TAG+="uaccess"' ` +
	`to /etc/udev/rules.d/ and reconnect the device)`

func withLinuxOpenHint(err error) error {
	return withOpenHintForGOOS(err, runtime.GOOS)
}

// withOpenHintForGOOS is withLinuxOpenHint's testable core — goos is passed
// in explicitly so tests can exercise the linux/non-linux branches without
// needing to actually run on Linux.
func withOpenHintForGOOS(err error, goos string) error {
	if err == nil || goos != "linux" {
		return err
	}
	return errTransport("%v%s", err, linuxUdevHint)
}

// EnumeratedDevice is one HID device discovered on the system.
type EnumeratedDevice struct {
	VidPid       VidPid
	Product      string
	Manufacturer string
	Serial       string
	Path         string
	UsagePage    uint16
	Usage        uint16
	Interface    int
}

// IsVendorConfigInterface reports whether this logical HID interface is the
// vendor control channel OpenBitdo may send configuration commands through.
func (d EnumeratedDevice) IsVendorConfigInterface() bool {
	return d.UsagePage == vendorConfigUsagePage && d.Usage == vendorConfigUsage
}

// EnumerateHIDDevices lists every connected HID device.
func EnumerateHIDDevices() []EnumeratedDevice {
	return enumeratedDevicesFromHIDInfos(hid.Enumerate(0, 0))
}

func enumeratedDevicesFromHIDInfos(infos []hid.DeviceInfo) []EnumeratedDevice {
	devices := make([]EnumeratedDevice, 0, len(infos))
	for _, info := range infos {
		devices = append(devices, EnumeratedDevice{
			VidPid:       VidPid{VID: info.VendorID, PID: info.ProductID},
			Product:      info.Product,
			Manufacturer: info.Manufacturer,
			Serial:       info.Serial,
			Path:         info.Path,
			UsagePage:    info.UsagePage,
			Usage:        info.Usage,
			Interface:    info.Interface,
		})
	}
	return devices
}

// IsDevicePresent re-enumerates to check whether target is still physically
// connected. karalabe/hid gives no structured error types (see
// withLinuxOpenHint's comment) and never will reliably distinguish "the
// device was unplugged" from any other I/O failure by error string alone —
// so rather than guess from an error message, this asks the OS directly.
// Used after an operation fails, to tell a genuine disconnect apart from a
// transient error on a device that's still there.
func IsDevicePresent(target VidPid) bool {
	for _, info := range hid.Enumerate(target.VID, target.PID) {
		if info.VendorID == target.VID && info.ProductID == target.PID {
			return true
		}
	}
	return false
}

// hidDevice is the read/write/close surface HidTransport needs from an
// opened device handle — satisfied by both *hid.Device (openHidDevice on
// non-darwin platforms) and *machid.Device (openHidDevice on darwin; see
// hid_device_darwin.go for why darwin needs a different Open path).
type hidDevice interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}

// HidTransport is the real hidapi-backed Transport.
type HidTransport struct {
	mu        sync.Mutex
	device    hidDevice
	target    VidPid
	enumerate func(uint16, uint16) []hid.DeviceInfo
	open      func(hid.DeviceInfo) (hidDevice, error)
}

// NewHidTransport returns an unopened HID transport.
func NewHidTransport() *HidTransport {
	return newHidTransport(hid.Enumerate, openHidDevice)
}

func newHidTransport(enumerate func(uint16, uint16) []hid.DeviceInfo, open func(hid.DeviceInfo) (hidDevice, error)) *HidTransport {
	return &HidTransport{enumerate: enumerate, open: open}
}

func isVendorConfigInterface(info hid.DeviceInfo) bool {
	return info.UsagePage == vendorConfigUsagePage && info.Usage == vendorConfigUsage
}

// selectVendorConfigInterface deterministically selects the vendor control
// interface regardless of enumeration order. Known gamepad/other interfaces
// are never a fallback. Linux's sole-interface 0/0 metadata case is allowed
// only when there is exactly one matching interface and therefore no choice
// to guess between.
func selectVendorConfigInterface(target VidPid, infos []hid.DeviceInfo) (hid.DeviceInfo, error) {
	return selectVendorConfigInterfaceForGOOS(target, infos, runtime.GOOS)
}

func selectVendorConfigInterfaceForGOOS(target VidPid, infos []hid.DeviceInfo, goos string) (hid.DeviceInfo, error) {
	matches := make([]hid.DeviceInfo, 0, 1)
	targetInfos := make([]hid.DeviceInfo, 0, len(infos))
	available := make([]string, 0, len(infos))
	for _, info := range infos {
		available = append(available, fmt.Sprintf("%#04x:%#04x interface=%d path=%q serial=%q", info.UsagePage, info.Usage, info.Interface, info.Path, info.Serial))
		if info.VendorID != target.VID || info.ProductID != target.PID {
			continue
		}
		targetInfos = append(targetInfos, info)
		if isVendorConfigInterface(info) {
			matches = append(matches, info)
		}
	}
	if len(matches) == 0 {
		// karalabe/hid cannot provide UsagePage/Usage on Linux. A sole
		// matching 0/0 interface is unambiguous and therefore safe; multiple
		// unknown interfaces are never guessed between, and known non-vendor
		// metadata never falls back.
		if goos == "linux" && len(targetInfos) == 1 && targetInfos[0].UsagePage == 0 && targetInfos[0].Usage == 0 {
			return targetInfos[0], nil
		}
		sort.Strings(available)
		detail := "none enumerated"
		if len(available) > 0 {
			detail = strings.Join(available, ", ")
		}
		if len(targetInfos) > 1 {
			allUnknown := true
			for _, info := range targetInfos {
				if info.UsagePage != 0 || info.Usage != 0 {
					allUnknown = false
					break
				}
			}
			if allUnknown {
				return hid.DeviceInfo{}, errTransport(
					"ambiguous HID interfaces for %s: %d interfaces have unknown usage metadata; vendor configuration interface %#04x:%#04x cannot be selected safely; available: %s",
					target, len(targetInfos), vendorConfigUsagePage, vendorConfigUsage, detail,
				)
			}
		}
		return hid.DeviceInfo{}, errTransport(
			"no vendor configuration HID interface (usage page %#04x, usage %#04x) found for %s; available: %s",
			vendorConfigUsagePage, vendorConfigUsage, target, detail)
	}
	if goos == "darwin" && len(matches) > 1 {
		// internal/machid re-matches by VID/PID/usage because macOS HID paths
		// are unavailable in this stack. It cannot honor a path chosen here,
		// so only duplicate matches carrying the same non-empty physical serial
		// are safe to treat as one device. Distinct/missing serials are an
		// ambiguous same-PID multi-controller open and must fail closed.
		serial := strings.TrimSpace(matches[0].Serial)
		for _, info := range matches[1:] {
			if serial == "" || strings.TrimSpace(info.Serial) != serial {
				return hid.DeviceInfo{}, errTransport(
					"ambiguous macOS vendor configuration interfaces for %s: selected physical identity cannot be honored without a unique shared serial",
					target)
			}
		}
		if serial == "" {
			return hid.DeviceInfo{}, errTransport(
				"ambiguous macOS vendor configuration interfaces for %s: selected physical identity cannot be honored without a unique shared serial",
				target)
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		left, right := matches[i], matches[j]
		if left.Serial != right.Serial {
			return left.Serial < right.Serial
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Interface < right.Interface
	})
	return matches[0], nil
}

func (h *HidTransport) Open(ctx context.Context, target VidPid) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	enumerate := h.enumerate
	if enumerate == nil {
		enumerate = hid.Enumerate
	}
	open := h.open
	if open == nil {
		open = openHidDevice
	}
	infos := enumerate(target.VID, target.PID)
	if len(infos) == 0 {
		return errTransport("no HID device found for %s", target)
	}
	selected, err := selectVendorConfigInterface(target, infos)
	if err != nil {
		return err
	}
	device, err := open(selected)
	if err != nil {
		return withLinuxOpenHint(errTransport("open failed for %s: %v", target, err))
	}

	h.mu.Lock()
	h.device = device
	h.target = target
	h.mu.Unlock()
	return nil
}

func (h *HidTransport) Close() error {
	h.mu.Lock()
	device := h.device
	h.device = nil
	h.mu.Unlock()
	if device == nil {
		return nil
	}
	if err := device.Close(); err != nil {
		return errTransport("close failed: %v", err)
	}
	return nil
}

func (h *HidTransport) Write(data []byte) (int, error) {
	h.mu.Lock()
	device := h.device
	target := h.target
	h.mu.Unlock()
	if device == nil {
		return 0, errDeviceNotOpen(target)
	}
	n, err := device.Write(data)
	if err != nil {
		return 0, errTransport("%v", err)
	}
	return n, nil
}

// Read blocks for at most timeoutMs waiting for a report. karalabe/hid's
// Device.Read has no native timeout, so the blocking read runs in its own
// goroutine and this call races it against a timer/ctx. If the device never
// responds the goroutine outlives this call (it exits once the device
// eventually returns data, errors, or is Closed) — an inherent limitation of
// the underlying library, not a leak this code introduces on the happy path.
func (h *HidTransport) Read(ctx context.Context, length int, timeoutMs uint64) ([]byte, error) {
	h.mu.Lock()
	device := h.device
	target := h.target
	h.mu.Unlock()
	if device == nil {
		return nil, errDeviceNotOpen(target)
	}

	type result struct {
		buf []byte
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		// This goroutine is independent of Bubbletea's Cmd system, so an
		// unrecovered panic here would crash the whole process. The main
		// risk is device.Read returning n > len(buf), which would panic on
		// buf[:n] below -- trusting a value the underlying cgo library
		// returns, not something this code fully controls.
		defer func() {
			if r := recover(); r != nil {
				select {
				case resultCh <- result{err: errTransport("internal error during read: %v", r)}:
				default:
				}
			}
		}()
		buf := make([]byte, length)
		n, err := device.Read(buf)
		if err != nil {
			resultCh <- result{err: errTransport("%v", err)}
			return
		}
		resultCh <- result{buf: buf[:n]}
	}()

	timer := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
	defer timer.Stop()

	select {
	case r := <-resultCh:
		if r.err != nil {
			return nil, r.err
		}
		if len(r.buf) == 0 {
			return nil, ErrTimeout
		}
		return r.buf, nil
	case <-timer.C:
		return nil, ErrTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
