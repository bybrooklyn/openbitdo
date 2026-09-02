package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestDiagramNamesForKind_MatchesWireOrder(t *testing.T) {
	jp108 := diagramNamesForKind(core.KindJP108)
	if len(jp108) != 10 {
		t.Fatalf("expected 10 JP108 dedicated buttons, got %d: %v", len(jp108), jp108)
	}
	if jp108[0] != "A" || jp108[1] != "B" || jp108[2] != "K1" || jp108[9] != "K8" {
		t.Fatalf("unexpected JP108 name order: %v", jp108)
	}

	u2 := diagramNamesForKind(core.KindUltimate2)
	if len(u2) != 17 {
		t.Fatalf("expected 17 Ultimate2 buttons, got %d: %v", len(u2), u2)
	}
	if u2[0] != "A" || u2[3] != "Y" || u2[16] != "DPadRight" {
		t.Fatalf("unexpected Ultimate2 name order: %v", u2)
	}
}

// TestRenderControllerDiagramASCII_HighlightsOnlySelectedButton guards the
// actual point of the diagram: the selected button (and only the selected
// button) must render with styleSelectedRow, the same selection idiom used
// everywhere else in this app.
func TestRenderControllerDiagramASCII_HighlightsOnlySelectedButton(t *testing.T) {
	prevProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prevProfile) })

	names := []string{"A", "B", "K1"}
	view := renderControllerDiagramASCII(names, 1)

	selectedCell := styleSelectedRow.Render(fmt.Sprintf("[%-*s]", diagramCellLabelWidth, "B"))
	if !strings.Contains(view, selectedCell) {
		t.Fatalf("expected the selected button (B) to render with styleSelectedRow, got:\n%s", ansi.Strip(view))
	}

	unselectedA := styleFaint.Render(fmt.Sprintf("[%-*s]", diagramCellLabelWidth, "A"))
	unselectedK1 := styleFaint.Render(fmt.Sprintf("[%-*s]", diagramCellLabelWidth, "K1"))
	if !strings.Contains(view, unselectedA) || !strings.Contains(view, unselectedK1) {
		t.Fatalf("expected unselected buttons to render with styleFaint, got:\n%s", ansi.Strip(view))
	}

	// The regression this guards against: an unselected button must never
	// pick up the selected style just because it shares theme colors.
	wrongHighlight := styleSelectedRow.Render(fmt.Sprintf("[%-*s]", diagramCellLabelWidth, "A"))
	if strings.Contains(view, wrongHighlight) {
		t.Fatal("expected only the selected button to use styleSelectedRow, but an unselected one did too")
	}
}

func TestRenderControllerDiagramASCII_NoSelectionHighlightsNothing(t *testing.T) {
	prevProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prevProfile) })

	names := []string{"A", "B"}
	view := renderControllerDiagramASCII(names, -1)
	for _, name := range names {
		if strings.Contains(view, styleSelectedRow.Render(fmt.Sprintf("[%-*s]", diagramCellLabelWidth, name))) {
			t.Fatalf("expected nothing highlighted with selectedIdx=-1, got:\n%s", ansi.Strip(view))
		}
	}
}

func TestControllerDiagramSupportsKittyGraphics(t *testing.T) {
	cases := []struct {
		name                          string
		kittyWindowID, term, termProg string
		want                          bool
	}{
		{"kitty window id set", "1", "", "", true},
		{"TERM contains kitty", "", "xterm-kitty", "", true},
		{"TERM_PROGRAM ghostty", "", "", "ghostty", true},
		{"TERM_PROGRAM WezTerm", "", "", "WezTerm", true},
		{"plain xterm, no program", "", "xterm-256color", "", false},
		{"TERM_PROGRAM Apple_Terminal", "", "xterm-256color", "Apple_Terminal", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KITTY_WINDOW_ID", tc.kittyWindowID)
			t.Setenv("TERM", tc.term)
			t.Setenv("TERM_PROGRAM", tc.termProg)
			if got := controllerDiagramSupportsKittyGraphics(); got != tc.want {
				t.Fatalf("controllerDiagramSupportsKittyGraphics() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRenderControllerDiagramKitty_ProducesValidKittyEscapeSequence checks
// the raster path end to end: the returned sequence must be a real Kitty
// graphics APC escape sequence wrapping a base64 payload that decodes to a
// genuinely valid PNG image (not just "some bytes that look plausible").
func TestRenderControllerDiagramKitty_ProducesValidKittyEscapeSequence(t *testing.T) {
	names := diagramNamesForKind(core.KindUltimate2)
	seq, ok := renderControllerDiagramKitty(names, 3)
	if !ok {
		t.Fatal("expected renderControllerDiagramKitty to succeed for a non-empty name list")
	}
	if !strings.HasPrefix(seq, "\x1b_G") {
		t.Fatalf("expected a Kitty graphics APC prefix (ESC _ G), got: %q", seq[:min(20, len(seq))])
	}
	if !strings.HasSuffix(seq, "\x1b\\") {
		t.Fatal("expected the sequence to end with the ST terminator (ESC \\)")
	}

	// Extract the base64 payload between the last ';' before the terminator
	// and the terminator itself, per KittyGraphics' own
	// "[opts];[payload]ST" format.
	body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b_G"), "\x1b\\")
	semi := strings.Index(body, ";")
	if semi < 0 {
		t.Fatalf("expected an opts;payload separator, got: %q", body)
	}
	payload := body[semi+1:]

	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload is not valid base64: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatalf("decoded payload is not a valid PNG: %v", err)
	}
}

func TestRenderControllerDiagramKitty_EmptyNamesFails(t *testing.T) {
	if _, ok := renderControllerDiagramKitty(nil, -1); ok {
		t.Fatal("expected renderControllerDiagramKitty to fail for an empty name list")
	}
}

// TestRenderControllerDiagram_AlwaysIncludesLabeledASCII guards the design
// decision that the raster image is always paired with the real labeled
// grid, never shown alone -- a colored square with no label wouldn't tell
// you which button it represents.
func TestRenderControllerDiagram_AlwaysIncludesLabeledASCII(t *testing.T) {
	t.Setenv("KITTY_WINDOW_ID", "1") // force the Kitty path on
	t.Setenv("TERM", "")
	t.Setenv("TERM_PROGRAM", "")

	view := renderControllerDiagram(core.KindJP108, 2)
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "[K1") {
		t.Fatalf("expected the labeled ASCII grid to still be present alongside the Kitty image, got:\n%s", plain)
	}
	if !strings.Contains(view, "\x1b_G") {
		t.Fatal("expected a Kitty graphics sequence to be present when the terminal supports it")
	}

	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM", "xterm-256color")
	view = renderControllerDiagram(core.KindJP108, 2)
	if strings.Contains(view, "\x1b_G") {
		t.Fatal("expected no Kitty graphics sequence when the terminal doesn't support it")
	}
	if !strings.Contains(ansi.Strip(view), "[K1") {
		t.Fatal("expected the labeled ASCII grid regardless of Kitty support")
	}
}

// TestViewMapping_IncludesControllerDiagram is the integration check: the
// real Mapping Editor screen must actually include the diagram, not just
// the isolated renderControllerDiagram function.
func TestViewMapping_IncludesControllerDiagram(t *testing.T) {
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "")

	m, _ := newTestModel(t, filepath.Join(t.TempDir(), "config.toml"))
	m.mapping = mappingState{
		device: core.AppDevice{Name: "JP108"},
		kind:   core.KindJP108,
		jp108Draft: []core.DedicatedButtonMapping{
			{Button: core.ButtonA, TargetHIDUsage: 0x0004},
			{Button: core.ButtonB, TargetHIDUsage: 0x0005},
		},
		cursor: 1,
	}
	view := m.viewMapping(30)
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "[A") || !strings.Contains(plain, "[B") {
		t.Fatalf("expected the controller diagram's button grid in the rendered Mapping Editor, got:\n%s", plain)
	}
}
