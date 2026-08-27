package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

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
	Background lipgloss.TerminalColor
	Surface    lipgloss.TerminalColor
	BorderDim  lipgloss.TerminalColor

	Text      lipgloss.TerminalColor
	TextFaint lipgloss.TerminalColor

	Accent lipgloss.TerminalColor

	Positive lipgloss.TerminalColor
	Warning  lipgloss.TerminalColor
	Danger   lipgloss.TerminalColor

	SelectedText lipgloss.TerminalColor
}

func semanticColor(light, dark string) lipgloss.TerminalColor {
	return semanticColorForEnv(os.Getenv("NO_COLOR") != "", light, dark)
}

func semanticColorForEnv(noColor bool, light, dark string) lipgloss.TerminalColor {
	if noColor {
		return lipgloss.NoColor{}
	}
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var theme = themeT{
	Surface:   semanticColor("255", "235"),
	BorderDim: semanticColor("244", "238"),

	Text:      semanticColor("235", "252"),
	TextFaint: semanticColor("244", "240"),

	Accent: semanticColor("25", "111"), // deep/soft azure

	Positive: semanticColor("28", "114"),  // green
	Warning:  semanticColor("130", "179"), // amber
	Danger:   semanticColor("124", "174"), // coral-red

	SelectedText: semanticColor("255", "235"),
}

// leftBar is a border with only a left-side vertical rule — no corners, no
// top/right/bottom. Matches opencode's actual SplitBorder technique (a plain
// heavy vertical bar, not a lipgloss "rounded box"). Used for every panel and
// for block-level prose (warnings, tier explanations) instead of a full box.
var leftBar = lipgloss.Border{Left: "┃"}

// barred applies a left-only accent bar in the given color, with a small
// left-padding gap so the bar doesn't sit flush against the text.
func barred(c lipgloss.TerminalColor) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(leftBar, false, false, false, true).
		BorderForeground(c).
		PaddingLeft(1)
}

var (
	styleTitle = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	// styleBody is the default style for plain informational body text —
	// status/detail lines, unselected list rows, diagnostic detail fields —
	// anywhere content isn't already carrying a more specific semantic style
	// (Faint, Warning, Danger, Positive, Accent). Applied consistently
	// across every screen, not just the one call site it started in.
	styleBody = lipgloss.NewStyle().Foreground(theme.Text)

	styleFaint = lipgloss.NewStyle().Foreground(theme.TextFaint)

	stylePositive = lipgloss.NewStyle().Foreground(theme.Positive).Bold(true)
	styleWarning  = lipgloss.NewStyle().Foreground(theme.Warning).Bold(true)
	styleDanger   = lipgloss.NewStyle().Foreground(theme.Danger).Bold(true)

	// styleAccent is for genuinely one-off accent-colored emphasis that
	// isn't a list selection (a modal's title, a firmware progress bar) —
	// selection has its own dedicated styleSelectedRow/styleSelectedMarker
	// now (see below), specifically so this and stylePanelTitle can never
	// silently drift back into being byte-identical the way they used to.
	styleAccent = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	// stylePanel/stylePanelActive: left-bar only, no full box — see the
	// package doc comment above for why. Active/focused panels get an Accent
	// bar; inactive ones get a dim neutral bar, same distinction the old
	// rounded-box colors made, just expressed as a rule instead of a box.
	stylePanel       = barred(theme.BorderDim)
	stylePanelActive = barred(theme.Accent)

	// stylePanelTitle marks section/panel headings. Underlined, on top of
	// Accent+Bold, specifically so it is never byte-identical to
	// styleAccent again (see styleAccent's own comment below) — a heading
	// and a selected row rendered with the same style is what caused the
	// user-reported "it's weird to tell what button I'm selecting" bug.
	stylePanelTitle = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true).Underline(true)

	// styleSelectedRow is this app's one "this is the current cursor
	// selection" visual idiom, used everywhere a list has a keyboard-movable
	// cursor: the Devices device list and Actions pane, Mapping Editor rows,
	// and Settings rows. Inverted (dark text on an Accent background)
	// specifically so a selected row can never be confused with
	// stylePanelTitle's plain underlined heading text at a glance. Wrap the
	// row's full plain-text content in one Render call so the inverted
	// background isn't cut short by an embedded style's own reset code —
	// see styleSelectedMarker below for the one case (Diagnostics) where a
	// row's content already carries its own embedded styling and this
	// whole-row wrap isn't safe to use.
	styleSelectedRow = lipgloss.NewStyle().
				Foreground(theme.SelectedText).
				Background(theme.Accent).
				Bold(true)

	// styleSelectedMarker is styleSelectedRow's marker-only counterpart: it
	// highlights just a leading "›" rather than the whole row. Use this
	// instead of styleSelectedRow when the row's own content already
	// contains embedded ANSI styling (e.g. Diagnostics' colored pass/fail
	// icon) — wrapping already-styled content in styleSelectedRow.Render
	// would have the inner content's own reset code cut the outer
	// background off partway through the row, a real (if subtle) rendering
	// bug, not just a style-consistency nicety.
	styleSelectedMarker = lipgloss.NewStyle().
				Foreground(theme.SelectedText).
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
