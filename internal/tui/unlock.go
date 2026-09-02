package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// The guarded candidate-readonly write probe requires a per-PID on-disk
// unlock file as its third, out-of-band gate (alongside advanced mode and
// the local risk acknowledgement) — an intentionally inconvenient step so
// the probe can't be triggered by UI navigation alone.

type unlockFileContents struct {
	CandidateWriteUnlock bool   `toml:"candidate_write_unlock"`
	PID                  string `toml:"pid"`
}

// candidateUnlockDir, candidateUnlockFilePath, and candidateUnlockFilePresent
// take settingsPath explicitly (callers pass m.settingsPath) rather than
// reading the global SettingsPath() directly, so the unlock-file location
// actually follows the same injectable settings path the rest of the app
// uses — and so it's testable without touching the real user's home
// directory.
func candidateUnlockDir(settingsPath string) string {
	return filepath.Join(filepath.Dir(settingsPath), "candidate-unlocks")
}

func candidateUnlockFilePath(settingsPath string, v protocol.VidPid) string {
	return filepath.Join(candidateUnlockDir(settingsPath), fmt.Sprintf("%04x_%04x.toml", v.VID, v.PID))
}

func candidateUnlockFilePresent(settingsPath string, v protocol.VidPid) bool {
	raw, err := os.ReadFile(candidateUnlockFilePath(settingsPath, v))
	if err != nil {
		return false
	}
	var contents unlockFileContents
	if _, err := toml.Decode(string(raw), &contents); err != nil {
		return false
	}
	if !contents.CandidateWriteUnlock {
		return false
	}
	pid := strings.ToLower(strings.TrimSpace(contents.PID))
	return pid == fmt.Sprintf("%04x", v.PID) || pid == fmt.Sprintf("0x%04x", v.PID)
}
