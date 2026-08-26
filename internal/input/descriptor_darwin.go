//go:build darwin

package input

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/hid/IOHIDManager.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

static int get_int_prop(IOHIDDeviceRef dev, CFStringRef key) {
    CFTypeRef v = IOHIDDeviceGetProperty(dev, key);
    if (v == NULL || CFGetTypeID(v) != CFNumberGetTypeID()) {
        return -1;
    }
    int out = 0;
    CFNumberGetValue((CFNumberRef)v, kCFNumberIntType, &out);
    return out;
}

// fetch_report_descriptor re-enumerates HID devices via IOHIDManager (a
// modern, non-deprecated API) and matches by vendor/product/usage-page/usage
// rather than trusting karalabe/hid's DeviceInfo.Path. That field is
// unreliable on modern macOS in the vendored hidapi version this project
// depends on: hidapi's mac backend resolves the IOHIDDevice's io_service_t
// via dlopen("/System/Library/IOKit.framework/IOKit", ...) to find
// IOHIDDeviceGetService dynamically (an OS X 10.5-era compatibility shim),
// that hardcoded path no longer resolves on this SDK/OS (confirmed directly:
// dlopen fails with "not in dyld cache"), so it silently falls through to a
// stale struct-offset hack that produces a garbage io_service_t -- and every
// device's Path ends up empty as a result. VendorID/ProductID/UsagePage/Usage
// are populated correctly (they come from get_int_property() on the
// IOHIDDeviceRef directly, not through that broken path), so those are what
// this matches on instead.
//
// Returns 0 on success (with *out_bytes malloc'd, caller frees), or a
// non-zero code identifying which step failed / no match found.
static int fetch_report_descriptor(int vendorID, int productID, int usagePage, int usage, unsigned char **out_bytes, int *out_len) {
    *out_bytes = NULL;
    *out_len = 0;

    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (mgr == NULL) {
        return 1;
    }
    IOHIDManagerSetDeviceMatching(mgr, NULL); // match everything; filter manually below
    IOReturn openRC = IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);
    if (openRC != kIOReturnSuccess) {
        CFRelease(mgr);
        return 2;
    }

    CFSetRef deviceSet = IOHIDManagerCopyDevices(mgr);
    if (deviceSet == NULL) {
        IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
        CFRelease(mgr);
        return 3;
    }

    CFIndex count = CFSetGetCount(deviceSet);
    IOHIDDeviceRef *devices = (IOHIDDeviceRef *)malloc(sizeof(IOHIDDeviceRef) * (size_t)(count > 0 ? count : 1));
    if (devices == NULL) {
        CFRelease(deviceSet);
        IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
        CFRelease(mgr);
        return 4;
    }
    CFSetGetValues(deviceSet, (const void **)devices);

    int rc = 5; // "no matching device found" unless overwritten below
    for (CFIndex i = 0; i < count; i++) {
        IOHIDDeviceRef dev = devices[i];
        if (dev == NULL) {
            continue;
        }
        if (get_int_prop(dev, CFSTR("VendorID")) != vendorID) continue;
        if (get_int_prop(dev, CFSTR("ProductID")) != productID) continue;
        if (usagePage != 0 && get_int_prop(dev, CFSTR("PrimaryUsagePage")) != usagePage) continue;
        if (usage != 0 && get_int_prop(dev, CFSTR("PrimaryUsage")) != usage) continue;

        CFTypeRef prop = IOHIDDeviceGetProperty(dev, CFSTR("ReportDescriptor"));
        if (prop == NULL || CFGetTypeID(prop) != CFDataGetTypeID()) {
            rc = 6; // matched a device but it has no usable descriptor property; keep looking
            continue;
        }
        CFDataRef data = (CFDataRef)prop;
        CFIndex n = CFDataGetLength(data);
        if (n <= 0) {
            rc = 7;
            continue;
        }
        unsigned char *buf = (unsigned char *)malloc((size_t)n);
        if (buf == NULL) {
            rc = 8;
            break;
        }
        CFDataGetBytes(data, CFRangeMake(0, n), buf);
        *out_bytes = buf;
        *out_len = (int)n;
        rc = 0;
        break;
    }

    free(devices);
    CFRelease(deviceSet);
    IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
    CFRelease(mgr);
    return rc;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/karalabe/hid"
)

// fetchReportDescriptor reads a HID device's report descriptor via IOKit's
// IOHIDManager, matching the device by vendor/product/usage-page/usage. See
// the cgo comment above fetch_report_descriptor for why this doesn't use
// info.Path the way descriptor_linux.go uses its platform's path.
func fetchReportDescriptor(info hid.DeviceInfo) ([]byte, error) {
	var cBytes *C.uchar
	var cLen C.int
	rc := C.fetch_report_descriptor(
		C.int(info.VendorID), C.int(info.ProductID),
		C.int(info.UsagePage), C.int(info.Usage),
		&cBytes, &cLen,
	)
	if rc != 0 {
		return nil, fmt.Errorf("fetch report descriptor for vid=%#04x pid=%#04x: IOHIDManager lookup failed (code %d)",
			info.VendorID, info.ProductID, int(rc))
	}
	defer C.free(unsafe.Pointer(cBytes))

	return C.GoBytes(unsafe.Pointer(cBytes), cLen), nil
}
