package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Recovery is a forced full-app takeover once writeLockUntilRestart trips —
// route() redirects every message here regardless of m.screen. The lock is
// never cleared at runtime, only by restarting the process, matching the
// prior Rust TUI's deliberately hard stop: a write failure serious enough
// to trigger this is serious enough not to paper over with an in-app reset.

func (m Model) updateRecovery(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "r":
			if m.recoveryHasBackup && !m.recoveryRestoreDone {
				return m, cmdRestoreBackup(m.ctx, m.core, m.recoveryBackupID)
			}
		case "q":
			m.cancel()
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) viewRecovery(height int) string {
	var b strings.Builder
	b.WriteString(styleDanger.Render("Write lock active") + "\n\n")
	b.WriteString(styleBody.Render(m.recoveryReason) + "\n\n")
	b.WriteString(styleBody.Render("To protect your device, further writes, mapping, and firmware\n"+
		"operations are disabled for the rest of this session. Restart\n"+
		"OpenBitdo once you're ready to try again.") + "\n\n")

	if m.recoveryHasBackup {
		switch {
		case m.recoveryRestoreDone:
			b.WriteString(stylePositive.Render("Backup restored.") + "\n\n")
		case m.recoveryRestoreErr != nil:
			b.WriteString(styleDanger.Render("Restore failed: "+m.recoveryRestoreErr.Error()) + "\n\n")
		default:
			b.WriteString(styleKey.Render("r") + " restore the last known-good backup\n\n")
		}
	} else {
		b.WriteString(styleFaint.Render("No backup is available to restore for this failure.") + "\n\n")
	}
	b.WriteString(styleKey.Render("q") + " quit")

	return renderBoundedPanel(m.width-2, height-2, b.String())
}
