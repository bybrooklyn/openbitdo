use crate::command::CommandId;
use crate::types::{
    CommandConfidence, DeviceProfile, PidCapability, ProtocolFamily, SafetyClass, SupportEvidence,
    SupportLevel, VidPid,
};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PidRegistryRow {
    pub name: &'static str,
    pub pid: u16,
    pub support_level: SupportLevel,
    pub protocol_family: ProtocolFamily,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CommandRegistryRow {
    pub id: CommandId,
    pub safety_class: SafetyClass,
    pub confidence: CommandConfidence,
    pub experimental_default: bool,
    pub report_id: u8,
    pub request: &'static [u8],
    pub expected_response: &'static str,
}

include!(concat!(env!("OUT_DIR"), "/generated_pid_registry.rs"));
include!(concat!(env!("OUT_DIR"), "/generated_command_registry.rs"));

pub fn pid_registry() -> &'static [PidRegistryRow] {
    PID_REGISTRY
}

pub fn command_registry() -> &'static [CommandRegistryRow] {
    COMMAND_REGISTRY
}

pub fn find_pid(pid: u16) -> Option<&'static PidRegistryRow> {
    PID_REGISTRY.iter().find(|row| row.pid == pid)
}

pub fn find_command(id: CommandId) -> Option<&'static CommandRegistryRow> {
    COMMAND_REGISTRY.iter().find(|row| row.id == id)
}

pub fn default_capability_for(
    support_level: SupportLevel,
    _protocol_family: ProtocolFamily,
) -> PidCapability {
    match support_level {
        SupportLevel::Full => PidCapability::full(),
        SupportLevel::DetectOnly => PidCapability::identify_only(),
    }
}

pub fn default_evidence_for(support_level: SupportLevel) -> SupportEvidence {
    match support_level {
        SupportLevel::Full => SupportEvidence::Confirmed,
        SupportLevel::DetectOnly => SupportEvidence::Inferred,
    }
}

pub fn device_profile_for(vid_pid: VidPid) -> DeviceProfile {
    if let Some(row) = find_pid(vid_pid.pid) {
        DeviceProfile {
            vid_pid,
            name: row.name.to_owned(),
            support_level: row.support_level,
            protocol_family: row.protocol_family,
            capability: default_capability_for(row.support_level, row.protocol_family),
            evidence: default_evidence_for(row.support_level),
        }
    } else {
        DeviceProfile {
            vid_pid,
            name: "PID_UNKNOWN".to_owned(),
            support_level: SupportLevel::DetectOnly,
            protocol_family: ProtocolFamily::Unknown,
            capability: PidCapability::identify_only(),
            evidence: SupportEvidence::Untested,
        }
    }
}
