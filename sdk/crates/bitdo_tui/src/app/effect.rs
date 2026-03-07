use crate::{DashboardLayoutMode, PanelFocus, ReportSaveMode};
use bitdo_app_core::{
    ConfigBackupId, DedicatedButtonMapping, FirmwareFinalReport, FirmwareUpdateSessionId,
    U2CoreProfile,
};
use bitdo_proto::DiagProbeResult;
use bitdo_proto::VidPid;
use std::path::PathBuf;

#[derive(Clone, Debug)]
pub enum MappingApplyDraft {
    Jp108(Vec<DedicatedButtonMapping>),
    Ultimate2(U2CoreProfile),
}

#[derive(Clone, Debug)]
pub enum Effect {
    RefreshDevices,
    RunDiagnostics {
        vid_pid: VidPid,
    },
    LoadMappings {
        vid_pid: VidPid,
    },
    ApplyMappings {
        vid_pid: VidPid,
        draft: MappingApplyDraft,
    },
    RestoreBackup {
        backup_id: ConfigBackupId,
    },
    PreparePreflight {
        vid_pid: VidPid,
        firmware_path_override: Option<PathBuf>,
        allow_unsafe: bool,
        brick_risk_ack: bool,
        experimental: bool,
        chunk_size: Option<usize>,
    },
    StartFirmware {
        session_id: FirmwareUpdateSessionId,
        acknowledged_risk: bool,
    },
    CancelFirmware {
        session_id: FirmwareUpdateSessionId,
    },
    PollFirmwareReport {
        session_id: FirmwareUpdateSessionId,
    },
    DeleteTempFile {
        path: PathBuf,
    },
    PersistSettings {
        path: PathBuf,
        advanced_mode: bool,
        report_save_mode: ReportSaveMode,
        device_filter_text: String,
        dashboard_layout_mode: DashboardLayoutMode,
        last_panel_focus: PanelFocus,
    },
    PersistSupportReport {
        operation: String,
        vid_pid: Option<VidPid>,
        status: String,
        message: String,
        diag: Option<DiagProbeResult>,
        firmware: Option<FirmwareFinalReport>,
    },
}
