package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
)

// buildOnce compiles the real openbitdo binary a single time and shares the
// path across tests -- these tests drive the actual compiled program via a
// subprocess (not just the internal flag-parsing helper) because the bug
// this guards against was in main()'s exit-code/stream-routing behavior
// around flag.ErrHelp, not in flag.FlagSet.Parse itself: --help used to
// print usage, THEN print a spurious "error: flag: help requested" line,
// THEN exit 1 -- a subprocess test is the only way to catch a regression in
// that exact interaction.
//
// Built into a dedicated os.MkdirTemp directory rather than a per-test
// t.TempDir(): the latter is cleaned up when the *first* test using it
// finishes, which would delete the shared binary out from under every test
// that runs after it.
var (
	buildOnceMu    sync.Mutex
	builtBinPath   string
	buildOnceErr   error
	buildAttempted bool
)

func builtBinary(t *testing.T) string {
	t.Helper()
	buildOnceMu.Lock()
	defer buildOnceMu.Unlock()
	if !buildAttempted {
		buildAttempted = true
		dir, err := os.MkdirTemp("", "openbitdo-main-test-bin-")
		if err != nil {
			buildOnceErr = err
		} else {
			binPath := filepath.Join(dir, "openbitdo")
			cmd := exec.Command("go", "build", "-o", binPath, ".")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				buildOnceErr = err
				t.Logf("build stderr: %s", stderr.String())
			} else {
				builtBinPath = binPath
			}
		}
	}
	if buildOnceErr != nil {
		t.Fatalf("build openbitdo binary: %v", buildOnceErr)
	}
	return builtBinPath
}

func TestHelpFlagExitsCleanlyAndWritesToStdout(t *testing.T) {
	bin := builtBinary(t)
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			cmd := exec.Command(bin, flag)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			if err != nil {
				t.Fatalf("expected exit code 0 for %s, got error: %v (stderr=%q)", flag, err, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected empty stderr for %s, got %q", flag, stderr.String())
			}
			if !strings.Contains(stdout.String(), "Usage: openbitdo") {
				t.Fatalf("expected usage text on stdout for %s, got %q", flag, stdout.String())
			}
			if strings.Contains(stdout.String(), "error:") {
				t.Fatalf("stdout should not contain a spurious error line for %s, got %q", flag, stdout.String())
			}
		})
	}
}

func TestUnexpectedArgumentExitsNonZeroWithUsage(t *testing.T) {
	bin := builtBinary(t)
	cmd := exec.Command(bin, "not-a-real-subcommand")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected a non-zero exit code for an unexpected positional argument")
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got %v", err)
	}
	if !strings.Contains(stdout.String(), "Usage: openbitdo") {
		t.Fatalf("expected usage text on stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("expected an explanatory error on stderr, got %q", stderr.String())
	}
}

func TestVersionFlagExitsCleanlyAndWritesToStdout(t *testing.T) {
	bin := builtBinary(t)
	cmd := exec.Command(bin, "--version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected exit code 0, got error: %v (stderr=%q)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "openbitdo") {
		t.Fatalf("expected version info on stdout, got %q", stdout.String())
	}
}

func TestDiagnosticsDumpPrintsTOMLReportsPerMockDevice(t *testing.T) {
	bin := builtBinary(t)
	cmd := exec.Command(bin, "--mock", "--diagnostics-dump")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected exit code 0, got error: %v (stderr=%q)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	out := stdout.String()
	deviceCount := strings.Count(out, "[device]")
	if deviceCount < 2 {
		t.Fatalf("expected reports for multiple mock devices, got %d [device] sections in:\n%s", deviceCount, out)
	}
	if strings.Count(out, "\n---\n") != deviceCount-1 {
		t.Fatalf("expected %d '---' separators between %d device reports, got %d in:\n%s",
			deviceCount-1, deviceCount, strings.Count(out, "\n---\n"), out)
	}
	if !strings.Contains(out, "schema_version = 2") {
		t.Fatalf("expected the standard support-report schema, got:\n%s", out)
	}
}

func TestDiagnosticsDumpReportsNoDevicesOnStderrNotStdout(t *testing.T) {
	if devices, err := core.New(core.Config{}).ListDevices(context.Background()); err == nil && len(devices) > 0 {
		t.Skip("a real 8BitDo device is attached to this machine — this test needs none to be present")
	}
	bin := builtBinary(t)
	// Real (non-mock) mode with no hardware attached to this test environment
	// -- exercises the "no devices found" path distinctly from the mock path
	// above, and confirms that message goes to stderr, not stdout (stdout is
	// meant to be pipeable TOML output; a "no devices" notice mixed into it
	// would corrupt that for a script parsing it).
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
