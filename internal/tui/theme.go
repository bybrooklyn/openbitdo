package tui

import "github.com/charmbracelet/lipgloss"

// theme is OpenBitdo's visual identity: a restrained, modern palette in the
// register of well-designed terminal apps (opencode and similar) rather than
// a translation of the prior Rust TUI's plain ANSI colors. Semantic meaning
// (danger/warning/positive/accent) is what carries over from the original;
// the exact hues do not.
//
// Structure (not just color) also follows opencode's actual technique,
// verified by reading its TUI source directly rather than guessing from
// screenshots: it uses no rectangular box borders anywhere — every border in
// its codebase is a single-side left (or occasionally bottom) rule, and
// dialogs are borderless solid-color panels floating on a dimmed backdrop.
// stylePanel/stylePanelActive and the modal styles below follow that same
// left-bar-only, no-full-box approach.
type themeT struct {
	Background lipgloss.Color
	Surface    lipgloss.Color
	BorderDim  lipgloss.Color

	Text      lipgloss.Color
	TextFaint lipgloss.Color

	Accent lipgloss.Color

	Positive lipgloss.Color
	Warning  lipgloss.Color
	Danger   lipgloss.Color
}

var theme = themeT{
	Surface:   lipgloss.Color("235"),
	BorderDim: lipgloss.Color("238"),

	Text:      lipgloss.Color("252"),
	TextFaint: lipgloss.Color("240"),

	Accent: lipgloss.Color("111"), // soft azure

	Positive: lipgloss.Color("114"), // muted green
	Warning:  lipgloss.Color("179"), // amber
	Danger:   lipgloss.Color("174"), // muted coral-red
}

// leftBar is a border with only a left-side vertical rule — no corners, no
// top/right/bottom. Matches opencode's actual SplitBorder technique (a plain
// heavy vertical bar, not a lipgloss "rounded box"). Used for every panel and
// for block-level prose (warnings, tier explanations) instead of a full box.
var leftBar = lipgloss.Border{Left: "┃"}

// barred applies a left-only accent bar in the given color, with a small
// left-padding gap so the bar doesn't sit flush against the text.
func barred(c lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(leftBar, false, false, false, true).
		BorderForeground(c).
		PaddingLeft(1)
}

var (
	styleTitle = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	styleBody = lipgloss.NewStyle().Foreground(theme.Text)

	styleFaint = lipgloss.NewStyle().Foreground(theme.TextFaint)

	stylePositive = lipgloss.NewStyle().Foreground(theme.Positive).Bold(true)
	styleWarning  = lipgloss.NewStyle().Foreground(theme.Warning).Bold(true)
	styleDanger   = lipgloss.NewStyle().Foreground(theme.Danger).Bold(true)

	styleAccent = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	// stylePanel/stylePanelActive: left-bar only, no full box — see the
	// package doc comment above for why. Active/focused panels get an Accent
	// bar; inactive ones get a dim neutral bar, same distinction the old
	// rounded-box colors made, just expressed as a rule instead of a box.
	stylePanel       = barred(theme.BorderDim)
	stylePanelActive = barred(theme.Accent)

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

	// styleModal: no border at all, matching opencode's actual dialog
	// styling exactly (verified by reading dialog.tsx/dialog-confirm.tsx) —
	// a solid Surface-colored panel floating on the dimmed backdrop
	// (see compositeDimmed in modal.go). The danger/normal distinction is
	// carried entirely by the title and button text color (see modal.go),
	// not by a colored box outline — same as opencode's own confirm dialog.
	styleModal = lipgloss.NewStyle().
			Padding(1, 2).
			Background(theme.Surface)

	// styleWarningBlock/stylePositiveBlock/styleAccentBlock/styleDangerBlock:
	// left-bar treatment for block-level prose (tier explanations, firmware
	// warnings) — the same technique as the panels above, applied to a
	// smaller unit. Mirrors how opencode colors whole message blocks by
	// role instead of only coloring inline text.
	styleWarningBlock  = barred(theme.Warning)
	stylePositiveBlock = barred(theme.Positive)
	styleAccentBlock   = barred(theme.Accent)
	styleDangerBlock   = barred(theme.Danger)
)

// Icon* are the shared glyph vocabulary — every screen pulls from these
// instead of choosing literal glyphs ad hoc. Kept as a small, deliberate set
// rather than growing one-off symbols per screen.
const (
	IconPass       = "✓"
	IconFail       = "✗"
	IconWarn       = "⚠"
	IconInProgress = "◆"

	IconTierFull      = "●"
	IconTierCandidate = "◐"
	IconTierDetect    = "○"

	IconProgressFilled = "█"
	IconProgressEmpty  = "░"
)

func supportTierBadge(label string, kind string) string {
	switch kind {
	case "full":
		return styleBadgeFull.Render(IconTierFull + " " + label)
	case "candidate":
		return styleBadgeCandidate.Render(IconTierCandidate + " " + label)
	default:
		return styleBadgeDetect.Render(IconTierDetect + " " + label)
	}
}
