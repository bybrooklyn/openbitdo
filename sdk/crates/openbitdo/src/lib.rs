use bitdo_app_core::{signing_key_fingerprint_active_sha256, signing_key_fingerprint_next_sha256};
use bitdo_tui::ReportSaveMode;
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BuildInfo {
    pub app_version: String,
    pub git_commit_short: String,
    pub git_commit_full: String,
    pub build_date_utc: String,
    pub target_triple: String,
    pub runtime_platform: String,
    pub signing_key_fingerprint_short: String,
    pub signing_key_fingerprint_full: String,
    pub signing_key_next_fingerprint_short: String,
}

impl BuildInfo {
    pub fn current() -> Self {
        Self::from_raw(
            option_env!("OPENBITDO_APP_VERSION"),
            option_env!("OPENBITDO_GIT_COMMIT_SHORT"),
            option_env!("OPENBITDO_GIT_COMMIT_FULL"),
            option_env!("OPENBITDO_BUILD_DATE_UTC"),
            option_env!("OPENBITDO_TARGET_TRIPLE"),
        )
    }

    pub fn to_tui_info(&self) -> bitdo_tui::BuildInfo {
        bitdo_tui::BuildInfo {
            app_version: self.app_version.clone(),
            git_commit_short: self.git_commit_short.clone(),
            git_commit_full: self.git_commit_full.clone(),
            build_date_utc: self.build_date_utc.clone(),
            target_triple: self.target_triple.clone(),
            runtime_platform: self.runtime_platform.clone(),
            signing_key_fingerprint_short: self.signing_key_fingerprint_short.clone(),
            signing_key_fingerprint_full: self.signing_key_fingerprint_full.clone(),
            signing_key_next_fingerprint_short: self.signing_key_next_fingerprint_short.clone(),
        }
    }

    fn from_raw(
        app_version: Option<&'static str>,
        git_commit_short: Option<&'static str>,
        git_commit_full: Option<&'static str>,
        build_date_utc: Option<&'static str>,
        target_triple: Option<&'static str>,
    ) -> Self {
        Self {
            app_version: normalize(app_version),
            git_commit_short: normalize(git_commit_short),
            git_commit_full: normalize(git_commit_full),
            build_date_utc: normalize(build_date_utc),
            target_triple: normalize(target_triple),
            runtime_platform: format!("{}/{}", std::env::consts::OS, std::env::consts::ARCH),
            signing_key_fingerprint_short: short_fingerprint(
                &signing_key_fingerprint_active_sha256(),
            ),
            signing_key_fingerprint_full: signing_key_fingerprint_active_sha256(),
            signing_key_next_fingerprint_short: short_fingerprint(
                &signing_key_fingerprint_next_sha256(),
            ),
        }
    }
}

fn normalize(value: Option<&str>) -> String {
    value
        .map(str::trim)
        .filter(|v| !v.is_empty())
        .unwrap_or("unknown")
        .to_owned()
}

fn short_fingerprint(full: &str) -> String {
    if full == "unknown" {
        return "unknown".to_owned();
    }
    full.chars().take(16).collect()
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct UserSettings {
    #[serde(default = "default_settings_schema_version")]
    pub schema_version: u32,
    #[serde(default)]
    pub advanced_mode: bool,
    #[serde(default)]
    pub report_save_mode: ReportSaveMode,
}

impl Default for UserSettings {
    fn default() -> Self {
        Self {
            schema_version: default_settings_schema_version(),
            advanced_mode: false,
            report_save_mode: ReportSaveMode::FailureOnly,
        }
    }
}

const fn default_settings_schema_version() -> u32 {
    1
}

pub fn user_settings_path() -> PathBuf {
    if cfg!(target_os = "macos") {
        return home_directory()
            .join("Library")
            .join("Application Support")
            .join("OpenBitdo")
            .join("config.toml");
    }

    if cfg!(target_os = "linux") {
        if let Some(xdg_config_home) = std::env::var_os("XDG_CONFIG_HOME") {
            return PathBuf::from(xdg_config_home)
                .join("openbitdo")
                .join("config.toml");
        }

        return home_directory()
            .join(".config")
            .join("openbitdo")
            .join("config.toml");
    }

    std::env::temp_dir().join("openbitdo").join("config.toml")
}

pub fn load_user_settings(path: &Path) -> UserSettings {
    let Ok(raw) = std::fs::read_to_string(path) else {
        return UserSettings::default();
    };
    let mut settings: UserSettings = toml::from_str(&raw).unwrap_or_default();
    if !settings.advanced_mode && settings.report_save_mode == ReportSaveMode::Off {
        settings.report_save_mode = ReportSaveMode::FailureOnly;
    }
    settings
}

pub fn save_user_settings(path: &Path, settings: &UserSettings) -> anyhow::Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let body = toml::to_string_pretty(settings)?;
    std::fs::write(path, body)?;
    Ok(())
}

fn home_directory() -> PathBuf {
    std::env::var_os("HOME")
        .map(PathBuf::from)
        .unwrap_or_else(std::env::temp_dir)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    #[test]
    fn build_info_falls_back_to_unknown_when_missing() {
        let info = BuildInfo::from_raw(None, None, None, None, None);
        assert_eq!(info.app_version, "unknown");
        assert_eq!(info.git_commit_short, "unknown");
        assert_eq!(info.git_commit_full, "unknown");
        assert_eq!(info.build_date_utc, "unknown");
        assert_eq!(info.target_triple, "unknown");
    }

    #[test]
    fn runtime_platform_has_expected_separator() {
        let info = BuildInfo::from_raw(None, None, None, None, None);
        assert!(info.runtime_platform.contains('/'));
    }

    #[test]
    fn normalize_trims_and_preserves_values() {
        let info = BuildInfo::from_raw(
            Some(" 0.1.0 "),
            Some(" abc123 "),
            Some(" abc123def456 "),
            Some(" 2026-01-01T00:00:00Z "),
            Some(" x86_64-unknown-linux-gnu "),
        );
        assert_eq!(info.app_version, "0.1.0");
        assert_eq!(info.git_commit_short, "abc123");
        assert_eq!(info.git_commit_full, "abc123def456");
        assert_eq!(info.build_date_utc, "2026-01-01T00:00:00Z");
        assert_eq!(info.target_triple, "x86_64-unknown-linux-gnu");
    }

    #[test]
    fn settings_roundtrip_toml() {
        let tmp =
            std::env::temp_dir().join(format!("openbitdo-settings-{}.toml", std::process::id()));
        let settings = UserSettings {
            schema_version: 1,
            advanced_mode: true,
            report_save_mode: ReportSaveMode::Always,
        };
        save_user_settings(&tmp, &settings).expect("save settings");
        let loaded = load_user_settings(&tmp);
        assert_eq!(loaded, settings);
        let _ = std::fs::remove_file(tmp);
    }

    #[test]
    fn missing_settings_uses_defaults() {
        let path = PathBuf::from("/tmp/openbitdo-nonexistent-settings.toml");
        let loaded = load_user_settings(&path);
        assert!(!loaded.advanced_mode);
        assert_eq!(loaded.report_save_mode, ReportSaveMode::FailureOnly);
    }
}
