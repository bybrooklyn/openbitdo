//go:build manual

package tui

import "os"

// With -tags manual, OPENBITDO_FRAME_REAL=1 makes TestDumpFrame render what
// the attached hardware actually reports instead of the mock devices:
//
//	OPENBITDO_FRAME=100x30 OPENBITDO_FRAME_REAL=1 go test -tags manual ./internal/tui -run TestDumpFrame -v
//
// It runs the safe-read diagnostics against every attached 8BitDo device,
// like any other live probe, and sends nothing else.
func init() {
	frameUsesHardware = func() bool { return os.Getenv("OPENBITDO_FRAME_REAL") != "" }
}
