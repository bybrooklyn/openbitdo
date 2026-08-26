package input

// Direction is an 8-way d-pad/hat-switch reading.
type Direction int

const (
	DirNone Direction = iota
	DirUp
	DirUpRight
	DirRight
	DirDownRight
	DirDown
	DirDownLeft
	DirLeft
	DirUpLeft
)

// GamepadState is one decoded input report: the d-pad/hat direction and the
// set of currently pressed button-usage IDs.
type GamepadState struct {
	DPad    Direction
	Buttons map[uint16]bool
}

// DecodeReport extracts a GamepadState from one raw input report using the
// fields a ParseReportDescriptor call already produced for this device. Only
// fields matching reportID are considered (report[0] is expected to be the
// report ID byte whenever any field in fields declares a non-zero one).
func DecodeReport(fields []Field, reportID byte, report []byte) GamepadState {
	state := GamepadState{DPad: DirNone, Buttons: map[uint16]bool{}}

	var hatField, xField, yField *Field
	for idx := range fields {
		f := &fields[idx]
		if f.ReportID != reportID || f.IsConstant {
			continue
		}
		switch {
		case f.UsagePage == UsagePageGenericDesktop && f.Usage == UsageHatSwitch:
			hatField = f
		case f.UsagePage == UsagePageGenericDesktop && f.Usage == UsageX:
			xField = f
		case f.UsagePage == UsagePageGenericDesktop && f.Usage == UsageY:
			yField = f
		case f.UsagePage == UsagePageButton:
			decodeButtonField(f, report, state.Buttons)
		}
	}

	switch {
	case hatField != nil:
		state.DPad = decodeHatSwitch(*hatField, report)
	case xField != nil && yField != nil:
		state.DPad = decodeStickAsDPad(*xField, *yField, report)
	}

	return state
}

func decodeButtonField(f *Field, report []byte, out map[uint16]bool) {
	if f.IsArray {
		for slot := 0; slot < f.Count; slot++ {
			v := extractBits(report, f.BitOffset+slot*f.BitSize, f.BitSize)
			if v == 0 {
				continue // 0 conventionally means "no button in this slot"
			}
			usage := uint16(v)
			if usage >= f.UsageMin && usage <= f.UsageMax {
				out[usage] = true
			}
		}
		return
	}
	v := extractBits(report, f.BitOffset, f.BitSize)
	if v != 0 {
		out[f.Usage] = true
	}
}

func decodeHatSwitch(f Field, report []byte) Direction {
	v := int32(extractBits(report, f.BitOffset, f.BitSize))
	// Standard hat-switch encoding: 0=N,1=NE,2=E,3=SE,4=S,5=SW,6=W,7=NW,
	// any value outside [LogicalMin,LogicalMax] (commonly 8) = neutral.
	if v < f.LogicalMin || v > f.LogicalMax {
		return DirNone
	}
	normalized := v - f.LogicalMin
	directions := [8]Direction{DirUp, DirUpRight, DirRight, DirDownRight, DirDown, DirDownLeft, DirLeft, DirUpLeft}
	if normalized < 0 || int(normalized) >= len(directions) {
		return DirNone
	}
	return directions[normalized]
}

func decodeStickAsDPad(xField, yField Field, report []byte) Direction {
	xDir := axisDirection(xField, report, DirLeft, DirRight)
	yDir := axisDirection(yField, report, DirUp, DirDown)

	switch {
	case xDir == DirLeft && yDir == DirUp:
		return DirUpLeft
	case xDir == DirRight && yDir == DirUp:
		return DirUpRight
	case xDir == DirLeft && yDir == DirDown:
		return DirDownLeft
	case xDir == DirRight && yDir == DirDown:
		return DirDownRight
	case xDir != DirNone:
		return xDir
	case yDir != DirNone:
		return yDir
	default:
		return DirNone
	}
}

// axisDirection thresholds an analog axis at ±25% of its logical range
// around the midpoint, mapping it to neg/pos/DirNone.
func axisDirection(f Field, report []byte, neg, pos Direction) Direction {
	v := int32(extractBits(report, f.BitOffset, f.BitSize))
	midpoint := (f.LogicalMin + f.LogicalMax) / 2
	span := f.LogicalMax - f.LogicalMin
	if span <= 0 {
		return DirNone
	}
	deadzone := span / 4
	switch {
	case v < midpoint-deadzone:
		return neg
	case v > midpoint+deadzone:
		return pos
	default:
		return DirNone
	}
}

// extractBits reads a little-endian, LSB-first-packed bitfield from report,
// per the HID report-packing convention (bit 0 of byte 0 is the first bit
// of the report).
func extractBits(report []byte, bitOffset, bitSize int) uint32 {
	if bitSize <= 0 || bitSize > 32 {
		return 0
	}
	var v uint32
	for i := 0; i < bitSize; i++ {
		bitPos := bitOffset + i
		byteIdx := bitPos / 8
		if byteIdx >= len(report) {
			break
		}
		bit := (report[byteIdx] >> (bitPos % 8)) & 1
		v |= uint32(bit) << i
	}
	return v
}
