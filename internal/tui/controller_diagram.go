package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/charmbracelet/x/ansi"
)

// diagramNamesForKind returns the physical button names for kind, in wire
// order -- JP108's 10 dedicated buttons or Ultimate2's 17 core buttons.
func diagramNamesForKind(kind core.DeviceKind) []string {
	if kind == core.KindJP108 {
		names := make([]string, len(core.AllDedicatedButtons))
		for i, b := range core.AllDedicatedButtons {
			names[i] = b.String()
		}
		return names
	}
	names := make([]string, len(core.AllU2Buttons))
	for i, b := range core.AllU2Buttons {
		names[i] = b.String()
	}
	return names
}

const (
	diagramCols           = 6
	diagramCellLabelWidth = 10 // longest name ("DPadRight") is 9 chars
)

// renderControllerDiagramASCII renders names as a plain bracketed grid,
// wrapping every diagramCols entries onto a new row, with the entry at
// selectedIdx highlighted using styleSelectedRow -- the same "current
// selection" visual idiom used everywhere else in this app (see theme.go),
// so this diagram reads as one more selection list, not a one-off style.
//
// This is deliberately a plain grid in wire order, not a spatial gamepad
// silhouette: JP108 (PID_108JP = "Retro 108 Mechanical Keyboard", per
// docs/spec/device_name_catalog.md) is a full mechanical keyboard, and
// nothing in this project's spec or evidence dossiers documents where its
// 10 dedicated buttons (A, B, K1-K8) physically sit on that keyboard --
// drawing a specific physical layout would be exactly the kind of guessed
// hardware fact this project's own conventions deliberately avoid (see
// internal/input/descriptor_other.go's comment on the same principle).
// Ultimate2 is a conventional gamepad and could support a real spatial
// diagram, but is rendered the same grid way here so both device kinds get
// one consistent, honest diagram style rather than a precise-looking layout
// for one and an admittedly-approximate one for the other.
func renderControllerDiagramASCII(names []string, selectedIdx int) string {
	var b strings.Builder
	for i, name := range names {
		if len(name) > diagramCellLabelWidth {
			name = name[:diagramCellLabelWidth]
		}
		cell := fmt.Sprintf("[%-*s]", diagramCellLabelWidth, name)
		if i == selectedIdx {
			b.WriteString(styleSelectedRow.Render(cell))
		} else {
			b.WriteString(styleFaint.Render(cell))
		}
		if (i+1)%diagramCols == 0 || i == len(names)-1 {
			b.WriteString("\n")
		} else {
			b.WriteString(" ")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// controllerDiagramSupportsKittyGraphics detects whether the running
// terminal understands the Kitty graphics protocol. Checked directly
// against real terminal documentation, not assumed: Ghostty's own docs
// (ghostty.5, image-storage-limit) confirm it implements "the Kitty image
// protocol"; kitty is the protocol's origin; WezTerm documents support for
// it too. TERM_PROGRAM=ghostty and KITTY_WINDOW_ID are the terminals' own
// documented identification conventions, not guessed heuristics.
func controllerDiagramSupportsKittyGraphics() bool {
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	if strings.Contains(os.Getenv("TERM"), "kitty") {
		return true
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty", "WezTerm", "kitty":
		return true
	}
	return false
}

// diagramColorForANSI256 gives a plain RGB approximation of a handful of
// this theme's ANSI 256-palette colors, for the raster path only -- the
// terminal itself resolves ANSI colors for text, but a PNG payload needs
// literal RGB bytes. Close enough for a small "selected vs not" swatch,
// not a color-accurate reproduction.
func diagramColorForANSI256(selected bool) color.RGBA {
	if selected {
		return color.RGBA{R: 0x87, G: 0xaf, B: 0xd7, A: 0xff} // theme.Accent (111): soft azure
	}
	return color.RGBA{R: 0x44, G: 0x44, B: 0x44, A: 0xff} // theme.BorderDim (238)-ish: dim neutral
}

// renderControllerDiagramKitty draws names as a grid of solid-color
// rectangles (selected vs not), PNG-encoded and wrapped in a Kitty graphics
// escape sequence via github.com/charmbracelet/x/ansi.KittyGraphics. No
// text is drawn into the image itself (no font-rendering dependency) --
// renderControllerDiagram always prints the labeled ASCII grid alongside
// this, so the image is a visual accent on top of the real, readable
// labels, not a replacement for them.
func renderControllerDiagramKitty(names []string, selectedIdx int) (string, bool) {
	if len(names) == 0 {
		return "", false
	}
	const cellW, cellH, gap = 40, 24, 4
	cols := diagramCols
	rows := (len(names) + cols - 1) / cols
	imgW := cols*cellW + (cols-1)*gap
	imgH := rows*cellH + (rows-1)*gap

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{}}, image.Point{}, draw.Src)

	for i := range names {
		col, row := i%cols, i/cols
		x0, y0 := col*(cellW+gap), row*(cellH+gap)
		rect := image.Rect(x0, y0, x0+cellW, y0+cellH)
		draw.Draw(img, rect, &image.Uniform{diagramColorForANSI256(i == selectedIdx)}, image.Point{}, draw.Src)
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return "", false
	}
	payload := []byte(base64.StdEncoding.EncodeToString(pngBuf.Bytes()))

	// a=T: transmit and display immediately. f=100: PNG payload. c/r: scale
	// the image to fit this many terminal columns/rows, so it doesn't need
	// to know this terminal's actual pixel-per-cell ratio.
	opts := []string{"a=T", "f=100", fmt.Sprintf("c=%d", cols*2), fmt.Sprintf("r=%d", rows)}
	return ansi.KittyGraphics(payload, opts...), true
}

// renderControllerDiagram is the Mapping Editor's entry point: a real Kitty
// graphics image on top when the terminal supports one, always followed by
// the labeled ASCII grid (see renderControllerDiagramASCII for why it's a
// wire-order grid, not a spatial controller silhouette). selectedIdx is the
// physical button's index into diagramNamesForKind(kind)'s order; pass a
// negative or out-of-range index to render with nothing highlighted.
func renderControllerDiagram(kind core.DeviceKind, selectedIdx int) string {
	names := diagramNamesForKind(kind)
	if selectedIdx < 0 || selectedIdx >= len(names) {
		selectedIdx = -1
	}
	ascii := renderControllerDiagramASCII(names, selectedIdx)
	if !controllerDiagramSupportsKittyGraphics() {
		return ascii
	}
	seq, ok := renderControllerDiagramKitty(names, selectedIdx)
	if !ok {
		return ascii
	}
	return seq + "\n" + ascii
}
