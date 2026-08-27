// Command openbitdo is the CLI entrypoint: a single locked-down command
// surface (openbitdo, openbitdo --mock) with no subcommands, matching the
// prior Rust CLI's contract exactly.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// Set by scripts/build_metadata.sh via -ldflags. Keep the first three symbol
// names stable: existing downstream packaging already sets them directly.
var (
	appVersion    = "v0.1.0-rc.1"
	gitCommit     = ""
	buildDate     = ""
	buildPlatform = ""
	gitDirty      = ""
)

type buildMetadata struct {
	Version  string
	Commit   string
	Date     string
	Platform string
	Dirty    string
}

// resolvedBuildMetadata combines explicit release linker values with Go's
// embedded VCS settings. The latter keep ordinary local `go build` binaries
// informative without making release builds depend on implicit tool behavior.
func resolvedBuildMetadata() buildMetadata {
	meta := buildMetadata{
		Version:  valueOrUnknown(appVersion),
		Commit:   strings.TrimSpace(gitCommit),
		Date:     valueOrUnknown(buildDate),
		Platform: strings.TrimSpace(buildPlatform),
		Dirty:    strings.ToLower(strings.TrimSpace(gitDirty)),
	}

	var vcsRevision, vcsDirty string
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				vcsRevision = setting.Value
			case "vcs.modified":
				vcsDirty = strings.ToLower(setting.Value)
			}
		}
	}

	if meta.Commit == "" {
		meta.Commit = vcsRevision
	}
	meta.Commit = shortCommit(valueOrUnknown(meta.Commit))
	if meta.Platform == "" {
		meta.Platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	if meta.Dirty == "" {
		meta.Dirty = vcsDirty
	}
	if meta.Dirty != "true" && meta.Dirty != "false" {
		meta.Dirty = "unknown"
	}
	return meta
}

func valueOrUnknown(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "unknown"
}

func shortCommit(commit string) string {
	if len(commit) > 12 && commit != "unknown" {
		return commit[:12]
	}
	return commit
}

func (m buildMetadata) versionString() string {
	return fmt.Sprintf("openbitdo %s (commit %s, built %s, %s, dirty=%s)",
		m.Version, m.Commit, m.Date, m.Platform, m.Dirty)
}

const helpText = `Usage: openbitdo [OPTIONS]

Beginner-first 8BitDo controller utility.

Options:
      --mock              Use mock transport/devices
      --debug-log <path>  Write detailed protocol traces (commands sent, raw
                           responses, timing) to <path>, for troubleshooting.
                           Off by default; never used in mock mode.
      --version           Print version/build info and exit
      --diagnostics-dump  Run diagnostics against every enumerated device
                           (or mock devices with --mock) and print a TOML
                           report per device to stdout, without launching
                           the interactive TUI. For scripting/support use.
  -h, --help              Print this help

Examples:
  openbitdo
  openbitdo --mock
  openbitdo --debug-log ~/openbitdo-debug.log
  openbitdo --version
  openbitdo --mock --diagnostics-dump

Install:
  Homebrew: brew tap bybrooklyn/openbitdo && brew install openbitdo
  AUR:      paru -S openbitdo-bin
  Releases: download a tarball, then run bin/openbitdo

Notes:
  --mock starts the app without real hardware.
  macOS packages are currently unsigned and non-notarized.
`

func main() {
	if err := run(); err != nil {
		if err == flag.ErrHelp {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type cliOptions struct {
	mock            *bool
	debugLogPath    *string
	showVersion     *bool
	diagnosticsDump *bool
}

func newFlagSet() (*flag.FlagSet, cliOptions) {
	fs := flag.NewFlagSet("openbitdo", flag.ContinueOnError)
	fs.Usage = func() {
		// A failed write to stdout here isn't actionable — there's nothing
		// left to report it through — so the error is deliberately discarded.
		_, _ = fmt.Fprint(os.Stdout, helpText)
	}
	options := cliOptions{
		mock:            fs.Bool("mock", false, "Use mock transport/devices"),
		debugLogPath:    fs.String("debug-log", "", "Write detailed protocol traces to this file"),
		showVersion:     fs.Bool("version", false, "Print version/build info and exit"),
		diagnosticsDump: fs.Bool("diagnostics-dump", false, "Run diagnostics against every enumerated device and print TOML reports to stdout"),
	}
	return fs, options
}

func run() error {
	fs, options := newFlagSet()
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	metadata := resolvedBuildMetadata()
	build := tui.BuildInfo{
		AppVersion: metadata.Version, Commit: metadata.Commit, BuildDate: metadata.Date,
		Platform: metadata.Platform, Dirty: metadata.Dirty,
	}
	if *options.showVersion {
		// Same reasoning as --help: nothing left to report a write failure
		// through, so it's deliberately discarded rather than checked.
		_, _ = fmt.Fprintln(os.Stdout, metadata.versionString())
		return nil
	}

	path := tui.SettingsPath()
	settings, warning := tui.LoadSettings(path)
	if warning != "" {
		fmt.Fprintln(os.Stderr, warning)
	}

	var debugLog *log.Logger
	if *options.debugLogPath != "" {
		f, err := os.OpenFile(*options.debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("open --debug-log file: %w", err)
		}
		defer func() { _ = f.Close() }()
		debugLog = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
		debugLog.Printf("=== %s starting (mock=%v) ===", metadata.versionString(), *options.mock)
	}

	c := core.New(core.Config{
		MockMode: *options.mock, AdvancedMode: settings.AdvancedMode,
		DefaultChunkSize: 56, ProgressIntervalMs: 5,
		DebugLog: debugLog,
	})

	if *options.diagnosticsDump {
		return runDiagnosticsDump(context.Background(), c)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	nav := input.Start(ctx)

	model := tui.NewModel(ctx, cancel, c, nav, tui.Options{
		Build:    build,
		Settings: settings, SettingsPath: path, MockMode: *options.mock, NavNotes: nav.Notes,
	})

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
	_, err := program.Run()
	return normalizeProgramExitError(err)
}

// normalizeProgramExitError distinguishes an expected user/signal shutdown
// from an actual Bubble Tea failure. The TUI cancels its shared context before
// returning tea.Quit so background HID work stops promptly; Bubble Tea reports
// that otherwise-graceful path as ErrProgramKilled wrapping context.Canceled.
func normalizeProgramExitError(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// runDiagnosticsDump enumerates every currently visible device and prints a
// TOML diagnostics report per device to stdout, without launching the TUI —
// for scripting/support use (e.g. attaching output to a bug report without
// needing to drive the interactive UI).
func runDiagnosticsDump(ctx context.Context, c *core.OpenBitdoCore) error {
	devices, err := c.ListDevices(ctx)
	if err != nil {
		return fmt.Errorf("list devices: %w", err)
	}
	if len(devices) == 0 {
		fmt.Fprintln(os.Stderr, "no devices found")
		return nil
	}
	for i, device := range devices {
		diag, err := c.DiagProbe(ctx, device.VidPid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "diag probe for %s: %v\n", device.Name, err)
			continue
		}
		report, err := tui.DiagnosticsReportTOML(device, diag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "render report for %s: %v\n", device.Name, err)
			continue
		}
		if i > 0 {
			fmt.Println("---")
		}
		fmt.Print(report)
	}
	return nil
}
