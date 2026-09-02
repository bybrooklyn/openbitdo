package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type layoutMode int

const (
	layoutTooSmall layoutMode = iota
	layoutCompact
	layoutWide
)

type appLayout struct {
	width, height int
	mode          layoutMode
	headerHeight  int
	footerHeight  int
	bodyHeight    int
}

type rect struct {
	x, y, w, h int
}

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

func calculateLayout(width, height int) appLayout {
	mode := layoutWide
	switch {
	case width < 60 || height < 18:
		mode = layoutTooSmall
	case width < 96 || height < 24:
		mode = layoutCompact
	}
	l := appLayout{width: width, height: height, mode: mode, headerHeight: 2, footerHeight: 1}
	l.bodyHeight = max(1, height-l.headerHeight-l.footerHeight)
	return l
}

func clampRendered(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = ansi.Cut(line, 0, width)
	}
	return strings.Join(lines, "\n")
}

func renderBoundedPanel(width, height int, content string) string {
	return renderBoundedPanelWithStyle(stylePanel, width, height, content)
}

func renderBoundedPanelWithStyle(style lipgloss.Style, width, height int, content string) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	limit := max(1, width-2)
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = ansi.Cut(line, 0, limit)
	}
	return style.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func viewportWindow(total, cursor, offset, limit int) (int, int, string) {
	if limit < 1 {
		limit = 1
	}
	if total <= limit {
		return 0, total, ""
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+limit {
		offset = cursor - limit + 1
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total-limit {
		offset = total - limit
	}
	indicator := ""
	if offset > 0 {
		indicator += "more above"
	}
	if offset+limit < total {
		if indicator != "" {
			indicator += " / "
		}
		indicator += "more below"
	}
	return offset, offset + limit, indicator
}

func modalGeometry(m modal, width, height int) (box, confirm, cancel rect) {
	rendered := m.view(width)
	box = rect{
		x: max(0, (width-lipgloss.Width(rendered))/2),
		y: max(0, (height-lipgloss.Height(rendered))/2),
		w: lipgloss.Width(rendered),
		h: lipgloss.Height(rendered),
	}
	confirmText := "[ " + m.confirmLabel + " ]"
	cancelText := "[ " + m.cancelLabel + " ]"
	for row, line := range strings.Split(ansi.Strip(rendered), "\n") {
		if idx := strings.Index(line, confirmText); idx >= 0 {
			confirm = rect{x: box.x + lipgloss.Width(line[:idx]), y: box.y + row, w: lipgloss.Width(confirmText), h: 1}
		}
		if idx := strings.Index(line, cancelText); idx >= 0 {
			cancel = rect{x: box.x + lipgloss.Width(line[:idx]), y: box.y + row, w: lipgloss.Width(cancelText), h: 1}
		}
	}
	return box, confirm, cancel
}
