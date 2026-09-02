//go:build darwin

// Package machid provides real read/write HID I/O on macOS via IOKit's
// IOHIDManager, matching devices by vendor/product/usage-page/usage instead
// of trusting github.com/karalabe/hid's DeviceInfo.Path.
//
// Why this package exists: karalabe/hid's vendored hidapi mac backend
// resolves a device's io_service_t via
// dlopen("/System/Library/IOKit.framework/IOKit", ...) + dlsym to find
// IOHIDDeviceGetService dynamically (an OS X 10.5-era compatibility shim).
// That hardcoded path does not resolve on this SDK/OS (confirmed directly:
// dlopen fails), so hidapi silently falls back to a stale struct-offset hack
// that yields a garbage io_service_t -- every enumerated device's Path ends
// up empty as a result, and DeviceInfo.Open() (which opens purely by Path
// via IORegistryEntryFromPath) fails for every real device on this platform.
// internal/input/descriptor_darwin.go already worked around this for
// *reading the HID report descriptor* by re-matching via IOHIDManager
// instead of Path; this package extends the same technique to genuine
// read/write I/O, replacing karalabe/hid's *hid.Device for both
// internal/protocol's command/diagnostic session transport and
// internal/input's gamepad nav stream -- the two real callers that need an
// actual working device handle, not just descriptor metadata.
//
// Wire semantics deliberately do NOT mirror hidapi's mac hid_write/hid_read
// -- an earlier version of this package did, and that turned out to be a
// dead end (see below):
//   - Write: data[0] is always treated as the report ID and always stripped
//     before calling IOHIDDeviceSetReport, which is called with the
//     remaining bytes as report/reportLength and data[0] as its own
//     reportID parameter. This matches IOHIDDeviceSetReport's documented
//     contract and this device's actual HID report descriptor (Output
//     report ID 0x81 declares ReportCount=63 -- 63 bytes of *data*, separate
//     from the ID byte), not hidapi mac's own set_report(), which only
//     strips the leading byte when it's 0 and otherwise sends the ID byte a
//     second time embedded in the data portion for numbered reports (read
//     directly from hid.c) -- confirmed, by A/B testing both conventions
//     against real hardware, to make no difference to the open question
//     below, so this package keeps the semantically-documented one.
//   - Read: raw input report bytes as delivered by
//     IOHIDDeviceRegisterInputReportCallback, WITH the report ID prepended
//     as buf[0] -- IOKit's callback delivers report bytes without it (report
//     ID arrives as a separate parameter instead, confirmed directly against
//     this device), unlike hidapi mac's own behavior, but matching this
//     codebase's protocol-parsing convention (buf[0] treated as the report
//     ID; see e.g. session_commands.go's response validator) requires
//     putting it back.
//
// Open, Write, and Read are all confirmed working end-to-end at the
// IOKit/USB transport level against this project's real hardware (a wired
// 8BitDo Ultimate 2, PID 0x6013): matching, opening, and IOHIDDeviceSetReport
// all succeed cleanly (IOReturn 0 = kIOReturnSuccess) with no permission,
// callback-registration-ordering, or exclusive-seize issues. What is NOT yet
// confirmed is an actual command-level response from the device's
// vendor-page (0xffa0) config channel: several commands, including ones the
// generated protocol registry marks Confidence:"confirmed" with no PID
// restriction, produced zero input-report callbacks and zero response to a
// synchronous IOHIDDeviceGetReport (which itself returns a real USB STALL,
// proving the device is live and responding to at least some requests, just
// not this one) across repeated attempts and both write-byte conventions
// above. The registry's "confirmed" here means confirmed present in the
// vendor's own binary via static analysis (evidence_source: "static" in the
// underlying dossier), not confirmed to elicit a response on real hardware
// -- the dossier for a comparable PID says so explicitly ("no runtime trace
// or hardware write confirmation yet"), and no dossier exists yet for
// 0x6013 at all. Getting an actual protocol response therefore needs
// hardware-informed protocol reverse-engineering (handshake sequence? raw
// USB transfer instead of the HID class Set/Get Report path? a PID-specific
// quirk?), not further transport-layer changes -- that work belongs in this
// project's separate, isolated dirty-room evidence process, not in this
// package.
package machid

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/hid/IOHIDManager.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

static int get_int_prop(IOHIDDeviceRef dev, CFStringRef key) {
    CFTypeRef v = IOHIDDeviceGetProperty(dev, key);
    if (v == NULL || CFGetTypeID(v) != CFNumberGetTypeID()) {
        return -1;
    }
    int out = 0;
    CFNumberGetValue((CFNumberRef)v, kCFNumberIntType, &out);
    return out;
}

// match_and_retain_device re-enumerates HID devices via IOHIDManager and
// matches by vendor/product/usage-page/usage (see package doc for why not
// Path). Returns a CFRetain'd IOHIDDeviceRef the caller owns (must
// eventually CFRelease it), or NULL if no match was found -- the temporary
// IOHIDManager used for matching is torn down before returning either way,
// so the retain is what keeps the match alive past that.
static IOHIDDeviceRef match_and_retain_device(int vendorID, int productID, int usagePage, int usage) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (mgr == NULL) {
        return NULL;
    }
    IOHIDManagerSetDeviceMatching(mgr, NULL);
    if (IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone) != kIOReturnSuccess) {
        CFRelease(mgr);
        return NULL;
    }

    CFSetRef deviceSet = IOHIDManagerCopyDevices(mgr);
    if (deviceSet == NULL) {
        IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
        CFRelease(mgr);
        return NULL;
    }

    CFIndex count = CFSetGetCount(deviceSet);
    IOHIDDeviceRef *devices = (IOHIDDeviceRef *)malloc(sizeof(IOHIDDeviceRef) * (size_t)(count > 0 ? count : 1));
    IOHIDDeviceRef match = NULL;
    if (devices != NULL) {
        CFSetGetValues(deviceSet, (const void **)devices);
        for (CFIndex i = 0; i < count; i++) {
            IOHIDDeviceRef dev = devices[i];
            if (dev == NULL) continue;
            if (get_int_prop(dev, CFSTR("VendorID")) != vendorID) continue;
            if (get_int_prop(dev, CFSTR("ProductID")) != productID) continue;
            if (usagePage != 0 && get_int_prop(dev, CFSTR("PrimaryUsagePage")) != usagePage) continue;
            if (usage != 0 && get_int_prop(dev, CFSTR("PrimaryUsage")) != usage) continue;
            match = dev;
            CFRetain(match);
            break;
        }
        free(devices);
    }

    CFRelease(deviceSet);
    IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
    CFRelease(mgr);
    return match;
}

// write_report sends an Output report. data[0] is always treated as the
// report ID and always stripped before the call, matching
// IOHIDDeviceSetReport's documented contract and this device's actual HID
// report descriptor (Output report ID 0x81 declares ReportCount=63, i.e. 63
// bytes of *data*, separate from the ID byte): the report/reportLength
// parameters are the data only, with the ID passed solely via its own
// reportID parameter.
//
// This deliberately does NOT replicate hidapi mac's own set_report(), which
// only strips the leading byte when it's 0 and otherwise sends the ID byte
// a second time embedded in the data portion (verified by reading hid.c) --
// that appears to be a real bug in the vendored library for numbered
// reports, not a convention this device follows. Both conventions were A/B
// tested directly against real hardware and made no difference to whether
// the device responds (see the package doc) -- this one is kept because
// it's the semantically documented behavior, not because it was shown to
// work better.
static int write_report(IOHIDDeviceRef dev, unsigned char *data, int length, int *out_res) {
    if (dev == NULL || data == NULL || length <= 0) {
        if (out_res != NULL) *out_res = -1;
        return -1;
    }
    unsigned char reportID = data[0];
    unsigned char *toSend = data + 1;
    CFIndex toSendLen = (CFIndex)(length - 1);
    IOReturn res = IOHIDDeviceSetReport(dev, kIOHIDReportTypeOutput, reportID, toSend, toSendLen);
    if (out_res != NULL) *out_res = (int)res;
    if (res != kIOReturnSuccess) {
        return -1;
    }
    return length;
}

// DIAGNOSTIC ONLY (temporary, for empirically determining whether this
// device's vendor-page command protocol actually uses Output/Input reports
// (hidapi's assumption, replicated above) or Feature reports (SetReport
// with kIOHIDReportTypeFeature + a matching GetReport, common for
// vendor-defined-usage-page HID devices that don't do async Input
// reporting at all) -- to be removed once the real mechanism is confirmed.
static int write_feature_report(IOHIDDeviceRef dev, unsigned char *data, int length) {
    if (dev == NULL || data == NULL || length <= 0) return -1;
    unsigned char *toSend = data;
    CFIndex toSendLen = (CFIndex)length;
    if (data[0] == 0) { toSend = data + 1; toSendLen = (CFIndex)(length - 1); }
    IOReturn res = IOHIDDeviceSetReport(dev, kIOHIDReportTypeFeature, data[0], toSend, toSendLen);
    if (res != kIOReturnSuccess) return -1;
    return length;
}

static int read_feature_report(IOHIDDeviceRef dev, unsigned char reportID, unsigned char *out, int outLen) {
    if (dev == NULL || out == NULL || outLen <= 0) return -1;
    CFIndex len = (CFIndex)outLen;
    out[0] = reportID;
    IOReturn res = IOHIDDeviceGetReport(dev, kIOHIDReportTypeFeature, reportID, out, &len);
    if (res != kIOReturnSuccess) return -1;
    return (int)len;
}

// DIAGNOSTIC ONLY -- synchronous IOHIDDeviceGetReport for an Input report,
// entirely bypassing IOHIDDeviceRegisterInputReportCallback/CFRunLoop. Used
// to determine whether the device responds to a write at all (independent of
// whether the async callback delivery path is the thing that's broken).
// out_res receives the raw IOReturn code for diagnostic printing regardless
// of success/failure.
static int read_input_report_sync(IOHIDDeviceRef dev, unsigned char reportID, unsigned char *out, int outLen, int *out_res) {
    if (dev == NULL || out == NULL || outLen <= 0) return -1;
    CFIndex len = (CFIndex)outLen;
    IOReturn res = IOHIDDeviceGetReport(dev, kIOHIDReportTypeInput, reportID, out, &len);
    *out_res = (int)res;
    if (res != kIOReturnSuccess) return -1;
    return (int)len;
}

// get_report_descriptor is a diagnostic helper mirroring
// internal/input/descriptor_darwin.go's fetch_report_descriptor, kept local
// here (cross-package cgo sharing isn't practical) purely to inspect what
// this specific already-open device declares. Returns 0 on success with
// *out_bytes malloc'd (caller frees), non-zero otherwise.
static int get_report_descriptor(IOHIDDeviceRef dev, unsigned char **out_bytes, int *out_len) {
    *out_bytes = NULL;
    *out_len = 0;
    CFTypeRef prop = IOHIDDeviceGetProperty(dev, CFSTR("ReportDescriptor"));
    if (prop == NULL || CFGetTypeID(prop) != CFDataGetTypeID()) {
        return 1;
    }
    CFDataRef data = (CFDataRef)prop;
    CFIndex n = CFDataGetLength(data);
    if (n <= 0) {
        return 2;
    }
    unsigned char *buf = (unsigned char *)malloc((size_t)n);
    if (buf == NULL) {
        return 3;
    }
    CFDataGetBytes(data, CFRangeMake(0, n), buf);
    *out_bytes = buf;
    *out_len = (int)n;
    return 0;
}

// max_input_report_len mirrors hidapi mac's get_max_report_length -- the
// buffer IOHIDDeviceRegisterInputReportCallback writes into must be at
// least this large. Falls back to 64 (this project's report size
// throughout) if the property is absent, never returns less than that.
static int max_input_report_len(IOHIDDeviceRef dev) {
    int n = get_int_prop(dev, CFSTR(kIOHIDMaxInputReportSizeKey));
    if (n < 64) {
        return 64;
    }
    return n;
}

extern void goHIDReportCallback(uintptr_t handleToken, int result, void *sender,
                                 int reportType, unsigned int reportID,
                                 unsigned char *report, long reportLength);

// report_callback_trampoline has the exact C signature
// IOHIDDeviceRegisterInputReportCallback requires. context is really a
// cgo.Handle token (see schedule_and_register) smuggled through IOKit's
// void* as a uintptr_t -- narrowing/widening between void* and uintptr_t
// happens here, entirely in C, specifically so no Go code ever converts an
// unsafe.Pointer to/from a uintptr (which is the pattern `go vet`'s
// unsafeptr check flags, correctly, as generally unsafe -- this specific
// use is fine since the token is never dereferenced as a pointer, but it's
// cleaner to just not do that conversion in Go at all than to argue with
// the checker about it).
static void report_callback_trampoline(void *context, IOReturn result, void *sender,
                                        IOHIDReportType type, uint32_t reportID,
                                        uint8_t *report, CFIndex reportLength) {
    goHIDReportCallback((uintptr_t)context, (int)result, sender, (int)type, reportID, report, (long)reportLength);
}

// schedule_and_register schedules dev on the calling thread's current run
// loop and registers the input-report callback. Must be called from the
// dedicated, OS-thread-locked goroutine that will go on to run that loop --
// IOHIDDeviceScheduleWithRunLoop ties the device to whichever run loop is
// current when this is called.
static void schedule_and_register(IOHIDDeviceRef dev, uintptr_t goHandle, unsigned char *buf, int bufLen) {
    CFRunLoopRef loop = CFRunLoopGetCurrent();
    IOHIDDeviceScheduleWithRunLoop(dev, loop, kCFRunLoopDefaultMode);
    IOHIDDeviceRegisterInputReportCallback(dev, buf, (CFIndex)bufLen, report_callback_trampoline, (void *)goHandle);
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"runtime/cgo"
	"sync"
	"unsafe"
)

// Device is a real, read/write-capable HID device handle opened via
// IOHIDManager matching. The zero value is not usable; construct with Open.
type Device struct {
	ref C.IOHIDDeviceRef

	handle cgo.Handle // passed as the C callback's context, freed on Close

	loopMu sync.Mutex
	loop   C.CFRunLoopRef // set once the read goroutine's run loop starts; guarded so Close can read it safely from another goroutine

	reports chan []byte // decoded input reports, delivered by the callback
	closed  chan struct{}
	once    sync.Once
}

// Open matches a HID device by vendor/product ID and (if non-zero)
// usage-page/usage, opens it for exclusive read/write access, and starts a
// dedicated background goroutine pumping its input reports. usagePage/usage
// of 0 match any.
func Open(vendorID, productID, usagePage, usage int) (*Device, error) {
	ref := C.match_and_retain_device(C.int(vendorID), C.int(productID), C.int(usagePage), C.int(usage))
	if ref == 0 {
		return nil, fmt.Errorf("machid: no device matched vid=%#04x pid=%#04x usagePage=%#x usage=%#x", vendorID, productID, usagePage, usage)
	}

	d := &Device{
		ref:     ref,
		reports: make(chan []byte, 64),
		closed:  make(chan struct{}),
	}
	d.handle = cgo.NewHandle(d)
	// d.handle's own uintptr value (not a pointer to it) is what crosses
	// into C -- IOKit retains this context pointer indefinitely across
	// callback invocations, and cgo forbids C from retaining an actual Go
	// pointer past the call that provided it. cgo.Handle exists exactly for
	// this: it's an opaque integer token, not a real pointer, safe to hand
	// to C and convert back later via cgo.Handle(uintptr(token)).Value().

	// Register the input-report callback and schedule the device on this
	// goroutine's run loop BEFORE opening it -- the reverse order (open,
	// then register/schedule afterward from a separately-spawned goroutine,
	// as this used to do) is exactly the ordering Apple's own IOHIDDevice
	// sample code avoids, and empirically here it silently starved every
	// input-report callback: no report was ever delivered, not even for a
	// registry-confirmed command with no PID restriction, even though the
	// write itself reported IOKit-level success. Both steps happen inside
	// readLoop on its dedicated, OS-thread-locked run loop, since
	// IOHIDDeviceScheduleWithRunLoop ties the device to whichever run loop
	// is current on the calling thread.
	started := make(chan error, 1)
	go d.readLoop(started)
	if err := <-started; err != nil {
		C.CFRelease(C.CFTypeRef(ref))
		d.handle.Delete()
		return nil, err
	}

	return d, nil
}

// readLoop owns the device's dedicated OS thread and CFRunLoop for its
// whole lifetime -- IOHIDDeviceScheduleWithRunLoop ties the device to
// whichever run loop is current on the calling thread when it's invoked, so
// that thread must stay fixed (LockOSThread) and keep running that loop
// until Close. It also performs IOHIDDeviceOpen itself, after scheduling and
// registering the callback (see the comment in Open for why that order
// matters), so the callback buffer must outlive Close, not just this
// function -- it's allocated here and freed by Close via d.buf, not a local
// defer.
func (d *Device) readLoop(started chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	maxLen := int(C.max_input_report_len(d.ref))
	buf := (*C.uchar)(C.malloc(C.size_t(maxLen)))

	C.schedule_and_register(d.ref, C.uintptr_t(d.handle), buf, C.int(maxLen))

	if res := C.IOHIDDeviceOpen(d.ref, C.kIOHIDOptionsTypeNone); res != C.kIOReturnSuccess {
		C.free(unsafe.Pointer(buf))
		started <- fmt.Errorf("machid: IOHIDDeviceOpen failed: IOReturn %d", int(res))
		return
	}

	d.loopMu.Lock()
	d.loop = C.CFRunLoopGetCurrent()
	d.loopMu.Unlock()
	started <- nil

	C.CFRunLoopRun() // blocks until CFRunLoopStop(d.loop) is called from Close
	C.free(unsafe.Pointer(buf))
}

//export goHIDReportCallback
func goHIDReportCallback(handleToken C.uintptr_t, _result C.int, _sender unsafe.Pointer,
	_reportType C.int, reportID C.uint, report *C.uchar, reportLength C.long) {
	h := cgo.Handle(uintptr(handleToken))
	d, ok := h.Value().(*Device)
	if !ok || d == nil {
		return
	}
	if reportLength <= 0 {
		return
	}
	// IOKit's input-report callback delivers report bytes WITHOUT the report
	// ID prepended (reportID arrives as a separate parameter instead) --
	// unlike this codebase's protocol-parsing convention, which treats
	// buf[0] as the report ID (see e.g. session_commands.go's response
	// validator, which checks byte0 against this device's declared Input
	// report ID of 0x02). Prepend it here so Read() callers see the same
	// buffer shape the rest of the codebase already assumes.
	buf := make([]byte, int(reportLength)+1)
	buf[0] = byte(reportID)
	copy(buf[1:], unsafe.Slice((*byte)(unsafe.Pointer(report)), int(reportLength)))
	select {
	case d.reports <- buf:
	default:
		// Reader isn't keeping up; drop the oldest queued report rather
		// than blocking IOKit's callback thread, matching hidapi mac's own
		// bounded-queue behavior (it drops after 30 queued reports).
		select {
		case <-d.reports:
		default:
		}
		select {
		case d.reports <- buf:
		default:
		}
	}
}

// Write sends an output report. data[0] is the report ID; see the package
// doc for the exact wire semantics this matches.
func (d *Device) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("machid: write: empty report")
	}
	cData := C.CBytes(data)
	defer C.free(cData)
	var cRes C.int
	n := C.write_report(d.ref, (*C.uchar)(cData), C.int(len(data)), &cRes)
	if n < 0 {
		return 0, fmt.Errorf("machid: write_report failed: IOReturn %#x", uint32(cRes))
	}
	return int(n), nil
}

// Read blocks until an input report arrives or the device is closed.
func (d *Device) Read(buf []byte) (int, error) {
	select {
	case report, ok := <-d.reports:
		if !ok {
			return 0, fmt.Errorf("machid: device closed")
		}
		n := copy(buf, report)
		return n, nil
	case <-d.closed:
		return 0, fmt.Errorf("machid: device closed")
	}
}

// Close stops the read loop and releases the device. Safe to call once;
// subsequent calls are no-ops.
func (d *Device) Close() error {
	d.once.Do(func() {
		close(d.closed)

		d.loopMu.Lock()
		loop := d.loop
		d.loopMu.Unlock()
		if loop != 0 {
			// CFRunLoopStop is documented safe to call from any thread
			// targeting a specific CFRunLoopRef, unlike most other
			// CFRunLoop APIs -- this is what actually unblocks readLoop's
			// CFRunLoopRun from this (different) goroutine.
			C.CFRunLoopStop(loop)
		}

		C.IOHIDDeviceClose(d.ref, C.kIOHIDOptionsTypeNone)
		C.CFRelease(C.CFTypeRef(d.ref))
		d.handle.Delete()
	})
	return nil
}

// DIAGNOSTIC ONLY -- see write_feature_report's C comment. To be removed
// once the real report-type mechanism for this protocol is confirmed.

func (d *Device) WriteFeature(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("machid: write feature: empty report")
	}
	cData := C.CBytes(data)
	defer C.free(cData)
	n := C.write_feature_report(d.ref, (*C.uchar)(cData), C.int(len(data)))
	if n < 0 {
		return 0, fmt.Errorf("machid: write_feature_report failed")
	}
	return int(n), nil
}

func (d *Device) ReadFeature(reportID byte, length int) ([]byte, error) {
	buf := (*C.uchar)(C.malloc(C.size_t(length)))
	defer C.free(unsafe.Pointer(buf))
	n := C.read_feature_report(d.ref, C.uchar(reportID), buf, C.int(length))
	if n < 0 {
		return nil, fmt.Errorf("machid: read_feature_report failed")
	}
	return C.GoBytes(unsafe.Pointer(buf), n), nil
}

// ReadInputSync is DIAGNOSTIC ONLY -- see read_input_report_sync's C
// comment. Bypasses the async callback/CFRunLoop path entirely via a
// synchronous IOHIDDeviceGetReport, to isolate whether the device is
// responding at all from whether the async delivery path is what's broken.
func (d *Device) ReadInputSync(reportID byte, length int) ([]byte, int, error) {
	buf := (*C.uchar)(C.malloc(C.size_t(length)))
	defer C.free(unsafe.Pointer(buf))
	var cRes C.int
	n := C.read_input_report_sync(d.ref, C.uchar(reportID), buf, C.int(length), &cRes)
	if n < 0 {
		return nil, int(cRes), fmt.Errorf("machid: read_input_report_sync failed: IOReturn %d", int(cRes))
	}
	return C.GoBytes(unsafe.Pointer(buf), n), int(cRes), nil
}

// DumpDescriptor is a temporary diagnostic helper (see manual test) --
// fetches the raw HID report descriptor for the already-open device so its
// declared report types/sizes/IDs can be inspected directly rather than
// guessed at.
func (d *Device) DumpDescriptor() ([]byte, error) {
	var cBytes *C.uchar
	var cLen C.int
	if C.get_report_descriptor(d.ref, &cBytes, &cLen) != 0 {
		return nil, fmt.Errorf("machid: no ReportDescriptor property")
	}
	defer C.free(unsafe.Pointer(cBytes))
	return C.GoBytes(unsafe.Pointer(cBytes), cLen), nil
}
