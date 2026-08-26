package protocol

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/karalabe/hid"
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
}

// EnumerateHIDDevices lists every connected HID device.
func EnumerateHIDDevices() []EnumeratedDevice {
	infos := hid.Enumerate(0, 0)
	devices := make([]EnumeratedDevice, 0, len(infos))
	for _, info := range infos {
		devices = append(devices, EnumeratedDevice{
			VidPid:       VidPid{VID: info.VendorID, PID: info.ProductID},
			Product:      info.Product,
			Manufacturer: info.Manufacturer,
			Serial:       info.Serial,
			Path:         info.Path,
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
	mu     sync.Mutex
	device hidDevice
	target VidPid
}

// NewHidTransport returns an unopened HID transport.
func NewHidTransport() *HidTransport {
	return &HidTransport{}
}

func (h *HidTransport) Open(ctx context.Context, target VidPid) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	infos := hid.Enumerate(target.VID, target.PID)
	if len(infos) == 0 {
		return errTransport("no HID device found for %s", target)
	}
	device, err := openHidDevice(infos[0])
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
