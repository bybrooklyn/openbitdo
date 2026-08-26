//go:build darwin

package input

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

// fetch_report_descriptor resolves an IOService-plane registry path (the
// same format hidapi's mac backend puts in karalabe/hid's DeviceInfo.Path,
// built via IORegistryEntryGetPath in hidapi/mac/hid.c) back to its IOKit
// registry entry, then reads the "ReportDescriptor" property
// (kIOHIDReportDescriptorKey, see IOKit/hid/IOHIDDeviceKeys.h) directly off
// it via IORegistryEntryCreateCFProperty -- no IOHIDManager/IOHIDDeviceRef
// needed, since ReportDescriptor is a plain Data property on the registry
// entry itself.
//
// Returns 0 on success, with *out_bytes malloc'd (caller must free) and
// *out_len set. On any non-zero return, *out_bytes is NULL.
static int fetch_report_descriptor(const char *path, unsigned char **out_bytes, int *out_len) {
    *out_bytes = NULL;
    *out_len = 0;

    io_registry_entry_t entry = IORegistryEntryFromPath(kIOMainPortDefault, path);
    if (entry == MACH_PORT_NULL) {
        return 1; // no such registry entry (device removed, or an invalid path)
    }

    CFTypeRef prop = IORegistryEntryCreateCFProperty(entry, CFSTR("ReportDescriptor"), kCFAllocatorDefault, 0);
    IOObjectRelease(entry);
    if (prop == NULL) {
        return 2; // property not present on this entry
    }
    if (CFGetTypeID(prop) != CFDataGetTypeID()) {
        CFRelease(prop);
        return 3; // unexpected property type
    }

    CFDataRef data = (CFDataRef)prop;
    CFIndex n = CFDataGetLength(data);
    if (n <= 0) {
        CFRelease(prop);
        return 4; // empty descriptor
    }

    unsigned char *buf = (unsigned char *)malloc((size_t)n);
    if (buf == NULL) {
        CFRelease(prop);
        return 5; // allocation failure
    }
    CFDataGetBytes(data, CFRangeMake(0, n), buf);
    CFRelease(prop);

    *out_bytes = buf;
    *out_len = (int)n;
    return 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// fetchReportDescriptor reads a HID device's report descriptor via IOKit,
// given the IOService-plane registry path karalabe/hid exposes as
// DeviceInfo.Path on darwin.
func fetchReportDescriptor(devicePath string) ([]byte, error) {
	cPath := C.CString(devicePath)
	defer C.free(unsafe.Pointer(cPath))

	var cBytes *C.uchar
	var cLen C.int
	if rc := C.fetch_report_descriptor(cPath, &cBytes, &cLen); rc != 0 {
		return nil, fmt.Errorf("fetch report descriptor for %s: IOKit lookup failed (code %d)", devicePath, int(rc))
	}
	defer C.free(unsafe.Pointer(cBytes))

	return C.GoBytes(unsafe.Pointer(cBytes), cLen), nil
}
