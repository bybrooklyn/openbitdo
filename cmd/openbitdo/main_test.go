package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestNormalizeProgramExitErrorTreatsUserCancellationAsCleanExit(t *testing.T) {
	for _, cancelled := range []error{
		context.Canceled,
		fmt.Errorf("%w: %w", tea.ErrProgramKilled, context.Canceled),
	} {
		if err := normalizeProgramExitError(cancelled); err != nil {
			t.Fatalf("expected user cancellation to be a clean exit, got %v", err)
		}
	}

	for _, err := range []error{tea.ErrProgramKilled, errors.New("renderer failed")} {
		if got := normalizeProgramExitError(err); !errors.Is(got, err) {
			t.Fatalf("expected non-cancellation error %v to be preserved, got %v", err, got)
		}
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
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "openbitdo v0.1.0-rc.1 (") {
		t.Fatalf("expected the v0.1.0-rc.1 version contract on stdout, got %q", out)
	}
	for _, field := range []string{"commit ", "built ", runtimePlatform(), "dirty="} {
		if !strings.Contains(out, field) {
			t.Fatalf("expected version output to contain %q, got %q", field, out)
		}
	}
}

func TestDefaultVersionMatchesVersionFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != appVersion {
		t.Fatalf("default app version %q does not match VERSION %q", appVersion, got)
	}
}

func runtimePlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func TestResolvedBuildMetadataPrefersExplicitReleaseValues(t *testing.T) {
	oldVersion, oldCommit, oldDate := appVersion, gitCommit, buildDate
	oldPlatform, oldDirty := buildPlatform, gitDirty
	t.Cleanup(func() {
		appVersion, gitCommit, buildDate = oldVersion, oldCommit, oldDate
		buildPlatform, gitDirty = oldPlatform, oldDirty
	})

	appVersion = "v9.8.7-rc.6"
	gitCommit = "0123456789abcdef0123456789abcdef01234567"
	buildDate = "2026-08-27T12:34:56Z"
	buildPlatform = "linux/amd64"
	gitDirty = "FALSE"

	got := resolvedBuildMetadata()
	want := buildMetadata{
		Version: "v9.8.7-rc.6", Commit: "0123456789ab",
		Date: "2026-08-27T12:34:56Z", Platform: "linux/amd64", Dirty: "false",
	}
	if got != want {
		t.Fatalf("resolved metadata mismatch:\n got: %#v\nwant: %#v", got, want)
	}
	wantVersion := "openbitdo v9.8.7-rc.6 (commit 0123456789ab, built 2026-08-27T12:34:56Z, linux/amd64, dirty=false)"
	if got.versionString() != wantVersion {
		t.Fatalf("version string mismatch:\n got: %q\nwant: %q", got.versionString(), wantVersion)
	}
}

func TestBuildMetadataHelperEmbedsEveryField(t *testing.T) {
	helper := filepath.Join("..", "..", "scripts", "build_metadata.sh")
	cmd := exec.Command(helper, "v9.8.7-rc.6")
	cmd.Env = append(filteredEnvironment(
		"OPENBITDO_GIT_COMMIT",
		"OPENBITDO_BUILD_DATE",
		"OPENBITDO_BUILD_PLATFORM",
		"OPENBITDO_GIT_DIRTY",
	),
		"OPENBITDO_GIT_COMMIT=0123456789abcdef0123456789abcdef01234567",
		"OPENBITDO_BUILD_DATE=2026-08-27T12:34:56Z",
		"OPENBITDO_BUILD_PLATFORM=linux/amd64",
		"OPENBITDO_GIT_DIRTY=false",
	)
	ldflags, err := cmd.Output()
	if err != nil {
		t.Fatalf("run build metadata helper: %v", err)
	}

	out := string(ldflags)
	for _, want := range []string{
		"-X main.appVersion=v9.8.7-rc.6",
		"-X main.gitCommit=0123456789abcdef0123456789abcdef01234567",
		"-X main.buildDate=2026-08-27T12:34:56Z",
		"-X main.buildPlatform=linux/amd64",
		"-X main.gitDirty=false",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("helper output missing %q: %q", want, out)
		}
	}
}

func filteredEnvironment(names ...string) []string {
	prefixes := make([]string, 0, len(names))
	for _, name := range names {
		prefixes = append(prefixes, name+"=")
	}
	var result []string
	for _, entry := range os.Environ() {
		keep := true
		for _, prefix := range prefixes {
			if strings.HasPrefix(entry, prefix) {
				keep = false
				break
			}
		}
		if keep {
			result = append(result, entry)
		}
	}
	return result
}

func TestCompletionOptionsMatchCLIHelpAndRegisteredFlags(t *testing.T) {
	registered := map[string]struct{}{"-h": {}, "--help": {}}
	fs, _ := newFlagSet()
	fs.VisitAll(func(f *flag.Flag) {
		registered["--"+f.Name] = struct{}{}
	})

	helpOptions := longOptions(helpText)
	helpOptions["-h"] = struct{}{}
	assertSameOptions(t, "CLI help", helpOptions, registered)

	completionDir := filepath.Join("..", "..", "completions")
	tests := []struct {
		name  string
		file  string
		parse func(string) map[string]struct{}
	}{
		{name: "bash", file: "openbitdo.bash", parse: bashCompletionOptions},
		{name: "zsh", file: "openbitdo.zsh", parse: zshCompletionOptions},
		{name: "fish", file: "openbitdo.fish", parse: fishCompletionOptions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(completionDir, tt.file))
			if err != nil {
				t.Fatalf("read completion: %v", err)
			}
			assertSameOptions(t, tt.name+" completion", tt.parse(string(raw)), registered)
		})
	}
}

func TestProductionCLIHasNoFirmwareOverride(t *testing.T) {
	fs, _ := newFlagSet()
	fs.VisitAll(func(f *flag.Flag) {
		if strings.Contains(f.Name, "firmware") {
			t.Errorf("production CLI must not expose a firmware feature override: --%s", f.Name)
		}
	})
}

var longOptionRE = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

func longOptions(text string) map[string]struct{} {
	options := make(map[string]struct{})
	for _, option := range longOptionRE.FindAllString(text, -1) {
		options[option] = struct{}{}
	}
	return options
}

func bashCompletionOptions(text string) map[string]struct{} {
	options := make(map[string]struct{})
	match := regexp.MustCompile(`(?m)^\s*opts="([^"]*)"`).FindStringSubmatch(text)
	if len(match) != 2 {
		return options
	}
	for _, option := range strings.Fields(match[1]) {
		options[option] = struct{}{}
	}
	return options
}

func zshCompletionOptions(text string) map[string]struct{} {
	options := longOptions(text)
	if strings.Contains(text, "{-h,--help}") {
		options["-h"] = struct{}{}
	}
	return options
}

func fishCompletionOptions(text string) map[string]struct{} {
	options := make(map[string]struct{})
	for _, match := range regexp.MustCompile(`(?:^|\s)-l\s+([a-z][a-z0-9-]*)`).FindAllStringSubmatch(text, -1) {
		options["--"+match[1]] = struct{}{}
	}
	for _, match := range regexp.MustCompile(`(?:^|\s)-s\s+([a-zA-Z0-9])`).FindAllStringSubmatch(text, -1) {
		options["-"+match[1]] = struct{}{}
	}
	return options
}

func assertSameOptions(t *testing.T, label string, got, want map[string]struct{}) {
	t.Helper()
	missing, extra := optionDifference(want, got), optionDifference(got, want)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("%s option mismatch: missing=%v extra=%v", label, missing, extra)
	}
}

func optionDifference(left, right map[string]struct{}) []string {
	var difference []string
	for option := range left {
		if _, ok := right[option]; !ok {
			difference = append(difference, option)
		}
	}
	sort.Strings(difference)
	return difference
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
