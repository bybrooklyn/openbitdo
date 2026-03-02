use super::*;
use crate::support_report::report_subject_token;
use bitdo_app_core::{FirmwareOutcome, OpenBitdoCoreConfig};
use bitdo_proto::SupportLevel;

#[test]
fn about_state_roundtrip_returns_home() {
    let mut app = TuiApp::default();
    app.refresh_devices(vec![AppDevice {
        vid_pid: VidPid::new(0x2dc8, 0x6009),
        name: "Test".to_owned(),
        support_level: SupportLevel::Full,
        support_tier: SupportTier::Full,
        protocol_family: bitdo_proto::ProtocolFamily::Standard64,
        capability: bitdo_proto::PidCapability::full(),
        evidence: bitdo_proto::SupportEvidence::Confirmed,
        serial: Some("SERIAL1".to_owned()),
        connected: true,
    }]);
    app.open_about();
    assert_eq!(app.state, TuiWorkflowState::About);
    app.close_overlay();
    assert_eq!(app.state, TuiWorkflowState::Home);
}

#[test]
fn refresh_devices_without_any_device_enters_wait_state() {
    let mut app = TuiApp::default();
    app.refresh_devices(Vec::new());
    assert_eq!(app.state, TuiWorkflowState::WaitForDevice);
    assert!(app.selected.is_none());
}

#[test]
fn refresh_devices_autoselects_single_device() {
    let mut app = TuiApp::default();
    app.refresh_devices(vec![AppDevice {
        vid_pid: VidPid::new(0x2dc8, 0x6009),
        name: "One".to_owned(),
        support_level: SupportLevel::Full,
        support_tier: SupportTier::Full,
        protocol_family: bitdo_proto::ProtocolFamily::Standard64,
        capability: bitdo_proto::PidCapability::full(),
        evidence: bitdo_proto::SupportEvidence::Confirmed,
        serial: None,
        connected: true,
    }]);

    assert_eq!(app.state, TuiWorkflowState::Home);
    assert_eq!(app.selected_index, 0);
    assert_eq!(app.selected, Some(VidPid::new(0x2dc8, 0x6009)));
}

#[test]
fn serial_token_prefers_serial_then_vidpid() {
    let with_serial = AppDevice {
        vid_pid: VidPid::new(0x2dc8, 0x6009),
        name: "S".to_owned(),
        support_level: SupportLevel::Full,
        support_tier: SupportTier::Full,
        protocol_family: bitdo_proto::ProtocolFamily::Standard64,
        capability: bitdo_proto::PidCapability::full(),
        evidence: bitdo_proto::SupportEvidence::Confirmed,
        serial: Some("ABC 123".to_owned()),
        connected: true,
    };
    assert_eq!(report_subject_token(Some(&with_serial)), "ABC_123");

    let without_serial = AppDevice {
        serial: None,
        ..with_serial
    };
    assert_eq!(report_subject_token(Some(&without_serial)), "2dc86009");
}

#[test]
fn launch_options_default_to_failure_only_reports() {
    let opts = TuiLaunchOptions::default();
    assert_eq!(opts.report_save_mode, ReportSaveMode::FailureOnly);
}

#[test]
fn blocked_panel_text_matches_support_tier() {
    let mut app = TuiApp::default();
    app.refresh_devices(vec![AppDevice {
        vid_pid: VidPid::new(0x2dc8, 0x2100),
        name: "Candidate".to_owned(),
        support_level: SupportLevel::DetectOnly,
        support_tier: SupportTier::CandidateReadOnly,
        protocol_family: bitdo_proto::ProtocolFamily::Standard64,
        capability: bitdo_proto::PidCapability {
            supports_mode: true,
            supports_profile_rw: true,
            supports_boot: false,
            supports_firmware: false,
            supports_jp108_dedicated_map: false,
            supports_u2_slot_config: false,
            supports_u2_button_map: false,
        },
        evidence: bitdo_proto::SupportEvidence::Inferred,
        serial: None,
        connected: true,
    }]);
    let selected = app.selected_device().expect("selected");
    let text = blocked_action_panel_text(selected);
    assert!(text.contains("blocked"));
    assert!(text.contains("Status shown as Blocked"));
    assert_eq!(beginner_status_label(selected), "Blocked");
}

#[test]
fn non_advanced_report_mode_skips_off_setting() {
    let mut app = TuiApp {
        advanced_mode: false,
        ..Default::default()
    };
    assert_eq!(app.report_save_mode, ReportSaveMode::FailureOnly);
    app.cycle_report_save_mode().expect("cycle");
    assert_eq!(app.report_save_mode, ReportSaveMode::Always);
    app.cycle_report_save_mode().expect("cycle");
    assert_eq!(app.report_save_mode, ReportSaveMode::FailureOnly);
}

#[test]
fn unknown_device_label_is_beginner_friendly() {
    let device = AppDevice {
        vid_pid: VidPid::new(0x2dc8, 0xabcd),
        name: "PID_UNKNOWN".to_owned(),
        support_level: SupportLevel::DetectOnly,
        support_tier: SupportTier::DetectOnly,
        protocol_family: bitdo_proto::ProtocolFamily::Unknown,
        capability: bitdo_proto::PidCapability::identify_only(),
        evidence: bitdo_proto::SupportEvidence::Untested,
        serial: None,
        connected: true,
    };
    let label = super::display_device_name(&device);
    assert!(label.contains("Unknown 8BitDo Device"));
    assert!(label.contains("2dc8:abcd"));
}

#[tokio::test]
async fn home_refresh_loads_devices() {
    let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        default_chunk_size: 16,
        progress_interval_ms: 1,
        ..Default::default()
    });

    let mut app = TuiApp::default();
    app.refresh_devices(core.list_devices().await.expect("devices"));

    assert!(!app.devices.is_empty());
    assert!(app.selected_device().is_some());
}

#[tokio::test]
async fn run_tui_app_no_ui_blocks_detect_only_pid() {
    let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        default_chunk_size: 16,
        progress_interval_ms: 1,
        ..Default::default()
    });

    let result = run_tui_app(
        core,
        TuiLaunchOptions {
            no_ui: true,
            selected_vid_pid: Some(VidPid::new(0x2dc8, 0x2100)),
            ..Default::default()
        },
    )
    .await;

    assert!(result.is_err());
}

#[tokio::test]
async fn run_tui_app_no_ui_full_support_completes() {
    let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        default_chunk_size: 16,
        progress_interval_ms: 1,
        ..Default::default()
    });

    run_tui_app(
        core,
        TuiLaunchOptions {
            no_ui: true,
            selected_vid_pid: Some(VidPid::new(0x2dc8, 0x6009)),
            ..Default::default()
        },
    )
    .await
    .expect("run app");
}

#[tokio::test]
async fn tui_flow_with_manual_path_completes() {
    let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        default_chunk_size: 16,
        progress_interval_ms: 1,
        ..Default::default()
    });

    let path = std::env::temp_dir().join("openbitdo-tui-flow.bin");
    tokio::fs::write(&path, vec![1u8; 128])
        .await
        .expect("write");

    let report = run_tui_flow(
        core,
        TuiRunRequest {
            vid_pid: VidPid::new(0x2dc8, 0x6009),
            firmware_path: path.clone(),
            allow_unsafe: true,
            brick_risk_ack: true,
            experimental: true,
            chunk_size: Some(32),
            acknowledged_risk: true,
            no_ui: true,
        },
    )
    .await
    .expect("run tui flow");

    assert_eq!(report.status, FirmwareOutcome::Completed);
    let _ = tokio::fs::remove_file(path).await;
}

#[tokio::test]
async fn support_report_is_toml_file() {
    let device = AppDevice {
        vid_pid: VidPid::new(0x2dc8, 0x6009),
        name: "Test".to_owned(),
        support_level: SupportLevel::Full,
        support_tier: SupportTier::Full,
        protocol_family: bitdo_proto::ProtocolFamily::Standard64,
        capability: bitdo_proto::PidCapability::full(),
        evidence: bitdo_proto::SupportEvidence::Confirmed,
        serial: Some("RPT-1".to_owned()),
        connected: true,
    };

    let report_path = persist_support_report(
        "diag-probe",
        Some(&device),
        "ok",
        "all checks passed".to_owned(),
        None,
        None,
    )
    .await
    .expect("report path");

    assert_eq!(
        report_path.extension().and_then(|s| s.to_str()),
        Some("toml")
    );
    let _ = tokio::fs::remove_file(report_path).await;
}

#[tokio::test]
async fn update_action_enters_jp108_wizard_for_jp108_device() {
    let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });
    let mut app = TuiApp::default();
    app.refresh_devices(core.list_devices().await.expect("devices"));
    let jp108_idx = app
        .devices
        .iter()
        .position(|d| d.vid_pid.pid == 0x5209)
        .expect("jp108 fixture");
    app.select_index(jp108_idx);
    app.state = TuiWorkflowState::Home;

    let mut terminal = None;
    let mut events = None;
    let opts = TuiLaunchOptions::default();
    execute_home_action(
        &core,
        &mut terminal,
        &mut app,
        &opts,
        &mut events,
        HomeAction::Update,
    )
    .await
    .expect("execute");

    assert_eq!(app.state, TuiWorkflowState::Jp108Mapping);
    assert!(!app.jp108_mappings.is_empty());
}

#[tokio::test]
async fn update_action_enters_u2_wizard_for_ultimate2_device() {
    let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });
    let mut app = TuiApp::default();
    app.refresh_devices(core.list_devices().await.expect("devices"));
    let u2_idx = app
        .devices
        .iter()
        .position(|d| d.vid_pid.pid == 0x6012)
        .expect("u2 fixture");
    app.select_index(u2_idx);
    app.state = TuiWorkflowState::Home;

    let mut terminal = None;
    let mut events = None;
    let opts = TuiLaunchOptions::default();
    execute_home_action(
        &core,
        &mut terminal,
        &mut app,
        &opts,
        &mut events,
        HomeAction::Update,
    )
    .await
    .expect("execute");

    assert_eq!(app.state, TuiWorkflowState::U2CoreProfile);
    assert!(app.u2_profile.is_some());
}

#[tokio::test]
async fn device_flow_backup_apply_sets_backup_id() {
    let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: true,
        ..Default::default()
    });
    let mut app = TuiApp::default();
    app.refresh_devices(core.list_devices().await.expect("devices"));
    let jp108_idx = app
        .devices
        .iter()
        .position(|d| d.vid_pid.pid == 0x5209)
        .expect("jp108 fixture");
    app.select_index(jp108_idx);
    app.begin_jp108_mapping(
        core.jp108_read_dedicated_mapping(VidPid::new(0x2dc8, 0x5209))
            .await
            .expect("read"),
    );

    let mut terminal = None;
    let mut events = None;
    let opts = TuiLaunchOptions::default();
    execute_device_flow_action(
        &core,
        &mut terminal,
        &mut app,
        &opts,
        &mut events,
        DeviceFlowAction::BackupApply,
    )
    .await
    .expect("apply");

    assert!(app.latest_backup.is_some());
}
