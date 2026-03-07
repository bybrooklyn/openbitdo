use crate::app::effect::{Effect, MappingApplyDraft};
use crate::app::event::AppEvent;
use crate::app::state::AppState;
use crate::persistence::ui_state::persist_ui_state;
use crate::support_report::persist_support_report;
use bitdo_app_core::{
    FirmwareCancelRequest, FirmwareConfirmRequest, FirmwarePreflightRequest, FirmwareStartRequest,
    OpenBitdoCore, U2SlotId,
};
use std::path::Path;

pub async fn execute_effect(
    core: &OpenBitdoCore,
    state: &AppState,
    effect: Effect,
) -> Vec<AppEvent> {
    match effect {
        Effect::RefreshDevices => match core.list_devices().await {
            Ok(mut devices) => {
                devices.sort_by_key(|d| (d.vid_pid.vid, d.vid_pid.pid));
                vec![AppEvent::DevicesLoaded(devices)]
            }
            Err(err) => vec![AppEvent::DevicesLoadFailed(err.to_string())],
        },
        Effect::RunDiagnostics { vid_pid } => match core.diag_probe(vid_pid).await {
            Ok(result) => {
                let summary = state
                    .devices
                    .iter()
                    .find(|device| device.vid_pid == vid_pid)
                    .map(|device| core.beginner_diag_summary(device, &result))
                    .unwrap_or_else(|| "Diagnostics completed".to_owned());
                vec![AppEvent::DiagnosticsCompleted {
                    vid_pid,
                    result,
                    summary,
                }]
            }
            Err(err) => vec![AppEvent::DiagnosticsFailed {
                vid_pid,
                error: err.to_string(),
            }],
        },
        Effect::LoadMappings { vid_pid } => {
            let device = state.devices.iter().find(|d| d.vid_pid == vid_pid);
            if let Some(device) = device {
                if device.capability.supports_jp108_dedicated_map {
                    match core.jp108_read_dedicated_mapping(vid_pid).await {
                        Ok(mappings) => vec![AppEvent::MappingsLoadedJp108 { vid_pid, mappings }],
                        Err(err) => vec![AppEvent::MappingLoadFailed(err.to_string())],
                    }
                } else if device.capability.supports_u2_button_map
                    && device.capability.supports_u2_slot_config
                {
                    match core.u2_read_core_profile(vid_pid, U2SlotId::Slot1).await {
                        Ok(profile) => vec![AppEvent::MappingsLoadedUltimate2 { vid_pid, profile }],
                        Err(err) => vec![AppEvent::MappingLoadFailed(err.to_string())],
                    }
                } else {
                    vec![AppEvent::MappingLoadFailed(
                        "Device does not support mapping editor".to_owned(),
                    )]
                }
            } else {
                vec![AppEvent::MappingLoadFailed("No device selected".to_owned())]
            }
        }
        Effect::ApplyMappings { vid_pid, draft } => match draft {
            MappingApplyDraft::Jp108(mappings) => match core
                .jp108_apply_dedicated_mapping_with_recovery(vid_pid, mappings, true)
                .await
            {
                Ok(report) => {
                    let recovery_lock = report.rollback_failed();
                    let message = if report.write_applied {
                        "JP108 mapping applied".to_owned()
                    } else if recovery_lock {
                        "Apply failed and rollback failed; writes locked until restart".to_owned()
                    } else {
                        "Apply failed but rollback restored prior mapping".to_owned()
                    };
                    vec![AppEvent::MappingApplied {
                        backup_id: report.backup_id,
                        message,
                        recovery_lock,
                    }]
                }
                Err(err) => vec![AppEvent::MappingApplyFailed(err.to_string())],
            },
            MappingApplyDraft::Ultimate2(profile) => match core
                .u2_apply_core_profile_with_recovery(
                    vid_pid,
                    profile.slot,
                    profile.mode,
                    profile.mappings,
                    profile.l2_analog,
                    profile.r2_analog,
                    true,
                )
                .await
            {
                Ok(report) => {
                    let recovery_lock = report.rollback_failed();
                    let message = if report.write_applied {
                        "Ultimate2 profile applied".to_owned()
                    } else if recovery_lock {
                        "Apply failed and rollback failed; writes locked until restart".to_owned()
                    } else {
                        "Apply failed but rollback restored prior profile".to_owned()
                    };
                    vec![AppEvent::MappingApplied {
                        backup_id: report.backup_id,
                        message,
                        recovery_lock,
                    }]
                }
                Err(err) => vec![AppEvent::MappingApplyFailed(err.to_string())],
            },
        },
        Effect::RestoreBackup { backup_id } => match core.restore_backup(backup_id).await {
            Ok(_) => vec![AppEvent::BackupRestoreCompleted(
                "Backup restore completed".to_owned(),
            )],
            Err(err) => vec![AppEvent::BackupRestoreFailed(format!(
                "Backup restore failed: {err}"
            ))],
        },
        Effect::PreparePreflight {
            vid_pid,
            firmware_path_override,
            allow_unsafe,
            brick_risk_ack,
            experimental,
            chunk_size,
        } => {
            let device = state.devices.iter().find(|d| d.vid_pid == vid_pid);
            let Some(device) = device else {
                return vec![AppEvent::PreflightBlocked("No selected device".to_owned())];
            };
            let (firmware_path, source, version, downloaded_firmware_path) =
                if let Some(path) = firmware_path_override {
                    (path, "local file".to_owned(), "manual".to_owned(), None)
                } else {
                    match core.download_recommended_firmware(vid_pid).await {
                        Ok(download) => {
                            let path = download.firmware_path;
                            (
                                path.clone(),
                                "recommended verified download".to_owned(),
                                download.version,
                                Some(path),
                            )
                        }
                        Err(err) => {
                            return vec![AppEvent::PreflightBlocked(format!(
                                "Recommended firmware unavailable: {err}"
                            ))]
                        }
                    }
                };

            match core
                .preflight_firmware(FirmwarePreflightRequest {
                    vid_pid: device.vid_pid,
                    firmware_path: firmware_path.clone(),
                    allow_unsafe,
                    brick_risk_ack,
                    experimental,
                    chunk_size,
                })
                .await
            {
                Ok(preflight) => {
                    if !preflight.gate.allowed {
                        if let Some(path) = downloaded_firmware_path.as_ref() {
                            let _ = cleanup_temp_file(path).await;
                        }
                        vec![AppEvent::PreflightBlocked(
                            preflight
                                .gate
                                .message
                                .unwrap_or_else(|| "Preflight denied by policy".to_owned()),
                        )]
                    } else if let Some(plan) = preflight.plan {
                        vec![AppEvent::PreflightReady {
                            vid_pid,
                            firmware_path,
                            source,
                            version,
                            plan,
                            downloaded_firmware_path,
                        }]
                    } else {
                        if let Some(path) = downloaded_firmware_path.as_ref() {
                            let _ = cleanup_temp_file(path).await;
                        }
                        vec![AppEvent::PreflightBlocked(
                            "Preflight allowed but no transfer plan was returned".to_owned(),
                        )]
                    }
                }
                Err(err) => {
                    if let Some(path) = downloaded_firmware_path.as_ref() {
                        let _ = cleanup_temp_file(path).await;
                    }
                    vec![AppEvent::PreflightBlocked(format!(
                        "Preflight failed: {err}"
                    ))]
                }
            }
        }
        Effect::StartFirmware {
            session_id,
            acknowledged_risk,
        } => {
            if let Err(err) = core
                .start_firmware(FirmwareStartRequest {
                    session_id: session_id.clone(),
                })
                .await
            {
                return vec![AppEvent::UpdateFailed(err.to_string())];
            }

            if let Err(err) = core
                .confirm_firmware(FirmwareConfirmRequest {
                    session_id: session_id.clone(),
                    acknowledged_risk,
                })
                .await
            {
                return vec![AppEvent::UpdateFailed(err.to_string())];
            }

            vec![AppEvent::UpdateStarted {
                session_id: session_id.0,
                source: "selected firmware".to_owned(),
                version: "target".to_owned(),
            }]
        }
        Effect::CancelFirmware { session_id } => match core
            .cancel_firmware(FirmwareCancelRequest { session_id })
            .await
        {
            Ok(report) => vec![AppEvent::UpdateFinished(report)],
            Err(err) => vec![AppEvent::UpdateFailed(err.to_string())],
        },
        Effect::PollFirmwareReport { session_id } => {
            match core.firmware_report(&session_id.0).await {
                Ok(Some(report)) => vec![AppEvent::UpdateFinished(report)],
                Ok(None) => Vec::new(),
                Err(err) => vec![AppEvent::UpdateFailed(err.to_string())],
            }
        }
        Effect::DeleteTempFile { path } => match cleanup_temp_file(&path).await {
            Ok(_) => Vec::new(),
            Err(err) => vec![AppEvent::Error(format!(
                "Failed to delete temporary firmware {}: {err}",
                path.display()
            ))],
        },
        Effect::PersistSettings {
            path,
            advanced_mode,
            report_save_mode,
            device_filter_text,
            dashboard_layout_mode,
            last_panel_focus,
        } => match persist_ui_state(
            &path,
            advanced_mode,
            report_save_mode,
            device_filter_text,
            dashboard_layout_mode,
            last_panel_focus,
        ) {
            Ok(_) => vec![AppEvent::SettingsPersisted],
            Err(err) => vec![AppEvent::Error(format!("Settings save failed: {err}"))],
        },
        Effect::PersistSupportReport {
            operation,
            vid_pid,
            status,
            message,
            diag,
            firmware,
        } => {
            let device = vid_pid.and_then(|id| state.devices.iter().find(|d| d.vid_pid == id));
            match persist_support_report(
                &operation,
                device,
                &status,
                message,
                diag.as_ref(),
                firmware.as_ref(),
            )
            .await
            {
                Ok(path) => vec![AppEvent::SupportReportSaved(path)],
                Err(err) => vec![AppEvent::Error(format!(
                    "Support report save failed: {err}"
                ))],
            }
        }
    }
}

async fn cleanup_temp_file(path: &Path) -> std::io::Result<()> {
    match tokio::fs::remove_file(path).await {
        Ok(()) => Ok(()),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(err) => Err(err),
    }
}
