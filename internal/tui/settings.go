package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// ReportSaveMode controls when a TOML support report is written to disk.
type ReportSaveMode string

const (
	ReportSaveOff         ReportSaveMode = "off"
	ReportSaveAlways      ReportSaveMode = "always"
	ReportSaveFailureOnly ReportSaveMode = "failure_only"
)

// settingsSchemaVersion is Go's own settings schema, deliberately not
// continuous with the prior Rust TUI's schema_version=2: the redesigned UI
// has no dashboard-layout-mode or panel-focus concepts to persist. An old
// Rust config.toml is not migrated — see loadSettings.
const settingsSchemaVersion = 1

// Settings is Go's own settings schema for the redesigned UI.
type Settings struct {
	SchemaVersion  int            `toml:"schema_version"`
	AdvancedMode   bool           `toml:"advanced_mode"`
	ReportSaveMode ReportSaveMode `toml:"report_save_mode"`
}

func defaultSettings() Settings {
	return Settings{SchemaVersion: settingsSchemaVersion, AdvancedMode: false, ReportSaveMode: ReportSaveFailureOnly}
}

// SettingsPath mirrors the prior Rust TUI's OS convention so the config
// directory stays where users already expect it, even though the schema
// inside is new. Exported for cmd/openbitdo, which must load settings
// before constructing the Model (advanced mode feeds core.Config).
func SettingsPath() string {
	home, herr := os.UserHomeDir()
	if herr != nil {
		home = os.TempDir()
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "OpenBitdo", "config.toml")
	case "linux":
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "openbitdo", "config.toml")
		}
		return filepath.Join(home, ".config", "openbitdo", "config.toml")
	default:
		return filepath.Join(os.TempDir(), "openbitdo", "config.toml")
	}
}

// loadSettings reads settings from path, falling back to defaults (with a
// warning) if the file is missing or fails to parse against Go's schema —
// including an old Rust-schema config.toml, which is not migrated.
func LoadSettings(path string) (Settings, string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return defaultSettings(), ""
	}
	var s Settings
	if _, err := toml.Decode(string(raw), &s); err != nil {
		return defaultSettings(), fmt.Sprintf("warning: failed to parse settings %s: %v; using defaults", path, err)
	}
	s.SchemaVersion = settingsSchemaVersion
	if s.ReportSaveMode == "" {
		s.ReportSaveMode = ReportSaveFailureOnly
	}
	return s, ""
}

func saveSettings(path string, s Settings) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return toml.NewEncoder(f).Encode(s)
}
