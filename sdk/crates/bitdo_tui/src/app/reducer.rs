use super::action::QuickAction;
use super::effect::{Effect, MappingApplyDraft};
use super::event::AppEvent;
use super::state::{
    AppState, DiagnosticsFilter, DiagnosticsState, EventLevel, MappingDraftState, PanelFocus,
    Screen, TaskMode, TaskState,
};

pub fn reduce(state: &mut AppState, event: AppEvent) -> Vec<Effect> {
    let mut effects = Vec::new();

    match event {
        AppEvent::Init => {
            state.append_event(EventLevel::Info, "Initializing dashboard");
            effects.push(Effect::RefreshDevices);
        }
        AppEvent::Tick => {
            if let Some(task) = state.task_state.as_ref() {
                if matches!(task.mode, TaskMode::Updating) {
                    if let Some(plan) = task.plan.as_ref() {
                        effects.push(Effect::PollFirmwareReport {
                            session_id: plan.session_id.clone(),
                        });
                    }
                }
            }
        }
        AppEvent::DeviceFilterSet(next) => {
            state.device_filter = next;
            state.selected_filtered_index = 0;
            state.last_panel_focus = PanelFocus::Devices;
            state.select_filtered_index(0);
            if let Some(effect) = persist_settings_effect(state) {
                effects.push(effect);
            }
        }
        AppEvent::DeviceFilterInput(ch) => {
            state.device_filter.push(ch);
            state.selected_filtered_index = 0;
            state.select_filtered_index(0);
            if let Some(effect) = persist_settings_effect(state) {
                effects.push(effect);
            }
        }
        AppEvent::DeviceFilterBackspace => {
            state.device_filter.pop();
            state.selected_filtered_index = 0;
            state.select_filtered_index(0);
            if let Some(effect) = persist_settings_effect(state) {
                effects.push(effect);
            }
        }
        AppEvent::SelectFilteredDevice(index) => {
            state.select_filtered_index(index);
            state.last_panel_focus = PanelFocus::Devices;
            state.recompute_quick_actions();
        }
        AppEvent::SelectNextDevice => {
            state.select_next_device();
        }
        AppEvent::SelectPrevDevice => {
            state.select_prev_device();
        }
        AppEvent::SelectNextAction => {
            state.select_next_action();
            state.last_panel_focus = PanelFocus::QuickActions;
        }
        AppEvent::SelectPrevAction => {
            state.select_prev_action();
            state.last_panel_focus = PanelFocus::QuickActions;
        }
        AppEvent::DiagnosticsSelectCheck(index) => {
            state.select_diagnostics_filtered_index(index);
        }
        AppEvent::DiagnosticsSelectNextCheck => {
            state.select_next_diagnostics_check();
        }
        AppEvent::DiagnosticsSelectPrevCheck => {
            state.select_prev_diagnostics_check();
        }
        AppEvent::DiagnosticsShiftFilter(delta) => {
            state.shift_diagnostics_filter(delta);
        }
        AppEvent::DiagnosticsSetFilter(filter) => {
            state.set_diagnostics_filter(filter);
        }
        AppEvent::TriggerAction(action) => {
            effects.extend(handle_action(state, action));
        }
        AppEvent::ConfirmPrimary => {
            if let Some(action) = state.selected_action() {
                effects.extend(handle_action(state, action));
            }
        }
        AppEvent::Back => {
            let keep_task_state = state
                .task_state
                .as_ref()
                .map(|task| task.mode == TaskMode::Updating)
                .unwrap_or(false);
            if let Some(path) = take_cleanup_path_for_navigation(state) {
                effects.push(Effect::DeleteTempFile { path });
            }
            state.screen = Screen::Dashboard;
            if !keep_task_state {
                state.task_state = None;
            } else {
                state.set_status("Firmware update continues in background");
            }
            state.diagnostics_state = None;
            state.mapping_draft_state = None;
            state.recompute_quick_actions();
        }
        AppEvent::Quit => {
            state.quit_requested = true;
        }
        AppEvent::ToggleAdvancedMode => {
            state.advanced_mode = !state.advanced_mode;
            if !state.advanced_mode && state.report_save_mode == crate::ReportSaveMode::Off {
                state.report_save_mode = crate::ReportSaveMode::FailureOnly;
            }
            state.append_event(
                EventLevel::Info,
                if state.advanced_mode {
                    "Advanced mode enabled"
                } else {
                    "Advanced mode disabled"
                },
            );
            if let Some(effect) = persist_settings_effect(state) {
                effects.push(effect);
            }
            state.recompute_quick_actions();
        }
        AppEvent::CycleReportSaveMode => {
            state.report_save_mode = state.report_save_mode.next(state.advanced_mode);
            state.append_event(
                EventLevel::Info,
                format!(
                    "Report save mode changed to {}",
                    state.report_save_mode.as_str()
                ),
            );
            if let Some(effect) = persist_settings_effect(state) {
                effects.push(effect);
            }
        }
        AppEvent::MappingMoveSelection(delta) => {
            if let Some(mapping) = state.mapping_draft_state.as_mut() {
                match mapping {
                    MappingDraftState::Jp108 {
                        selected_row,
                        current,
                        ..
                    } => {
                        if current.is_empty() {
                            return effects;
                        }
                        let len = current.len() as i32;
                        let mut idx = *selected_row as i32 + delta;
                        while idx < 0 {
                            idx += len;
                        }
                        *selected_row = (idx % len) as usize;
                    }
                    MappingDraftState::Ultimate2 {
                        selected_row,
                        current,
                        ..
                    } => {
                        if current.mappings.is_empty() {
                            return effects;
                        }
                        let len = current.mappings.len() as i32;
                        let mut idx = *selected_row as i32 + delta;
                        while idx < 0 {
                            idx += len;
                        }
                        *selected_row = (idx % len) as usize;
                    }
                }
            }
            state.recompute_quick_actions();
        }
        AppEvent::MappingAdjust(delta) => {
            adjust_mapping(state, delta);
            state.recompute_quick_actions();
        }
        AppEvent::DevicesLoaded(devices) => {
            state.devices = devices;
            let filtered = state.filtered_device_indices();
            if filtered.is_empty() {
                state.selected_device_id = None;
                state.selected_filtered_index = 0;
                state.set_status("No controller detected");
            } else {
                let selected = filtered
                    .get(state.selected_filtered_index)
                    .copied()
                    .unwrap_or(filtered[0]);
                state.selected_filtered_index =
                    state.selected_filtered_index.min(filtered.len() - 1);
                state.selected_device_id = Some(state.devices[selected].vid_pid);
                state.set_status("Controllers refreshed");
            }
            state.append_event(
                EventLevel::Info,
                format!("Device refresh complete ({} found)", state.devices.len()),
            );
            state.recompute_quick_actions();
        }
        AppEvent::DevicesLoadFailed(err) => {
            state.set_status(format!("Refresh failed: {err}"));
            state.append_event(EventLevel::Error, format!("Refresh failed: {err}"));
        }
        AppEvent::DiagnosticsCompleted {
            vid_pid,
            result,
            summary,
        } => {
            let check_count = result.command_checks.len();
            state.screen = Screen::Diagnostics;
            state.task_state = None;
            state.diagnostics_state = Some(DiagnosticsState {
                result: result.clone(),
                summary: summary.clone(),
                selected_check_index: 0,
                active_filter: DiagnosticsFilter::All,
                latest_report_path: None,
            });
            state.ensure_diagnostics_selection();
            state.set_status("Diagnostics complete");
            state.append_event(
                EventLevel::Info,
                format!("Diagnostics complete for {vid_pid} ({check_count} checks)"),
            );
            if crate::should_save_support_report(state.report_save_mode, false) {
                effects.push(Effect::PersistSupportReport {
                    operation: "diag-probe".to_owned(),
                    vid_pid: Some(vid_pid),
                    status: "ok".to_owned(),
                    message: summary,
                    diag: Some(result),
                    firmware: None,
                });
            }
            state.recompute_quick_actions();
        }
        AppEvent::DiagnosticsFailed { vid_pid, error } => {
            state.screen = Screen::Task;
            state.diagnostics_state = None;
            state.task_state = Some(TaskState {
                mode: TaskMode::Final,
                plan: None,
                progress: 100,
                status: format!("Diagnostics failed for {vid_pid}: {error}"),
                final_report: None,
                downloaded_firmware_path: None,
            });
            state.set_status("Diagnostics failed");
            state.append_event(
                EventLevel::Error,
                format!("Diagnostics failed for {vid_pid}: {error}"),
            );
            if crate::should_save_support_report(state.report_save_mode, true) {
                effects.push(Effect::PersistSupportReport {
                    operation: "diag-probe".to_owned(),
                    vid_pid: Some(vid_pid),
                    status: "failed".to_owned(),
                    message: error,
                    diag: None,
                    firmware: None,
                });
            }
            state.recompute_quick_actions();
        }
        AppEvent::MappingsLoadedJp108 { vid_pid, mappings } => {
            state.screen = Screen::MappingEditor;
            state.mapping_draft_state = Some(MappingDraftState::Jp108 {
                loaded: mappings.clone(),
                current: mappings,
                undo_stack: Vec::new(),
                selected_row: 0,
            });
            state.append_event(
                EventLevel::Info,
                format!("Loaded JP108 mappings for {vid_pid}"),
            );
            state.set_status("Mapping draft loaded");
            state.recompute_quick_actions();
        }
        AppEvent::MappingsLoadedUltimate2 { vid_pid, profile } => {
            state.screen = Screen::MappingEditor;
            state.mapping_draft_state = Some(MappingDraftState::Ultimate2 {
                loaded: profile.clone(),
                current: profile,
                undo_stack: Vec::new(),
                selected_row: 0,
            });
            state.append_event(
                EventLevel::Info,
                format!("Loaded Ultimate2 profile mapping for {vid_pid}"),
            );
            state.set_status("Mapping draft loaded");
            state.recompute_quick_actions();
        }
        AppEvent::MappingLoadFailed(err) => {
            state.set_status(format!("Mapping load failed: {err}"));
            state.append_event(EventLevel::Error, format!("Mapping load failed: {err}"));
        }
        AppEvent::MappingApplied {
            backup_id,
            message,
            recovery_lock,
        } => {
            state.latest_backup = backup_id;
            if recovery_lock {
                state.write_lock_until_restart = true;
                state.screen = Screen::Recovery;
                state.set_status("Write lock enabled until restart");
                state.append_event(EventLevel::Error, message);
            } else {
                state.set_status("Mapping applied");
                state.append_event(EventLevel::Info, message);
            }
            state.recompute_quick_actions();
        }
        AppEvent::MappingApplyFailed(err) => {
            state.set_status(format!("Apply failed: {err}"));
            state.append_event(EventLevel::Error, format!("Apply failed: {err}"));
        }
        AppEvent::BackupRestoreCompleted(message) => {
            state.set_status("Backup restored");
            state.append_event(EventLevel::Info, message);
        }
        AppEvent::BackupRestoreFailed(message) => {
            state.set_status("Backup restore failed");
            state.append_event(EventLevel::Error, message);
        }
        AppEvent::PreflightReady {
            vid_pid,
            firmware_path,
            source,
            version,
            plan,
            downloaded_firmware_path,
        } => {
            state.screen = Screen::Task;
            state.task_state = Some(TaskState {
                mode: TaskMode::Preflight,
                plan: Some(plan.clone()),
                progress: 0,
                status: format!(
                    "Ready to update {vid_pid} to {version} from {}",
                    firmware_path.display()
                ),
                final_report: None,
                downloaded_firmware_path,
            });
            state.append_event(
                EventLevel::Info,
                format!("Preflight passed ({source}, {version})"),
            );
            state.set_status("Preflight ready, confirm to acknowledge risk and start");
            state.recompute_quick_actions();
        }
        AppEvent::PreflightBlocked(reason) => {
            state.screen = Screen::Task;
            state.task_state = Some(TaskState {
                mode: TaskMode::Final,
                plan: None,
                progress: 100,
                status: format!("Preflight blocked: {reason}"),
                final_report: None,
                downloaded_firmware_path: None,
            });
            state.set_status("Preflight blocked");
            state.append_event(EventLevel::Warning, reason);
            state.recompute_quick_actions();
        }
        AppEvent::UpdateStarted {
            session_id,
            source,
            version,
        } => {
            if let Some(task) = state.task_state.as_mut() {
                task.mode = TaskMode::Updating;
                task.progress = 1;
                task.status =
                    format!("Session {session_id}: updating from {source} (target {version})");
            } else {
                state.task_state = Some(TaskState {
                    mode: TaskMode::Updating,
                    plan: None,
                    progress: 1,
                    status: format!("Session {session_id}: update started"),
                    final_report: None,
                    downloaded_firmware_path: None,
                });
            }
            state.screen = Screen::Task;
            state.append_event(EventLevel::Info, "Firmware transfer started");
            state.set_status("Firmware update in progress");
            state.recompute_quick_actions();
        }
        AppEvent::UpdateProgress(progress_event) => {
            if let Some(task) = state.task_state.as_mut() {
                task.mode = TaskMode::Updating;
                task.progress = progress_event.progress;
                task.status = format!("{}: {}", progress_event.stage, progress_event.message);
            }
            state.append_event(
                EventLevel::Info,
                format!(
                    "{}% {}: {}",
                    progress_event.progress, progress_event.stage, progress_event.message
                ),
            );
        }
        AppEvent::UpdateFinished(report) => {
            let downloaded_firmware_path = take_any_downloaded_firmware_path(state);
            let failed = report.status != bitdo_app_core::FirmwareOutcome::Completed;
            state.screen = Screen::Task;
            state.task_state = Some(TaskState {
                mode: TaskMode::Final,
                plan: None,
                progress: 100,
                status: format!("Update {:?}: {}", report.status, report.message),
                final_report: Some(report.clone()),
                downloaded_firmware_path: None,
            });
            state.set_status(format!("Update {:?}", report.status));
            state.append_event(
                if failed {
                    EventLevel::Error
                } else {
                    EventLevel::Info
                },
                format!(
                    "Update {:?} (chunks {}/{})",
                    report.status, report.chunks_sent, report.chunks_total
                ),
            );
            if crate::should_save_support_report(state.report_save_mode, failed) {
                effects.push(Effect::PersistSupportReport {
                    operation: "fw-write".to_owned(),
                    vid_pid: state.selected_device().map(|d| d.vid_pid),
                    status: if failed {
                        "failed".to_owned()
                    } else {
                        "completed".to_owned()
                    },
                    message: report.message.clone(),
                    diag: None,
                    firmware: Some(report),
                });
            }
            if let Some(path) = downloaded_firmware_path {
                effects.push(Effect::DeleteTempFile { path });
            }
            state.recompute_quick_actions();
        }
        AppEvent::UpdateFailed(err) => {
            let downloaded_firmware_path = take_any_downloaded_firmware_path(state);
            state.screen = Screen::Task;
            state.task_state = Some(TaskState {
                mode: TaskMode::Final,
                plan: None,
                progress: 100,
                status: format!("Update failed: {err}"),
                final_report: None,
                downloaded_firmware_path: None,
            });
            state.set_status("Update failed");
            state.append_event(EventLevel::Error, format!("Update failed: {err}"));
            if let Some(path) = downloaded_firmware_path {
                effects.push(Effect::DeleteTempFile { path });
            }
            state.recompute_quick_actions();
        }
        AppEvent::SettingsPersisted => {
            state.append_event(EventLevel::Info, "Settings saved");
        }
        AppEvent::SupportReportSaved(path) => {
            state.latest_report_path = Some(path.clone());
            if let Some(diagnostics) = state.diagnostics_state.as_mut() {
                diagnostics.latest_report_path = Some(path.clone());
            }
            state.append_event(
                EventLevel::Info,
                format!("Support report saved: {}", path.display()),
            );
        }
        AppEvent::Error(message) => {
            state.set_status(message.clone());
            state.append_event(EventLevel::Error, message);
        }
    }

    effects
}

fn handle_action(state: &mut AppState, action: QuickAction) -> Vec<Effect> {
    let mut effects = Vec::new();
    match state.screen {
        Screen::Dashboard => match action {
            QuickAction::Refresh => {
                effects.push(Effect::RefreshDevices);
            }
            QuickAction::Diagnose => {
                if let Some(vid_pid) = state.selected_device().map(|d| d.vid_pid) {
                    state.screen = Screen::Task;
                    state.task_state = Some(TaskState {
                        mode: TaskMode::Diagnostics,
                        plan: None,
                        progress: 5,
                        status: format!("Running diagnostics for {vid_pid}"),
                        final_report: None,
                        downloaded_firmware_path: None,
                    });
                    state.diagnostics_state = None;
                    effects.push(Effect::RunDiagnostics { vid_pid });
                }
            }
            QuickAction::RecommendedUpdate => {
                if let Some(vid_pid) = state.selected_device().map(|d| d.vid_pid) {
                    state.screen = Screen::Task;
                    state.task_state = Some(TaskState {
                        mode: TaskMode::Preflight,
                        plan: None,
                        progress: 0,
                        status: format!("Preparing preflight for {vid_pid}"),
                        final_report: None,
                        downloaded_firmware_path: None,
                    });
                    effects.push(Effect::PreparePreflight {
                        vid_pid,
                        firmware_path_override: state.firmware_path_override.clone(),
                        allow_unsafe: true,
                        brick_risk_ack: true,
                        experimental: state.experimental,
                        chunk_size: state.chunk_size,
                    });
                }
            }
            QuickAction::EditMappings => {
                if let Some(vid_pid) = state.selected_device().map(|d| d.vid_pid) {
                    effects.push(Effect::LoadMappings { vid_pid });
                }
            }
            QuickAction::Settings => {
                state.screen = Screen::Settings;
            }
            QuickAction::Quit => state.quit_requested = true,
            _ => {}
        },
        Screen::Task => match action {
            QuickAction::Confirm => {
                if let Some(task) = state.task_state.as_ref() {
                    if let Some(plan) = task.plan.as_ref() {
                        effects.push(Effect::StartFirmware {
                            session_id: plan.session_id.clone(),
                            acknowledged_risk: true,
                        });
                    }
                }
            }
            QuickAction::Cancel => {
                if let Some(task) = state.task_state.as_ref() {
                    if task.mode == TaskMode::Updating {
                        if let Some(plan) = task.plan.as_ref() {
                            effects.push(Effect::CancelFirmware {
                                session_id: plan.session_id.clone(),
                            });
                        } else {
                            state.screen = Screen::Dashboard;
                            state.task_state = None;
                        }
                    } else {
                        if let Some(path) = take_cleanup_path_for_navigation(state) {
                            effects.push(Effect::DeleteTempFile { path });
                        }
                        state.screen = Screen::Dashboard;
                        state.task_state = None;
                    }
                } else {
                    state.screen = Screen::Dashboard;
                }
            }
            QuickAction::Back => {
                state.screen = Screen::Dashboard;
                if state
                    .task_state
                    .as_ref()
                    .map(|task| task.mode == TaskMode::Updating)
                    .unwrap_or(false)
                {
                    state.set_status("Firmware update continues in background");
                } else {
                    if let Some(path) = take_cleanup_path_for_navigation(state) {
                        effects.push(Effect::DeleteTempFile { path });
                    }
                    state.task_state = None;
                }
            }
            QuickAction::Quit => state.quit_requested = true,
            _ => {}
        },
        Screen::Diagnostics => match action {
            QuickAction::RunAgain => {
                let vid_pid = state
                    .diagnostics_state
                    .as_ref()
                    .map(|diagnostics| diagnostics.result.target)
                    .or_else(|| state.selected_device().map(|device| device.vid_pid));
                if let Some(vid_pid) = vid_pid {
                    state.screen = Screen::Task;
                    state.task_state = Some(TaskState {
                        mode: TaskMode::Diagnostics,
                        plan: None,
                        progress: 5,
                        status: format!("Running diagnostics for {vid_pid}"),
                        final_report: None,
                        downloaded_firmware_path: None,
                    });
                    state.diagnostics_state = None;
                    effects.push(Effect::RunDiagnostics { vid_pid });
                }
            }
            QuickAction::SaveReport => {
                if let Some(diagnostics) = state.diagnostics_state.as_ref() {
                    let has_issues =
                        diagnostics.result.command_checks.iter().any(|check| {
                            !check.ok || check.severity != bitdo_proto::DiagSeverity::Ok
                        });
                    let target = diagnostics.result.target;
                    let result = diagnostics.result.clone();
                    let summary = diagnostics.summary.clone();
                    state.set_status("Saving diagnostics report");
                    effects.push(Effect::PersistSupportReport {
                        operation: "diag-probe".to_owned(),
                        vid_pid: Some(target),
                        status: if has_issues {
                            "attention".to_owned()
                        } else {
                            "ok".to_owned()
                        },
                        message: summary,
                        diag: Some(result),
                        firmware: None,
                    });
                }
            }
            QuickAction::Back => {
                state.screen = Screen::Dashboard;
                state.task_state = None;
                state.diagnostics_state = None;
            }
            QuickAction::Quit => state.quit_requested = true,
            _ => {}
        },
        Screen::MappingEditor => match action {
            QuickAction::ApplyDraft => {
                if let Some(vid_pid) = state.selected_device().map(|d| d.vid_pid) {
                    if let Some(draft) = state.mapping_draft_state.as_ref() {
                        let payload = match draft {
                            MappingDraftState::Jp108 { current, .. } => {
                                MappingApplyDraft::Jp108(current.clone())
                            }
                            MappingDraftState::Ultimate2 { current, .. } => {
                                MappingApplyDraft::Ultimate2(current.clone())
                            }
                        };
                        effects.push(Effect::ApplyMappings {
                            vid_pid,
                            draft: payload,
                        });
                    }
                }
            }
            QuickAction::UndoDraft => {
                mapping_undo(state);
            }
            QuickAction::ResetDraft => {
                mapping_reset(state);
            }
            QuickAction::RestoreBackup => {
                if let Some(backup) = state.latest_backup.clone() {
                    effects.push(Effect::RestoreBackup { backup_id: backup });
                }
            }
            QuickAction::Firmware => {
                if let Some(vid_pid) = state.selected_device().map(|d| d.vid_pid) {
                    state.screen = Screen::Task;
                    state.task_state = Some(TaskState {
                        mode: TaskMode::Preflight,
                        plan: None,
                        progress: 0,
                        status: format!("Preparing preflight for {vid_pid}"),
                        final_report: None,
                        downloaded_firmware_path: None,
                    });
                    effects.push(Effect::PreparePreflight {
                        vid_pid,
                        firmware_path_override: state.firmware_path_override.clone(),
                        allow_unsafe: true,
                        brick_risk_ack: true,
                        experimental: state.experimental,
                        chunk_size: state.chunk_size,
                    });
                }
            }
            QuickAction::Back => {
                state.screen = Screen::Dashboard;
                state.mapping_draft_state = None;
            }
            QuickAction::Quit => state.quit_requested = true,
            _ => {}
        },
        Screen::Recovery => match action {
            QuickAction::RestoreBackup => {
                if let Some(backup) = state.latest_backup.clone() {
                    effects.push(Effect::RestoreBackup { backup_id: backup });
                }
            }
            QuickAction::Back => {
                state.screen = Screen::Dashboard;
            }
            QuickAction::Quit => state.quit_requested = true,
            _ => {}
        },
        Screen::Settings => match action {
            QuickAction::Back => state.screen = Screen::Dashboard,
            QuickAction::Quit => state.quit_requested = true,
            _ => {}
        },
    }

    state.recompute_quick_actions();
    effects
}

fn persist_settings_effect(state: &AppState) -> Option<Effect> {
    state
        .settings_path
        .clone()
        .map(|path| Effect::PersistSettings {
            path,
            advanced_mode: state.advanced_mode,
            report_save_mode: state.report_save_mode,
            device_filter_text: state.device_filter.clone(),
            dashboard_layout_mode: state.dashboard_layout_mode,
            last_panel_focus: state.last_panel_focus,
        })
}

fn mapping_undo(state: &mut AppState) {
    match state.mapping_draft_state.as_mut() {
        Some(MappingDraftState::Jp108 {
            current,
            undo_stack,
            ..
        }) => {
            if let Some(previous) = undo_stack.pop() {
                *current = previous;
            }
        }
        Some(MappingDraftState::Ultimate2 {
            current,
            undo_stack,
            ..
        }) => {
            if let Some(previous) = undo_stack.pop() {
                *current = previous;
            }
        }
        None => {}
    }
}

fn mapping_reset(state: &mut AppState) {
    match state.mapping_draft_state.as_mut() {
        Some(MappingDraftState::Jp108 {
            loaded,
            current,
            undo_stack,
            ..
        }) => {
            undo_stack.push(current.clone());
            *current = loaded.clone();
        }
        Some(MappingDraftState::Ultimate2 {
            loaded,
            current,
            undo_stack,
            ..
        }) => {
            undo_stack.push(current.clone());
            *current = loaded.clone();
        }
        None => {}
    }
}

fn adjust_mapping(state: &mut AppState, delta: i32) {
    match state.mapping_draft_state.as_mut() {
        Some(MappingDraftState::Jp108 {
            current,
            undo_stack,
            selected_row,
            ..
        }) => {
            if *selected_row < current.len() {
                undo_stack.push(current.clone());
                let entry = &mut current[*selected_row];
                entry.target_hid_usage = cycle_jp108(entry.target_hid_usage, delta);
            }
        }
        Some(MappingDraftState::Ultimate2 {
            current,
            undo_stack,
            selected_row,
            ..
        }) => {
            if *selected_row < current.mappings.len() {
                undo_stack.push(current.clone());
                let entry = &mut current.mappings[*selected_row];
                entry.target_hid_usage = cycle_u2(entry.target_hid_usage, delta);
            }
        }
        None => {}
    }
}

fn take_any_downloaded_firmware_path(state: &mut AppState) -> Option<std::path::PathBuf> {
    state
        .task_state
        .as_mut()
        .and_then(|task| task.downloaded_firmware_path.take())
}

fn take_cleanup_path_for_navigation(state: &mut AppState) -> Option<std::path::PathBuf> {
    let should_cleanup = state
        .task_state
        .as_ref()
        .map(|task| task.mode != TaskMode::Updating)
        .unwrap_or(false);

    if should_cleanup {
        take_any_downloaded_firmware_path(state)
    } else {
        None
    }
}

const JP108_PRESETS: [u16; 16] = [
    0x0004, 0x0005, 0x0006, 0x0007, 0x0008, 0x0009, 0x000a, 0x000b, 0x0028, 0x0029, 0x002c, 0x003a,
    0x003b, 0x003c, 0x00e0, 0x00e1,
];

const U2_PRESETS: [u16; 17] = [
    0x0100, 0x0101, 0x0102, 0x0103, 0x0104, 0x0105, 0x0106, 0x0107, 0x0108, 0x0109, 0x010a, 0x010b,
    0x010c, 0x010d, 0x010e, 0x010f, 0x0110,
];

fn cycle_jp108(current: u16, delta: i32) -> u16 {
    cycle_from_table(&JP108_PRESETS, current, delta)
}

fn cycle_u2(current: u16, delta: i32) -> u16 {
    cycle_from_table(&U2_PRESETS, current, delta)
}

fn cycle_from_table(table: &[u16], current: u16, delta: i32) -> u16 {
    let pos = table.iter().position(|item| *item == current).unwrap_or(0) as i32;
    let len = table.len() as i32;
    let mut next = pos + delta;
    while next < 0 {
        next += len;
    }
    table[(next as usize) % table.len()]
}
