package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSettings_Roundtrip ports Rust's settings_schema_v2_roundtrip, adapted
// to Go's own fresh schema (schema_version=1, no dashboard-layout-mode or
// panel-focus fields — see settings.go's doc comment on why those aren't
// carried over from Rust's schema_version=2).
func TestSettings_Roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	s := Settings{SchemaVersion: settingsSchemaVersion, AdvancedMode: true, ReportSaveMode: ReportSaveAlways}
	if err := saveSettings(path, s); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}

	loaded, warning := LoadSettings(path)
	if warning != "" {
		t.Fatalf("unexpected warning loading a valid settings file: %s", warning)
	}
	if loaded.SchemaVersion != settingsSchemaVersion {
		t.Fatalf("expected schema_version %d, got %d", settingsSchemaVersion, loaded.SchemaVersion)
	}
	if !loaded.AdvancedMode {
		t.Fatal("expected advanced_mode to round-trip true")
	}
	if loaded.ReportSaveMode != ReportSaveAlways {
		t.Fatalf("expected report_save_mode to round-trip, got %q", loaded.ReportSaveMode)
	}
}

// TestSettings_InvalidFileFallsBackToDefaults ports Rust's
// invalid_ui_state_returns_error — but Go's loadSettings is deliberately
// warn-and-fallback rather than hard-erroring (see the doc comment on
// LoadSettings and the "Settings compatibility" plan decision: an old or
// corrupt config.toml must never block startup).
func TestSettings_InvalidFileFallsBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("advanced_mode = ["), 0o644); err != nil {
		t.Fatalf("write invalid settings file: %v", err)
	}

	loaded, warning := LoadSettings(path)
	if warning == "" {
		t.Fatal("expected a warning for an unparseable settings file")
	}
	if loaded != defaultSettings() {
		t.Fatalf("expected defaults on parse failure, got %+v", loaded)
	}
}

// TestSettings_MissingFileFallsBackToDefaultsSilently mirrors the "an old
// Rust config.toml is not migrated" decision: a settings path that simply
// doesn't exist yet (first run, or a foreign schema Go can't read) must not
// warn — only an existing-but-unparseable file does.
func TestSettings_MissingFileFallsBackToDefaultsSilently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.toml")
	loaded, warning := LoadSettings(path)
	if warning != "" {
		t.Fatalf("expected no warning for a missing settings file, got %q", warning)
	}
	if loaded != defaultSettings() {
		t.Fatalf("expected defaults for a missing settings file, got %+v", loaded)
	}
}
