package tui

import "github.com/charmbracelet/lipgloss"

// theme is OpenBitdo's visual identity: a restrained, modern palette in the
// register of well-designed terminal apps (opencode and similar) rather than
// a translation of the prior Rust TUI's plain ANSI colors. Semantic meaning
// (danger/warning/positive/accent) is what carries over from the original;
// the exact hues do not.
type themeT struct {
	Background lipgloss.Color
	Surface    lipgloss.Color
	Border     lipgloss.Color
	BorderDim  lipgloss.Color

	Text      lipgloss.Color
	TextDim   lipgloss.Color
	TextFaint lipgloss.Color

	Accent    lipgloss.Color
	AccentDim lipgloss.Color

	Positive lipgloss.Color
	Warning  lipgloss.Color
	Danger   lipgloss.Color
}

var theme = themeT{
	Surface:   lipgloss.Color("235"),
	Border:    lipgloss.Color("60"),
	BorderDim: lipgloss.Color("238"),

	Text:      lipgloss.Color("252"),
	TextDim:   lipgloss.Color("246"),
	TextFaint: lipgloss.Color("240"),

	Accent:    lipgloss.Color("111"), // soft azure
	AccentDim: lipgloss.Color("67"),

	Positive: lipgloss.Color("114"), // muted green
	Warning:  lipgloss.Color("179"), // amber
	Danger:   lipgloss.Color("174"), // muted coral-red
}

var (
	styleTitle = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	styleBody = lipgloss.NewStyle().Foreground(theme.Text)

	styleFaint = lipgloss.NewStyle().Foreground(theme.TextFaint)

	stylePositive = lipgloss.NewStyle().Foreground(theme.Positive).Bold(true)
	styleWarning  = lipgloss.NewStyle().Foreground(theme.Warning).Bold(true)
	styleDanger   = lipgloss.NewStyle().Foreground(theme.Danger).Bold(true)

	styleAccent = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	stylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.BorderDim).
			Padding(0, 1)

	stylePanelActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(theme.Accent).
				Padding(0, 1)

	stylePanelTitle = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	styleSelectedRow = lipgloss.NewStyle().
				Foreground(lipgloss.Color("235")).
				Background(theme.Accent).
				Bold(true)

	styleHelp = lipgloss.NewStyle().Foreground(theme.TextFaint)

	styleKey = lipgloss.NewStyle().Foreground(theme.Accent)

	styleBadgeFull      = lipgloss.NewStyle().Foreground(theme.Positive).Bold(true)
	styleBadgeCandidate = lipgloss.NewStyle().Foreground(theme.Warning).Bold(true)
	styleBadgeDetect    = lipgloss.NewStyle().Foreground(theme.TextFaint)

	styleModalBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(theme.Accent).
				Padding(1, 2).
				Background(theme.Surface)

	styleModalBorderDanger = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(theme.Danger).
				Padding(1, 2).
				Background(theme.Surface)
)

func supportTierBadge(label string, kind string) string {
	switch kind {
	case "full":
		return styleBadgeFull.Render("● " + label)
	case "candidate":
		return styleBadgeCandidate.Render("◐ " + label)
	default:
		return styleBadgeDetect.Render("○ " + label)
	}
}
