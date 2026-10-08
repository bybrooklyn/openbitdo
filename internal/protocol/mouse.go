package protocol

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"
)

// A Retro R8 mouse (0x5205 by cable, 0x5206 through its 2.4G receiver) has
// no configuration record: each setting has a command of its own, sent in
// a 64-byte report with no report id on the interface with usage page
// 0xff00:
//
//	request  01 | group | command | op   | data
//	reply    mark | group | command | flags | data from byte 4
//
// op is 1 to read and 4 to write, and a reply answers a request when its
// flags have that bit set. mark is 21 from the mouse by cable and 01
// through the receiver. There is no commit. The receiver itself answers one
// question, whether a mouse is linked to it, in a frame of its own.
//
// The Riviera mouse (0x205d) is a different device: it keeps one 140-byte
// record, read and written by byte range in the frames a record keyboard
// uses (see kbrecord.go).
//
// Everything here follows docs/clean-room-evidence/dossiers/5205/mouse.toml;
// none of it has been exchanged with a real mouse.

const (
	mouseOpRead  byte = 0x01
	mouseOpWrite byte = 0x04

	// A request is the report id (none, so zero), the 01, group, command
	// and op, then data; a reply's data starts after its four header bytes.
	mouseRequestData = 5
	mouseReplyData   = 4

	mouseMarkWired    byte = 0x21
	mouseMarkReceiver byte = 0x01

	// mouseReceiverPID is the Retro R8's 2.4G receiver.
	mouseReceiverPID uint16 = 0x5206

	// A name travels ten bytes a report, a macro two records a report.
	mouseNameChunk     = 10
	mouseMacroPerFrame = 2
	mouseMacroRecord   = 5

	// Where the table of rates starts in a polling rate reply and in the
	// request that writes it.
	mouseRateReadTable  = 4
	mouseRateWriteTable = 6

	// The first data byte of a macro request says which part it is about.
	mouseMacroHeader   byte = 0x01
	mouseMacroRecords  byte = 0x02
	mouseMacroLastPart byte = 0x03
	mouseMacroName     byte = 0x04

	// MouseNameLen is the size of a Retro R8's profile name: ten UTF-16
	// big-endian characters.
	MouseNameLen = 20
	// MouseButtons is how many of a Retro R8's buttons can be assigned,
	// numbered 1 to 6. The left button is not one of them.
	MouseButtons = 6
	// MouseDpiStages is how many DPI stages either mouse has.
	MouseDpiStages = 6
	// MouseMacroNameLen is the size of a macro's name as written. Only the
	// first MouseMacroNameRead bytes are read back: the vendor application
	// never asks for the rest.
	MouseMacroNameLen  = 30
	MouseMacroNameRead = 20
	// MouseMacroMaxRecords is how many records a macro holds.
	MouseMacroMaxRecords = 80
	// MouseMacroFirstButton and MouseMacroLastButton are the buttons that
	// can play a macro.
	MouseMacroFirstButton = 3
	MouseMacroLastButton  = 6

	// MouseRecordSize is the size of the Riviera mouse's configuration
	// record.
	MouseRecordSize = 0x8c

	// The vendor application waits this long before every read.
	mouseReadWait = 100 * time.Millisecond
)

// mousePIDs are the mice this file talks to; retroMousePIDs the two that
// take the Retro R8's per-setting commands.
var (
	mousePIDs      = map[uint16]bool{0x5205: true, mouseReceiverPID: true, 0x205d: true}
	retroMousePIDs = map[uint16]bool{0x5205: true, mouseReceiverPID: true}
)

// isMouseCommand reports whether command is one of this file's.
func isMouseCommand(command CommandID) bool {
	row, ok := FindCommand(command)
	return ok && row.OperationGroup == "Mouse"
}

// mouseCommandCode gives the group, command and op a Retro R8 command's
// row template carries.
func mouseCommandCode(command CommandID) (group, cmd, op byte, ok bool) {
	row, found := FindCommand(command)
	if !found || row.OperationGroup != "Mouse" || row.ReportID != 0 || len(row.Request) < mouseRequestData {
		return 0, 0, 0, false
	}
	return row.Request[2], row.Request[3], row.Request[4], true
}

// mouseReplyFor reports whether response answers the request with this
// group, command and op, and returns its data.
func mouseReplyFor(response []byte, group, cmd, op byte) ([]byte, bool) {
	if len(response) < mouseReplyData || (response[0] != mouseMarkWired && response[0] != mouseMarkReceiver) ||
		response[1] != group || response[2] != cmd || response[3]&op == 0 {
		return nil, false
	}
	return response[mouseReplyData:], true
}

// mouseReceiverReplyOK reports whether response is the receiver's answer
// to the link question.
func mouseReceiverReplyOK(response []byte) bool {
	return len(response) > mouseReplyData && response[0]&0x06 != 0 &&
		response[1] == 0x03 && response[2] == 0x0a && response[3] == 0x11
}

// mouseExchange sends a Retro R8 command with payload as its data and
// returns the reply's data, which must be at least need bytes.
func (s *DeviceSession) mouseExchange(ctx context.Context, command CommandID, payload []byte, need int) ([]byte, error) {
	row, err := s.ensureCommandAllowed(command)
	if err != nil {
		return nil, err
	}
	frame := append([]byte(nil), row.Request...)
	copy(frame[mouseRequestData:], payload)
	if row.SafetyClass == SafeRead {
		if err := s.kbRecordWait(ctx, mouseReadWait); err != nil {
			return nil, err
		}
	}
	resp, err := s.sendRow(ctx, row, frame)
	if err != nil {
		return nil, err
	}
	// The mouse by cable and the receiver mark their replies differently,
	// and the vendor application takes only the one it expects.
	mark := mouseMarkWired
	if s.target.PID == mouseReceiverPID {
		mark = mouseMarkReceiver
	}
	if resp.Raw[0] != mark {
		return nil, errInvalidResponse(row.ID, fmt.Sprintf("reply marked %#02x, expected %#02x", resp.Raw[0], mark))
	}
	data := resp.Raw[mouseReplyData:]
	if len(data) < need {
		return nil, errMalformedResponse(row.ID, len(resp.Raw))
	}
	return data, nil
}

// mouseNameWord is the word a name request carries after two zero bytes:
// how many bytes, and which ten-byte block.
func mouseNameWord(block, length int) []byte {
	word := uint16(length&0x1f) | uint16(block&0x1f)<<5
	return []byte{0, 0, byte(word), byte(word >> 8)}
}

// MouseReceiverLinked asks a Retro R8 receiver whether a mouse is linked
// to it. The receiver answers this itself, mouse or no mouse.
func (s *DeviceSession) MouseReceiverLinked(ctx context.Context) (bool, error) {
	resp, err := s.SendCommand(ctx, CommandMouseReceiverLinked, nil)
	if err != nil {
		return false, err
	}
	return resp.Raw[mouseReplyData] == 1, nil
}

// MouseReadProfileName reads a Retro R8's profile name, MouseNameLen bytes
// of UTF-16 big-endian. A name that starts with a zero character means the
// mouse holds no profile.
func (s *DeviceSession) MouseReadProfileName(ctx context.Context) ([]byte, error) {
	name := make([]byte, MouseNameLen)
	for block := 0; block < MouseNameLen/mouseNameChunk; block++ {
		data, err := s.mouseExchange(ctx, CommandMouseReadProfileName, mouseNameWord(block, mouseNameChunk), 4+mouseNameChunk)
		if err != nil {
			return nil, err
		}
		copy(name[block*mouseNameChunk:], data[4:4+mouseNameChunk])
		// The third data byte is how much the mouse sent; after a short
		// block the vendor application asks for no more.
		if data[2] < mouseNameChunk {
			break
		}
	}
	return name, nil
}

// MouseWriteProfileName writes a Retro R8's profile name, MouseNameLen
// bytes.
func (s *DeviceSession) MouseWriteProfileName(ctx context.Context, name []byte) error {
	if len(name) != MouseNameLen {
		return errInvalidInput("a profile name is %d bytes, not %d", MouseNameLen, len(name))
	}
	for block := 0; block < MouseNameLen/mouseNameChunk; block++ {
		chunk := name[block*mouseNameChunk : (block+1)*mouseNameChunk]
		if _, err := s.mouseExchange(ctx, CommandMouseWriteProfileName, append(mouseNameWord(block, len(chunk)), chunk...), 0); err != nil {
			return err
		}
	}
	return nil
}

// MouseClearProfile removes a Retro R8's profile, which the vendor
// application sends to reset the mouse to its defaults.
func (s *DeviceSession) MouseClearProfile(ctx context.Context) error {
	_, err := s.mouseExchange(ctx, CommandMouseClearProfile, nil, 0)
	return err
}

// MouseButtonEntry is one Retro R8 button's assignment as stored.
type MouseButtonEntry struct {
	// Macro is 1 when the button plays its macro instead of Function.
	Macro byte
	// Function is an entry of the mouse's function table.
	Function uint32
}

// MouseReadButtons reads the assignments of button first and the one after
// it. The vendor application asks for 1, 3 and 5.
func (s *DeviceSession) MouseReadButtons(ctx context.Context, first int) ([2]MouseButtonEntry, error) {
	var entries [2]MouseButtonEntry
	if first != 1 && first != 3 && first != 5 {
		return entries, errInvalidInput("buttons are read in pairs from 1, 3 or 5, not %d", first)
	}
	// The 9 and the 2 are sent as the vendor application sends them; the 2
	// is presumably how many buttons to answer for.
	data, err := s.mouseExchange(ctx, CommandMouseReadButtons, []byte{0x09, byte(first), 0x02}, 3+2*6)
	if err != nil {
		return entries, err
	}
	for i := range entries {
		entry := data[3+i*6:]
		entries[i] = MouseButtonEntry{Macro: entry[0], Function: binary.LittleEndian.Uint32(entry[2:])}
	}
	return entries, nil
}

// MouseWriteButton writes one Retro R8 button's assignment; button is 1 to
// MouseButtons.
func (s *DeviceSession) MouseWriteButton(ctx context.Context, button int, entry MouseButtonEntry) error {
	if button < 1 || button > MouseButtons {
		return errInvalidInput("button %d is outside 1-%d", button, MouseButtons)
	}
	payload := []byte{entry.Macro, byte(button), 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(payload[2:], entry.Function)
	_, err := s.mouseExchange(ctx, CommandMouseWriteButton, payload, 0)
	return err
}

// MouseSetting names one of a Retro R8's one-byte settings.
type MouseSetting int

const (
	// MouseLeftHanded is 0 for a right-handed mouse, 1 for a left-handed one.
	MouseLeftHanded MouseSetting = iota
	// MouseLiftOff is the lift-off distance: 1 for 1.0 mm, 2 for 2.0 mm.
	MouseLiftOff
	// MouseWheelSpeed is 0 (slowest) to 14.
	MouseWheelSpeed
	// MouseWheelDirection is 0 for a standard wheel, 1 for a natural one.
	MouseWheelDirection
)

func (m MouseSetting) commands() (read, write CommandID, ok bool) {
	switch m {
	case MouseLeftHanded:
		return CommandMouseReadLeftHanded, CommandMouseWriteLeftHanded, true
	case MouseLiftOff:
		return CommandMouseReadLiftOff, CommandMouseWriteLiftOff, true
	case MouseWheelSpeed:
		return CommandMouseReadWheelSpeed, CommandMouseWriteWheelSpeed, true
	case MouseWheelDirection:
		return CommandMouseReadWheelDirection, CommandMouseWriteWheelDirection, true
	}
	return "", "", false
}

// MouseReadSetting reads one of a Retro R8's one-byte settings.
func (s *DeviceSession) MouseReadSetting(ctx context.Context, setting MouseSetting) (byte, error) {
	read, _, ok := setting.commands()
	if !ok {
		return 0, errInvalidInput("there is no mouse setting %d", setting)
	}
	data, err := s.mouseExchange(ctx, read, nil, 1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

// MouseWriteSetting writes one of a Retro R8's one-byte settings. The value
// is sent as given; what the mouse does with one outside the setting's
// range is not known.
func (s *DeviceSession) MouseWriteSetting(ctx context.Context, setting MouseSetting, value byte) error {
	_, write, ok := setting.commands()
	if !ok {
		return errInvalidInput("there is no mouse setting %d", setting)
	}
	_, err := s.mouseExchange(ctx, write, []byte{value}, 0)
	return err
}

// MouseDpi is a Retro R8's DPI stages along one axis. On the X axis bit n
// of Mask says stage n is switched on; on the Y axis it says stage n has a
// Y value of its own rather than following X.
type MouseDpi struct {
	Mask   byte
	Values [MouseDpiStages]uint16
}

// Axes of a Retro R8's DPI stages.
const (
	MouseAxisX = 0
	MouseAxisY = 1
)

// MouseReadDpi reads a Retro R8's DPI stages along axis.
func (s *DeviceSession) MouseReadDpi(ctx context.Context, axis int) (MouseDpi, error) {
	var dpi MouseDpi
	if axis != MouseAxisX && axis != MouseAxisY {
		return dpi, errInvalidInput("there is no DPI axis %d", axis)
	}
	// The reply repeats the axis, then holds the mask and the six values.
	data, err := s.mouseExchange(ctx, CommandMouseReadDpi, []byte{byte(axis)}, 2+2*MouseDpiStages)
	if err != nil {
		return dpi, err
	}
	dpi.Mask = data[1]
	for i := range dpi.Values {
		dpi.Values[i] = binary.LittleEndian.Uint16(data[2+2*i:])
	}
	return dpi, nil
}

// MouseWriteDpi writes a Retro R8's DPI stages along axis, all six at once.
func (s *DeviceSession) MouseWriteDpi(ctx context.Context, axis int, dpi MouseDpi) error {
	if axis != MouseAxisX && axis != MouseAxisY {
		return errInvalidInput("there is no DPI axis %d", axis)
	}
	payload := make([]byte, 2+2*MouseDpiStages)
	payload[0], payload[1] = byte(axis), dpi.Mask
	for i, value := range dpi.Values {
		binary.LittleEndian.PutUint16(payload[2+2*i:], value)
	}
	_, err := s.mouseExchange(ctx, CommandMouseWriteDpi, payload, 0)
	return err
}

// MouseReadDpiStage reads which DPI stage a Retro R8 is using, from 0.
func (s *DeviceSession) MouseReadDpiStage(ctx context.Context) (byte, error) {
	data, err := s.mouseExchange(ctx, CommandMouseReadDpiStage, nil, 2)
	if err != nil {
		return 0, err
	}
	return data[1], nil
}

// MouseWriteDpiStage makes stage (0 to 5) the one a Retro R8 uses. dpi is
// that stage's X value, which the vendor application sends along.
func (s *DeviceSession) MouseWriteDpiStage(ctx context.Context, stage int, dpi uint16) error {
	if stage < 0 || stage >= MouseDpiStages {
		return errInvalidInput("DPI stage %d is outside 1-%d", stage+1, MouseDpiStages)
	}
	// The leading 6 is sent as the vendor application sends it; presumably
	// the number of stages.
	_, err := s.mouseExchange(ctx, CommandMouseWriteDpiStage, []byte{MouseDpiStages, byte(stage), byte(dpi), byte(dpi >> 8)}, 0)
	return err
}

// MousePollingRate is a Retro R8's polling rate setting: a table of rates
// in Hz and which of them is in use.
type MousePollingRate struct {
	// Count is how many of Rates the mouse has. The vendor application
	// passes it back as read; a sixth rate is only written when it is 6.
	Count byte
	// Current is the place in Rates of the rate in use, from 0.
	Current byte
	Rates   [6]uint16
}

// MouseReadPollingRate reads a Retro R8's polling rate setting.
func (s *DeviceSession) MouseReadPollingRate(ctx context.Context) (MousePollingRate, error) {
	var rate MousePollingRate
	data, err := s.mouseExchange(ctx, CommandMouseReadPollingRate, nil, mouseRateReadTable+2*len(rate.Rates))
	if err != nil {
		return rate, err
	}
	// Two bytes after the first two are not used by the vendor application.
	rate.Count, rate.Current = data[0], data[1]
	for i := range rate.Rates {
		rate.Rates[i] = binary.LittleEndian.Uint16(data[mouseRateReadTable+2*i:])
	}
	return rate, nil
}

// MouseWritePollingRate writes a Retro R8's polling rate setting, table and
// all, as the vendor application does to change the rate in use.
//
// The vendor application writes the table two bytes further along than it
// reads it: after four zero bytes where the reply has two. Whether the
// mouse really lays the two out differently is not known; both are done
// here exactly as there.
func (s *DeviceSession) MouseWritePollingRate(ctx context.Context, rate MousePollingRate) error {
	payload := make([]byte, mouseRateWriteTable+2*len(rate.Rates))
	payload[0], payload[1] = rate.Count, rate.Current
	for i, hz := range rate.Rates {
		// The sixth rate is left out unless the mouse has six.
		if i < 5 || rate.Count == 6 {
			binary.LittleEndian.PutUint16(payload[mouseRateWriteTable+2*i:], hz)
		}
	}
	_, err := s.mouseExchange(ctx, CommandMouseWritePollingRate, payload, 0)
	return err
}

// MouseMacroRecord is one step of a Retro R8 macro.
type MouseMacroRecord struct {
	// State says what happens: a key or a mouse button going down or up.
	State byte
	// Code is the key's HID usage or the mouse button's number.
	Code uint16
	// Timer is a delay in milliseconds that goes with the step.
	Timer uint16
}

// MouseMacro is the macro one of a Retro R8's buttons plays.
type MouseMacro struct {
	// Cycles is how many times it plays; 0xffff for until the button is
	// let go.
	Cycles uint16
	// IntervalMs is the wait between two plays.
	IntervalMs uint16
	// Name is UTF-16 big-endian: MouseMacroNameRead bytes as read, up to
	// MouseMacroNameLen to write.
	Name    []byte
	Records []MouseMacroRecord
}

func mouseMacroButtonOK(button int) bool {
	return button >= MouseMacroFirstButton && button <= MouseMacroLastButton
}

// MouseReadMacro reads the macro stored for button (3 to 6): its header,
// its name, then its records. The records request does not name a button;
// the mouse answers for the macro whose header was last asked for.
func (s *DeviceSession) MouseReadMacro(ctx context.Context, button int) (MouseMacro, error) {
	var macro MouseMacro
	if !mouseMacroButtonOK(button) {
		return macro, errInvalidInput("button %d does not play a macro", button)
	}
	// Header reply: the 1 repeated, record count, button, cycles, the
	// macro's number, two bytes, a 0x20, the interval.
	header, err := s.mouseExchange(ctx, CommandMouseReadMacro, []byte{mouseMacroHeader, 0, byte(button)}, 11)
	if err != nil {
		return macro, err
	}
	count := int(header[1])
	if count > MouseMacroMaxRecords {
		return macro, errInvalidResponse(CommandMouseReadMacro, fmt.Sprintf("a macro of %d records is more than the mouse holds", count))
	}
	macro.Cycles = binary.LittleEndian.Uint16(header[3:])
	macro.IntervalMs = binary.LittleEndian.Uint16(header[9:])

	// The name is asked for ten bytes at a time, the third byte being
	// eight times the block's number. The vendor application stops after
	// two blocks although a name is three long.
	macro.Name = make([]byte, 0, MouseMacroNameRead)
	for block := 0; block < MouseMacroNameRead/mouseNameChunk; block++ {
		data, err := s.mouseExchange(ctx, CommandMouseReadMacro, []byte{mouseMacroName, byte(button), byte(block * 8)}, 3+mouseNameChunk)
		if err != nil {
			return macro, err
		}
		macro.Name = append(macro.Name, data[3:3+mouseNameChunk]...)
	}

	macro.Records = make([]MouseMacroRecord, 0, count)
	for at := 0; at < count; at += mouseMacroPerFrame {
		n := min(mouseMacroPerFrame, count-at)
		data, err := s.mouseExchange(ctx, CommandMouseReadMacro, []byte{mouseMacroRecords, 0, 0, byte(at), byte(n)}, 5+n*mouseMacroRecord)
		if err != nil {
			return macro, err
		}
		for i := 0; i < n; i++ {
			raw := data[5+i*mouseMacroRecord:]
			macro.Records = append(macro.Records, MouseMacroRecord{
				State: raw[0], Code: binary.LittleEndian.Uint16(raw[1:]), Timer: binary.LittleEndian.Uint16(raw[3:]),
			})
		}
	}
	return macro, nil
}

// MouseWriteMacro stores macro for button (3 to 6): its header, its name,
// then its records two at a time, the last report carrying the sum of all
// the record bytes. The button plays it once its assignment says so; see
// MouseWriteButton.
func (s *DeviceSession) MouseWriteMacro(ctx context.Context, button int, macro MouseMacro) error {
	if !mouseMacroButtonOK(button) {
		return errInvalidInput("button %d does not play a macro", button)
	}
	if len(macro.Records) < 1 || len(macro.Records) > MouseMacroMaxRecords {
		return errInvalidInput("a macro of %d records is outside 1-%d", len(macro.Records), MouseMacroMaxRecords)
	}
	if len(macro.Name) > MouseMacroNameLen {
		return errInvalidInput("a macro name of %d bytes is longer than %d", len(macro.Name), MouseMacroNameLen)
	}
	// The macros are numbered from 1 in the order of their buttons.
	number := byte(button - MouseMacroFirstButton + 1)
	header := []byte{
		mouseMacroHeader, byte(len(macro.Records)), byte(button),
		byte(macro.Cycles), byte(macro.Cycles >> 8), number, 0, 0, 0x20,
		byte(macro.IntervalMs), byte(macro.IntervalMs >> 8),
	}
	if _, err := s.mouseExchange(ctx, CommandMouseWriteMacro, header, 0); err != nil {
		return err
	}

	name := make([]byte, MouseMacroNameLen)
	copy(name, macro.Name)
	for block := 0; block < MouseMacroNameLen/mouseNameChunk; block++ {
		payload := append([]byte{mouseMacroName, byte(button), byte(block * 8)}, name[block*mouseNameChunk:(block+1)*mouseNameChunk]...)
		if _, err := s.mouseExchange(ctx, CommandMouseWriteMacro, payload, 0); err != nil {
			return err
		}
	}

	raw := make([]byte, 0, len(macro.Records)*mouseMacroRecord)
	var sum uint16
	for _, record := range macro.Records {
		raw = append(raw, record.State, byte(record.Code), byte(record.Code>>8), byte(record.Timer), byte(record.Timer>>8))
	}
	for _, b := range raw {
		sum += uint16(b)
	}
	for at := 0; at < len(macro.Records); at += mouseMacroPerFrame {
		n := min(mouseMacroPerFrame, len(macro.Records)-at)
		payload := []byte{mouseMacroRecords, 0, 0, byte(at), byte(n)}
		if at+n == len(macro.Records) {
			payload[0], payload[1], payload[2] = mouseMacroLastPart, byte(sum), byte(sum>>8)
		}
		payload = append(payload, raw[at*mouseMacroRecord:(at+n)*mouseMacroRecord]...)
		if _, err := s.mouseExchange(ctx, CommandMouseWriteMacro, payload, 0); err != nil {
			return err
		}
	}
	return nil
}

// MouseRecordRead reads length bytes of a Riviera mouse's configuration
// record at offset; 0 and MouseRecordSize read all of it.
func (s *DeviceSession) MouseRecordRead(ctx context.Context, offset, length int) ([]byte, error) {
	if offset < 0 || length < 1 || offset+length > MouseRecordSize {
		return nil, errInvalidInput("record range %d+%d is outside the %d-byte record", offset, length, MouseRecordSize)
	}
	return s.kbRecordReadChunks(ctx, CommandMouseRecordRead, offset, length)
}

// MouseRecordWriteRange writes record[offset:offset+length] to a Riviera
// mouse's configuration record. The mouse stores each report as it
// acknowledges it.
func (s *DeviceSession) MouseRecordWriteRange(ctx context.Context, record []byte, offset, length int) error {
	if len(record) != MouseRecordSize {
		return errInvalidInput("%d bytes is not the size of the configuration record", len(record))
	}
	if offset < 0 || length < 1 || offset+length > len(record) {
		return errInvalidInput("range %d+%d is outside the record", offset, length)
	}
	return s.kbRecordWriteChunks(ctx, CommandMouseRecordWrite, offset, record[offset:offset+length], 0)
}
