use crate::{DashboardLayoutMode, PanelFocus, ReportSaveMode};
use anyhow::{anyhow, Result};
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct PersistedUiState {
    #[serde(default = "default_settings_schema_version")]
    pub schema_version: u32,
    #[serde(default)]
    pub advanced_mode: bool,
    #[serde(default)]
    pub report_save_mode: ReportSaveMode,
    #[serde(default)]
    pub device_filter_text: String,
    #[serde(default)]
    pub dashboard_layout_mode: DashboardLayoutMode,
    #[serde(default)]
    pub last_panel_focus: PanelFocus,
}

impl Default for PersistedUiState {
    fn default() -> Self {
        Self {
            schema_version: default_settings_schema_version(),
            advanced_mode: false,
            report_save_mode: ReportSaveMode::FailureOnly,
            device_filter_text: String::new(),
            dashboard_layout_mode: DashboardLayoutMode::Wide,
            last_panel_focus: PanelFocus::Devices,
        }
    }
}

const fn default_settings_schema_version() -> u32 {
    2
}

pub fn load_ui_state(path: &Path) -> Result<PersistedUiState> {
    let raw = match std::fs::read_to_string(path) {
        Ok(raw) => raw,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {
            return Ok(PersistedUiState::default())
        }
        Err(err) => return Err(err.into()),
    };

    let mut loaded: PersistedUiState = toml::from_str(&raw)
        .map_err(|err| anyhow!("failed to parse ui state {}: {err}", path.display()))?;
    loaded.schema_version = default_settings_schema_version();

    if !loaded.advanced_mode && loaded.report_save_mode == ReportSaveMode::Off {
        loaded.report_save_mode = ReportSaveMode::FailureOnly;
    }

    Ok(loaded)
}

pub fn persist_ui_state(
    path: &Path,
    advanced_mode: bool,
    report_save_mode: ReportSaveMode,
    device_filter_text: String,
    dashboard_layout_mode: DashboardLayoutMode,
    last_panel_focus: PanelFocus,
) -> Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }

    let body = toml::to_string_pretty(&PersistedUiState {
        schema_version: default_settings_schema_version(),
        advanced_mode,
        report_save_mode,
        device_filter_text,
        dashboard_layout_mode,
        last_panel_focus,
    })
    .map_err(|err| anyhow!("failed to serialize ui state: {err}"))?;
    std::fs::write(path, body)?;
    Ok(())
}
