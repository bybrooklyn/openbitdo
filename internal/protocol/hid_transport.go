package protocol

import (
	"context"
	"sync"
	"time"

	"github.com/karalabe/hid"
)

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

// HidTransport is the real hidapi-backed Transport.
type HidTransport struct {
	mu     sync.Mutex
	device *hid.Device
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
	device, err := infos[0].Open()
	if err != nil {
		return errTransport("open failed for %s: %v", target, err)
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
