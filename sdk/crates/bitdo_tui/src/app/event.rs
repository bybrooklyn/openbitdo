use crate::AppDevice;
use bitdo_app_core::{
    ConfigBackupId, DedicatedButtonMapping, FirmwareFinalReport, FirmwareProgressEvent,
    FirmwareUpdatePlan, U2CoreProfile,
};
use bitdo_proto::{DiagProbeResult, VidPid};
use std::path::PathBuf;

use super::action::QuickAction;
use super::state::DiagnosticsFilter;

#[derive(Clone, Debug)]
pub enum AppEvent {
    Init,
    Tick,
    DeviceFilterSet(String),
    DeviceFilterInput(char),
    DeviceFilterBackspace,
    SelectFilteredDevice(usize),
    SelectNextDevice,
    SelectPrevDevice,
    SelectNextAction,
    SelectPrevAction,
    DiagnosticsSelectCheck(usize),
    DiagnosticsSelectNextCheck,
    DiagnosticsSelectPrevCheck,
    DiagnosticsShiftFilter(i32),
    DiagnosticsSetFilter(DiagnosticsFilter),
    TriggerAction(QuickAction),
    ConfirmPrimary,
    Back,
    Quit,
    ToggleAdvancedMode,
    CycleReportSaveMode,
    MappingAdjust(i32),
    MappingMoveSelection(i32),
    DevicesLoaded(Vec<AppDevice>),
    DevicesLoadFailed(String),
    DiagnosticsCompleted {
        vid_pid: VidPid,
        result: DiagProbeResult,
        summary: String,
    },
    DiagnosticsFailed {
        vid_pid: VidPid,
        error: String,
    },
    MappingsLoadedJp108 {
        vid_pid: VidPid,
        mappings: Vec<DedicatedButtonMapping>,
    },
    MappingsLoadedUltimate2 {
        vid_pid: VidPid,
        profile: U2CoreProfile,
    },
    MappingLoadFailed(String),
    MappingApplied {
        backup_id: Option<ConfigBackupId>,
        message: String,
        recovery_lock: bool,
    },
    MappingApplyFailed(String),
    BackupRestoreCompleted(String),
    BackupRestoreFailed(String),
    PreflightReady {
        vid_pid: VidPid,
        firmware_path: PathBuf,
        source: String,
        version: String,
        plan: FirmwareUpdatePlan,
        downloaded_firmware_path: Option<PathBuf>,
    },
    PreflightBlocked(String),
    UpdateStarted {
        session_id: String,
        source: String,
        version: String,
    },
    UpdateProgress(FirmwareProgressEvent),
    UpdateFinished(FirmwareFinalReport),
    UpdateFailed(String),
    SettingsPersisted,
    SupportReportSaved(PathBuf),
    Error(String),
}
