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
				m.recoveryRestoreErr = nil
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
	text := max(1, m.width-4)
	var b strings.Builder
	b.WriteString(styleDanger.Render("Writes are locked for this session") + "\n\n")
	b.WriteString(wrapStyled(styleBody, m.recoveryReason, text) + "\n\n")
	b.WriteString(wrapStyled(styleBody, "To protect your device, mapping, firmware and every other write is disabled until you restart OpenBitdo. Nothing else in the app is available; that is deliberate.", text) + "\n\n")

	if m.recoveryHasBackup {
		switch {
		case m.recoveryRestoreDone:
			b.WriteString(stylePositive.Render("Backup restored. You can quit and restart now.") + "\n\n")
		case m.recoveryRestoreErr != nil:
			b.WriteString(styleDanger.Render("Restoring the backup failed.") + "\n")
			b.WriteString(wrapStyled(styleFaint, m.recoveryRestoreErr.Error(), text) + "\n\n")
			b.WriteString(styleKey.Render("r") + " try the restore again\n\n")
		default:
			b.WriteString(styleKey.Render("r") + " restore the backup taken before the failed write\n\n")
		}
	} else {
		b.WriteString(wrapStyled(styleFaint, "No backup is available to restore for this failure.", text) + "\n\n")
	}
	b.WriteString(styleKey.Render("q") + " quit")

	return renderBoundedPanel(m.width-2, height-2, b.String())
}
