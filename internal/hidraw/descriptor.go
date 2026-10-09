// Package hidraw talks to HID devices through the Linux kernel's hidraw
// interface: enumeration from sysfs, and read/write on /dev/hidrawN.
//
// Unlike a libusb-backed HID library it never detaches the kernel's own HID
// driver, so a keyboard keeps typing while its vendor interface is open, and
// it only needs access to the hidraw node rather than the raw USB device.
package hidraw

// Usage is one top-level application collection declared by a HID report
// descriptor.
type Usage struct {
	Page  uint16
	Usage uint16
}

// TopLevelUsages returns the usage page/usage of every top-level collection
// in a HID report descriptor, in declaration order. It reads only what it
// needs to identify an interface; a truncated descriptor yields whatever was
// declared before the truncation.
func TopLevelUsages(descriptor []byte) []Usage {
	const (
		itemUsagePage     = 0x04 // global
		itemUsage         = 0x08 // local
		itemCollection    = 0xa0 // main
		itemEndCollection = 0xc0 // main
		longItemPrefix    = 0xfe
	)

	var (
		usages    []Usage
		page      uint16
		usage     uint16
		haveUsage bool
		depth     int
	)
	for i := 0; i < len(descriptor); {
		prefix := descriptor[i]
		if prefix == longItemPrefix {
			if i+1 >= len(descriptor) {
				break
			}
			i += 3 + int(descriptor[i+1])
			continue
		}
		size := int(prefix & 0x03)
		if size == 3 {
			size = 4
		}
		if i+1+size > len(descriptor) {
			break
		}
		var value uint32
		for b := 0; b < size; b++ {
			value |= uint32(descriptor[i+1+b]) << (8 * b)
		}
		i += 1 + size

		switch prefix & 0xfc {
		case itemUsagePage:
			page = uint16(value)
		case itemUsage:
			// Only the first usage before a collection names it.
			if depth == 0 && !haveUsage {
				if size == 4 {
					// A 32-bit usage carries its own page in the high word.
					page = uint16(value >> 16)
				}
				usage = uint16(value)
				haveUsage = true
			}
		case itemCollection:
			if depth == 0 {
				usages = append(usages, Usage{Page: page, Usage: usage})
			}
			depth++
			haveUsage = false
		case itemEndCollection:
			if depth > 0 {
				depth--
			}
			haveUsage = false
		}
	}
	return usages
}
