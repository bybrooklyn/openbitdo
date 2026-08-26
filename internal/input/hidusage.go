// Package input decodes standard USB-HID gamepad reports for TUI menu
// navigation. See spec/gamepad_input.md for the design and its one real
// platform limitation (report descriptor acquisition on non-Linux).
package input

// Usage pages and usages from the USB HID Usage Tables (public USB-IF
// standard) that matter for gamepad navigation.
const (
	UsagePageGenericDesktop uint16 = 0x01
	UsagePageButton         uint16 = 0x09

	UsageX         uint16 = 0x30
	UsageY         uint16 = 0x31
	UsageZ         uint16 = 0x32
	UsageRx        uint16 = 0x33
	UsageRy        uint16 = 0x34
	UsageRz        uint16 = 0x35
	UsageHatSwitch uint16 = 0x39
)
