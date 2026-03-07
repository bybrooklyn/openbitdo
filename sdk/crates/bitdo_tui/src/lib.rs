use anyhow::Result;
use bitdo_app_core::{FirmwareFinalReport, OpenBitdoCore};
use bitdo_proto::VidPid;
use serde::{Deserialize, Serialize};
use std::path::PathBuf;

pub type AppDevice = bitdo_app_core::AppDevice;

pub mod app;
pub mod headless;
pub mod persistence;
pub mod runtime;
pub mod ui;

mod support_report;

pub use app::action::QuickAction;
pub use app::state::{DashboardLayoutMode, PanelFocus, Screen};

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
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

impl Default for BuildInfo {
    fn default() -> Self {
        Self {
            app_version: "unknown".to_owned(),
            git_commit_short: "unknown".to_owned(),
            git_commit_full: "unknown".to_owned(),
            build_date_utc: "unknown".to_owned(),
            target_triple: "unknown".to_owned(),
            runtime_platform: format!("{}/{}", std::env::consts::OS, std::env::consts::ARCH),
            signing_key_fingerprint_short: "unknown".to_owned(),
            signing_key_fingerprint_full: "unknown".to_owned(),
            signing_key_next_fingerprint_short: "unknown".to_owned(),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize, Default)]
#[serde(rename_all = "snake_case")]
pub enum ReportSaveMode {
    Off,
    Always,
    #[default]
    FailureOnly,
}

impl ReportSaveMode {
    pub fn as_str(self) -> &'static str {
        match self {
            ReportSaveMode::Off => "off",
            ReportSaveMode::Always => "always",
            ReportSaveMode::FailureOnly => "failure_only",
        }
    }

    pub fn next(self, advanced_mode: bool) -> Self {
        match (self, advanced_mode) {
            (ReportSaveMode::FailureOnly, false) => ReportSaveMode::Always,
            (ReportSaveMode::Always, false) => ReportSaveMode::FailureOnly,
            (ReportSaveMode::Off, false) => ReportSaveMode::FailureOnly,
            (ReportSaveMode::FailureOnly, true) => ReportSaveMode::Always,
            (ReportSaveMode::Always, true) => ReportSaveMode::Off,
            (ReportSaveMode::Off, true) => ReportSaveMode::FailureOnly,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize, Default)]
#[serde(rename_all = "snake_case")]
pub enum HeadlessOutputMode {
    #[default]
    Human,
    Json,
}

#[derive(Clone, Debug)]
pub struct UiLaunchOptions {
    pub build_info: BuildInfo,
    pub advanced_mode: bool,
    pub report_save_mode: ReportSaveMode,
    pub settings_path: Option<PathBuf>,
    pub firmware_path: Option<PathBuf>,
    pub allow_unsafe: bool,
    pub brick_risk_ack: bool,
    pub experimental: bool,
    pub chunk_size: Option<usize>,
}

impl Default for UiLaunchOptions {
    fn default() -> Self {
        Self {
            build_info: BuildInfo::default(),
            advanced_mode: false,
            report_save_mode: ReportSaveMode::FailureOnly,
            settings_path: None,
            firmware_path: None,
            allow_unsafe: false,
            brick_risk_ack: false,
            experimental: false,
            chunk_size: None,
        }
    }
}

#[derive(Clone, Debug)]
pub struct RunLaunchOptions {
    pub vid_pid: VidPid,
    pub firmware_path: Option<PathBuf>,
    pub use_recommended: bool,
    pub allow_unsafe: bool,
    pub brick_risk_ack: bool,
    pub experimental: bool,
    pub chunk_size: Option<usize>,
    pub acknowledged_risk: bool,
    pub output_mode: HeadlessOutputMode,
    pub emit_events: bool,
    pub report_save_mode: ReportSaveMode,
}

impl Default for RunLaunchOptions {
    fn default() -> Self {
        Self {
            vid_pid: VidPid::new(0x2dc8, 0x6009),
            firmware_path: None,
            use_recommended: true,
            allow_unsafe: false,
            brick_risk_ack: false,
            experimental: false,
            chunk_size: None,
            acknowledged_risk: false,
            output_mode: HeadlessOutputMode::Human,
            emit_events: true,
            report_save_mode: ReportSaveMode::FailureOnly,
        }
    }
}

pub async fn run_ui(core: OpenBitdoCore, opts: UiLaunchOptions) -> Result<()> {
    runtime::r#loop::run_ui_loop(core, opts).await
}

pub async fn run_headless(
    core: OpenBitdoCore,
    opts: RunLaunchOptions,
) -> Result<FirmwareFinalReport> {
    headless::run_headless(core, opts).await
}

pub(crate) fn should_save_support_report(mode: ReportSaveMode, is_failure: bool) -> bool {
    match mode {
        ReportSaveMode::Off => false,
        ReportSaveMode::Always => true,
        ReportSaveMode::FailureOnly => is_failure,
    }
}

#[cfg(test)]
mod tests;
