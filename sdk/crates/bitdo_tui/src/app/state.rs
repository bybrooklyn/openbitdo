use crate::{AppDevice, BuildInfo, ReportSaveMode, UiLaunchOptions};
use bitdo_app_core::{
    ConfigBackupId, DedicatedButtonMapping, FirmwareFinalReport, FirmwareUpdatePlan, U2CoreProfile,
};
use bitdo_proto::{DiagCommandStatus, DiagProbeResult, DiagSeverity, SupportTier, VidPid};
use chrono::Utc;
use fuzzy_matcher::skim::SkimMatcherV2;
use fuzzy_matcher::FuzzyMatcher;
use serde::{Deserialize, Serialize};
use std::collections::VecDeque;
use std::path::PathBuf;

use super::action::QuickAction;

pub const EVENT_LOG_CAPACITY: usize = 200;

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Screen {
    Dashboard,
    Task,
    Diagnostics,
    MappingEditor,
    Recovery,
    Settings,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize, Default)]
#[serde(rename_all = "snake_case")]
pub enum DashboardLayoutMode {
    #[default]
    Wide,
    Compact,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize, Default)]
#[serde(rename_all = "snake_case")]
pub enum PanelFocus {
    #[default]
    Devices,
    QuickActions,
    EventLog,
    Status,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum EventLevel {
    Info,
    Warning,
    Error,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct EventEntry {
    pub timestamp_utc: String,
    pub level: EventLevel,
    pub message: String,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum TaskMode {
    Diagnostics,
    Preflight,
    Updating,
    Final,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct TaskState {
    pub mode: TaskMode,
    pub plan: Option<FirmwareUpdatePlan>,
    pub progress: u8,
    pub status: String,
    pub final_report: Option<FirmwareFinalReport>,
    pub downloaded_firmware_path: Option<PathBuf>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize, Default)]
#[serde(rename_all = "snake_case")]
pub enum DiagnosticsFilter {
    #[default]
    All,
    Issues,
    Experimental,
}

impl DiagnosticsFilter {
    pub const ALL: [DiagnosticsFilter; 3] = [
        DiagnosticsFilter::All,
        DiagnosticsFilter::Issues,
        DiagnosticsFilter::Experimental,
    ];

    pub fn label(self) -> &'static str {
        match self {
            DiagnosticsFilter::All => "All",
            DiagnosticsFilter::Issues => "Issues",
            DiagnosticsFilter::Experimental => "Experimental",
        }
    }

    pub fn matches(self, check: &DiagCommandStatus) -> bool {
        match self {
            DiagnosticsFilter::All => true,
            DiagnosticsFilter::Issues => !check.ok || check.severity != DiagSeverity::Ok,
            DiagnosticsFilter::Experimental => check.is_experimental,
        }
    }

    pub fn shift(self, delta: i32) -> Self {
        let current = Self::ALL
            .iter()
            .position(|candidate| *candidate == self)
            .unwrap_or(0) as i32;
        let len = Self::ALL.len() as i32;
        let mut next = current + delta;
        while next < 0 {
            next += len;
        }
        Self::ALL[(next as usize) % Self::ALL.len()]
    }
}

#[derive(Clone, Debug)]
pub struct DiagnosticsState {
    pub result: DiagProbeResult,
    pub summary: String,
    pub selected_check_index: usize,
    pub active_filter: DiagnosticsFilter,
    pub latest_report_path: Option<PathBuf>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum MappingEditorKind {
    Jp108,
    Ultimate2,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub enum MappingDraftState {
    Jp108 {
        loaded: Vec<DedicatedButtonMapping>,
        current: Vec<DedicatedButtonMapping>,
        undo_stack: Vec<Vec<DedicatedButtonMapping>>,
        selected_row: usize,
    },
    Ultimate2 {
        loaded: U2CoreProfile,
        current: U2CoreProfile,
        undo_stack: Vec<U2CoreProfile>,
        selected_row: usize,
    },
}

#[derive(Clone, Debug)]
pub struct QuickActionState {
    pub action: QuickAction,
    pub enabled: bool,
    pub reason: Option<String>,
}

#[derive(Clone, Debug)]
pub struct AppState {
    pub screen: Screen,
    pub build_info: BuildInfo,
    pub advanced_mode: bool,
    pub report_save_mode: ReportSaveMode,
    pub settings_path: Option<PathBuf>,
    pub dashboard_layout_mode: DashboardLayoutMode,
    pub last_panel_focus: PanelFocus,
    pub devices: Vec<AppDevice>,
    pub selected_device_id: Option<VidPid>,
    pub selected_filtered_index: usize,
    pub device_filter: String,
    pub quick_actions: Vec<QuickActionState>,
    pub selected_action_index: usize,
    pub event_log: VecDeque<EventEntry>,
    pub task_state: Option<TaskState>,
    pub diagnostics_state: Option<DiagnosticsState>,
    pub mapping_draft_state: Option<MappingDraftState>,
    pub latest_backup: Option<ConfigBackupId>,
    pub write_lock_until_restart: bool,
    pub latest_report_path: Option<PathBuf>,
    pub status_line: String,
    pub firmware_path_override: Option<PathBuf>,
    pub allow_unsafe: bool,
    pub brick_risk_ack: bool,
    pub experimental: bool,
    pub chunk_size: Option<usize>,
    pub quit_requested: bool,
}

impl AppState {
    pub fn new(opts: &UiLaunchOptions) -> Self {
        let mut state = Self {
            screen: Screen::Dashboard,
            build_info: opts.build_info.clone(),
            advanced_mode: opts.advanced_mode,
            report_save_mode: if !opts.advanced_mode && opts.report_save_mode == ReportSaveMode::Off
            {
                ReportSaveMode::FailureOnly
            } else {
                opts.report_save_mode
            },
            settings_path: opts.settings_path.clone(),
            dashboard_layout_mode: DashboardLayoutMode::Wide,
            last_panel_focus: PanelFocus::Devices,
            devices: Vec::new(),
            selected_device_id: None,
            selected_filtered_index: 0,
            device_filter: String::new(),
            quick_actions: Vec::new(),
            selected_action_index: 0,
            event_log: VecDeque::with_capacity(EVENT_LOG_CAPACITY),
            task_state: None,
            diagnostics_state: None,
            mapping_draft_state: None,
            latest_backup: None,
            write_lock_until_restart: false,
            latest_report_path: None,
            status_line: "OpenBitdo ready".to_owned(),
            firmware_path_override: opts.firmware_path.clone(),
            allow_unsafe: opts.allow_unsafe,
            brick_risk_ack: opts.brick_risk_ack,
            experimental: opts.experimental,
            chunk_size: opts.chunk_size,
            quit_requested: false,
        };
        state.recompute_quick_actions();
        state
    }

    pub fn set_layout_from_size(&mut self, width: u16, height: u16) {
        self.dashboard_layout_mode = if width < 80 || height < 24 {
            DashboardLayoutMode::Compact
        } else {
            DashboardLayoutMode::Wide
        };
    }

    pub fn filtered_device_indices(&self) -> Vec<usize> {
        if self.device_filter.trim().is_empty() {
            return (0..self.devices.len()).collect();
        }

        let query = self.device_filter.to_lowercase();
        let matcher = SkimMatcherV2::default();
        let mut scored: Vec<(i64, usize)> = self
            .devices
            .iter()
            .enumerate()
            .filter_map(|(idx, dev)| {
                let text = format!(
                    "{:04x}:{:04x} {}",
                    dev.vid_pid.vid,
                    dev.vid_pid.pid,
                    dev.name.to_lowercase()
                );
                matcher.fuzzy_match(&text, &query).map(|score| (score, idx))
            })
            .collect();
        scored.sort_by(|a, b| b.0.cmp(&a.0).then_with(|| a.1.cmp(&b.1)));
        scored.into_iter().map(|(_, idx)| idx).collect()
    }

    pub fn selected_device(&self) -> Option<&AppDevice> {
        self.selected_device_id
            .and_then(|id| self.devices.iter().find(|d| d.vid_pid == id))
            .or_else(|| self.devices.first())
    }

    pub fn select_filtered_index(&mut self, index: usize) {
        let filtered = self.filtered_device_indices();
        if let Some(device_idx) = filtered.get(index).copied() {
            self.selected_filtered_index = index;
            self.selected_device_id = Some(self.devices[device_idx].vid_pid);
            self.recompute_quick_actions();
        }
    }

    pub fn select_next_device(&mut self) {
        let filtered = self.filtered_device_indices();
        if filtered.is_empty() {
            return;
        }
        self.selected_filtered_index = (self.selected_filtered_index + 1) % filtered.len();
        self.select_filtered_index(self.selected_filtered_index);
    }

    pub fn select_prev_device(&mut self) {
        let filtered = self.filtered_device_indices();
        if filtered.is_empty() {
            return;
        }
        if self.selected_filtered_index == 0 {
            self.selected_filtered_index = filtered.len().saturating_sub(1);
        } else {
            self.selected_filtered_index -= 1;
        }
        self.select_filtered_index(self.selected_filtered_index);
    }

    pub fn append_event(&mut self, level: EventLevel, message: impl Into<String>) {
        if self.event_log.len() >= EVENT_LOG_CAPACITY {
            self.event_log.pop_front();
        }
        self.event_log.push_back(EventEntry {
            timestamp_utc: Utc::now().format("%H:%M:%S").to_string(),
            level,
            message: message.into(),
        });
    }

    pub fn set_status(&mut self, message: impl Into<String>) {
        self.status_line = message.into();
    }

    pub fn select_next_action(&mut self) {
        if self.quick_actions.is_empty() {
            return;
        }
        self.selected_action_index = (self.selected_action_index + 1) % self.quick_actions.len();
    }

    pub fn select_prev_action(&mut self) {
        if self.quick_actions.is_empty() {
            return;
        }
        if self.selected_action_index == 0 {
            self.selected_action_index = self.quick_actions.len().saturating_sub(1);
        } else {
            self.selected_action_index -= 1;
        }
    }

    pub fn selected_action(&self) -> Option<QuickAction> {
        self.quick_actions
            .get(self.selected_action_index)
            .filter(|a| a.enabled)
            .map(|a| a.action)
    }

    pub fn diagnostics_filtered_indices(&self) -> Vec<usize> {
        let Some(diagnostics) = self.diagnostics_state.as_ref() else {
            return Vec::new();
        };

        diagnostics
            .result
            .command_checks
            .iter()
            .enumerate()
            .filter_map(|(idx, check)| diagnostics.active_filter.matches(check).then_some(idx))
            .collect()
    }

    pub fn selected_diagnostics_check(&self) -> Option<&DiagCommandStatus> {
        let diagnostics = self.diagnostics_state.as_ref()?;
        diagnostics
            .result
            .command_checks
            .get(diagnostics.selected_check_index)
    }

    pub fn select_diagnostics_filtered_index(&mut self, filtered_index: usize) {
        let filtered = self.diagnostics_filtered_indices();
        if let Some(check_index) = filtered.get(filtered_index).copied() {
            if let Some(diagnostics) = self.diagnostics_state.as_mut() {
                diagnostics.selected_check_index = check_index;
            }
        }
    }

    pub fn select_next_diagnostics_check(&mut self) {
        let filtered = self.diagnostics_filtered_indices();
        if filtered.is_empty() {
            return;
        }

        let current = self
            .diagnostics_state
            .as_ref()
            .and_then(|diagnostics| {
                filtered
                    .iter()
                    .position(|idx| *idx == diagnostics.selected_check_index)
            })
            .unwrap_or(0);
        let next = (current + 1) % filtered.len();
        self.select_diagnostics_filtered_index(next);
    }

    pub fn select_prev_diagnostics_check(&mut self) {
        let filtered = self.diagnostics_filtered_indices();
        if filtered.is_empty() {
            return;
        }

        let current = self
            .diagnostics_state
            .as_ref()
            .and_then(|diagnostics| {
                filtered
                    .iter()
                    .position(|idx| *idx == diagnostics.selected_check_index)
            })
            .unwrap_or(0);
        let next = if current == 0 {
            filtered.len().saturating_sub(1)
        } else {
            current - 1
        };
        self.select_diagnostics_filtered_index(next);
    }

    pub fn set_diagnostics_filter(&mut self, filter: DiagnosticsFilter) {
        if let Some(diagnostics) = self.diagnostics_state.as_mut() {
            diagnostics.active_filter = filter;
        }
        self.ensure_diagnostics_selection();
    }

    pub fn shift_diagnostics_filter(&mut self, delta: i32) {
        if let Some(diagnostics) = self.diagnostics_state.as_mut() {
            diagnostics.active_filter = diagnostics.active_filter.shift(delta);
        }
        self.ensure_diagnostics_selection();
    }

    pub fn ensure_diagnostics_selection(&mut self) {
        let filtered = self.diagnostics_filtered_indices();
        let Some(diagnostics) = self.diagnostics_state.as_mut() else {
            return;
        };

        if filtered.is_empty() {
            diagnostics.selected_check_index = 0;
        } else if !filtered.contains(&diagnostics.selected_check_index) {
            diagnostics.selected_check_index = filtered[0];
        }
    }

    pub fn recompute_quick_actions(&mut self) {
        let firmware_enabled = self.allow_unsafe && self.brick_risk_ack;
        self.quick_actions = if matches!(self.screen, Screen::Dashboard) {
            let actions = vec![
                QuickActionState {
                    action: QuickAction::Refresh,
                    enabled: true,
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::Diagnose,
                    enabled: self.selected_device().is_some(),
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::RecommendedUpdate,
                    enabled: self
                        .selected_device()
                        .map(|d| d.support_tier == SupportTier::Full)
                        .unwrap_or(false)
                        && firmware_enabled
                        && !self.write_lock_until_restart,
                    reason: self.selected_device().and_then(|d| {
                        if !firmware_enabled {
                            Some("Firmware updates require explicit unsafe acknowledgement".to_owned())
                        } else if d.support_tier != SupportTier::Full {
                            Some("Read-only until hardware confirmation".to_owned())
                        } else if self.write_lock_until_restart {
                            Some("Write locked until restart".to_owned())
                        } else {
                            None
                        }
                    }),
                },
                QuickActionState {
                    action: QuickAction::EditMappings,
                    enabled: self
                        .selected_device()
                        .map(|d| {
                            (d.capability.supports_jp108_dedicated_map
                                || (d.capability.supports_u2_button_map
                                    && d.capability.supports_u2_slot_config))
                                && d.support_tier == SupportTier::Full
                                && !self.write_lock_until_restart
                        })
                        .unwrap_or(false),
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::Settings,
                    enabled: true,
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::Quit,
                    enabled: true,
                    reason: None,
                },
            ];
            if self.selected_action_index >= actions.len() {
                self.selected_action_index = 0;
            }
            actions
        } else if matches!(self.screen, Screen::Task) {
            vec![
                QuickActionState {
                    action: QuickAction::Confirm,
                    enabled: self
                        .task_state
                        .as_ref()
                        .map(|task| matches!(task.mode, TaskMode::Preflight))
                        .unwrap_or(false),
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::Cancel,
                    enabled: true,
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::Back,
                    enabled: true,
                    reason: None,
                },
            ]
        } else if matches!(self.screen, Screen::Diagnostics) {
            vec![
                QuickActionState {
                    action: QuickAction::RunAgain,
                    enabled: self.selected_device().is_some(),
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::SaveReport,
                    enabled: self.diagnostics_state.is_some(),
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::Back,
                    enabled: true,
                    reason: None,
                },
            ]
        } else if matches!(self.screen, Screen::MappingEditor) {
            vec![
                QuickActionState {
                    action: QuickAction::ApplyDraft,
                    enabled: !self.write_lock_until_restart,
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::UndoDraft,
                    enabled: self.mapping_can_undo(),
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::ResetDraft,
                    enabled: self.mapping_has_changes(),
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::RestoreBackup,
                    enabled: self.latest_backup.is_some(),
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::Firmware,
                    enabled: firmware_enabled && !self.write_lock_until_restart,
                    reason: if firmware_enabled {
                        None
                    } else {
                        Some("Firmware updates require explicit unsafe acknowledgement".to_owned())
                    },
                },
                QuickActionState {
                    action: QuickAction::Back,
                    enabled: true,
                    reason: None,
                },
            ]
        } else if matches!(self.screen, Screen::Recovery) {
            vec![
                QuickActionState {
                    action: QuickAction::RestoreBackup,
                    enabled: self.latest_backup.is_some(),
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::Back,
                    enabled: true,
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::Quit,
                    enabled: true,
                    reason: None,
                },
            ]
        } else {
            vec![
                QuickActionState {
                    action: QuickAction::Back,
                    enabled: true,
                    reason: None,
                },
                QuickActionState {
                    action: QuickAction::Quit,
                    enabled: true,
                    reason: None,
                },
            ]
        };
        if self.selected_action_index >= self.quick_actions.len() {
            self.selected_action_index = 0;
        }
    }

    pub fn mapping_can_undo(&self) -> bool {
        match self.mapping_draft_state.as_ref() {
            Some(MappingDraftState::Jp108 { undo_stack, .. }) => !undo_stack.is_empty(),
            Some(MappingDraftState::Ultimate2 { undo_stack, .. }) => !undo_stack.is_empty(),
            None => false,
        }
    }

    pub fn mapping_has_changes(&self) -> bool {
        match self.mapping_draft_state.as_ref() {
            Some(MappingDraftState::Jp108 {
                loaded, current, ..
            }) => loaded != current,
            Some(MappingDraftState::Ultimate2 {
                loaded, current, ..
            }) => loaded != current,
            None => false,
        }
    }
}
