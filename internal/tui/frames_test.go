package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// TestDumpFrame prints one rendered frame as plain text, for looking at the
// UI without a terminal. It is skipped unless OPENBITDO_FRAME is set:
//
//	OPENBITDO_FRAME=80x24 go test ./internal/tui -run TestDumpFrame -v
//	OPENBITDO_FRAME=100x30 OPENBITDO_FRAME_KEYS="enter down ?" go test ...
//	OPENBITDO_FRAME=100x30 OPENBITDO_FRAME_REAL=1 go test ...
//
// OPENBITDO_FRAME_KEYS is a space-separated key script (bubbletea key names;
// a longer word is typed rune by rune). OPENBITDO_FRAME_REAL=1 uses attached
// hardware instead of the mock devices, and sends it the diagnostic reads.
func TestDumpFrame(t *testing.T) {
	size := os.Getenv("OPENBITDO_FRAME")
	if size == "" {
		t.Skip("set OPENBITDO_FRAME=WIDTHxHEIGHT to dump a frame")
	}
	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil {
		t.Fatalf("OPENBITDO_FRAME must look like 100x30: %v", err)
	}

	mock := os.Getenv("OPENBITDO_FRAME_REAL") == ""
	c := core.New(core.Config{MockMode: mock, ProgressIntervalMs: 1})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	nav := input.StartResult{Events: make(chan input.NavEvent)}
	m := NewModel(ctx, cancel, c, nav, Options{
		SettingsPath: t.TempDir() + "/config.toml", Settings: defaultSettings(), MockMode: mock,
	})
	m.width, m.height = width, height
	m = loadDevicesAndDrain(t, m, c)
	if !mock {
		// Real probes outlast drainCmds' per-command timeout; they finish in
		// the background and land in the cache the views read from.
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			pending := false
			for _, device := range m.devices.devices {
				pending = pending || !c.HasDiagnosed(device)
			}
			if !pending {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	for _, key := range strings.Fields(os.Getenv("OPENBITDO_FRAME_KEYS")) {
		var msgs []tea.KeyMsg
		if named := keyMsg(key); named.Type != tea.KeyRunes || len([]rune(key)) == 1 {
			msgs = append(msgs, *named)
		} else {
			for _, r := range key {
				msgs = append(msgs, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			}
		}
		for _, msg := range msgs {
			next, cmd := m.Update(msg)
			m = drainCmds(t, next.(Model), cmd)
		}
	}

	frame := ansi.Strip(m.View())
	border := strings.Repeat("─", width)
	fmt.Printf("┌%s┐\n", border)
	for _, line := range strings.Split(frame, "\n") {
		fmt.Printf("│%s%s│\n", line, strings.Repeat(" ", max(0, width-ansi.StringWidth(line))))
	}
	fmt.Printf("└%s┘\n", border)
}
