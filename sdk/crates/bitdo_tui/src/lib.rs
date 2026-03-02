use anyhow::{anyhow, Result};
use bitdo_app_core::{
    AppDevice, ConfigBackupId, DedicatedButtonMapping, DeviceKind, FirmwareCancelRequest,
    FirmwareConfirmRequest, FirmwareFinalReport, FirmwareOutcome, FirmwarePreflightRequest,
    FirmwareProgressEvent, FirmwareStartRequest, FirmwareUpdatePlan, FirmwareUpdateSessionId,
    OpenBitdoCore, U2CoreProfile, U2SlotId, UserSupportStatus, WriteRecoveryReport,
};
use bitdo_proto::{SupportTier, VidPid};
use crossterm::event::{self, Event as CEvent, KeyCode, MouseButton, MouseEvent, MouseEventKind};
use crossterm::execute;
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, Borders, Gauge, List, ListItem, Paragraph};
use ratatui::{backend::CrosstermBackend, Frame, Terminal};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::io::{self, Stdout, Write};
use std::path::{Path, PathBuf};
use tokio::sync::broadcast;
use tokio::time::{sleep, Duration};

mod desktop_io;
mod settings;
mod support_report;

use desktop_io::{copy_text_to_clipboard, open_path_with_default_app};
use settings::persist_user_settings;
use support_report::{persist_support_report, prune_reports_on_startup};

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum TuiWorkflowState {
    WaitForDevice,
    Home,
    HelpOverlay,
    Jp108Mapping,
    U2CoreProfile,
    Recovery,
    Preflight,
    Updating,
    FinalReport,
    About,
}

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

    fn next(self, advanced_mode: bool) -> Self {
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

#[derive(Clone, Debug)]
struct PendingUpdate {
    target: AppDevice,
    firmware_path: PathBuf,
    firmware_source: String,
    firmware_version: String,
    plan: FirmwareUpdatePlan,
}

#[derive(Clone, Copy, Debug)]
struct MouseContextMenu {
    anchor_col: u16,
    anchor_row: u16,
    hovered_index: Option<usize>,
}

#[derive(Clone, Debug)]
pub struct TuiApp {
    pub state: TuiWorkflowState,
    pub devices: Vec<AppDevice>,
    pub selected_index: usize,
    pub selected: Option<VidPid>,
    pub session_id: Option<FirmwareUpdateSessionId>,
    pub progress: u8,
    pub last_message: String,
    pub final_report: Option<FirmwareFinalReport>,
    pub build_info: BuildInfo,
    pub advanced_mode: bool,
    pub report_save_mode: ReportSaveMode,
    pub settings_path: Option<PathBuf>,
    detail_scroll: u16,
    hovered_action: Option<HomeAction>,
    about_toggle_hovered: bool,
    about_report_mode_hovered: bool,
    about_fingerprint_hovered: bool,
    about_show_full_fingerprint: bool,
    context_menu: Option<MouseContextMenu>,
    pending_update: Option<PendingUpdate>,
    jp108_mappings: Vec<DedicatedButtonMapping>,
    jp108_selected: usize,
    u2_profile: Option<U2CoreProfile>,
    u2_selected: usize,
    latest_backup: Option<ConfigBackupId>,
    latest_report_path: Option<PathBuf>,
    write_lock_until_restart: bool,
    recovery_report: Option<WriteRecoveryReport>,
}

impl Default for TuiApp {
    fn default() -> Self {
        Self {
            state: TuiWorkflowState::WaitForDevice,
            devices: Vec::new(),
            selected_index: 0,
            selected: None,
            session_id: None,
            progress: 0,
            last_message: "Plug in your controller, then choose Refresh.".to_owned(),
            final_report: None,
            build_info: BuildInfo::default(),
            advanced_mode: false,
            report_save_mode: ReportSaveMode::FailureOnly,
            settings_path: None,
            detail_scroll: 0,
            hovered_action: None,
            about_toggle_hovered: false,
            about_report_mode_hovered: false,
            about_fingerprint_hovered: false,
            about_show_full_fingerprint: false,
            context_menu: None,
            pending_update: None,
            jp108_mappings: Vec::new(),
            jp108_selected: 0,
            u2_profile: None,
            u2_selected: 0,
            latest_backup: None,
            latest_report_path: None,
            write_lock_until_restart: false,
            recovery_report: None,
        }
    }
}

impl TuiApp {
    pub fn refresh_devices(&mut self, mut devices: Vec<AppDevice>) {
        devices.sort_by_key(|d| (d.vid_pid.vid, d.vid_pid.pid));
        self.devices = devices;

        if self.devices.is_empty() {
            self.selected_index = 0;
            self.selected = None;
            if !self.is_overlay_state() {
                self.state = TuiWorkflowState::WaitForDevice;
            }
            return;
        }

        if self.devices.len() == 1 || self.selected_index >= self.devices.len() {
            self.selected_index = 0;
        }

        self.selected = Some(self.devices[self.selected_index].vid_pid);

        if !matches!(
            self.state,
            TuiWorkflowState::About
                | TuiWorkflowState::HelpOverlay
                | TuiWorkflowState::Recovery
                | TuiWorkflowState::Preflight
                | TuiWorkflowState::Updating
                | TuiWorkflowState::FinalReport
        ) {
            self.state = TuiWorkflowState::Home;
        }
    }

    pub fn selected_device(&self) -> Option<&AppDevice> {
        self.devices.get(self.selected_index)
    }

    pub fn select_next(&mut self) {
        if self.devices.is_empty() {
            return;
        }
        self.selected_index = (self.selected_index + 1) % self.devices.len();
        self.selected = Some(self.devices[self.selected_index].vid_pid);
        self.context_menu = None;
    }

    pub fn select_prev(&mut self) {
        if self.devices.is_empty() {
            return;
        }
        if self.selected_index == 0 {
            self.selected_index = self.devices.len() - 1;
        } else {
            self.selected_index -= 1;
        }
        self.selected = Some(self.devices[self.selected_index].vid_pid);
        self.context_menu = None;
    }

    pub fn select_index(&mut self, idx: usize) {
        if idx < self.devices.len() {
            self.selected_index = idx;
            self.selected = Some(self.devices[idx].vid_pid);
            self.context_menu = None;
        }
    }

    pub fn set_home_message(&mut self, message: impl Into<String>) {
        self.progress = 0;
        self.session_id = None;
        self.pending_update = None;
        self.context_menu = None;
        self.detail_scroll = 0;
        self.last_message = message.into();
        self.state = if self.devices.is_empty() {
            TuiWorkflowState::WaitForDevice
        } else {
            TuiWorkflowState::Home
        };
    }

    pub fn open_about(&mut self) {
        self.state = TuiWorkflowState::About;
        self.context_menu = None;
        self.about_toggle_hovered = false;
        self.about_report_mode_hovered = false;
        self.about_fingerprint_hovered = false;
        self.last_message = "OpenBitdo build details and settings.".to_owned();
    }

    pub fn open_help(&mut self) {
        self.state = TuiWorkflowState::HelpOverlay;
        self.context_menu = None;
    }

    pub fn close_overlay(&mut self) {
        self.about_toggle_hovered = false;
        self.about_report_mode_hovered = false;
        self.about_fingerprint_hovered = false;
        self.about_show_full_fingerprint = false;
        if self.devices.is_empty() {
            self.state = TuiWorkflowState::WaitForDevice;
        } else {
            self.state = TuiWorkflowState::Home;
        }
    }

    fn begin_preflight(&mut self, pending: PendingUpdate) {
        self.pending_update = Some(pending);
        self.state = TuiWorkflowState::Preflight;
        self.progress = 0;
        self.session_id = None;
        self.final_report = None;
        self.context_menu = None;
        self.last_message = "Review preflight details and confirm.".to_owned();
    }

    fn begin_jp108_mapping(&mut self, mappings: Vec<DedicatedButtonMapping>) {
        self.jp108_mappings = mappings;
        self.jp108_selected = 0;
        self.state = TuiWorkflowState::Jp108Mapping;
        self.pending_update = None;
        self.context_menu = None;
        self.last_message =
            "Edit dedicated buttons, then click Backup + Apply. Firmware remains available."
                .to_owned();
    }

    fn begin_u2_profile(&mut self, profile: U2CoreProfile) {
        self.u2_profile = Some(profile);
        self.u2_selected = 0;
        self.state = TuiWorkflowState::U2CoreProfile;
        self.pending_update = None;
        self.context_menu = None;
        self.last_message =
            "Choose slot/mode and core button mappings, then click Backup + Apply.".to_owned();
    }

    pub fn set_session(&mut self, id: FirmwareUpdateSessionId) {
        self.session_id = Some(id);
        self.state = TuiWorkflowState::Updating;
        self.context_menu = None;
        self.pending_update = None;
    }

    pub fn apply_progress(&mut self, progress: u8, message: String) {
        self.progress = progress;
        self.last_message = message;
    }

    pub fn complete(&mut self, report: FirmwareFinalReport) {
        self.progress = 100;
        self.state = TuiWorkflowState::FinalReport;
        self.last_message = format!("final status: {:?}", report.status);
        self.final_report = Some(report);
        self.session_id = None;
        self.pending_update = None;
        self.context_menu = None;
    }

    pub fn open_context_menu(&mut self, col: u16, row: u16) {
        self.context_menu = Some(MouseContextMenu {
            anchor_col: col,
            anchor_row: row,
            hovered_index: None,
        });
    }

    pub fn close_context_menu(&mut self) {
        self.context_menu = None;
    }

    fn set_advanced_mode(&mut self, core: &OpenBitdoCore, enabled: bool) -> Result<()> {
        self.advanced_mode = enabled;
        core.set_advanced_mode(enabled);
        if !enabled && self.report_save_mode == ReportSaveMode::Off {
            self.report_save_mode = ReportSaveMode::FailureOnly;
        }
        if let Some(path) = self.settings_path.as_deref() {
            persist_user_settings(path, self.advanced_mode, self.report_save_mode)?;
        }
        self.last_message = if enabled {
            "Advanced mode enabled: inferred read diagnostics are available.".to_owned()
        } else {
            "Advanced mode disabled: beginner-safe defaults restored.".to_owned()
        };
        Ok(())
    }

    fn toggle_advanced_mode(&mut self, core: &OpenBitdoCore) -> Result<()> {
        self.set_advanced_mode(core, !self.advanced_mode)
    }

    fn cycle_report_save_mode(&mut self) -> Result<()> {
        self.report_save_mode = self.report_save_mode.next(self.advanced_mode);
        if let Some(path) = self.settings_path.as_deref() {
            persist_user_settings(path, self.advanced_mode, self.report_save_mode)?;
        }
        self.last_message = if self.report_save_mode == ReportSaveMode::Off {
            "Report save mode set to off. Disabling reports may make support impossible.".to_owned()
        } else {
            format!(
                "Report save mode set to {}.",
                self.report_save_mode.as_str()
            )
        };
        Ok(())
    }

    fn toggle_fingerprint_view(&mut self) {
        self.about_show_full_fingerprint = !self.about_show_full_fingerprint;
        self.last_message = if self.about_show_full_fingerprint {
            "Showing full signing-key fingerprint.".to_owned()
        } else {
            "Showing short signing-key fingerprint.".to_owned()
        };
    }

    fn enter_recovery(&mut self, report: WriteRecoveryReport) {
        self.write_lock_until_restart = true;
        self.recovery_report = Some(report);
        self.state = TuiWorkflowState::Recovery;
        self.pending_update = None;
        self.session_id = None;
        self.progress = 0;
        self.last_message =
            "Apply failed and rollback also failed. Write actions are locked until restart."
                .to_owned();
    }

    fn remember_report_path(&mut self, path: PathBuf) {
        self.latest_report_path = Some(path);
    }

    fn is_overlay_state(&self) -> bool {
        matches!(
            self.state,
            TuiWorkflowState::About | TuiWorkflowState::HelpOverlay
        )
    }
}

#[derive(Clone, Debug)]
pub struct TuiRunRequest {
    pub vid_pid: VidPid,
    pub firmware_path: PathBuf,
    pub allow_unsafe: bool,
    pub brick_risk_ack: bool,
    pub experimental: bool,
    pub chunk_size: Option<usize>,
    pub acknowledged_risk: bool,
    pub no_ui: bool,
}

#[derive(Clone, Debug)]
pub struct TuiLaunchOptions {
    pub no_ui: bool,
    pub selected_vid_pid: Option<VidPid>,
    pub firmware_path: Option<PathBuf>,
    pub allow_unsafe: bool,
    pub brick_risk_ack: bool,
    pub experimental: bool,
    pub chunk_size: Option<usize>,
    pub build_info: BuildInfo,
    pub advanced_mode: bool,
    pub report_save_mode: ReportSaveMode,
    pub settings_path: Option<PathBuf>,
}

impl Default for TuiLaunchOptions {
    fn default() -> Self {
        Self {
            no_ui: false,
            selected_vid_pid: None,
            firmware_path: None,
            allow_unsafe: true,
            brick_risk_ack: true,
            experimental: false,
            chunk_size: None,
            build_info: BuildInfo::default(),
            advanced_mode: false,
            report_save_mode: ReportSaveMode::FailureOnly,
            settings_path: None,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum HomeAction {
    Update,
    Diagnose,
    Refresh,
    About,
    Help,
    Quit,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum DeviceFlowAction {
    BackupApply,
    RetryRead,
    RestoreBackup,
    GuidedTest,
    Firmware,
    Done,
}

const HOME_ACTIONS: [HomeAction; 5] = [
    HomeAction::Update,
    HomeAction::Diagnose,
    HomeAction::Refresh,
    HomeAction::About,
    HomeAction::Quit,
];

const WAIT_ACTIONS: [HomeAction; 3] = [HomeAction::Refresh, HomeAction::Help, HomeAction::Quit];

const CONTEXT_ACTIONS: [HomeAction; 3] =
    [HomeAction::Diagnose, HomeAction::About, HomeAction::Refresh];

const DEVICE_FLOW_ACTIONS: [DeviceFlowAction; 6] = [
    DeviceFlowAction::BackupApply,
    DeviceFlowAction::RetryRead,
    DeviceFlowAction::RestoreBackup,
    DeviceFlowAction::GuidedTest,
    DeviceFlowAction::Firmware,
    DeviceFlowAction::Done,
];

impl HomeAction {
    fn label(self) -> &'static str {
        match self {
            HomeAction::Update => "Recommended Update",
            HomeAction::Diagnose => "Diagnose",
            HomeAction::Refresh => "Refresh",
            HomeAction::About => "About",
            HomeAction::Help => "Help",
            HomeAction::Quit => "Quit",
        }
    }
}

impl DeviceFlowAction {
    fn label(self) -> &'static str {
        match self {
            DeviceFlowAction::BackupApply => "Backup + Apply",
            DeviceFlowAction::RetryRead => "Retry Read",
            DeviceFlowAction::RestoreBackup => "Restore",
            DeviceFlowAction::GuidedTest => "Button Test",
            DeviceFlowAction::Firmware => "Firmware",
            DeviceFlowAction::Done => "Done",
        }
    }
}

pub async fn run_tui_app(core: OpenBitdoCore, opts: TuiLaunchOptions) -> Result<()> {
    let _ = prune_reports_on_startup().await;
    let initial_report_mode = if !opts.advanced_mode && opts.report_save_mode == ReportSaveMode::Off
    {
        ReportSaveMode::FailureOnly
    } else {
        opts.report_save_mode
    };

    let mut app = TuiApp {
        build_info: opts.build_info.clone(),
        advanced_mode: opts.advanced_mode,
        report_save_mode: initial_report_mode,
        settings_path: opts.settings_path.clone(),
        ..Default::default()
    };
    core.set_advanced_mode(opts.advanced_mode);

    let devices = core.list_devices().await?;
    app.refresh_devices(devices);
    match app.devices.len() {
        0 => {
            app.last_message =
                "No controller detected. Plug one in, then choose Refresh.".to_owned()
        }
        1 => {
            app.last_message =
                "Controller detected and auto-selected. Choose Recommended Update or Diagnose."
                    .to_owned()
        }
        _ => {
            app.last_message =
                "Select a controller, then choose Recommended Update or Diagnose.".to_owned()
        }
    }

    if opts.no_ui {
        let selected = opts
            .selected_vid_pid
            .or_else(|| {
                app.devices
                    .iter()
                    .find(|d| d.support_tier == SupportTier::Full)
                    .map(|d| d.vid_pid)
            })
            .or_else(|| app.devices.first().map(|d| d.vid_pid))
            .ok_or_else(|| anyhow!("no devices detected"))?;

        let firmware_path = match opts.firmware_path.clone() {
            Some(path) => path,
            None => core
                .download_recommended_firmware(selected)
                .await
                .map(|d| d.firmware_path)?,
        };

        run_tui_flow(
            core,
            TuiRunRequest {
                vid_pid: selected,
                firmware_path,
                allow_unsafe: opts.allow_unsafe,
                brick_risk_ack: opts.brick_risk_ack,
                experimental: opts.experimental,
                chunk_size: opts.chunk_size,
                acknowledged_risk: true,
                no_ui: true,
            },
        )
        .await?;

        return Ok(());
    }

    let mut terminal = Some(init_terminal()?);
    let mut firmware_events: Option<broadcast::Receiver<FirmwareProgressEvent>> = None;

    if app.devices.len() == 1 {
        let action = if app
            .selected_device()
            .map(|d| d.support_tier == SupportTier::Full)
            .unwrap_or(false)
        {
            HomeAction::Update
        } else {
            HomeAction::Diagnose
        };
        let _ = execute_home_action(
            &core,
            &mut terminal,
            &mut app,
            &opts,
            &mut firmware_events,
            action,
        )
        .await?;
    }

    loop {
        poll_firmware_progress(&core, &mut app, &mut firmware_events).await?;
        render_if_needed(&mut terminal, &app)?;

        if !event::poll(Duration::from_millis(120))? {
            continue;
        }

        match event::read()? {
            CEvent::Key(key) => {
                if handle_key_event(
                    &core,
                    &mut terminal,
                    &mut app,
                    &opts,
                    &mut firmware_events,
                    key.code,
                )
                .await?
                {
                    teardown_terminal(&mut terminal)?;
                    return Ok(());
                }
            }
            CEvent::Mouse(mouse) => {
                if handle_mouse_event(
                    &core,
                    &mut terminal,
                    &mut app,
                    &opts,
                    &mut firmware_events,
                    mouse,
                )
                .await?
                {
                    teardown_terminal(&mut terminal)?;
                    return Ok(());
                }
            }
            _ => {}
        }
    }
}

async fn handle_key_event(
    core: &OpenBitdoCore,
    terminal: &mut Option<Terminal<CrosstermBackend<Stdout>>>,
    app: &mut TuiApp,
    opts: &TuiLaunchOptions,
    firmware_events: &mut Option<broadcast::Receiver<FirmwareProgressEvent>>,
    key: KeyCode,
) -> Result<bool> {
    if app.advanced_mode && handle_report_hotkey(app, key)? {
        return Ok(false);
    }

    match app.state {
        TuiWorkflowState::About => match key {
            KeyCode::Esc | KeyCode::Enter => app.close_overlay(),
            KeyCode::Char('t') => {
                app.toggle_advanced_mode(core)?;
            }
            KeyCode::Char('r') => {
                app.cycle_report_save_mode()?;
            }
            KeyCode::Char('v') => app.toggle_fingerprint_view(),
            KeyCode::Char('q') => return Ok(true),
            _ => {}
        },
        TuiWorkflowState::HelpOverlay => match key {
            KeyCode::Esc | KeyCode::Enter => app.close_overlay(),
            KeyCode::Char('q') => return Ok(true),
            _ => {}
        },
        TuiWorkflowState::WaitForDevice => {
            let action = match key {
                KeyCode::Enter | KeyCode::Char('r') => Some(HomeAction::Refresh),
                KeyCode::Char('?') => Some(HomeAction::Help),
                KeyCode::Char('q') | KeyCode::Esc => Some(HomeAction::Quit),
                _ => None,
            };

            if let Some(action) = action {
                return execute_home_action(core, terminal, app, opts, firmware_events, action)
                    .await;
            }
        }
        TuiWorkflowState::Home => {
            let action = match key {
                KeyCode::Char('q') | KeyCode::Esc => Some(HomeAction::Quit),
                KeyCode::Down | KeyCode::Char('j') => {
                    app.select_next();
                    None
                }
                KeyCode::Up | KeyCode::Char('k') => {
                    app.select_prev();
                    None
                }
                KeyCode::Char('d') => Some(HomeAction::Diagnose),
                KeyCode::Char('r') => Some(HomeAction::Refresh),
                KeyCode::Char('a') => Some(HomeAction::About),
                KeyCode::Char('?') => Some(HomeAction::Help),
                KeyCode::Enter | KeyCode::Char('u') => Some(HomeAction::Update),
                _ => None,
            };

            if let Some(action) = action {
                return execute_home_action(core, terminal, app, opts, firmware_events, action)
                    .await;
            }
        }
        TuiWorkflowState::Jp108Mapping => {
            let action = match key {
                KeyCode::Down | KeyCode::Char('j') => {
                    if !app.jp108_mappings.is_empty() {
                        app.jp108_selected = (app.jp108_selected + 1) % app.jp108_mappings.len();
                    }
                    None
                }
                KeyCode::Up | KeyCode::Char('k') => {
                    if !app.jp108_mappings.is_empty() {
                        if app.jp108_selected == 0 {
                            app.jp108_selected = app.jp108_mappings.len().saturating_sub(1);
                        } else {
                            app.jp108_selected -= 1;
                        }
                    }
                    None
                }
                KeyCode::Left => {
                    jp108_adjust_selected_usage(app, -1);
                    None
                }
                KeyCode::Right => {
                    jp108_adjust_selected_usage(app, 1);
                    None
                }
                KeyCode::Enter => Some(DeviceFlowAction::BackupApply),
                KeyCode::Char('b') => Some(DeviceFlowAction::BackupApply),
                KeyCode::Char('r') => Some(DeviceFlowAction::RetryRead),
                KeyCode::Char('s') => Some(DeviceFlowAction::RestoreBackup),
                KeyCode::Char('t') => Some(DeviceFlowAction::GuidedTest),
                KeyCode::Char('f') => Some(DeviceFlowAction::Firmware),
                KeyCode::Esc | KeyCode::Char('q') => Some(DeviceFlowAction::Done),
                _ => None,
            };

            if let Some(action) = action {
                return execute_device_flow_action(
                    core,
                    terminal,
                    app,
                    opts,
                    firmware_events,
                    action,
                )
                .await;
            }
        }
        TuiWorkflowState::U2CoreProfile => {
            let action = match key {
                KeyCode::Down | KeyCode::Char('j') => {
                    if let Some(profile) = app.u2_profile.as_ref() {
                        if !profile.mappings.is_empty() {
                            app.u2_selected = (app.u2_selected + 1) % profile.mappings.len();
                        }
                    }
                    None
                }
                KeyCode::Up | KeyCode::Char('k') => {
                    if let Some(profile) = app.u2_profile.as_ref() {
                        if !profile.mappings.is_empty() {
                            if app.u2_selected == 0 {
                                app.u2_selected = profile.mappings.len().saturating_sub(1);
                            } else {
                                app.u2_selected -= 1;
                            }
                        }
                    }
                    None
                }
                KeyCode::Left => {
                    u2_adjust_selected_usage(app, -1);
                    None
                }
                KeyCode::Right => {
                    u2_adjust_selected_usage(app, 1);
                    None
                }
                KeyCode::Char('1') => {
                    if let Some(profile) = app.u2_profile.as_mut() {
                        profile.slot = U2SlotId::Slot1;
                    }
                    None
                }
                KeyCode::Char('2') => {
                    if let Some(profile) = app.u2_profile.as_mut() {
                        profile.slot = U2SlotId::Slot2;
                    }
                    None
                }
                KeyCode::Char('3') => {
                    if let Some(profile) = app.u2_profile.as_mut() {
                        profile.slot = U2SlotId::Slot3;
                    }
                    None
                }
                KeyCode::Char('m') => {
                    if let Some(profile) = app.u2_profile.as_mut() {
                        profile.mode = (profile.mode + 1) % 4;
                    }
                    None
                }
                KeyCode::Char('[') => {
                    if let Some(profile) = app.u2_profile.as_mut() {
                        if profile.supports_trigger_write {
                            profile.l2_analog = (profile.l2_analog - 0.05).clamp(0.0, 1.0);
                        }
                    }
                    None
                }
                KeyCode::Char(']') => {
                    if let Some(profile) = app.u2_profile.as_mut() {
                        if profile.supports_trigger_write {
                            profile.l2_analog = (profile.l2_analog + 0.05).clamp(0.0, 1.0);
                        }
                    }
                    None
                }
                KeyCode::Char(';') => {
                    if let Some(profile) = app.u2_profile.as_mut() {
                        if profile.supports_trigger_write {
                            profile.r2_analog = (profile.r2_analog - 0.05).clamp(0.0, 1.0);
                        }
                    }
                    None
                }
                KeyCode::Char('\'') => {
                    if let Some(profile) = app.u2_profile.as_mut() {
                        if profile.supports_trigger_write {
                            profile.r2_analog = (profile.r2_analog + 0.05).clamp(0.0, 1.0);
                        }
                    }
                    None
                }
                KeyCode::Enter => Some(DeviceFlowAction::BackupApply),
                KeyCode::Char('b') => Some(DeviceFlowAction::BackupApply),
                KeyCode::Char('r') => Some(DeviceFlowAction::RetryRead),
                KeyCode::Char('s') => Some(DeviceFlowAction::RestoreBackup),
                KeyCode::Char('t') => Some(DeviceFlowAction::GuidedTest),
                KeyCode::Char('f') => Some(DeviceFlowAction::Firmware),
                KeyCode::Esc | KeyCode::Char('q') => Some(DeviceFlowAction::Done),
                _ => None,
            };

            if let Some(action) = action {
                return execute_device_flow_action(
                    core,
                    terminal,
                    app,
                    opts,
                    firmware_events,
                    action,
                )
                .await;
            }
        }
        TuiWorkflowState::Recovery => {
            match key {
                KeyCode::Char('r') => {
                    if let Some(backup) = app.latest_backup.clone() {
                        match core.restore_backup(backup).await {
                            Ok(_) => {
                                app.last_message = "Recovery restore succeeded. Restart OpenBitdo before attempting writes again.".to_owned();
                            }
                            Err(err) => {
                                app.last_message = format!("Recovery restore failed: {err}");
                            }
                        }
                    } else {
                        app.last_message = "No backup available to restore. Use diagnostics and restart OpenBitdo.".to_owned();
                    }
                }
                KeyCode::Enter | KeyCode::Esc => {
                    app.set_home_message(
                        "Recovery mode exited. Write actions remain locked until restart.",
                    );
                }
                KeyCode::Char('q') => return Ok(true),
                _ => {}
            }
        }
        TuiWorkflowState::Preflight => match key {
            KeyCode::Enter | KeyCode::Char('y') => {
                start_pending_update(core, app, firmware_events).await?;
            }
            KeyCode::Esc | KeyCode::Char('c') => {
                app.set_home_message("Update cancelled before transfer.");
            }
            KeyCode::Char('q') => return Ok(true),
            _ => {}
        },
        TuiWorkflowState::Updating => match key {
            KeyCode::Esc | KeyCode::Char('c') => {
                cancel_running_update(core, app, firmware_events).await?;
            }
            KeyCode::Char('q') => {
                cancel_running_update(core, app, firmware_events).await?;
                return Ok(true);
            }
            _ => {}
        },
        TuiWorkflowState::FinalReport => match key {
            KeyCode::Enter | KeyCode::Esc => app.set_home_message("Ready for next action."),
            KeyCode::Char('q') => return Ok(true),
            KeyCode::Char('a') => app.open_about(),
            _ => {}
        },
    }

    Ok(false)
}

async fn handle_mouse_event(
    core: &OpenBitdoCore,
    terminal: &mut Option<Terminal<CrosstermBackend<Stdout>>>,
    app: &mut TuiApp,
    opts: &TuiLaunchOptions,
    firmware_events: &mut Option<broadcast::Receiver<FirmwareProgressEvent>>,
    mouse: MouseEvent,
) -> Result<bool> {
    let Some(size) = terminal.as_ref().map(|t| t.size()).transpose()? else {
        return Ok(false);
    };

    let area = Rect::new(0, 0, size.width, size.height);

    match app.state {
        TuiWorkflowState::About | TuiWorkflowState::HelpOverlay => {
            if app.state == TuiWorkflowState::About {
                let (toggle_rect, report_mode_rect, fingerprint_rect) = about_buttons_rects(area);
                if matches!(mouse.kind, MouseEventKind::Moved) {
                    app.about_toggle_hovered = point_in_rect(mouse.column, mouse.row, toggle_rect);
                    app.about_report_mode_hovered =
                        point_in_rect(mouse.column, mouse.row, report_mode_rect);
                    app.about_fingerprint_hovered =
                        point_in_rect(mouse.column, mouse.row, fingerprint_rect);
                }
                if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left)) {
                    if point_in_rect(mouse.column, mouse.row, toggle_rect) {
                        app.toggle_advanced_mode(core)?;
                    } else if point_in_rect(mouse.column, mouse.row, report_mode_rect) {
                        app.cycle_report_save_mode()?;
                    } else if point_in_rect(mouse.column, mouse.row, fingerprint_rect) {
                        app.toggle_fingerprint_view();
                    } else {
                        app.close_overlay();
                    }
                }
            } else if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left)) {
                app.close_overlay();
            }
        }
        TuiWorkflowState::WaitForDevice => {
            let layout = waiting_layout(area);
            let buttons = action_buttons(layout.actions, &WAIT_ACTIONS);
            if matches!(mouse.kind, MouseEventKind::Moved) {
                app.hovered_action = button_hit(mouse.column, mouse.row, &buttons);
            }
            if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left)) {
                if let Some(action) = button_hit(mouse.column, mouse.row, &buttons) {
                    app.hovered_action = Some(action);
                    return execute_home_action(core, terminal, app, opts, firmware_events, action)
                        .await;
                }
            }
        }
        TuiWorkflowState::Home => {
            let layout = home_layout(area);
            let buttons = action_buttons(layout.actions, &HOME_ACTIONS);

            if let Some(menu) = app.context_menu.as_mut() {
                if matches!(mouse.kind, MouseEventKind::Moved) {
                    menu.hovered_index = context_menu_item_at(area, *menu, mouse.column, mouse.row)
                        .map(action_index);
                }
                if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left)) {
                    if let Some(action) = context_menu_item_at(area, *menu, mouse.column, mouse.row)
                    {
                        app.close_context_menu();
                        return execute_home_action(
                            core,
                            terminal,
                            app,
                            opts,
                            firmware_events,
                            action,
                        )
                        .await;
                    }
                    app.close_context_menu();
                }
            }

            match mouse.kind {
                MouseEventKind::Moved => {
                    app.hovered_action = button_hit(mouse.column, mouse.row, &buttons);
                }
                MouseEventKind::Down(MouseButton::Left) => {
                    if let Some(action) = button_hit(mouse.column, mouse.row, &buttons) {
                        app.hovered_action = Some(action);
                        return execute_home_action(
                            core,
                            terminal,
                            app,
                            opts,
                            firmware_events,
                            action,
                        )
                        .await;
                    }

                    if let Some(idx) = device_row_at(app, layout.devices, mouse.row) {
                        app.select_index(idx);
                    }
                }
                MouseEventKind::Down(MouseButton::Right) => {
                    if let Some(idx) = device_row_at(app, layout.devices, mouse.row) {
                        app.select_index(idx);
                        app.open_context_menu(mouse.column, mouse.row);
                    }
                }
                MouseEventKind::ScrollDown => {
                    if point_in_rect(mouse.column, mouse.row, layout.devices) {
                        app.select_next();
                    } else if point_in_rect(mouse.column, mouse.row, layout.detail) {
                        app.detail_scroll = app.detail_scroll.saturating_add(1);
                    }
                }
                MouseEventKind::ScrollUp => {
                    if point_in_rect(mouse.column, mouse.row, layout.devices) {
                        app.select_prev();
                    } else if point_in_rect(mouse.column, mouse.row, layout.detail) {
                        app.detail_scroll = app.detail_scroll.saturating_sub(1);
                    }
                }
                _ => {}
            }
        }
        TuiWorkflowState::Jp108Mapping => {
            let layout = simple_action_layout(area);
            let buttons = flow_buttons(layout.actions, &DEVICE_FLOW_ACTIONS);
            if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left)) {
                if let Some(action) = flow_button_hit(mouse.column, mouse.row, &buttons) {
                    return execute_device_flow_action(
                        core,
                        terminal,
                        app,
                        opts,
                        firmware_events,
                        action,
                    )
                    .await;
                }
                if let Some(row_idx) =
                    mapping_row_hit(layout.body, mouse.row, app.jp108_mappings.len())
                {
                    app.jp108_selected = row_idx;
                }
            }
            match mouse.kind {
                MouseEventKind::ScrollDown => jp108_adjust_selected_usage(app, 1),
                MouseEventKind::ScrollUp => jp108_adjust_selected_usage(app, -1),
                _ => {}
            }
        }
        TuiWorkflowState::U2CoreProfile => {
            let layout = simple_action_layout(area);
            let buttons = flow_buttons(layout.actions, &DEVICE_FLOW_ACTIONS);
            if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left)) {
                if let Some(action) = flow_button_hit(mouse.column, mouse.row, &buttons) {
                    return execute_device_flow_action(
                        core,
                        terminal,
                        app,
                        opts,
                        firmware_events,
                        action,
                    )
                    .await;
                }
                if let Some(profile) = app.u2_profile.as_ref() {
                    if let Some(row_idx) =
                        mapping_row_hit(layout.body, mouse.row, profile.mappings.len())
                    {
                        app.u2_selected = row_idx;
                    }
                }
            }
            match mouse.kind {
                MouseEventKind::ScrollDown => u2_adjust_selected_usage(app, 1),
                MouseEventKind::ScrollUp => u2_adjust_selected_usage(app, -1),
                _ => {}
            }
        }
        TuiWorkflowState::Recovery => {
            let layout = simple_action_layout(area);
            let buttons = action_buttons(
                layout.actions,
                &[HomeAction::Refresh, HomeAction::About, HomeAction::Quit],
            );
            if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left)) {
                match button_hit(mouse.column, mouse.row, &buttons) {
                    Some(HomeAction::Refresh) => {
                        if let Some(backup) = app.latest_backup.clone() {
                            match core.restore_backup(backup).await {
                                Ok(_) => {
                                    app.last_message = "Recovery restore succeeded. Restart OpenBitdo before attempting writes again.".to_owned();
                                }
                                Err(err) => {
                                    app.last_message = format!("Recovery restore failed: {err}");
                                }
                            }
                        } else {
                            app.last_message = "No backup available to restore.".to_owned();
                        }
                    }
                    Some(HomeAction::About) => {
                        app.set_home_message(
                            "Recovery mode exited. Write actions remain locked until restart.",
                        );
                    }
                    Some(HomeAction::Quit) => return Ok(true),
                    _ => {}
                }
            }
        }
        TuiWorkflowState::Preflight => {
            let layout = simple_action_layout(area);
            let buttons = action_buttons(layout.actions, &[HomeAction::Update, HomeAction::Quit]);
            if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left)) {
                match button_hit(mouse.column, mouse.row, &buttons) {
                    Some(HomeAction::Update) => {
                        start_pending_update(core, app, firmware_events).await?
                    }
                    Some(HomeAction::Quit) => {
                        app.set_home_message("Update cancelled before transfer.")
                    }
                    _ => {}
                }
            }
        }
        TuiWorkflowState::Updating => {
            let layout = simple_action_layout(area);
            let buttons = action_buttons(layout.actions, &[HomeAction::Quit]);
            if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left))
                && button_hit(mouse.column, mouse.row, &buttons) == Some(HomeAction::Quit)
            {
                cancel_running_update(core, app, firmware_events).await?;
            }
        }
        TuiWorkflowState::FinalReport => {
            let layout = simple_action_layout(area);
            let buttons = action_buttons(layout.actions, &[HomeAction::Refresh, HomeAction::Quit]);
            if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left)) {
                match button_hit(mouse.column, mouse.row, &buttons) {
                    Some(HomeAction::Refresh) => app.set_home_message("Ready for next action."),
                    Some(HomeAction::Quit) => return Ok(true),
                    _ => {}
                }
            }
        }
    }

    Ok(false)
}

async fn execute_home_action(
    core: &OpenBitdoCore,
    terminal: &mut Option<Terminal<CrosstermBackend<Stdout>>>,
    app: &mut TuiApp,
    opts: &TuiLaunchOptions,
    firmware_events: &mut Option<broadcast::Receiver<FirmwareProgressEvent>>,
    action: HomeAction,
) -> Result<bool> {
    match action {
        HomeAction::About => app.open_about(),
        HomeAction::Help => app.open_help(),
        HomeAction::Quit => return Ok(true),
        HomeAction::Refresh => match core.list_devices().await {
            Ok(devices) => {
                app.refresh_devices(devices);
                app.last_message = match app.devices.len() {
                    0 => {
                        "Still waiting for a controller. Plug one in and choose Refresh.".to_owned()
                    }
                    1 => "Controller detected and auto-selected.".to_owned(),
                    count => format!("Detected {count} controllers. Select one to continue."),
                };
            }
            Err(err) => app.last_message = format!("Refresh failed: {err}"),
        },
        HomeAction::Diagnose => {
            if let Some(selected) = app.selected_device().cloned() {
                match core.diag_probe(selected.vid_pid).await {
                    Ok(diag) => {
                        let confirmed_total = diag
                            .command_checks
                            .iter()
                            .filter(|c| !c.is_experimental)
                            .count();
                        let confirmed_ok = diag
                            .command_checks
                            .iter()
                            .filter(|c| !c.is_experimental && c.ok)
                            .count();
                        let experimental_total = diag
                            .command_checks
                            .iter()
                            .filter(|c| c.is_experimental)
                            .count();
                        let experimental_ok = diag
                            .command_checks
                            .iter()
                            .filter(|c| c.is_experimental && c.ok)
                            .count();
                        let needs_attention = diag
                            .command_checks
                            .iter()
                            .filter(|c| c.severity == bitdo_proto::DiagSeverity::NeedsAttention)
                            .count();
                        app.last_message = format!(
                            "Diagnostics for {}\nConfirmed checks: {}/{}\nExperimental checks: {}/{}\nNeeds attention: {}\n{}",
                            selected.vid_pid,
                            confirmed_ok,
                            confirmed_total,
                            experimental_ok,
                            experimental_total,
                            needs_attention,
                            core.beginner_diag_summary(&selected, &diag)
                        );
                        if should_save_support_report(app.report_save_mode, false) {
                            if let Ok(path) = persist_support_report(
                                "diag-probe",
                                Some(&selected),
                                "ok",
                                app.last_message.clone(),
                                Some(&diag),
                                None,
                            )
                            .await
                            {
                                app.remember_report_path(path.clone());
                                app.last_message = format!(
                                    "Diagnostics complete. Support file saved: {}",
                                    path.to_string_lossy()
                                );
                            }
                        }
                    }
                    Err(err) => {
                        app.last_message = format!("Diagnostics failed: {err}");
                        if should_save_support_report(app.report_save_mode, true) {
                            if let Ok(path) = persist_support_report(
                                "diag-probe",
                                Some(&selected),
                                "failed",
                                app.last_message.clone(),
                                None,
                                None,
                            )
                            .await
                            {
                                app.remember_report_path(path.clone());
                                if app.advanced_mode {
                                    app.last_message = format!(
                                        "Diagnostics failed. Report saved: {} (c=copy o=open f=folder)",
                                        path.to_string_lossy()
                                    );
                                } else {
                                    app.last_message = format!(
                                        "Diagnostics failed. A support file was saved: {}",
                                        path.to_string_lossy()
                                    );
                                }
                            }
                        }
                    }
                }
            } else {
                app.last_message = "No device selected.".to_owned();
            }
        }
        HomeAction::Update => {
            let Some(selected) = app.selected_device().cloned() else {
                app.last_message = "No device selected.".to_owned();
                return Ok(false);
            };

            if app.write_lock_until_restart {
                app.state = TuiWorkflowState::Recovery;
                app.last_message =
                    "Write actions are locked until restart due to a failed rollback.".to_owned();
                return Ok(false);
            }

            if selected.support_tier != SupportTier::Full {
                app.last_message = format!(
                    "Recommended Update is coming soon for {} ({}). This device is currently read-only in OpenBitdo. Use Diagnose for now.",
                    selected.name, selected.vid_pid
                );
                return Ok(false);
            }

            if selected.capability.supports_jp108_dedicated_map {
                match core.jp108_read_dedicated_mapping(selected.vid_pid).await {
                    Ok(mappings) => app.begin_jp108_mapping(mappings),
                    Err(err) => app.last_message = format!("JP108 mapping read failed: {err}"),
                }
            } else if selected.capability.supports_u2_button_map
                && selected.capability.supports_u2_slot_config
            {
                match core
                    .u2_read_core_profile(selected.vid_pid, U2SlotId::Slot1)
                    .await
                {
                    Ok(profile) => app.begin_u2_profile(profile),
                    Err(err) => app.last_message = format!("Ultimate2 profile read failed: {err}"),
                }
            } else {
                prepare_update_preflight(core, terminal, app, opts, firmware_events).await?;
            }
        }
    }

    Ok(false)
}

async fn execute_device_flow_action(
    core: &OpenBitdoCore,
    terminal: &mut Option<Terminal<CrosstermBackend<Stdout>>>,
    app: &mut TuiApp,
    opts: &TuiLaunchOptions,
    firmware_events: &mut Option<broadcast::Receiver<FirmwareProgressEvent>>,
    action: DeviceFlowAction,
) -> Result<bool> {
    let Some(selected) = app.selected_device().cloned() else {
        app.last_message = "No device selected.".to_owned();
        return Ok(false);
    };

    if app.write_lock_until_restart
        && matches!(
            action,
            DeviceFlowAction::BackupApply | DeviceFlowAction::Firmware
        )
    {
        app.state = TuiWorkflowState::Recovery;
        app.last_message =
            "Write actions are locked until restart because recovery has not completed.".to_owned();
        return Ok(false);
    }

    match action {
        DeviceFlowAction::Done => app.set_home_message("Ready for next action."),
        DeviceFlowAction::Firmware => {
            prepare_update_preflight(core, terminal, app, opts, firmware_events).await?;
        }
        DeviceFlowAction::RetryRead => {
            if app.state == TuiWorkflowState::Jp108Mapping {
                match core.jp108_read_dedicated_mapping(selected.vid_pid).await {
                    Ok(mappings) => app.begin_jp108_mapping(mappings),
                    Err(err) => app.last_message = format!("Reload failed: {err}"),
                }
            } else if app.state == TuiWorkflowState::U2CoreProfile {
                let slot = app
                    .u2_profile
                    .as_ref()
                    .map(|p| p.slot)
                    .unwrap_or(U2SlotId::Slot1);
                match core.u2_read_core_profile(selected.vid_pid, slot).await {
                    Ok(profile) => app.begin_u2_profile(profile),
                    Err(err) => app.last_message = format!("Reload failed: {err}"),
                }
            }
        }
        DeviceFlowAction::BackupApply => {
            if app.state == TuiWorkflowState::Jp108Mapping {
                let warnings = jp108_mapping_warnings(&app.jp108_mappings);
                match core
                    .jp108_apply_dedicated_mapping_with_recovery(
                        selected.vid_pid,
                        app.jp108_mappings.clone(),
                        true,
                    )
                    .await
                {
                    Ok(result) => {
                        if result.write_applied {
                            app.latest_backup = result.backup_id;
                            if warnings.is_empty() {
                                app.last_message =
                                    "JP108 mapping applied. Run Button Test or continue to Firmware."
                                        .to_owned();
                            } else {
                                app.last_message = format!(
                                    "JP108 mapping applied with warnings (allowed): {}",
                                    warnings.join(" ")
                                );
                            }
                        } else if result.rollback_failed() {
                            app.latest_backup = result.backup_id.clone();
                            app.enter_recovery(result);
                        } else {
                            app.latest_backup = result.backup_id;
                            app.last_message =
                                "Apply failed, but rollback restored your previous mapping safely."
                                    .to_owned();
                        }
                    }
                    Err(err) => {
                        app.last_message = format!("Apply failed: {err}");
                    }
                }
            } else if app.state == TuiWorkflowState::U2CoreProfile {
                if let Some(profile) = app.u2_profile.clone() {
                    let warnings = u2_mapping_warnings(&profile.mappings);
                    match core
                        .u2_apply_core_profile_with_recovery(
                            selected.vid_pid,
                            profile.slot,
                            profile.mode,
                            profile.mappings,
                            profile.l2_analog,
                            profile.r2_analog,
                            true,
                        )
                        .await
                    {
                        Ok(result) => {
                            if result.write_applied {
                                app.latest_backup = result.backup_id;
                                if warnings.is_empty() {
                                    app.last_message = "Ultimate2 profile applied. Run Button Test or continue to Firmware."
                                        .to_owned();
                                } else {
                                    app.last_message = format!(
                                        "Ultimate2 profile applied with warnings (allowed): {}",
                                        warnings.join(" ")
                                    );
                                }
                            } else if result.rollback_failed() {
                                app.latest_backup = result.backup_id.clone();
                                app.enter_recovery(result);
                            } else {
                                app.latest_backup = result.backup_id;
                                app.last_message = "Apply failed, but rollback restored your previous Ultimate2 profile safely."
                                    .to_owned();
                            }
                        }
                        Err(err) => {
                            app.last_message = format!("Apply failed: {err}");
                        }
                    }
                }
            }
        }
        DeviceFlowAction::RestoreBackup => {
            if let Some(backup) = app.latest_backup.clone() {
                match core.restore_backup(backup).await {
                    Ok(_) => app.last_message = "Backup restored successfully.".to_owned(),
                    Err(err) => app.last_message = format!("Restore failed: {err}"),
                }
            } else {
                app.last_message = "No backup available yet. Use Backup + Apply first.".to_owned();
            }
        }
        DeviceFlowAction::GuidedTest => {
            let result = if app.state == TuiWorkflowState::Jp108Mapping {
                core.guided_button_test(
                    DeviceKind::Jp108,
                    app.jp108_mappings
                        .iter()
                        .map(|entry| {
                            format!("{:?} -> 0x{:04x}", entry.button, entry.target_hid_usage)
                        })
                        .collect(),
                )
                .await
            } else {
                let expected = app
                    .u2_profile
                    .as_ref()
                    .map(|profile| {
                        profile
                            .mappings
                            .iter()
                            .map(|entry| {
                                format!(
                                    "{:?} -> {} (0x{:04x})",
                                    entry.button,
                                    u2_target_label(entry.target_hid_usage),
                                    entry.target_hid_usage
                                )
                            })
                            .collect::<Vec<_>>()
                    })
                    .unwrap_or_default();
                core.guided_button_test(DeviceKind::Ultimate2, expected)
                    .await
            };

            match result {
                Ok(report) => app.last_message = report.guidance,
                Err(err) => app.last_message = format!("Guided test failed: {err}"),
            }
        }
    }

    Ok(false)
}

async fn prepare_update_preflight(
    core: &OpenBitdoCore,
    terminal: &mut Option<Terminal<CrosstermBackend<Stdout>>>,
    app: &mut TuiApp,
    opts: &TuiLaunchOptions,
    firmware_events: &mut Option<broadcast::Receiver<FirmwareProgressEvent>>,
) -> Result<()> {
    *firmware_events = None;

    let Some(selected) = app.selected_device().cloned() else {
        app.last_message = "No device selected.".to_owned();
        return Ok(());
    };

    if selected.support_tier != SupportTier::Full {
        app.last_message = format!(
            "Firmware update is blocked for {} until hardware confirmation is complete. You can still run diagnostics.",
            selected.vid_pid
        );
        return Ok(());
    }

    let (firmware_path, source_label, version_label) = match opts.firmware_path.clone() {
        Some(path) => (path, "local file".to_owned(), "manual".to_owned()),
        None => match core.download_recommended_firmware(selected.vid_pid).await {
            Ok(download) => (
                download.firmware_path,
                "recommended verified download".to_owned(),
                download.version,
            ),
            Err(err) => {
                let prompt = format!(
                    "Recommended firmware unavailable ({err}). Enter local firmware path: "
                );
                let input = prompt_line(terminal, &prompt)?;
                if input.trim().is_empty() {
                    app.last_message = "Update cancelled: no firmware file selected.".to_owned();
                    return Ok(());
                }
                (
                    PathBuf::from(input),
                    "local file fallback".to_owned(),
                    "manual".to_owned(),
                )
            }
        },
    };

    let preflight = core
        .preflight_firmware(FirmwarePreflightRequest {
            vid_pid: selected.vid_pid,
            firmware_path: firmware_path.clone(),
            allow_unsafe: opts.allow_unsafe,
            brick_risk_ack: opts.brick_risk_ack,
            experimental: opts.experimental,
            chunk_size: opts.chunk_size,
        })
        .await?;

    if !preflight.gate.allowed {
        let reason = preflight
            .gate
            .message
            .unwrap_or_else(|| "Update is not allowed for this device.".to_owned());
        app.last_message = format!("Preflight blocked: {reason}");
        return Ok(());
    }

    let plan = preflight
        .plan
        .ok_or_else(|| anyhow!("missing preflight plan for allowed request"))?;

    app.begin_preflight(PendingUpdate {
        target: selected,
        firmware_path,
        firmware_source: source_label,
        firmware_version: version_label,
        plan,
    });

    Ok(())
}

async fn start_pending_update(
    core: &OpenBitdoCore,
    app: &mut TuiApp,
    firmware_events: &mut Option<broadcast::Receiver<FirmwareProgressEvent>>,
) -> Result<()> {
    let Some(pending) = app.pending_update.clone() else {
        app.set_home_message("No preflight plan found. Start from Home.");
        return Ok(());
    };

    core.start_firmware(FirmwareStartRequest {
        session_id: pending.plan.session_id.clone(),
    })
    .await?;

    core.confirm_firmware(FirmwareConfirmRequest {
        session_id: pending.plan.session_id.clone(),
        acknowledged_risk: true,
    })
    .await?;

    *firmware_events = Some(core.subscribe_events(&pending.plan.session_id.0).await?);
    app.set_session(pending.plan.session_id.clone());
    app.last_message = format!(
        "Transferring firmware {} from {}. Press Esc or click Cancel to stop.",
        pending.firmware_version, pending.firmware_source
    );

    Ok(())
}

async fn cancel_running_update(
    core: &OpenBitdoCore,
    app: &mut TuiApp,
    firmware_events: &mut Option<broadcast::Receiver<FirmwareProgressEvent>>,
) -> Result<()> {
    let Some(session_id) = app.session_id.clone() else {
        return Ok(());
    };

    let report = core
        .cancel_firmware(FirmwareCancelRequest { session_id })
        .await?;
    app.complete(report.clone());
    *firmware_events = None;

    if report.status != FirmwareOutcome::Completed
        && should_save_support_report(app.report_save_mode, true)
    {
        let selected = app.selected_device().cloned();
        if let Ok(path) = persist_support_report(
            "fw-write",
            selected.as_ref(),
            "cancelled",
            report.message.clone(),
            None,
            Some(&report),
        )
        .await
        {
            app.remember_report_path(path.clone());
            if app.advanced_mode {
                app.last_message = format!(
                    "Update cancelled. Report saved: {} (c=copy o=open f=folder)",
                    path.to_string_lossy()
                );
            } else {
                app.last_message = format!(
                    "Update cancelled. A support file was saved: {}",
                    path.to_string_lossy()
                );
            }
        }
    }

    Ok(())
}

async fn poll_firmware_progress(
    core: &OpenBitdoCore,
    app: &mut TuiApp,
    firmware_events: &mut Option<broadcast::Receiver<FirmwareProgressEvent>>,
) -> Result<()> {
    if app.state != TuiWorkflowState::Updating {
        return Ok(());
    }

    if let Some(receiver) = firmware_events.as_mut() {
        loop {
            match receiver.try_recv() {
                Ok(evt) => {
                    app.apply_progress(evt.progress, format!("{}: {}", evt.stage, evt.message));
                }
                Err(broadcast::error::TryRecvError::Empty) => break,
                Err(broadcast::error::TryRecvError::Lagged(_)) => continue,
                Err(broadcast::error::TryRecvError::Closed) => {
                    *firmware_events = None;
                    break;
                }
            }
        }
    }

    if let Some(session_id) = app.session_id.as_ref() {
        if let Some(report) = core.firmware_report(&session_id.0).await? {
            app.complete(report.clone());
            *firmware_events = None;
            match report.status {
                FirmwareOutcome::Completed => {
                    app.last_message =
                        "Firmware update completed. Press Enter to continue.".to_owned();
                    if should_save_support_report(app.report_save_mode, false) {
                        let selected = app.selected_device().cloned();
                        if let Ok(path) = persist_support_report(
                            "fw-write",
                            selected.as_ref(),
                            "completed",
                            report.message.clone(),
                            None,
                            Some(&report),
                        )
                        .await
                        {
                            app.remember_report_path(path.clone());
                            app.last_message = format!(
                                "Firmware update completed. Support file saved: {}",
                                path.to_string_lossy()
                            );
                        }
                    }
                }
                FirmwareOutcome::Cancelled | FirmwareOutcome::Failed => {
                    app.last_message =
                        "Firmware update did not complete. Press Enter to return Home.".to_owned();
                    if should_save_support_report(app.report_save_mode, true) {
                        let selected = app.selected_device().cloned();
                        if let Ok(path) = persist_support_report(
                            "fw-write",
                            selected.as_ref(),
                            "failed",
                            report.message.clone(),
                            None,
                            Some(&report),
                        )
                        .await
                        {
                            app.remember_report_path(path.clone());
                            if app.advanced_mode {
                                app.last_message = format!(
                                    "Firmware update failed. Report saved: {} (c=copy o=open f=folder)",
                                    path.to_string_lossy()
                                );
                            } else {
                                app.last_message = format!(
                                    "Firmware update failed. A support file was saved: {}",
                                    path.to_string_lossy()
                                );
                            }
                        }
                    }
                }
            }
        }
    }

    Ok(())
}

fn home_layout(area: Rect) -> HomeLayout {
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(3),
            Constraint::Min(10),
            Constraint::Length(5),
            Constraint::Length(3),
            Constraint::Min(6),
        ])
        .split(area);

    let detail_chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Min(4), Constraint::Length(5)])
        .split(chunks[4]);

    HomeLayout {
        title: chunks[0],
        devices: chunks[1],
        actions: chunks[2],
        progress: chunks[3],
        detail: detail_chunks[0],
        blocked: detail_chunks[1],
    }
}

fn waiting_layout(area: Rect) -> WaitingLayout {
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(3),
            Constraint::Min(8),
            Constraint::Length(5),
            Constraint::Length(3),
        ])
        .split(area);

    WaitingLayout {
        header: chunks[0],
        body: chunks[1],
        actions: chunks[2],
        footer: chunks[3],
    }
}

fn simple_action_layout(area: Rect) -> SimpleActionLayout {
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Min(10),
            Constraint::Length(5),
            Constraint::Length(3),
        ])
        .split(area);

    SimpleActionLayout {
        body: chunks[0],
        actions: chunks[1],
        footer: chunks[2],
    }
}

#[derive(Clone, Copy)]
struct HomeLayout {
    title: Rect,
    devices: Rect,
    actions: Rect,
    progress: Rect,
    detail: Rect,
    blocked: Rect,
}

#[derive(Clone, Copy)]
struct WaitingLayout {
    header: Rect,
    body: Rect,
    actions: Rect,
    footer: Rect,
}

#[derive(Clone, Copy)]
struct SimpleActionLayout {
    body: Rect,
    actions: Rect,
    footer: Rect,
}

fn action_buttons(area: Rect, actions: &[HomeAction]) -> Vec<(Rect, HomeAction)> {
    if actions.is_empty() {
        return Vec::new();
    }

    let constraints =
        vec![Constraint::Percentage((100 / actions.len()).max(1) as u16); actions.len()];
    let chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints(constraints)
        .split(area);

    chunks
        .iter()
        .copied()
        .zip(actions.iter().copied())
        .collect::<Vec<_>>()
}

fn flow_buttons(area: Rect, actions: &[DeviceFlowAction]) -> Vec<(Rect, DeviceFlowAction)> {
    if actions.is_empty() {
        return Vec::new();
    }

    let constraints =
        vec![Constraint::Percentage((100 / actions.len()).max(1) as u16); actions.len()];
    let chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints(constraints)
        .split(area);

    chunks
        .iter()
        .copied()
        .zip(actions.iter().copied())
        .collect::<Vec<_>>()
}

fn button_hit(column: u16, row: u16, buttons: &[(Rect, HomeAction)]) -> Option<HomeAction> {
    buttons
        .iter()
        .find(|(rect, _)| point_in_rect(column, row, *rect))
        .map(|(_, action)| *action)
}

fn flow_button_hit(
    column: u16,
    row: u16,
    buttons: &[(Rect, DeviceFlowAction)],
) -> Option<DeviceFlowAction> {
    buttons
        .iter()
        .find(|(rect, _)| point_in_rect(column, row, *rect))
        .map(|(_, action)| *action)
}

fn mapping_row_hit(body_rect: Rect, row: u16, total_rows: usize) -> Option<usize> {
    let start = body_rect.y.saturating_add(2);
    if row < start {
        return None;
    }
    let idx = row.saturating_sub(start) as usize;
    if idx < total_rows {
        Some(idx)
    } else {
        None
    }
}

const HID_USAGE_PRESETS: [u16; 16] = [
    0x0004, 0x0005, 0x0006, 0x0007, 0x0008, 0x0009, 0x000a, 0x000b, 0x0028, 0x0029, 0x002c, 0x003a,
    0x003b, 0x003c, 0x00e0, 0x00e1,
];

// Ultimate2 target set is intentionally restricted to known controller-button
// codes for RC safety/readability.
const U2_TARGET_PRESETS: [u16; 17] = [
    0x0100, // A
    0x0101, // B
    0x0102, // X
    0x0103, // Y
    0x0104, // L1
    0x0105, // R1
    0x0106, // L2
    0x0107, // R2
    0x0108, // L3
    0x0109, // R3
    0x010a, // Select
    0x010b, // Start
    0x010c, // Home
    0x010d, // DPadUp
    0x010e, // DPadDown
    0x010f, // DPadLeft
    0x0110, // DPadRight
];

fn cycle_usage(current: u16, delta: i32) -> u16 {
    let pos = HID_USAGE_PRESETS
        .iter()
        .position(|value| *value == current)
        .unwrap_or(0) as i32;
    let len = HID_USAGE_PRESETS.len() as i32;
    let next = (pos + delta).rem_euclid(len) as usize;
    HID_USAGE_PRESETS[next]
}

fn cycle_u2_target(current: u16, delta: i32) -> u16 {
    let pos = U2_TARGET_PRESETS
        .iter()
        .position(|value| *value == current)
        .unwrap_or(0) as i32;
    let len = U2_TARGET_PRESETS.len() as i32;
    let next = (pos + delta).rem_euclid(len) as usize;
    U2_TARGET_PRESETS[next]
}

fn u2_target_label(target: u16) -> &'static str {
    match target {
        0x0100 => "A",
        0x0101 => "B",
        0x0102 => "X",
        0x0103 => "Y",
        0x0104 => "L1",
        0x0105 => "R1",
        0x0106 => "L2",
        0x0107 => "R2",
        0x0108 => "L3",
        0x0109 => "R3",
        0x010a => "Select",
        0x010b => "Start",
        0x010c => "Home",
        0x010d => "DPadUp",
        0x010e => "DPadDown",
        0x010f => "DPadLeft",
        0x0110 => "DPadRight",
        _ => "Unknown",
    }
}

fn u2_default_target_for_slot(slot: &bitdo_app_core::U2ButtonId) -> u16 {
    match slot {
        bitdo_app_core::U2ButtonId::A => 0x0100,
        bitdo_app_core::U2ButtonId::B => 0x0101,
        bitdo_app_core::U2ButtonId::K1 => 0x0102,
        bitdo_app_core::U2ButtonId::K2 => 0x0103,
        bitdo_app_core::U2ButtonId::K3 => 0x0104,
        bitdo_app_core::U2ButtonId::K4 => 0x0105,
        bitdo_app_core::U2ButtonId::K5 => 0x0106,
        bitdo_app_core::U2ButtonId::K6 => 0x0107,
        bitdo_app_core::U2ButtonId::K7 => 0x0108,
        bitdo_app_core::U2ButtonId::K8 => 0x0109,
    }
}

fn u2_mapping_warnings(mappings: &[bitdo_app_core::U2ButtonMapping]) -> Vec<String> {
    let mut warnings = Vec::new();
    let mut target_counts: HashMap<u16, usize> = HashMap::new();
    for mapping in mappings {
        *target_counts.entry(mapping.target_hid_usage).or_insert(0) += 1;
    }

    for (target, count) in target_counts {
        if count > 1 {
            warnings.push(format!(
                "Duplicate target {} (0x{:04x}) appears {} times.",
                u2_target_label(target),
                target,
                count
            ));
        }
    }

    for mapping in mappings {
        if mapping.target_hid_usage == u2_default_target_for_slot(&mapping.button) {
            warnings.push(format!(
                "Identity mapping kept for {:?} -> {} (0x{:04x}).",
                mapping.button,
                u2_target_label(mapping.target_hid_usage),
                mapping.target_hid_usage
            ));
        }
    }

    warnings
}

fn jp108_default_target_for_button(button: &bitdo_app_core::DedicatedButtonId) -> u16 {
    match button {
        bitdo_app_core::DedicatedButtonId::A => 0x0004,
        bitdo_app_core::DedicatedButtonId::B => 0x0005,
        bitdo_app_core::DedicatedButtonId::K1 => 0x0006,
        bitdo_app_core::DedicatedButtonId::K2 => 0x0007,
        bitdo_app_core::DedicatedButtonId::K3 => 0x0008,
        bitdo_app_core::DedicatedButtonId::K4 => 0x0009,
        bitdo_app_core::DedicatedButtonId::K5 => 0x000a,
        bitdo_app_core::DedicatedButtonId::K6 => 0x000b,
        bitdo_app_core::DedicatedButtonId::K7 => 0x0028,
        bitdo_app_core::DedicatedButtonId::K8 => 0x0029,
    }
}

fn jp108_mapping_warnings(mappings: &[bitdo_app_core::DedicatedButtonMapping]) -> Vec<String> {
    let mut warnings = Vec::new();
    let mut target_counts: HashMap<u16, usize> = HashMap::new();
    for mapping in mappings {
        *target_counts.entry(mapping.target_hid_usage).or_insert(0) += 1;
    }

    for (target, count) in target_counts {
        if count > 1 {
            warnings.push(format!(
                "Duplicate target 0x{:04x} appears {} times.",
                target, count
            ));
        }
    }

    for mapping in mappings {
        if mapping.target_hid_usage == jp108_default_target_for_button(&mapping.button) {
            warnings.push(format!(
                "Identity mapping kept for {:?} -> 0x{:04x}.",
                mapping.button, mapping.target_hid_usage
            ));
        }
    }

    warnings
}

fn jp108_adjust_selected_usage(app: &mut TuiApp, delta: i32) {
    if let Some(current) = app.jp108_mappings.get_mut(app.jp108_selected) {
        current.target_hid_usage = cycle_usage(current.target_hid_usage, delta);
    }
}

fn u2_adjust_selected_usage(app: &mut TuiApp, delta: i32) {
    if let Some(profile) = app.u2_profile.as_mut() {
        if let Some(current) = profile.mappings.get_mut(app.u2_selected) {
            current.target_hid_usage = cycle_u2_target(current.target_hid_usage, delta);
        }
    }
}

fn slot_label(slot: U2SlotId) -> &'static str {
    match slot {
        U2SlotId::Slot1 => "Slot1",
        U2SlotId::Slot2 => "Slot2",
        U2SlotId::Slot3 => "Slot3",
    }
}

fn point_in_rect(x: u16, y: u16, rect: Rect) -> bool {
    x >= rect.x
        && y >= rect.y
        && x < rect.x.saturating_add(rect.width)
        && y < rect.y.saturating_add(rect.height)
}

fn device_row_at(app: &TuiApp, devices_rect: Rect, row: u16) -> Option<usize> {
    let start = devices_rect.y.saturating_add(1);
    let end = devices_rect
        .y
        .saturating_add(devices_rect.height.saturating_sub(1));

    if row < start || row >= end {
        return None;
    }

    let visible_rows = devices_rect.height.saturating_sub(2) as usize;
    if visible_rows == 0 {
        return None;
    }

    let window_start = if app.selected_index >= visible_rows {
        app.selected_index + 1 - visible_rows
    } else {
        0
    };

    let offset = row.saturating_sub(start) as usize;
    let idx = window_start + offset;
    if idx < app.devices.len() {
        Some(idx)
    } else {
        None
    }
}

fn context_menu_rect(area: Rect, menu: MouseContextMenu) -> Rect {
    let width: u16 = 30;
    let height: u16 = CONTEXT_ACTIONS.len() as u16 + 2;

    let max_x = area.x.saturating_add(area.width.saturating_sub(width));
    let max_y = area.y.saturating_add(area.height.saturating_sub(height));

    let x = menu.anchor_col.min(max_x);
    let y = menu.anchor_row.min(max_y);

    Rect::new(x, y, width, height)
}

fn context_menu_item_at(
    area: Rect,
    menu: MouseContextMenu,
    column: u16,
    row: u16,
) -> Option<HomeAction> {
    let rect = context_menu_rect(area, menu);
    if !point_in_rect(column, row, rect) {
        return None;
    }

    let inner_y = row.saturating_sub(rect.y.saturating_add(1));
    CONTEXT_ACTIONS.get(inner_y as usize).copied()
}

fn action_index(action: HomeAction) -> usize {
    match action {
        HomeAction::Update => 0,
        HomeAction::Diagnose => 1,
        HomeAction::Refresh => 2,
        HomeAction::About => 3,
        HomeAction::Help => 4,
        HomeAction::Quit => 5,
    }
}

fn action_tooltip(action: HomeAction, advanced_mode: bool) -> &'static str {
    match (action, advanced_mode) {
        (HomeAction::Update, false) => "Recommended update starts the safest guided flow.",
        (HomeAction::Update, true) => {
            "Recommended update runs preflight, signature checks, and explicit confirmation."
        }
        (HomeAction::Diagnose, false) => "Diagnose checks device readiness without risky writes.",
        (HomeAction::Diagnose, true) => {
            "Diagnose includes inferred read-only checks while advanced mode is enabled."
        }
        (HomeAction::Refresh, _) => "Refresh rescans connected 8BitDo devices.",
        (HomeAction::About, false) => {
            "About shows version/build details and advanced mode setting."
        }
        (HomeAction::About, true) => "About also controls advanced mode and report hotkey details.",
        (HomeAction::Help, _) => "Help shows the beginner flow and optional shortcuts.",
        (HomeAction::Quit, _) => "Quit closes OpenBitdo safely.",
    }
}

fn available_actions_summary(device: &AppDevice) -> &'static str {
    match device.support_status() {
        UserSupportStatus::Supported => {
            if device.capability.supports_jp108_dedicated_map {
                "JP108 dedicated mapping (A/B/K1-K8), diagnostics, and firmware update"
            } else if device.capability.supports_u2_button_map
                && device.capability.supports_u2_slot_config
            {
                "Ultimate2 mode/slot/core mapping, diagnostics, and firmware update"
            } else {
                "Diagnostics and firmware update"
            }
        }
        UserSupportStatus::InProgress => {
            "Diagnostics only (mapping/update blocked in beginner mode)"
        }
        UserSupportStatus::Planned => "Detection and diagnostics only",
        UserSupportStatus::Blocked => "No actions available",
    }
}

fn display_device_name(device: &AppDevice) -> String {
    if device.name == "PID_UNKNOWN"
        || device.protocol_family == bitdo_proto::ProtocolFamily::Unknown
    {
        format!(
            "Unknown 8BitDo Device ({:04x}:{:04x})",
            device.vid_pid.vid, device.vid_pid.pid
        )
    } else {
        device.name.clone()
    }
}

fn beginner_status_label(device: &AppDevice) -> &'static str {
    match device.support_status() {
        UserSupportStatus::InProgress => UserSupportStatus::Blocked.as_str(),
        other => other.as_str(),
    }
}

fn blocked_action_panel_text(device: &AppDevice) -> String {
    match device.support_status() {
        UserSupportStatus::Supported => {
            "Blocked Actions: none. This device is fully supported in the current build."
                .to_owned()
        }
        UserSupportStatus::InProgress => format!(
            "Status shown as Blocked for {} in beginner mode.\nRecommended Update is visible with a Coming soon badge.\nDiagnostics are available, but mapping and firmware writes are blocked until hardware confirmation is complete.",
            device.name
        ),
        UserSupportStatus::Planned => format!(
            "Recommended Update is blocked for {} and marked Coming soon because support is still Planned.\nYou can run diagnostics now, and full actions unlock after confirmation work.",
            device.name
        ),
        UserSupportStatus::Blocked => format!(
            "This action is currently blocked for {} by policy. Use Diagnose for a safe check.",
            device.name
        ),
    }
}

fn should_save_support_report(mode: ReportSaveMode, is_failure: bool) -> bool {
    match mode {
        ReportSaveMode::Off => false,
        ReportSaveMode::Always => true,
        ReportSaveMode::FailureOnly => is_failure,
    }
}

fn about_buttons_rects(area: Rect) -> (Rect, Rect, Rect) {
    let width: u16 = 30;
    let height: u16 = 3;
    let spacing: u16 = 1;
    let total_height = height * 3 + spacing * 2;
    let x = area.x.saturating_add(area.width.saturating_sub(width) / 2);
    let y = area
        .y
        .saturating_add(area.height.saturating_sub(total_height + 2));
    let first = Rect::new(x, y, width.min(area.width), height);
    let second = Rect::new(
        x,
        y.saturating_add(height + spacing),
        width.min(area.width),
        height,
    );
    let third = Rect::new(
        x,
        y.saturating_add((height + spacing) * 2),
        width.min(area.width),
        height,
    );
    (first, second, third)
}

fn render_button(frame: &mut Frame<'_>, rect: Rect, label: &str, active: bool) {
    let style = if active {
        Style::default()
            .fg(Color::Black)
            .bg(Color::Cyan)
            .add_modifier(Modifier::BOLD)
    } else {
        Style::default().fg(Color::White)
    };

    let button = Paragraph::new(Line::from(Span::styled(label, style))).block(
        Block::default().borders(Borders::ALL).style(if active {
            Style::default().fg(Color::Cyan)
        } else {
            Style::default().fg(Color::Gray)
        }),
    );
    frame.render_widget(button, rect);
}

fn render_if_needed(
    terminal: &mut Option<Terminal<CrosstermBackend<Stdout>>>,
    app: &TuiApp,
) -> Result<()> {
    let Some(terminal) = terminal else {
        return Ok(());
    };

    terminal.draw(|frame| match app.state {
        TuiWorkflowState::About => render_about(frame, app),
        TuiWorkflowState::HelpOverlay => render_help(frame, app.advanced_mode),
        TuiWorkflowState::WaitForDevice => render_waiting(frame, app),
        TuiWorkflowState::Home => render_home(frame, app),
        TuiWorkflowState::Jp108Mapping => render_jp108_mapping(frame, app),
        TuiWorkflowState::U2CoreProfile => render_u2_profile(frame, app),
        TuiWorkflowState::Recovery => render_recovery(frame, app),
        TuiWorkflowState::Preflight => render_preflight(frame, app),
        TuiWorkflowState::Updating => render_updating(frame, app),
        TuiWorkflowState::FinalReport => render_final_report(frame, app),
    })?;

    Ok(())
}

fn render_about(frame: &mut Frame<'_>, app: &TuiApp) {
    let block = Block::default()
        .borders(Borders::ALL)
        .title("About OpenBitdo");
    let toggle_label = if app.advanced_mode {
        "Advanced Mode: ON"
    } else {
        "Advanced Mode: OFF"
    };
    let report_label = format!("Report Saving: {}", app.report_save_mode.as_str());
    let fingerprint_toggle_label = if app.about_show_full_fingerprint {
        "Fingerprint: full"
    } else {
        "Fingerprint: short"
    };
    let key_line = if app.about_show_full_fingerprint {
        app.build_info.signing_key_fingerprint_full.clone()
    } else {
        app.build_info.signing_key_fingerprint_short.clone()
    };
    let lines = vec![
        Line::from(format!("App version: {}", app.build_info.app_version)),
        Line::from(format!(
            "Git commit (short): {}",
            app.build_info.git_commit_short
        )),
        Line::from(format!(
            "Git commit (full): {}",
            app.build_info.git_commit_full
        )),
        Line::from(format!(
            "Build date (UTC): {}",
            app.build_info.build_date_utc
        )),
        Line::from(format!("Platform target: {}", app.build_info.target_triple)),
        Line::from(format!(
            "Runtime platform: {}",
            app.build_info.runtime_platform
        )),
        Line::from(format!("Signing key (active): {key_line}")),
        Line::from(format!(
            "Signing key (next, short): {}",
            app.build_info.signing_key_next_fingerprint_short
        )),
        Line::from(""),
        Line::from(format!(
            "{toggle_label} (press 't' or click button to toggle)"
        )),
        Line::from(format!(
            "{} (press 'r' or click button to cycle)",
            report_label
        )),
        Line::from("Press 'v' or click Fingerprint to toggle short/full view."),
        Line::from("Esc/Enter/click outside to return"),
    ];

    let paragraph = Paragraph::new(lines).block(block);
    frame.render_widget(paragraph, frame.area());

    let (toggle_rect, report_rect, fingerprint_rect) = about_buttons_rects(frame.area());
    render_button(frame, toggle_rect, toggle_label, app.about_toggle_hovered);
    render_button(
        frame,
        report_rect,
        report_label.as_str(),
        app.about_report_mode_hovered,
    );
    render_button(
        frame,
        fingerprint_rect,
        fingerprint_toggle_label,
        app.about_fingerprint_hovered,
    );
}

fn render_help(frame: &mut Frame<'_>, advanced_mode: bool) {
    let mut lines = vec![
        Line::from("Beginner flow:"),
        Line::from("1) Select your controller"),
        Line::from("2) Choose Recommended Update or Diagnose"),
        Line::from("3) Follow the on-screen confirmation"),
        Line::from(""),
        Line::from("Optional shortcuts:"),
        Line::from("u=update  d=diagnose  r=refresh  a=about  q=quit"),
    ];
    if advanced_mode {
        lines.push(Line::from(""));
        lines.push(Line::from(
            "Advanced hotkeys (reports): c=copy path, o=open report, f=open folder",
        ));
    }
    lines.push(Line::from("Esc/Enter/click to return"));
    let paragraph =
        Paragraph::new(lines).block(Block::default().borders(Borders::ALL).title("Help"));
    frame.render_widget(paragraph, frame.area());
}

fn render_waiting(frame: &mut Frame<'_>, app: &TuiApp) {
    let layout = waiting_layout(frame.area());

    let header = Paragraph::new(Line::from(vec![
        Span::styled("OpenBitdo", Style::default().fg(Color::Cyan)),
        Span::raw("  Beginner wizard"),
    ]))
    .block(Block::default().borders(Borders::ALL).title("Welcome"));

    let body_lines = vec![
        Line::from("No supported 8BitDo controller is detected yet."),
        Line::from(""),
        Line::from("Plug in your controller and click Refresh."),
        Line::from("If you need help, open Help for a quick walkthrough."),
        Line::from(""),
        Line::from(app.last_message.clone()),
    ];
    let body = Paragraph::new(body_lines).block(
        Block::default()
            .borders(Borders::ALL)
            .title("Waiting for Controller"),
    );

    frame.render_widget(header, layout.header);
    frame.render_widget(body, layout.body);

    for (rect, action) in action_buttons(layout.actions, &WAIT_ACTIONS) {
        render_button(
            frame,
            rect,
            action.label(),
            app.hovered_action == Some(action),
        );
    }

    let hover_hint = app
        .hovered_action
        .map(|action| action_tooltip(action, app.advanced_mode))
        .unwrap_or("Mouse: click buttons. Keyboard: Enter refresh, ? help, q quit");
    let footer =
        Paragraph::new(hover_hint).block(Block::default().borders(Borders::ALL).title("Controls"));
    frame.render_widget(footer, layout.footer);
}

fn render_home(frame: &mut Frame<'_>, app: &TuiApp) {
    let layout = home_layout(frame.area());

    let title = Paragraph::new(Line::from(vec![
        Span::styled("OpenBitdo", Style::default().fg(Color::Cyan)),
        Span::raw("  Choose a controller and action"),
    ]))
    .block(Block::default().borders(Borders::ALL).title("Home"));

    let visible_rows = layout.devices.height.saturating_sub(2) as usize;
    let window_start = if app.selected_index >= visible_rows && visible_rows > 0 {
        app.selected_index + 1 - visible_rows
    } else {
        0
    };
    let window_end = (window_start + visible_rows).min(app.devices.len());

    let mut device_items = Vec::new();
    if app.devices.is_empty() {
        device_items.push(ListItem::new("No controllers detected"));
    } else {
        for (idx, dev) in app.devices[window_start..window_end].iter().enumerate() {
            let absolute_idx = window_start + idx;
            let status = beginner_status_label(dev);
            let line = format!(
                "{:04x}:{:04x}  {}  [{}]",
                dev.vid_pid.vid,
                dev.vid_pid.pid,
                display_device_name(dev),
                status
            );
            let style = if absolute_idx == app.selected_index {
                Style::default()
                    .fg(Color::Black)
                    .bg(Color::Cyan)
                    .add_modifier(Modifier::BOLD)
            } else {
                Style::default().fg(Color::White)
            };
            device_items.push(ListItem::new(line).style(style));
        }
    }

    let device_list =
        List::new(device_items).block(Block::default().borders(Borders::ALL).title("Controllers"));

    frame.render_widget(title, layout.title);
    frame.render_widget(device_list, layout.devices);

    let update_blocked = app
        .selected_device()
        .map(|d| d.support_tier != SupportTier::Full)
        .unwrap_or(true);

    for (rect, action) in action_buttons(layout.actions, &HOME_ACTIONS) {
        let label = if action == HomeAction::Update && update_blocked {
            "Recommended Update [Coming soon]"
        } else {
            action.label()
        };
        render_button(frame, rect, label, app.hovered_action == Some(action));
    }

    let gauge = Gauge::default()
        .block(Block::default().title("Progress").borders(Borders::ALL))
        .gauge_style(Style::default().fg(Color::Green))
        .percent(app.progress as u16)
        .label(format!("{}%", app.progress));
    frame.render_widget(gauge, layout.progress);

    let selected_summary = app
        .selected_device()
        .map(|d| {
            let tooltip = app
                .hovered_action
                .map(|action| {
                    if action == HomeAction::Update && update_blocked {
                        "Recommended update support for this device is coming soon."
                    } else {
                        action_tooltip(action, app.advanced_mode)
                    }
                })
                .unwrap_or("");
            let actions = available_actions_summary(d);
            format!(
                "Selected: {} ({:04x}:{:04x})\nStatus: {}\nCurrent user actions: {}\n{}\n{}",
                display_device_name(d),
                d.vid_pid.vid,
                d.vid_pid.pid,
                beginner_status_label(d),
                actions,
                app.last_message,
                tooltip
            )
        })
        .unwrap_or_else(|| format!("No controller selected\n{}", app.last_message));

    let detail = Paragraph::new(selected_summary)
        .scroll((app.detail_scroll, 0))
        .block(Block::default().borders(Borders::ALL).title("Guidance"));
    frame.render_widget(detail, layout.detail);

    let blocked_text = app
        .selected_device()
        .map(blocked_action_panel_text)
        .unwrap_or_else(|| "Blocked Actions: none".to_owned());
    let blocked = Paragraph::new(blocked_text).block(
        Block::default()
            .borders(Borders::ALL)
            .title("Blocked Actions"),
    );
    frame.render_widget(blocked, layout.blocked);

    if let Some(menu) = app.context_menu {
        let menu_rect = context_menu_rect(frame.area(), menu);
        let items = CONTEXT_ACTIONS
            .iter()
            .enumerate()
            .map(|(idx, action)| {
                let style = if menu.hovered_index == Some(idx) {
                    Style::default()
                        .fg(Color::Black)
                        .bg(Color::Cyan)
                        .add_modifier(Modifier::BOLD)
                } else {
                    Style::default().fg(Color::White)
                };
                ListItem::new(action.label()).style(style)
            })
            .collect::<Vec<_>>();
        let popup = List::new(items).block(Block::default().borders(Borders::ALL).title("Actions"));
        frame.render_widget(popup, menu_rect);
    }
}

fn render_jp108_mapping(frame: &mut Frame<'_>, app: &TuiApp) {
    let layout = simple_action_layout(frame.area());
    let mut lines = vec![
        Line::from("JP108 Dedicated Button Mapping"),
        Line::from("Use Up/Down to select, Left/Right to change mapped HID usage."),
        Line::from(""),
    ];

    for (idx, mapping) in app.jp108_mappings.iter().enumerate() {
        let marker = if idx == app.jp108_selected { ">" } else { " " };
        lines.push(Line::from(format!(
            "{marker} {:?} -> 0x{:04x}",
            mapping.button, mapping.target_hid_usage
        )));
    }

    lines.push(Line::from(""));
    lines.push(Line::from(app.last_message.clone()));

    let body =
        Paragraph::new(lines).block(Block::default().borders(Borders::ALL).title("JP108 Wizard"));
    frame.render_widget(body, layout.body);

    for (rect, action) in flow_buttons(layout.actions, &DEVICE_FLOW_ACTIONS) {
        render_button(frame, rect, action.label(), false);
    }

    let footer = Paragraph::new("b=apply r=reload s=restore t=test f=firmware Esc=done")
        .block(Block::default().borders(Borders::ALL).title("Controls"));
    frame.render_widget(footer, layout.footer);
}

fn render_u2_profile(frame: &mut Frame<'_>, app: &TuiApp) {
    let layout = simple_action_layout(frame.area());
    let mut lines = vec![
        Line::from("Ultimate2 Core Profile Mapping"),
        Line::from("Use Up/Down to select button mapping, Left/Right to adjust usage."),
        Line::from("Press 1/2/3 to select slot, m to cycle mode."),
        Line::from(""),
    ];

    if let Some(profile) = app.u2_profile.as_ref() {
        lines.push(Line::from(format!("Slot: {}", slot_label(profile.slot))));
        lines.push(Line::from(format!("Mode: {}", profile.mode)));
        lines.push(Line::from(format!(
            "Firmware version: {}",
            profile.firmware_version
        )));
        lines.push(Line::from(format!(
            "L2 analog: {:.2} {}",
            profile.l2_analog,
            if profile.supports_trigger_write {
                "(write-enabled)"
            } else {
                "(read-only)"
            }
        )));
        lines.push(Line::from(format!(
            "R2 analog: {:.2} {}",
            profile.r2_analog,
            if profile.supports_trigger_write {
                "(write-enabled)"
            } else {
                "(read-only)"
            }
        )));
        lines.push(Line::from(""));

        for (idx, mapping) in profile.mappings.iter().enumerate() {
            let marker = if idx == app.u2_selected { ">" } else { " " };
            lines.push(Line::from(format!(
                "{marker} {:?} -> {} (0x{:04x})",
                mapping.button,
                u2_target_label(mapping.target_hid_usage),
                mapping.target_hid_usage
            )));
        }
    } else {
        lines.push(Line::from("No profile loaded."));
    }

    lines.push(Line::from(""));
    lines.push(Line::from(app.last_message.clone()));

    let body = Paragraph::new(lines).block(
        Block::default()
            .borders(Borders::ALL)
            .title("Ultimate2 Wizard"),
    );
    frame.render_widget(body, layout.body);

    for (rect, action) in flow_buttons(layout.actions, &DEVICE_FLOW_ACTIONS) {
        render_button(frame, rect, action.label(), false);
    }

    let footer =
        Paragraph::new("b=apply r=reload s=restore t=test f=firmware [ ]=L2 ; '=R2 Esc=done")
            .block(Block::default().borders(Borders::ALL).title("Controls"));
    frame.render_widget(footer, layout.footer);
}

fn render_recovery(frame: &mut Frame<'_>, app: &TuiApp) {
    let layout = simple_action_layout(frame.area());
    let mut lines = vec![
        Line::from("Recovery Wizard"),
        Line::from(""),
        Line::from("A write operation failed and automatic rollback also failed."),
        Line::from("Write actions are now locked until OpenBitdo is restarted."),
        Line::from(""),
        Line::from("Safe sequence: auto rollback -> verify -> retry -> safe exit."),
        Line::from("Current state: auto rollback did not fully recover."),
    ];

    if let Some(report) = app.recovery_report.as_ref() {
        if let Some(write_error) = report.write_error.as_deref() {
            lines.push(Line::from(format!("Write error: {write_error}")));
        }
        if let Some(rollback_error) = report.rollback_error.as_deref() {
            lines.push(Line::from(format!("Rollback error: {rollback_error}")));
        }
        lines.push(Line::from(format!(
            "Rollback attempted: {}",
            if report.rollback_attempted {
                "yes"
            } else {
                "no"
            }
        )));
    }
    lines.push(Line::from(""));
    lines.push(Line::from(app.last_message.clone()));

    let body = Paragraph::new(lines).block(
        Block::default()
            .borders(Borders::ALL)
            .title("Needs Attention"),
    );
    frame.render_widget(body, layout.body);

    let buttons = action_buttons(
        layout.actions,
        &[HomeAction::Refresh, HomeAction::About, HomeAction::Quit],
    );
    for (rect, action) in buttons {
        let label = match action {
            HomeAction::Refresh => "Try Restore Backup",
            HomeAction::About => "Safe Exit to Home",
            HomeAction::Quit => "Quit",
            _ => action.label(),
        };
        render_button(frame, rect, label, false);
    }

    let footer = Paragraph::new("r=restore backup  Enter/Esc=safe exit  q=quit")
        .block(Block::default().borders(Borders::ALL).title("Controls"));
    frame.render_widget(footer, layout.footer);
}

fn render_preflight(frame: &mut Frame<'_>, app: &TuiApp) {
    let layout = simple_action_layout(frame.area());

    let mut lines = vec![Line::from("Review update preflight"), Line::from("")];
    if let Some(pending) = app.pending_update.as_ref() {
        lines.push(Line::from(format!(
            "Device: {} ({:04x}:{:04x})",
            pending.target.name, pending.target.vid_pid.vid, pending.target.vid_pid.pid
        )));
        lines.push(Line::from(format!(
            "Firmware source: {}",
            pending.firmware_source
        )));
        lines.push(Line::from(format!(
            "Firmware version: {}",
            pending.firmware_version
        )));
        lines.push(Line::from(format!(
            "Image path: {}",
            pending.firmware_path.display()
        )));
        lines.push(Line::from(format!(
            "Chunk size: {} bytes",
            pending.plan.chunk_size
        )));
        lines.push(Line::from(format!("Chunks: {}", pending.plan.chunks_total)));
        lines.push(Line::from(format!(
            "Estimated transfer time: {}s",
            pending.plan.expected_seconds
        )));
        lines.push(Line::from(""));
        lines.push(Line::from("Warnings:"));
        for warning in &pending.plan.warnings {
            lines.push(Line::from(format!("- {warning}")));
        }
    } else {
        lines.push(Line::from("No preflight details available."));
    }

    lines.push(Line::from(""));
    lines.push(Line::from("Click I Understand to start transfer."));

    let body =
        Paragraph::new(lines).block(Block::default().borders(Borders::ALL).title("Preflight"));
    frame.render_widget(body, layout.body);

    let buttons = action_buttons(layout.actions, &[HomeAction::Update, HomeAction::Quit]);
    for (rect, action) in buttons {
        let label = match action {
            HomeAction::Update => "I Understand",
            HomeAction::Quit => "Cancel",
            _ => action.label(),
        };
        render_button(frame, rect, label, false);
    }

    let footer = Paragraph::new("Enter to confirm, Esc to cancel")
        .block(Block::default().borders(Borders::ALL).title("Controls"));
    frame.render_widget(footer, layout.footer);
}

fn render_updating(frame: &mut Frame<'_>, app: &TuiApp) {
    let layout = simple_action_layout(frame.area());
    let selected = app
        .selected_device()
        .map(|d| format!("{} ({:04x}:{:04x})", d.name, d.vid_pid.vid, d.vid_pid.pid))
        .unwrap_or_else(|| "Unknown controller".to_owned());

    let body_lines = vec![
        Line::from("Firmware transfer in progress"),
        Line::from(""),
        Line::from(format!("Device: {selected}")),
        Line::from(format!("Status: {}", app.last_message)),
        Line::from(""),
        Line::from("Do not disconnect the controller during transfer."),
    ];

    let body =
        Paragraph::new(body_lines).block(Block::default().borders(Borders::ALL).title("Updating"));
    frame.render_widget(body, layout.body);

    let buttons = action_buttons(layout.actions, &[HomeAction::Quit]);
    for (rect, action) in buttons {
        let label = if action == HomeAction::Quit {
            "Cancel Update"
        } else {
            action.label()
        };
        render_button(frame, rect, label, false);
    }

    let footer = Gauge::default()
        .block(Block::default().title("Progress").borders(Borders::ALL))
        .gauge_style(Style::default().fg(Color::Green))
        .percent(app.progress as u16)
        .label(format!("{}%", app.progress));
    frame.render_widget(footer, layout.footer);
}

fn render_final_report(frame: &mut Frame<'_>, app: &TuiApp) {
    let layout = simple_action_layout(frame.area());

    let (status, message) = app
        .final_report
        .as_ref()
        .map(|report| {
            (
                format!("{:?}", report.status),
                format!(
                    "{}\nChunks sent: {}/{}",
                    report.message, report.chunks_sent, report.chunks_total
                ),
            )
        })
        .unwrap_or_else(|| ("Unknown".to_owned(), app.last_message.clone()));

    let body = Paragraph::new(vec![
        Line::from("Update finished"),
        Line::from(""),
        Line::from(format!("Result: {status}")),
        Line::from(message),
        Line::from(""),
        Line::from("Done returns to Home. Quit exits OpenBitdo."),
    ])
    .block(
        Block::default()
            .borders(Borders::ALL)
            .title("Final Summary"),
    );
    frame.render_widget(body, layout.body);

    let buttons = action_buttons(layout.actions, &[HomeAction::Refresh, HomeAction::Quit]);
    for (rect, action) in buttons {
        let label = if action == HomeAction::Refresh {
            "Done"
        } else {
            action.label()
        };
        render_button(frame, rect, label, false);
    }

    let footer = Paragraph::new(app.last_message.as_str())
        .block(Block::default().borders(Borders::ALL).title("Status"));
    frame.render_widget(footer, layout.footer);
}

pub async fn run_tui_flow(
    core: OpenBitdoCore,
    request: TuiRunRequest,
) -> Result<FirmwareFinalReport> {
    let mut app = TuiApp {
        state: TuiWorkflowState::Preflight,
        selected: Some(request.vid_pid),
        last_message: format!("preflighting {}", request.vid_pid),
        ..Default::default()
    };

    let mut terminal = if request.no_ui {
        None
    } else {
        Some(init_terminal()?)
    };
    render_if_needed(&mut terminal, &app)?;

    let preflight = core
        .preflight_firmware(FirmwarePreflightRequest {
            vid_pid: request.vid_pid,
            firmware_path: request.firmware_path.clone(),
            allow_unsafe: request.allow_unsafe,
            brick_risk_ack: request.brick_risk_ack,
            experimental: request.experimental,
            chunk_size: request.chunk_size,
        })
        .await?;

    if !preflight.gate.allowed {
        teardown_terminal(&mut terminal)?;
        return Err(anyhow!(
            "preflight denied: {}",
            preflight
                .gate
                .message
                .unwrap_or_else(|| "policy denied".to_owned())
        ));
    }

    let plan = preflight.plan.expect("plan exists when gate is allowed");
    app.set_session(plan.session_id.clone());
    render_if_needed(&mut terminal, &app)?;

    core.start_firmware(FirmwareStartRequest {
        session_id: plan.session_id.clone(),
    })
    .await?;

    core.confirm_firmware(FirmwareConfirmRequest {
        session_id: plan.session_id.clone(),
        acknowledged_risk: request.acknowledged_risk,
    })
    .await?;

    let mut receiver = core.subscribe_events(&plan.session_id.0).await?;
    loop {
        tokio::select! {
            evt = receiver.recv() => {
                if let Ok(evt) = evt {
                    app.apply_progress(evt.progress, format!("{}: {}", evt.stage, evt.message));
                    render_if_needed(&mut terminal, &app)?;
                    if evt.terminal {
                        break;
                    }
                }
            }
            _ = sleep(Duration::from_millis(10)) => {
                if let Some(report) = core.firmware_report(&plan.session_id.0).await? {
                    app.complete(report.clone());
                    render_if_needed(&mut terminal, &app)?;
                    teardown_terminal(&mut terminal)?;
                    return Ok(report);
                }
            }
        }

        if !request.no_ui && event::poll(Duration::from_millis(1))? {
            if let CEvent::Key(key) = event::read()? {
                if matches!(key.code, KeyCode::Char('q') | KeyCode::Esc) {
                    let report = core
                        .cancel_firmware(FirmwareCancelRequest {
                            session_id: plan.session_id.clone(),
                        })
                        .await?;
                    app.complete(report.clone());
                    render_if_needed(&mut terminal, &app)?;
                    teardown_terminal(&mut terminal)?;
                    return Ok(report);
                }
            }
        }
    }

    let report = core
        .firmware_report(&plan.session_id.0)
        .await?
        .ok_or_else(|| anyhow!("missing final report"))?;
    app.complete(report.clone());
    render_if_needed(&mut terminal, &app)?;
    teardown_terminal(&mut terminal)?;
    Ok(report)
}

fn init_terminal() -> Result<Terminal<CrosstermBackend<Stdout>>> {
    use crossterm::event::EnableMouseCapture;

    enable_raw_mode()?;
    let mut stdout = io::stdout();
    execute!(stdout, EnterAlternateScreen, EnableMouseCapture)?;
    let backend = CrosstermBackend::new(stdout);
    let terminal = Terminal::new(backend)?;
    Ok(terminal)
}

fn teardown_terminal(terminal: &mut Option<Terminal<CrosstermBackend<Stdout>>>) -> Result<()> {
    use crossterm::event::DisableMouseCapture;

    if let Some(mut t) = terminal.take() {
        disable_raw_mode()?;
        execute!(t.backend_mut(), LeaveAlternateScreen, DisableMouseCapture)?;
        t.show_cursor()?;
    }
    Ok(())
}

fn prompt_line(
    terminal: &mut Option<Terminal<CrosstermBackend<Stdout>>>,
    prompt: &str,
) -> Result<String> {
    let had_terminal = terminal.is_some();
    if had_terminal {
        teardown_terminal(terminal)?;
    }

    print!("{prompt}");
    io::stdout().flush()?;

    let mut line = String::new();
    io::stdin().read_line(&mut line)?;

    if had_terminal {
        *terminal = Some(init_terminal()?);
    }

    Ok(line.trim().to_owned())
}

fn handle_report_hotkey(app: &mut TuiApp, key: KeyCode) -> Result<bool> {
    let Some(path) = app.latest_report_path.clone() else {
        return Ok(false);
    };

    match key {
        KeyCode::Char('c') => {
            copy_text_to_clipboard(path.to_string_lossy().as_ref())?;
            app.last_message = format!(
                "Copied report path to clipboard: {}",
                path.to_string_lossy()
            );
            Ok(true)
        }
        KeyCode::Char('o') => {
            open_path_with_default_app(&path)?;
            app.last_message = format!("Opened report: {}", path.to_string_lossy());
            Ok(true)
        }
        KeyCode::Char('f') => {
            let folder = path
                .parent()
                .map(Path::to_path_buf)
                .unwrap_or_else(std::env::temp_dir);
            open_path_with_default_app(&folder)?;
            app.last_message = format!("Opened report folder: {}", folder.to_string_lossy());
            Ok(true)
        }
        _ => Ok(false),
    }
}

#[cfg(test)]
mod tests;
