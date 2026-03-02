use crate::command::CommandId;
use crate::types::{
    CommandConfidence, CommandRuntimePolicy, DeviceProfile, EvidenceConfidence, PidCapability,
    ProtocolFamily, SafetyClass, SupportEvidence, SupportLevel, SupportTier, VidPid,
};
use std::collections::HashSet;
use std::sync::OnceLock;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PidRegistryRow {
    pub name: &'static str,
    pub pid: u16,
    pub support_level: SupportLevel,
    pub support_tier: SupportTier,
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
    pub applies_to: &'static [u16],
    pub operation_group: &'static str,
}

// Registry data is intentionally hardcoded in source files so support coverage
// is explicit in Rust code and does not depend on build-time CSV generation.
include!("pid_registry_table.rs");
include!("command_registry_table.rs");

impl CommandRegistryRow {
    /// Convert evidence confidence into a stable reporting enum.
    pub fn evidence_confidence(&self) -> EvidenceConfidence {
        match self.confidence {
            CommandConfidence::Confirmed => EvidenceConfidence::Confirmed,
            CommandConfidence::Inferred => EvidenceConfidence::Inferred,
        }
    }

    /// Runtime policy used by the session gate checker.
    ///
    /// Policy rationale:
    /// - Confirmed paths are enabled by default.
    /// - Inferred safe reads can run only when experimental mode is enabled.
    /// - Inferred write/unsafe paths stay blocked until explicit confirmation.
    pub fn runtime_policy(&self) -> CommandRuntimePolicy {
        match (self.confidence, self.safety_class) {
            (CommandConfidence::Confirmed, _) => CommandRuntimePolicy::EnabledDefault,
            (CommandConfidence::Inferred, SafetyClass::SafeRead) => {
                CommandRuntimePolicy::ExperimentalGate
            }
            (CommandConfidence::Inferred, _) => CommandRuntimePolicy::BlockedUntilConfirmed,
        }
    }
}

pub fn pid_registry() -> &'static [PidRegistryRow] {
    ensure_unique_pid_rows();
    PID_REGISTRY
}

pub fn command_registry() -> &'static [CommandRegistryRow] {
    COMMAND_REGISTRY
}

pub fn find_pid(pid: u16) -> Option<&'static PidRegistryRow> {
    pid_registry().iter().find(|row| row.pid == pid)
}

pub fn find_command(id: CommandId) -> Option<&'static CommandRegistryRow> {
    COMMAND_REGISTRY.iter().find(|row| row.id == id)
}

pub fn command_applies_to_pid(row: &CommandRegistryRow, pid: u16) -> bool {
    row.applies_to.is_empty() || row.applies_to.contains(&pid)
}

pub fn default_capability_for(
    pid: u16,
    support_tier: SupportTier,
    protocol_family: ProtocolFamily,
) -> PidCapability {
    if support_tier == SupportTier::DetectOnly {
        return PidCapability::identify_only();
    }

    const STANDARD_CANDIDATE_READ_DIAG_PIDS: &[u16] = &[
        0x6002, 0x6003, 0x3010, 0x3011, 0x3012, 0x3013, 0x3004, 0x3019, 0x3100, 0x3105, 0x2100,
        0x2101, 0x901a, 0x6006, 0x5203, 0x5204, 0x301a, 0x9028, 0x3026, 0x3027,
    ];
    const JP_CANDIDATE_DIAG_PIDS: &[u16] = &[0x5200, 0x5201, 0x203a, 0x2049, 0x2028, 0x202e];

    match (support_tier, pid) {
        (SupportTier::CandidateReadOnly, 0x6002)
        | (SupportTier::CandidateReadOnly, 0x6003)
        | (SupportTier::CandidateReadOnly, 0x3010)
        | (SupportTier::CandidateReadOnly, 0x3011)
        | (SupportTier::CandidateReadOnly, 0x3012)
        | (SupportTier::CandidateReadOnly, 0x3013)
        | (SupportTier::CandidateReadOnly, 0x3004)
        | (SupportTier::CandidateReadOnly, 0x3019)
        | (SupportTier::CandidateReadOnly, 0x3100)
        | (SupportTier::CandidateReadOnly, 0x3105)
        | (SupportTier::CandidateReadOnly, 0x2100)
        | (SupportTier::CandidateReadOnly, 0x2101)
        | (SupportTier::CandidateReadOnly, 0x901a)
        | (SupportTier::CandidateReadOnly, 0x6006)
        | (SupportTier::CandidateReadOnly, 0x5203)
        | (SupportTier::CandidateReadOnly, 0x5204)
        | (SupportTier::CandidateReadOnly, 0x301a)
        | (SupportTier::CandidateReadOnly, 0x9028)
        | (SupportTier::CandidateReadOnly, 0x3026)
        | (SupportTier::CandidateReadOnly, 0x3027) => PidCapability {
            supports_mode: true,
            supports_profile_rw: true,
            supports_boot: false,
            supports_firmware: false,
            supports_jp108_dedicated_map: false,
            supports_u2_slot_config: false,
            supports_u2_button_map: false,
        },
        (SupportTier::CandidateReadOnly, 0x5200)
        | (SupportTier::CandidateReadOnly, 0x5201)
        | (SupportTier::CandidateReadOnly, 0x203a)
        | (SupportTier::CandidateReadOnly, 0x2049)
        | (SupportTier::CandidateReadOnly, 0x2028)
        | (SupportTier::CandidateReadOnly, 0x202e) => PidCapability {
            supports_mode: false,
            supports_profile_rw: false,
            supports_boot: false,
            supports_firmware: false,
            supports_jp108_dedicated_map: false,
            supports_u2_slot_config: false,
            supports_u2_button_map: false,
        },
        (SupportTier::CandidateReadOnly, _) if STANDARD_CANDIDATE_READ_DIAG_PIDS.contains(&pid) => {
            PidCapability {
                supports_mode: true,
                supports_profile_rw: true,
                supports_boot: false,
                supports_firmware: false,
                supports_jp108_dedicated_map: false,
                supports_u2_slot_config: false,
                supports_u2_button_map: false,
            }
        }
        (SupportTier::CandidateReadOnly, _) if JP_CANDIDATE_DIAG_PIDS.contains(&pid) => {
            PidCapability {
                supports_mode: false,
                supports_profile_rw: false,
                supports_boot: false,
                supports_firmware: false,
                supports_jp108_dedicated_map: false,
                supports_u2_slot_config: false,
                supports_u2_button_map: false,
            }
        }
        (_, 0x5209) | (_, 0x520a) => PidCapability {
            supports_mode: false,
            supports_profile_rw: false,
            supports_boot: true,
            supports_firmware: true,
            supports_jp108_dedicated_map: true,
            supports_u2_slot_config: false,
            supports_u2_button_map: false,
        },
        (_, 0x6012) | (_, 0x6013) => PidCapability {
            supports_mode: true,
            supports_profile_rw: true,
            supports_boot: true,
            supports_firmware: true,
            supports_jp108_dedicated_map: false,
            supports_u2_slot_config: true,
            supports_u2_button_map: true,
        },
        _ => {
            let mut cap = PidCapability::full();
            if protocol_family == ProtocolFamily::JpHandshake {
                cap.supports_mode = false;
                cap.supports_profile_rw = false;
            }
            cap.supports_jp108_dedicated_map = false;
            cap.supports_u2_slot_config = false;
            cap.supports_u2_button_map = false;
            cap
        }
    }
}

pub fn default_evidence_for(support_tier: SupportTier) -> SupportEvidence {
    match support_tier {
        SupportTier::Full => SupportEvidence::Confirmed,
        SupportTier::CandidateReadOnly | SupportTier::DetectOnly => SupportEvidence::Inferred,
    }
}

pub fn device_profile_for(vid_pid: VidPid) -> DeviceProfile {
    if let Some(row) = find_pid(vid_pid.pid) {
        DeviceProfile {
            vid_pid,
            name: row.name.to_owned(),
            support_level: row.support_level,
            support_tier: row.support_tier,
            protocol_family: row.protocol_family,
            capability: default_capability_for(row.pid, row.support_tier, row.protocol_family),
            evidence: default_evidence_for(row.support_tier),
        }
    } else {
        DeviceProfile {
            vid_pid,
            name: "PID_UNKNOWN".to_owned(),
            support_level: SupportLevel::DetectOnly,
            support_tier: SupportTier::DetectOnly,
            protocol_family: ProtocolFamily::Unknown,
            capability: PidCapability::identify_only(),
            evidence: SupportEvidence::Untested,
        }
    }
}

fn ensure_unique_pid_rows() {
    static CHECK: OnceLock<()> = OnceLock::new();
    CHECK.get_or_init(|| {
        let mut seen = HashSet::new();
        for row in PID_REGISTRY {
            assert!(
                seen.insert(row.pid),
                "duplicate pid in runtime registry: {:#06x} ({})",
                row.pid,
                row.name
            );
        }
    });
}
