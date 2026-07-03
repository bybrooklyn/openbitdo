use super::*;
use crate::app::action::QuickAction;
use crate::app::event::AppEvent;
use crate::app::reducer::reduce;
use crate::app::state::{
    AppState, DiagnosticsFilter, DiagnosticsState, MappingDraftState, Screen, TaskMode,
};
use crate::persistence::ui_state::{load_ui_state, persist_ui_state};
use crate::runtime::effect_executor::execute_effect;
use bitdo_app_core::{DedicatedButtonId, DedicatedButtonMapping, OpenBitdoCoreConfig};
use bitdo_proto::{
    BitdoErrorCode, CommandId, DiagCommandStatus, DiagProbeResult, DiagSeverity,
    EvidenceConfidence, ResponseStatus, SupportTier, VidPid,
};
use insta::assert_snapshot;
use ratatui::Terminal;
use ratatui::backend::TestBackend;
use std::collections::BTreeMap;
use std::path::PathBuf;

#[tokio::test]
async fn quick_action_matrix_blocks_update_for_read_only() {
    let core = bitdo_app_core::OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });

    let mut state = AppState::new(&UiLaunchOptions::default());
    let devices = core.list_devices().await.expect("devices");
    let _ = reduce(&mut state, AppEvent::DevicesLoaded(devices));

    let update = state
        .quick_actions
        .iter()
        .find(|a| a.action == QuickAction::RecommendedUpdate)
        .expect("update action");
    assert!(!update.enabled);

    let mut state = AppState::new(&UiLaunchOptions {
        allow_unsafe: true,
        brick_risk_ack: true,
        ..UiLaunchOptions::default()
    });
    let devices = core.list_devices().await.expect("devices");
    let _ = reduce(&mut state, AppEvent::DevicesLoaded(devices));

    let update = state
        .quick_actions
        .iter()
        .find(|a| a.action == QuickAction::RecommendedUpdate)
        .expect("update action");
    assert!(update.enabled);

    let readonly_idx = state
        .devices
        .iter()
        .position(|d| d.support_tier != SupportTier::Full)
        .expect("readonly device");
    state.selected_device_id = Some(state.devices[readonly_idx].vid_pid);
    state.recompute_quick_actions();

    let update = state
        .quick_actions
        .iter()
        .find(|a| a.action == QuickAction::RecommendedUpdate)
        .expect("update action");
    assert!(!update.enabled);
}

#[tokio::test]
async fn dashboard_prioritizes_diagnostics_when_device_detected() {
    let core = bitdo_app_core::OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });

    let mut state = AppState::new(&UiLaunchOptions::default());
    let devices = core.list_devices().await.expect("devices");
    let _ = reduce(&mut state, AppEvent::DevicesLoaded(devices));

    assert_eq!(state.quick_actions[0].action, QuickAction::Diagnose);
    assert!(state.quick_actions[0].enabled);
    assert_eq!(state.selected_action(), Some(QuickAction::Diagnose));
}

#[tokio::test]
async fn dashboard_candidate_write_probe_uses_per_pid_unlock_file() {
    let core = bitdo_app_core::OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });
    let unique = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .expect("clock")
        .as_nanos();
    let test_dir = std::env::temp_dir().join(format!("openbitdo-tui-unlock-{unique}"));
    let settings_path = test_dir.join("ui-state.toml");
    let unlock_dir = test_dir.join("candidate-unlocks");
    std::fs::create_dir_all(&unlock_dir).expect("unlock dir");
    std::fs::write(
        unlock_dir.join("2dc8_2100.toml"),
        "pid = \"2dc8:2100\"\ncandidate_write_unlock = true\n",
    )
    .expect("unlock file");

    let mut state = AppState::new(&UiLaunchOptions {
        advanced_mode: true,
        settings_path: Some(settings_path),
        allow_unsafe: true,
        brick_risk_ack: true,
        ..UiLaunchOptions::default()
    });
    drive(&core, &mut state, AppEvent::Init).await;

    let candidate_display_index = state
        .filtered_device_indices()
        .iter()
        .position(|idx| state.devices[*idx].vid_pid == VidPid::new(0x2dc8, 0x2100))
        .expect("candidate display index");
    drive(
        &core,
        &mut state,
        AppEvent::SelectFilteredDevice(candidate_display_index),
    )
    .await;

    let probe = state
        .quick_actions
        .iter()
        .find(|action| action.action == QuickAction::UnlockWriteProbe)
        .expect("probe action");
    assert!(probe.enabled);

    drive(
        &core,
        &mut state,
        AppEvent::TriggerAction(QuickAction::UnlockWriteProbe),
    )
    .await;

    let report = state
        .latest_unlock_report
        .as_ref()
        .expect("unlock report");
    assert!(report.allowed);
    assert!(report.readback_verified);
    assert_eq!(report.vid_pid, VidPid::new(0x2dc8, 0x2100));
    assert_eq!(state.screen, Screen::Task);
    assert_eq!(
        state.task_state.as_ref().map(|task| task.mode),
        Some(TaskMode::Final)
    );

    if let Some(path) = state.latest_report_path.take() {
        assert!(path.exists());
        let body = std::fs::read_to_string(&path).expect("support report");
        assert!(body.contains("runtime_unlock"));
        assert!(body.contains("candidate-write-probe"));
        let _ = std::fs::remove_file(path);
    }
    let _ = std::fs::remove_dir_all(test_dir);
}

#[tokio::test]
async fn dashboard_candidate_write_probe_requires_advanced_and_ack() {
    let core = bitdo_app_core::OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });
    let mut state = AppState::new(&UiLaunchOptions::default());
    drive(&core, &mut state, AppEvent::Init).await;

    let candidate_display_index = state
        .filtered_device_indices()
        .iter()
        .position(|idx| state.devices[*idx].vid_pid == VidPid::new(0x2dc8, 0x2100))
        .expect("candidate display index");
    drive(
        &core,
        &mut state,
        AppEvent::SelectFilteredDevice(candidate_display_index),
    )
    .await;

    let probe = state
        .quick_actions
        .iter()
        .find(|action| action.action == QuickAction::UnlockWriteProbe)
        .expect("probe action");
    assert!(!probe.enabled);
    assert_eq!(probe.reason.as_deref(), Some("Enable advanced mode first"));

    state.advanced_mode = true;
    state.recompute_quick_actions();
    let probe = state
        .quick_actions
        .iter()
        .find(|action| action.action == QuickAction::UnlockWriteProbe)
        .expect("probe action");
    assert!(!probe.enabled);
    assert_eq!(
        probe.reason.as_deref(),
        Some("Acknowledge local write risk first")
    );
}

#[test]
fn dashboard_no_device_selects_refresh() {
    let mut state = AppState::new(&UiLaunchOptions::default());
    let _ = reduce(&mut state, AppEvent::DevicesLoaded(Vec::new()));

    assert_eq!(state.quick_actions[0].action, QuickAction::Diagnose);
    assert!(!state.quick_actions[0].enabled);
    assert_eq!(state.selected_action(), Some(QuickAction::Refresh));
}

#[test]
fn dashboard_groups_devices_by_support_tier() {
    let state = snapshot_state();
    let ordered = state
        .filtered_device_indices()
        .into_iter()
        .map(|idx| state.devices[idx].support_tier)
        .collect::<Vec<_>>();

    assert_eq!(ordered[0], SupportTier::Full);
    assert_eq!(ordered[1], SupportTier::CandidateReadOnly);
    assert_eq!(ordered.last().copied(), Some(SupportTier::DetectOnly));
}

#[tokio::test]
async fn toggling_advanced_mode_updates_core_runtime() {
    let core = bitdo_app_core::OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });
    let mut state = AppState::new(&UiLaunchOptions::default());

    assert!(!core.advanced_mode());
    drive(&core, &mut state, AppEvent::ToggleAdvancedMode).await;
    assert!(state.advanced_mode);
    assert!(core.advanced_mode());

    drive(&core, &mut state, AppEvent::ToggleAdvancedMode).await;
    assert!(!state.advanced_mode);
    assert!(!core.advanced_mode());
}

#[test]
fn mapping_draft_undo_and_reset() {
    let mut state = AppState::new(&UiLaunchOptions {
        allow_unsafe: true,
        brick_risk_ack: true,
        ..UiLaunchOptions::default()
    });
    state.screen = Screen::MappingEditor;
    state.mapping_draft_state = Some(MappingDraftState::Jp108 {
        loaded: vec![DedicatedButtonMapping {
            button: DedicatedButtonId::A,
            target_hid_usage: 0x0004,
        }],
        current: vec![DedicatedButtonMapping {
            button: DedicatedButtonId::A,
            target_hid_usage: 0x0004,
        }],
        undo_stack: Vec::new(),
        selected_row: 0,
    });

    let _ = reduce(&mut state, AppEvent::MappingAdjust(1));
    assert!(state.mapping_has_changes());

    let _ = reduce(&mut state, AppEvent::TriggerAction(QuickAction::UndoDraft));
    assert!(!state.mapping_has_changes());

    let _ = reduce(&mut state, AppEvent::MappingAdjust(1));
    assert!(state.mapping_has_changes());
    let _ = reduce(&mut state, AppEvent::TriggerAction(QuickAction::ResetDraft));
    assert!(!state.mapping_has_changes());
}

#[test]
fn settings_schema_v2_roundtrip() {
    let path = std::env::temp_dir().join("bitdo-tui-ui-state-v2.toml");
    persist_ui_state(
        &path,
        true,
        ReportSaveMode::Always,
        "ultimate".to_owned(),
        DashboardLayoutMode::Compact,
        PanelFocus::QuickActions,
    )
    .expect("persist");

    let loaded = load_ui_state(&path).expect("load");
    assert_eq!(loaded.schema_version, 2);
    assert!(loaded.advanced_mode);
    assert_eq!(loaded.report_save_mode, ReportSaveMode::Always);
    assert_eq!(loaded.device_filter_text, "ultimate");
    assert_eq!(loaded.dashboard_layout_mode, DashboardLayoutMode::Compact);
    assert_eq!(loaded.last_panel_focus, PanelFocus::QuickActions);

    let _ = std::fs::remove_file(path);
}

#[test]
fn invalid_ui_state_returns_error() {
    let path = std::env::temp_dir().join("bitdo-tui-invalid-ui-state.toml");
    std::fs::write(&path, "advanced_mode = [").expect("write invalid");

    let err = load_ui_state(&path).expect_err("invalid ui state must error");
    assert!(err.to_string().contains("failed to parse ui state"));

    let _ = std::fs::remove_file(path);
}

#[test]
fn launch_defaults_are_safe() {
    let ui = UiLaunchOptions::default();
    assert!(!ui.allow_unsafe);
    assert!(!ui.brick_risk_ack);

    let headless = RunLaunchOptions::default();
    assert!(!headless.allow_unsafe);
    assert!(!headless.brick_risk_ack);
    assert!(!headless.acknowledged_risk);
}

#[tokio::test]
async fn integration_refresh_select_preflight_cancel_path() {
    let core = bitdo_app_core::OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });

    let mut state = AppState::new(&UiLaunchOptions {
        allow_unsafe: true,
        brick_risk_ack: true,
        ..UiLaunchOptions::default()
    });
    drive(&core, &mut state, AppEvent::Init).await;

    assert!(!state.devices.is_empty());

    let full_support_index = state
        .devices
        .iter()
        .position(|device| device.support_tier == SupportTier::Full)
        .expect("full-support device");
    drive(
        &core,
        &mut state,
        AppEvent::SelectFilteredDevice(full_support_index),
    )
    .await;
    drive(
        &core,
        &mut state,
        AppEvent::TriggerAction(QuickAction::RecommendedUpdate),
    )
    .await;

    assert_eq!(state.screen, Screen::Task);
    assert!(state.task_state.is_some());
    let downloaded_path = state
        .task_state
        .as_ref()
        .and_then(|task| task.downloaded_firmware_path.clone())
        .expect("downloaded firmware path");
    assert!(downloaded_path.exists());

    drive(
        &core,
        &mut state,
        AppEvent::TriggerAction(QuickAction::Cancel),
    )
    .await;
    assert_eq!(state.screen, Screen::Dashboard);
    assert!(!downloaded_path.exists());
}

#[tokio::test]
async fn integration_diagnostics_run_rerun_save_and_back() {
    let core = bitdo_app_core::OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });

    let mut state = AppState::new(&UiLaunchOptions::default());
    drive(&core, &mut state, AppEvent::Init).await;
    drive(&core, &mut state, AppEvent::SelectFilteredDevice(0)).await;
    drive(
        &core,
        &mut state,
        AppEvent::TriggerAction(QuickAction::Diagnose),
    )
    .await;

    assert_eq!(state.screen, Screen::Diagnostics);
    assert!(state.diagnostics_state.is_some());
    assert!(state.task_state.is_none());

    drive(
        &core,
        &mut state,
        AppEvent::TriggerAction(QuickAction::RunAgain),
    )
    .await;
    assert_eq!(state.screen, Screen::Diagnostics);
    assert!(state.diagnostics_state.is_some());

    drive(
        &core,
        &mut state,
        AppEvent::TriggerAction(QuickAction::SaveReport),
    )
    .await;
    let saved_path = state
        .diagnostics_state
        .as_ref()
        .and_then(|diagnostics| diagnostics.latest_report_path.clone())
        .expect("diagnostics report path");
    assert!(saved_path.exists());

    drive(
        &core,
        &mut state,
        AppEvent::TriggerAction(QuickAction::Back),
    )
    .await;
    assert_eq!(state.screen, Screen::Dashboard);

    let _ = std::fs::remove_file(saved_path);
}

#[test]
fn diagnostics_filter_changes_visible_rows() {
    let mut state = snapshot_state();
    state.screen = Screen::Diagnostics;
    state.diagnostics_state = Some(sample_diagnostics_state(None));
    state.recompute_quick_actions();

    assert_eq!(state.diagnostics_filtered_indices(), vec![0, 1, 2, 3, 4]);

    let _ = reduce(
        &mut state,
        AppEvent::DiagnosticsSetFilter(DiagnosticsFilter::Issues),
    );
    assert_eq!(state.diagnostics_filtered_indices(), vec![3, 4]);
    assert_eq!(
        state
            .selected_diagnostics_check()
            .map(|check| check.command),
        Some(CommandId::ReadProfile)
    );

    let _ = reduce(
        &mut state,
        AppEvent::DiagnosticsSetFilter(DiagnosticsFilter::Experimental),
    );
    assert_eq!(state.diagnostics_filtered_indices(), vec![2, 3]);
    assert_eq!(
        state
            .selected_diagnostics_check()
            .map(|check| check.command),
        Some(CommandId::ReadProfile)
    );
}

#[tokio::test]
async fn manual_save_report_updates_diagnostics_state() {
    let core = bitdo_app_core::OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });

    let mut state = snapshot_state();
    state.screen = Screen::Diagnostics;
    state.diagnostics_state = Some(sample_diagnostics_state(None));
    state.recompute_quick_actions();

    drive(
        &core,
        &mut state,
        AppEvent::TriggerAction(QuickAction::SaveReport),
    )
    .await;

    let saved_path = state
        .diagnostics_state
        .as_ref()
        .and_then(|diagnostics| diagnostics.latest_report_path.clone())
        .expect("saved diagnostics report path");
    assert!(saved_path.exists());
    let report = std::fs::read_to_string(&saved_path).expect("read report");
    assert!(report.contains("schema_version = 2"));
    assert!(report.contains("protocol_family = \"Standard64\""));
    assert!(report.contains("blocked_operations"));
    assert!(report.contains("missing_evidence"));

    let _ = std::fs::remove_file(saved_path);
}

#[test]
fn recovery_transition_is_preserved() {
    let mut state = AppState::new(&UiLaunchOptions::default());
    let _ = reduce(
        &mut state,
        AppEvent::MappingApplied {
            backup_id: None,
            message: "rollback failed".to_owned(),
            recovery_lock: true,
        },
    );
    assert_eq!(state.screen, Screen::Recovery);
    assert!(state.write_lock_until_restart);
}

#[tokio::test]
async fn headless_human_and_json_modes_complete() {
    let core = bitdo_app_core::OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        progress_interval_ms: 1,
        ..Default::default()
    });

    let report_human = run_headless(
        core.clone(),
        RunLaunchOptions {
            vid_pid: VidPid::new(0x2dc8, 0x6009),
            use_recommended: true,
            allow_unsafe: true,
            brick_risk_ack: true,
            acknowledged_risk: true,
            output_mode: HeadlessOutputMode::Human,
            emit_events: false,
            ..Default::default()
        },
    )
    .await
    .expect("human mode");
    assert_eq!(
        report_human.status,
        bitdo_app_core::FirmwareOutcome::Completed
    );

    let report_json = run_headless(
        core,
        RunLaunchOptions {
            vid_pid: VidPid::new(0x2dc8, 0x6009),
            use_recommended: true,
            allow_unsafe: true,
            brick_risk_ack: true,
            acknowledged_risk: true,
            output_mode: HeadlessOutputMode::Json,
            emit_events: true,
            ..Default::default()
        },
    )
    .await
    .expect("json mode");
    assert_eq!(
        report_json.status,
        bitdo_app_core::FirmwareOutcome::Completed
    );
}

#[test]
fn snapshot_dashboard_80x24() {
    let mut state = snapshot_state();
    state.dashboard_layout_mode = DashboardLayoutMode::Wide;
    let rendered = render_state(&mut state, 80, 24);
    assert_snapshot!(rendered);
}

#[test]
fn snapshot_task_screen_100x30() {
    let mut state = snapshot_state();
    state.screen = Screen::Task;
    state.task_state = Some(crate::app::state::TaskState {
        mode: TaskMode::Preflight,
        plan: None,
        progress: 12,
        status: "Ready to confirm transfer".to_owned(),
        final_report: None,
        downloaded_firmware_path: None,
    });
    state.recompute_quick_actions();
    let rendered = render_state(&mut state, 100, 30);
    assert_snapshot!(rendered);
}

#[test]
fn snapshot_diagnostics_screen_100x30() {
    let mut state = snapshot_state();
    state.screen = Screen::Diagnostics;
    state.diagnostics_state = Some(sample_diagnostics_state(None));
    state.recompute_quick_actions();
    let rendered = render_state(&mut state, 100, 30);
    assert_snapshot!(rendered);
}

#[test]
fn snapshot_diagnostics_screen_80x24() {
    let mut state = snapshot_state();
    state.screen = Screen::Diagnostics;
    state.diagnostics_state = Some(sample_diagnostics_state(None));
    state.recompute_quick_actions();
    let rendered = render_state(&mut state, 80, 24);
    assert_snapshot!(rendered);
}

#[test]
fn snapshot_diagnostics_screen_with_saved_report() {
    let mut state = snapshot_state();
    state.screen = Screen::Diagnostics;
    state.diagnostics_state = Some(sample_diagnostics_state(Some(PathBuf::from(
        "/tmp/openbitdo-diag-report.toml",
    ))));
    if let Some(diagnostics) = state.diagnostics_state.as_mut() {
        diagnostics.active_filter = DiagnosticsFilter::Issues;
        diagnostics.selected_check_index = 4;
    }
    state.recompute_quick_actions();
    let rendered = render_state(&mut state, 100, 30);
    assert_snapshot!(rendered);
}

#[test]
fn snapshot_mapping_editor_screen() {
    let mut state = snapshot_state();
    state.screen = Screen::MappingEditor;
    state.mapping_draft_state = Some(MappingDraftState::Jp108 {
        loaded: vec![DedicatedButtonMapping {
            button: DedicatedButtonId::A,
            target_hid_usage: 0x0004,
        }],
        current: vec![DedicatedButtonMapping {
            button: DedicatedButtonId::A,
            target_hid_usage: 0x0005,
        }],
        undo_stack: Vec::new(),
        selected_row: 0,
    });
    state.recompute_quick_actions();
    let rendered = render_state(&mut state, 100, 30);
    assert_snapshot!(rendered);
}

#[test]
fn snapshot_recovery_screen() {
    let mut state = snapshot_state();
    state.screen = Screen::Recovery;
    state.write_lock_until_restart = true;
    state.recompute_quick_actions();
    let rendered = render_state(&mut state, 80, 24);
    assert_snapshot!(rendered);
}

async fn drive(core: &bitdo_app_core::OpenBitdoCore, state: &mut AppState, initial: AppEvent) {
    let mut queue = std::collections::VecDeque::from([initial]);
    while let Some(event) = queue.pop_front() {
        let effects = reduce(state, event);
        for effect in effects {
            let emitted = execute_effect(core, state, effect).await;
            for next in emitted {
                queue.push_back(next);
            }
        }
    }
}

fn snapshot_state() -> AppState {
    let mut state = AppState::new(&UiLaunchOptions::default());
    let _ = reduce(
        &mut state,
        AppEvent::DevicesLoaded(vec![
            bitdo_app_core::AppDevice {
                vid_pid: VidPid::new(0x2dc8, 0x5209),
                name: "Ultimate2".to_owned(),
                support_level: bitdo_proto::SupportLevel::Full,
                support_tier: bitdo_proto::SupportTier::Full,
                protocol_family: bitdo_proto::ProtocolFamily::Standard64,
                capability: bitdo_proto::PidCapability::full(),
                evidence: bitdo_proto::SupportEvidence::Confirmed,
                serial: None,
                connected: true,
            },
            bitdo_app_core::AppDevice {
                vid_pid: VidPid::new(0x2dc8, 0x6009),
                name: "Ultimate".to_owned(),
                support_level: bitdo_proto::SupportLevel::DetectOnly,
                support_tier: bitdo_proto::SupportTier::CandidateReadOnly,
                protocol_family: bitdo_proto::ProtocolFamily::Standard64,
                capability: bitdo_proto::PidCapability::identify_only(),
                evidence: bitdo_proto::SupportEvidence::Inferred,
                serial: None,
                connected: true,
            },
            bitdo_app_core::AppDevice {
                vid_pid: VidPid::new(0x2dc8, 0x901a),
                name: "Candidate".to_owned(),
                support_level: bitdo_proto::SupportLevel::DetectOnly,
                support_tier: bitdo_proto::SupportTier::CandidateReadOnly,
                protocol_family: bitdo_proto::ProtocolFamily::Unknown,
                capability: bitdo_proto::PidCapability::identify_only(),
                evidence: bitdo_proto::SupportEvidence::Untested,
                serial: None,
                connected: true,
            },
            bitdo_app_core::AppDevice {
                vid_pid: VidPid::new(0x2dc8, 0x2056),
                name: "Detect Only".to_owned(),
                support_level: bitdo_proto::SupportLevel::DetectOnly,
                support_tier: bitdo_proto::SupportTier::DetectOnly,
                protocol_family: bitdo_proto::ProtocolFamily::Unknown,
                capability: bitdo_proto::PidCapability::identify_only(),
                evidence: bitdo_proto::SupportEvidence::Untested,
                serial: None,
                connected: true,
            },
        ]),
    );
    state.event_log.clear();
    state.status_line = "Ready".to_owned();
    state
}

fn sample_diagnostics_state(report_path: Option<PathBuf>) -> DiagnosticsState {
    DiagnosticsState {
        result: sample_diagnostics_result(),
        summary: "Checks: 3/5 passed. Confirmed checks: 2/3 passed. Experimental checks: 1/2 passed. Issues: 2 total, 1 need attention. Transport ready: yes. Blocked operations: none for confirmed capabilities. Standard64 diagnostics are available. This device is full-support.".to_owned(),
        selected_check_index: 0,
        active_filter: DiagnosticsFilter::All,
        latest_report_path: report_path,
    }
}

fn sample_diagnostics_result() -> DiagProbeResult {
    DiagProbeResult {
        target: VidPid::new(0x2dc8, 0x5209),
        profile_name: "Ultimate2".to_owned(),
        support_level: bitdo_proto::SupportLevel::Full,
        support_tier: bitdo_proto::SupportTier::Full,
        protocol_family: bitdo_proto::ProtocolFamily::Standard64,
        capability: bitdo_proto::PidCapability::full(),
        evidence: bitdo_proto::SupportEvidence::Confirmed,
        transport_ready: true,
        command_checks: vec![
            diag_check(
                CommandId::GetPid,
                DiagCheckFixture {
                    ok: true,
                    confidence: EvidenceConfidence::Confirmed,
                    is_experimental: false,
                    severity: DiagSeverity::Ok,
                    error_code: None,
                    detail: "detected pid 0x5209",
                    parsed_facts: [("detected_pid", 0x5209)].into_iter().collect(),
                },
            ),
            diag_check(
                CommandId::GetMode,
                DiagCheckFixture {
                    ok: true,
                    confidence: EvidenceConfidence::Confirmed,
                    is_experimental: false,
                    severity: DiagSeverity::Ok,
                    error_code: None,
                    detail: "mode 2",
                    parsed_facts: [("mode", 2)].into_iter().collect(),
                },
            ),
            diag_check(
                CommandId::GetSuperButton,
                DiagCheckFixture {
                    ok: true,
                    confidence: EvidenceConfidence::Inferred,
                    is_experimental: true,
                    severity: DiagSeverity::Ok,
                    error_code: None,
                    detail: "ok",
                    parsed_facts: BTreeMap::new(),
                },
            ),
            diag_check(
                CommandId::ReadProfile,
                DiagCheckFixture {
                    ok: false,
                    confidence: EvidenceConfidence::Inferred,
                    is_experimental: true,
                    severity: DiagSeverity::Warning,
                    error_code: Some(BitdoErrorCode::Timeout),
                    detail: "timeout while waiting for device response",
                    parsed_facts: BTreeMap::new(),
                },
            ),
            diag_check(
                CommandId::Version,
                DiagCheckFixture {
                    ok: false,
                    confidence: EvidenceConfidence::Confirmed,
                    is_experimental: false,
                    severity: DiagSeverity::NeedsAttention,
                    error_code: Some(BitdoErrorCode::InvalidResponse),
                    detail: "invalid response for Version: response signature mismatch",
                    parsed_facts: [("version_x100", 4200), ("beta", 0)].into_iter().collect(),
                },
            ),
        ],
    }
}

struct DiagCheckFixture<'a> {
    ok: bool,
    confidence: EvidenceConfidence,
    is_experimental: bool,
    severity: DiagSeverity,
    error_code: Option<BitdoErrorCode>,
    detail: &'a str,
    parsed_facts: BTreeMap<&'a str, u32>,
}

fn diag_check(command: CommandId, fixture: DiagCheckFixture<'_>) -> DiagCommandStatus {
    DiagCommandStatus {
        command,
        ok: fixture.ok,
        confidence: fixture.confidence,
        is_experimental: fixture.is_experimental,
        severity: fixture.severity,
        attempts: 1,
        validator: format!("test:{command:?}"),
        response_status: if fixture.ok {
            ResponseStatus::Ok
        } else {
            ResponseStatus::Invalid
        },
        bytes_written: 64,
        bytes_read: if fixture.ok { 64 } else { 8 },
        error_code: fixture.error_code,
        detail: fixture.detail.to_owned(),
        parsed_facts: fixture
            .parsed_facts
            .into_iter()
            .map(|(key, value)| (key.to_owned(), value))
            .collect(),
    }
}

fn render_state(state: &mut AppState, width: u16, height: u16) -> String {
    state.set_layout_from_size(width, height);
    let backend = TestBackend::new(width, height);
    let mut terminal = Terminal::new(backend).expect("terminal");
    terminal
        .draw(|frame| {
            let _ = crate::ui::layout::render(frame, state);
        })
        .expect("draw");

    let backend = terminal.backend();
    let buffer = backend.buffer();
    let mut lines = Vec::new();
    for y in 0..height {
        let mut line = String::new();
        for x in 0..width {
            line.push_str(buffer[(x, y)].symbol());
        }
        lines.push(line.trim_end().to_owned());
    }

    lines.join("\n")
}
