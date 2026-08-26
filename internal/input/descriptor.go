package input

import "fmt"

// Field is one decoded Input field from a parsed HID report descriptor:
// where it lives in a report (bit offset/size) and what it means (usage
// page/usage, or a usage range for a bitmask/array group).
type Field struct {
	ReportID   byte
	UsagePage  uint16
	Usage      uint16 // meaningful when UsageMin==UsageMax==Usage (a single-usage field)
	UsageMin   uint16
	UsageMax   uint16
	BitOffset  int
	BitSize    int
	Count      int // number of repeated BitSize-bit slots (>1 for array fields)
	LogicalMin int32
	LogicalMax int32
	IsArray    bool
	IsConstant bool
}

// ParseReportDescriptor parses a USB HID 1.11 report descriptor into its
// flat list of Input fields. Main/Global/Local items are handled; Push/Pop
// (global-state stack) is not — vanishingly rare in gamepad descriptors,
// and any descriptor that relies on it will parse global state incorrectly
// after the first Pop rather than silently succeed, so this is a documented
// gap, not a silent one.
func ParseReportDescriptor(desc []byte) ([]Field, error) {
	var fields []Field

	var usagePage uint16
	var logicalMin, logicalMax int32
	var reportSize, reportCount int
	var reportID byte
	var localUsages []uint16
	var localUsageMin, localUsageMax uint16
	haveLocalRange := false

	// bit offset within the current report, keyed by report ID (0 = "no
	// report ID declared for this device").
	bitOffsets := map[byte]int{}

	resetLocal := func() {
		localUsages = nil
		localUsageMin, localUsageMax = 0, 0
		haveLocalRange = false
	}

	i := 0
	for i < len(desc) {
		itemByte := desc[i]
		if itemByte == 0xFE { // long item — skip per HID 1.11 §6.2.2.3
			if i+1 >= len(desc) {
				return nil, fmt.Errorf("truncated long item at byte %d", i)
			}
			dataLen := int(desc[i+1])
			i += 3 + dataLen
			continue
		}

		bTag := itemByte >> 4
		bType := (itemByte >> 2) & 0x3
		bSizeCode := itemByte & 0x3
		dataLen := map[byte]int{0: 0, 1: 1, 2: 2, 3: 4}[bSizeCode]
		if i+1+dataLen > len(desc) {
			return nil, fmt.Errorf("truncated item at byte %d", i)
		}
		data := desc[i+1 : i+1+dataLen]
		i += 1 + dataLen

		unsignedData := func() uint32 {
			var v uint32
			for idx, b := range data {
				v |= uint32(b) << (8 * idx)
			}
			return v
		}
		signedData := func() int32 {
			v := unsignedData()
			switch dataLen {
			case 1:
				return int32(int8(v))
			case 2:
				return int32(int16(v))
			default:
				return int32(v)
			}
		}

		switch bType {
		case 1: // Global
			switch bTag {
			case 0x0:
				usagePage = uint16(unsignedData())
			case 0x1:
				logicalMin = signedData()
			case 0x2:
				logicalMax = signedData()
			case 0x7:
				reportSize = int(unsignedData())
			case 0x8:
				reportID = byte(unsignedData())
			case 0x9:
				reportCount = int(unsignedData())
				// 0xA (Push) / 0xB (Pop): unsupported, see doc comment.
			}
		case 2: // Local
			switch bTag {
			case 0x0:
				localUsages = append(localUsages, uint16(unsignedData()))
			case 0x1:
				localUsageMin = uint16(unsignedData())
				haveLocalRange = true
			case 0x2:
				localUsageMax = uint16(unsignedData())
				haveLocalRange = true
			}
		case 0: // Main
			switch bTag {
			case 0x8: // Input
				flags := unsignedData()
				isConstant := flags&0x01 != 0
				isVariable := flags&0x02 != 0

				start := bitOffsets[reportID]
				if reportID != 0 {
					// account for the 1-byte report-ID prefix once, the
					// first time this report ID is used.
					if start == 0 {
						start = 8
					}
				}

				if !isVariable {
					// Array field: Count repeated BitSize-bit slots, each
					// holding an index into [UsageMin,UsageMax].
					umin, umax := localUsageMin, localUsageMax
					if !haveLocalRange && len(localUsages) > 0 {
						umin, umax = localUsages[0], localUsages[0]
					}
					fields = append(fields, Field{
						ReportID: reportID, UsagePage: usagePage, UsageMin: umin, UsageMax: umax,
						BitOffset: start, BitSize: reportSize, Count: reportCount,
						LogicalMin: logicalMin, LogicalMax: logicalMax, IsArray: true, IsConstant: isConstant,
					})
					bitOffsets[reportID] = start + reportSize*reportCount
				} else if haveLocalRange && int(localUsageMax-localUsageMin)+1 == reportCount {
					// Variable range: one usage per bit-slot, e.g. Button 1..N.
					for idx := 0; idx < reportCount; idx++ {
						usage := localUsageMin + uint16(idx)
						fields = append(fields, Field{
							ReportID: reportID, UsagePage: usagePage, Usage: usage, UsageMin: usage, UsageMax: usage,
							BitOffset: start + idx*reportSize, BitSize: reportSize,
							Count: 1, LogicalMin: logicalMin, LogicalMax: logicalMax, IsConstant: isConstant,
						})
					}
					bitOffsets[reportID] = start + reportSize*reportCount
				} else {
					// Variable, explicit usage list (or a single repeated
					// usage, e.g. X/Y/Hat-switch fields).
					for idx := 0; idx < reportCount; idx++ {
						usage := usagePage
						if idx < len(localUsages) {
							usage = localUsages[idx]
						} else if len(localUsages) > 0 {
							usage = localUsages[len(localUsages)-1]
						}
						fields = append(fields, Field{
							ReportID: reportID, UsagePage: usagePage, Usage: usage, UsageMin: usage, UsageMax: usage,
							BitOffset: start + idx*reportSize, BitSize: reportSize,
							Count: 1, LogicalMin: logicalMin, LogicalMax: logicalMax, IsConstant: isConstant,
						})
					}
					bitOffsets[reportID] = start + reportSize*reportCount
				}
				resetLocal()
			case 0xA, 0xC: // Collection / End Collection
				resetLocal()
			case 0x9, 0xB: // Output / Feature: not needed for input decoding
				resetLocal()
			}
		}
	}

	return fields, nil
}
