// Command openbitdo is the CLI entrypoint: a single locked-down command
// surface (openbitdo, openbitdo --mock) with no subcommands, matching the
// prior Rust CLI's contract exactly.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// Set via -ldflags "-X main.appVersion=... -X main.gitCommit=... -X main.buildDate=..."
var (
	appVersion = "dev"
	gitCommit  = "unknown"
	buildDate  = "unknown"
)

const helpText = `Usage: openbitdo [OPTIONS]

Beginner-first 8BitDo controller utility.

Options:
      --mock              Use mock transport/devices
      --debug-log <path>  Write detailed protocol traces (commands sent, raw
                           responses, timing) to <path>, for troubleshooting.
                           Off by default; never used in mock mode.
  -h, --help              Print this help

Examples:
  openbitdo
  openbitdo --mock
  openbitdo --debug-log ~/openbitdo-debug.log

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

func run() error {
	fs := flag.NewFlagSet("openbitdo", flag.ContinueOnError)
	fs.Usage = func() {
		// A failed write to stdout here isn't actionable — there's nothing
		// left to report it through — so the error is deliberately discarded.
		_, _ = fmt.Fprint(os.Stdout, helpText)
	}
	mock := fs.Bool("mock", false, "Use mock transport/devices")
	debugLogPath := fs.String("debug-log", "", "Write detailed protocol traces to this file")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	path := tui.SettingsPath()
	settings, warning := tui.LoadSettings(path)
	if warning != "" {
		fmt.Fprintln(os.Stderr, warning)
	}

	var debugLog *log.Logger
	if *debugLogPath != "" {
		f, err := os.OpenFile(*debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("open --debug-log file: %w", err)
		}
		defer func() { _ = f.Close() }()
		debugLog = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
		debugLog.Printf("=== openbitdo %s starting (mock=%v) ===", appVersion, *mock)
	}

	c := core.New(core.Config{
		MockMode: *mock, AdvancedMode: settings.AdvancedMode,
		DefaultChunkSize: 56, ProgressIntervalMs: 5,
		FirmwareManifestURL: core.DefaultConfig().FirmwareManifestURL,
		DebugLog:            debugLog,
	})

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
		Build: tui.BuildInfo{
			AppVersion: appVersion, Commit: gitCommit, BuildDate: buildDate,
			Platform: runtime.GOOS + "/" + runtime.GOARCH,
		},
		Settings: settings, SettingsPath: path, MockMode: *mock, NavNotes: nav.Notes,
	})

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
	_, err := program.Run()
	return err
}
