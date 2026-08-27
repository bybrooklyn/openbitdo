//go:build manual

package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
)

// This test intentionally enumerates live HID devices and launches the real,
// non-mock diagnostics path. It must remain behind the manual build tag.
func TestDiagnosticsDumpReportsNoDevicesOnStderrNotStdout(t *testing.T) {
	if devices, err := core.New(core.Config{}).ListDevices(context.Background()); err == nil && len(devices) > 0 {
		t.Skip("a real 8BitDo device is attached to this machine — this test needs none to be present")
	}

	bin := builtBinary(t)
	// Real (non-mock) mode with no hardware attached to this test environment
	// exercises the "no devices found" path distinctly from the mock path and
	// confirms that notice cannot corrupt pipeable TOML on stdout.
	cmd := exec.Command(bin, "--diagnostics-dump")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected exit code 0 even with no devices, got error: %v (stderr=%q)", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when no devices are found, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no devices found") {
		t.Fatalf("expected a 'no devices found' notice on stderr, got %q", stderr.String())
	}
}
