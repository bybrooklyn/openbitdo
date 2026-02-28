use crate::types::{CommandConfidence, SafetyClass};
use serde::{Deserialize, Serialize};

#[derive(Clone, Copy, Debug, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub enum CommandId {
    GetPid,
    GetReportRevision,
    GetMode,
    GetModeAlt,
    GetControllerVersion,
    GetSuperButton,
    SetModeDInput,
    Idle,
    Version,
    ReadProfile,
    WriteProfile,
    EnterBootloaderA,
    EnterBootloaderB,
    EnterBootloaderC,
    ExitBootloader,
    FirmwareChunk,
    FirmwareCommit,
}

impl CommandId {
    pub const ALL: [CommandId; 17] = [
        CommandId::GetPid,
        CommandId::GetReportRevision,
        CommandId::GetMode,
        CommandId::GetModeAlt,
        CommandId::GetControllerVersion,
        CommandId::GetSuperButton,
        CommandId::SetModeDInput,
        CommandId::Idle,
        CommandId::Version,
        CommandId::ReadProfile,
        CommandId::WriteProfile,
        CommandId::EnterBootloaderA,
        CommandId::EnterBootloaderB,
        CommandId::EnterBootloaderC,
        CommandId::ExitBootloader,
        CommandId::FirmwareChunk,
        CommandId::FirmwareCommit,
    ];

    pub fn all() -> &'static [CommandId] {
        &Self::ALL
    }
}

#[derive(Clone, Debug)]
pub struct CommandDefinition {
    pub id: CommandId,
    pub safety_class: SafetyClass,
    pub confidence: CommandConfidence,
    pub experimental_default: bool,
    pub report_id: u8,
    pub request: &'static [u8],
    pub expected_response: &'static str,
}
