// Command openbitdo is the CLI entrypoint: a single locked-down command
// surface (openbitdo, openbitdo --mock) with no subcommands, matching the
// prior Rust CLI's contract exactly.
package main

import (
	"context"
	"flag"
	"fmt"
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
      --mock   Use mock transport/devices
  -h, --help   Print this help

Examples:
  openbitdo
  openbitdo --mock

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
	fs.Usage = func() { fmt.Fprint(os.Stdout, helpText) }
	mock := fs.Bool("mock", false, "Use mock transport/devices")
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

	c := core.New(core.Config{
		MockMode: *mock, AdvancedMode: settings.AdvancedMode,
		DefaultChunkSize: 56, ProgressIntervalMs: 5,
		FirmwareManifestURL: core.DefaultConfig().FirmwareManifestURL,
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
