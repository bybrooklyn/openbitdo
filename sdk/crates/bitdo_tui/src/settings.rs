use crate::ReportSaveMode;
use anyhow::{anyhow, Result};
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Clone, Debug, Serialize, Deserialize)]
struct PersistedSettings {
    #[serde(default = "default_settings_schema_version")]
    schema_version: u32,
    #[serde(default)]
    advanced_mode: bool,
    #[serde(default)]
    report_save_mode: ReportSaveMode,
}

const fn default_settings_schema_version() -> u32 {
    1
}

/// Persist beginner/advanced preferences in a small TOML config file.
pub(crate) fn persist_user_settings(
    path: &Path,
    advanced_mode: bool,
    report_save_mode: ReportSaveMode,
) -> Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }

    let body = toml::to_string_pretty(&PersistedSettings {
        schema_version: default_settings_schema_version(),
        advanced_mode,
        report_save_mode,
    })
    .map_err(|err| anyhow!("failed to serialize user settings: {err}"))?;
    std::fs::write(path, body)?;
    Ok(())
}
