use crate::command::CommandId;
use crate::error::{BitdoError, BitdoErrorCode, Result};
use crate::frame::{CommandFrame, ResponseFrame, ResponseStatus};
use crate::profile::ProfileBlob;
use crate::registry::{
    command_applies_to_pid, device_profile_for, find_command, find_pid, CommandRegistryRow,
};
use crate::transport::Transport;
use crate::types::{
    CommandRuntimePolicy, DeviceProfile, EvidenceConfidence, PidCapability, ProtocolFamily,
    SafetyClass, SupportEvidence, SupportLevel, SupportTier, VidPid,
};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::thread;
use std::time::Duration;

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct RetryPolicy {
    pub max_attempts: u8,
    pub backoff_ms: u64,
}

impl Default for RetryPolicy {
    fn default() -> Self {
        Self {
            max_attempts: 3,
            backoff_ms: 10,
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct TimeoutProfile {
    pub probe_ms: u64,
    pub io_ms: u64,
    pub firmware_ms: u64,
}

impl Default for TimeoutProfile {
    fn default() -> Self {
        Self {
            probe_ms: 200,
            io_ms: 400,
            firmware_ms: 1_200,
        }
    }
}

#[derive(Clone, Debug)]
pub struct SessionConfig {
    pub retry_policy: RetryPolicy,
    pub timeout_profile: TimeoutProfile,
    pub allow_unsafe: bool,
    pub brick_risk_ack: bool,
    pub experimental: bool,
    pub trace_enabled: bool,
}

impl Default for SessionConfig {
    fn default() -> Self {
        Self {
            retry_policy: RetryPolicy::default(),
            timeout_profile: TimeoutProfile::default(),
            allow_unsafe: false,
            brick_risk_ack: false,
            experimental: false,
            trace_enabled: true,
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct CommandExecutionReport {
    pub command: CommandId,
    pub attempts: u8,
    pub validator: String,
    pub status: ResponseStatus,
    pub bytes_written: usize,
    pub bytes_read: usize,
    pub error_code: Option<BitdoErrorCode>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct DiagCommandStatus {
    pub command: CommandId,
    pub ok: bool,
    pub confidence: EvidenceConfidence,
    pub is_experimental: bool,
    pub severity: DiagSeverity,
    pub error_code: Option<BitdoErrorCode>,
    pub detail: String,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum DiagSeverity {
    Ok,
    Warning,
    NeedsAttention,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct DiagProbeResult {
    pub target: VidPid,
    pub profile_name: String,
    pub support_level: SupportLevel,
    pub support_tier: SupportTier,
    pub protocol_family: ProtocolFamily,
    pub capability: PidCapability,
    pub evidence: SupportEvidence,
    pub transport_ready: bool,
    pub command_checks: Vec<DiagCommandStatus>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct IdentifyResult {
    pub target: VidPid,
    pub profile_name: String,
    pub support_level: SupportLevel,
    pub support_tier: SupportTier,
    pub protocol_family: ProtocolFamily,
    pub capability: PidCapability,
    pub evidence: SupportEvidence,
    pub detected_pid: Option<u16>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ModeState {
    pub mode: u8,
    pub source: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwareTransferReport {
    pub bytes_total: usize,
    pub chunk_size: usize,
    pub chunks_sent: usize,
    pub dry_run: bool,
}

pub struct DeviceSession<T: Transport> {
    transport: T,
    target: VidPid,
    profile: DeviceProfile,
    config: SessionConfig,
    trace: Vec<CommandExecutionReport>,
    last_execution: Option<CommandExecutionReport>,
}

impl<T: Transport> DeviceSession<T> {
    pub fn new(mut transport: T, target: VidPid, config: SessionConfig) -> Result<Self> {
        transport.open(target)?;
        let profile = device_profile_for(target);
        Ok(Self {
            transport,
            target,
            profile,
            config,
            trace: Vec::new(),
            last_execution: None,
        })
    }

    pub fn profile(&self) -> &DeviceProfile {
        &self.profile
    }

    pub fn trace(&self) -> &[CommandExecutionReport] {
        &self.trace
    }

    pub fn last_execution_report(&self) -> Option<&CommandExecutionReport> {
        self.last_execution.as_ref()
    }

    pub fn close(&mut self) -> Result<()> {
        self.transport.close()
    }

    pub fn into_transport(self) -> T {
        self.transport
    }

    pub fn identify(&mut self) -> Result<IdentifyResult> {
        let detected_pid = match self.send_command(CommandId::GetPid, None) {
            Ok(resp) => resp
                .parsed_fields
                .get("detected_pid")
                .copied()
                .map(|v| v as u16),
            Err(_) => None,
        };

        let profile_row = detected_pid.and_then(find_pid);
        let mut profile = self.profile.clone();
        if let Some(row) = profile_row {
            profile = device_profile_for(VidPid::new(self.target.vid, row.pid));
        }

        Ok(IdentifyResult {
            target: self.target,
            profile_name: profile.name,
            support_level: profile.support_level,
            support_tier: profile.support_tier,
            protocol_family: profile.protocol_family,
            capability: profile.capability,
            evidence: profile.evidence,
            detected_pid,
        })
    }

    pub fn diag_probe(&mut self) -> DiagProbeResult {
        let target_pid = self.target.pid;
        let checks_to_run = [
            CommandId::GetPid,
            CommandId::GetReportRevision,
            CommandId::GetMode,
            CommandId::GetControllerVersion,
            // Inferred safe reads are intentionally included in diagnostics so
            // users always see signal quality, but results are labeled
            // experimental and only strict safety conditions escalate.
            CommandId::GetSuperButton,
            CommandId::ReadProfile,
        ]
        .iter()
        .filter_map(|cmd| {
            let row = find_command(*cmd)?;
            if row.safety_class != SafetyClass::SafeRead {
                return None;
            }
            if !command_applies_to_pid(row, target_pid) {
                return None;
            }
            Some((*cmd, row.runtime_policy(), row.evidence_confidence()))
        })
        .collect::<Vec<_>>();

        let mut checks = Vec::with_capacity(checks_to_run.len());
        for (cmd, runtime_policy, confidence) in checks_to_run {
            match self.send_command(cmd, None) {
                Ok(_) => checks.push(DiagCommandStatus {
                    command: cmd,
                    ok: true,
                    confidence,
                    is_experimental: runtime_policy == CommandRuntimePolicy::ExperimentalGate,
                    severity: DiagSeverity::Ok,
                    error_code: None,
                    detail: "ok".to_owned(),
                }),
                Err(err) => checks.push(DiagCommandStatus {
                    command: cmd,
                    ok: false,
                    confidence,
                    is_experimental: runtime_policy == CommandRuntimePolicy::ExperimentalGate,
                    severity: classify_diag_failure(
                        cmd,
                        runtime_policy,
                        confidence,
                        err.code(),
                        self.target.pid,
                    ),
                    error_code: Some(err.code()),
                    detail: err.to_string(),
                }),
            }
        }

        DiagProbeResult {
            target: self.target,
            profile_name: self.profile.name.clone(),
            support_level: self.profile.support_level,
            support_tier: self.profile.support_tier,
            protocol_family: self.profile.protocol_family,
            capability: self.profile.capability,
            evidence: self.profile.evidence,
            transport_ready: true,
            command_checks: checks,
        }
    }

    pub fn get_mode(&mut self) -> Result<ModeState> {
        let resp = self.send_command(CommandId::GetMode, None)?;
        if let Some(mode) = resp.parsed_fields.get("mode").copied() {
            return Ok(ModeState {
                mode: mode as u8,
                source: "GetMode".to_owned(),
            });
        }

        let resp = self.send_command(CommandId::GetModeAlt, None)?;
        let mode = resp.parsed_fields.get("mode").copied().unwrap_or_default() as u8;
        Ok(ModeState {
            mode,
            source: "GetModeAlt".to_owned(),
        })
    }

    pub fn set_mode(&mut self, mode: u8) -> Result<ModeState> {
        let row = self.ensure_command_allowed(CommandId::SetModeDInput)?;
        let mut payload = row.request.to_vec();
        if payload.len() < 5 {
            return Err(BitdoError::InvalidInput(
                "SetModeDInput payload shorter than expected".to_owned(),
            ));
        }
        payload[4] = mode;
        self.send_row(row, Some(&payload))?;
        self.get_mode()
    }

    pub fn read_profile(&mut self, slot: u8) -> Result<ProfileBlob> {
        let row = self.ensure_command_allowed(CommandId::ReadProfile)?;
        let mut payload = row.request.to_vec();
        if payload.len() > 3 {
            payload[3] = slot;
        }
        let resp = self.send_row(row, Some(&payload))?;
        Ok(ProfileBlob {
            slot,
            payload: resp.raw,
        })
    }

    pub fn write_profile(&mut self, slot: u8, profile: &ProfileBlob) -> Result<()> {
        let row = self.ensure_command_allowed(CommandId::WriteProfile)?;
        let mut payload = row.request.to_vec();
        if payload.len() > 3 {
            payload[3] = slot;
        }

        let serialized = profile.to_bytes();
        let copy_len = (payload.len().saturating_sub(8)).min(serialized.len());
        if copy_len > 0 {
            payload[8..8 + copy_len].copy_from_slice(&serialized[..copy_len]);
        }

        self.send_row(row, Some(&payload))?;
        Ok(())
    }

    pub fn jp108_read_dedicated_mappings(&mut self) -> Result<Vec<(u8, u16)>> {
        let resp = self.send_command(CommandId::Jp108ReadDedicatedMappings, None)?;
        Ok(parse_indexed_u16_table(&resp.raw, 10))
    }

    pub fn jp108_write_dedicated_mapping(
        &mut self,
        index: u8,
        target_hid_usage: u16,
    ) -> Result<()> {
        let row = self.ensure_command_allowed(CommandId::Jp108WriteDedicatedMapping)?;
        let mut payload = row.request.to_vec();
        if payload.len() < 7 {
            return Err(BitdoError::InvalidInput(
                "Jp108WriteDedicatedMapping payload shorter than expected".to_owned(),
            ));
        }

        payload[4] = index;
        let usage = target_hid_usage.to_le_bytes();
        payload[5] = usage[0];
        payload[6] = usage[1];
        self.send_row(row, Some(&payload))?;
        Ok(())
    }

    pub fn u2_get_current_slot(&mut self) -> Result<u8> {
        let resp = self.send_command(CommandId::U2GetCurrentSlot, None)?;
        Ok(resp.parsed_fields.get("slot").copied().unwrap_or(0) as u8)
    }

    pub fn u2_read_config_slot(&mut self, slot: u8) -> Result<Vec<u8>> {
        let row = self.ensure_command_allowed(CommandId::U2ReadConfigSlot)?;
        let mut payload = row.request.to_vec();
        if payload.len() > 4 {
            payload[4] = slot;
        }
        let resp = self.send_row(row, Some(&payload))?;
        Ok(resp.raw)
    }

    pub fn u2_write_config_slot(&mut self, slot: u8, config_blob: &[u8]) -> Result<()> {
        let row = self.ensure_command_allowed(CommandId::U2WriteConfigSlot)?;
        let mut payload = row.request.to_vec();
        if payload.len() < 8 {
            return Err(BitdoError::InvalidInput(
                "U2WriteConfigSlot payload shorter than expected".to_owned(),
            ));
        }

        payload[4] = slot;
        let copy_len = config_blob.len().min(payload.len().saturating_sub(8));
        if copy_len > 0 {
            payload[8..8 + copy_len].copy_from_slice(&config_blob[..copy_len]);
        }

        self.send_row(row, Some(&payload))?;
        Ok(())
    }

    pub fn u2_read_button_map(&mut self, slot: u8) -> Result<Vec<(u8, u16)>> {
        let row = self.ensure_command_allowed(CommandId::U2ReadButtonMap)?;
        let mut payload = row.request.to_vec();
        if payload.len() > 4 {
            payload[4] = slot;
        }
        let resp = self.send_row(row, Some(&payload))?;
        Ok(parse_indexed_u16_table(&resp.raw, 17))
    }

    pub fn u2_write_button_map(&mut self, slot: u8, mappings: &[(u8, u16)]) -> Result<()> {
        let row = self.ensure_command_allowed(CommandId::U2WriteButtonMap)?;
        let mut payload = row.request.to_vec();
        if payload.len() < 8 {
            return Err(BitdoError::InvalidInput(
                "U2WriteButtonMap payload shorter than expected".to_owned(),
            ));
        }

        payload[4] = slot;
        for (index, usage) in mappings {
            let pos = 8usize.saturating_add((*index as usize).saturating_mul(2));
            if pos + 1 < payload.len() {
                let bytes = usage.to_le_bytes();
                payload[pos] = bytes[0];
                payload[pos + 1] = bytes[1];
            }
        }

        self.send_row(row, Some(&payload))?;
        Ok(())
    }

    pub fn u2_set_mode(&mut self, mode: u8) -> Result<ModeState> {
        let row = self.ensure_command_allowed(CommandId::U2SetMode)?;
        let mut payload = row.request.to_vec();
        if payload.len() < 5 {
            return Err(BitdoError::InvalidInput(
                "U2SetMode payload shorter than expected".to_owned(),
            ));
        }

        payload[4] = mode;
        self.send_row(row, Some(&payload))?;
        Ok(ModeState {
            mode,
            source: "U2SetMode".to_owned(),
        })
    }

    pub fn enter_bootloader(&mut self) -> Result<()> {
        self.send_command(CommandId::EnterBootloaderA, None)?;
        self.send_command(CommandId::EnterBootloaderB, None)?;
        self.send_command(CommandId::EnterBootloaderC, None)?;
        Ok(())
    }

    pub fn firmware_transfer(
        &mut self,
        image: &[u8],
        chunk_size: usize,
        dry_run: bool,
    ) -> Result<FirmwareTransferReport> {
        if chunk_size == 0 {
            return Err(BitdoError::InvalidInput(
                "chunk size must be greater than zero".to_owned(),
            ));
        }

        let chunk_count = image.len().div_ceil(chunk_size);
        if dry_run {
            return Ok(FirmwareTransferReport {
                bytes_total: image.len(),
                chunk_size,
                chunks_sent: chunk_count,
                dry_run,
            });
        }

        let row = self.ensure_command_allowed(CommandId::FirmwareChunk)?;
        for chunk in image.chunks(chunk_size) {
            let mut payload = row.request.to_vec();
            let offset = 4;
            let copy_len = chunk.len().min(payload.len().saturating_sub(offset));
            if copy_len > 0 {
                payload[offset..offset + copy_len].copy_from_slice(&chunk[..copy_len]);
            }
            self.send_row(row, Some(&payload))?;
        }

        self.send_command(CommandId::FirmwareCommit, None)?;
        Ok(FirmwareTransferReport {
            bytes_total: image.len(),
            chunk_size,
            chunks_sent: chunk_count,
            dry_run,
        })
    }

    pub fn exit_bootloader(&mut self) -> Result<()> {
        self.send_command(CommandId::ExitBootloader, None)?;
        Ok(())
    }

    pub fn send_command(
        &mut self,
        command: CommandId,
        override_payload: Option<&[u8]>,
    ) -> Result<ResponseFrame> {
        let row = self.ensure_command_allowed(command)?;
        self.send_row(row, override_payload)
    }

    fn send_row(
        &mut self,
        row: &CommandRegistryRow,
        override_payload: Option<&[u8]>,
    ) -> Result<ResponseFrame> {
        let payload = override_payload.unwrap_or(row.request).to_vec();
        let frame = CommandFrame {
            id: row.id,
            payload,
            report_id: row.report_id,
            expected_response: row.expected_response,
        };
        let encoded = frame.encode();
        let bytes_written = self.transport.write(&encoded)?;

        if row.expected_response == "none" {
            let report = CommandExecutionReport {
                command: row.id,
                attempts: 1,
                validator: self.validator_name(row),
                status: ResponseStatus::Ok,
                bytes_written,
                bytes_read: 0,
                error_code: None,
            };
            self.record_execution(report);
            return Ok(ResponseFrame {
                raw: Vec::new(),
                status: ResponseStatus::Ok,
                parsed_fields: BTreeMap::new(),
            });
        }

        let timeout_ms = self.timeout_for_command(row);
        let expected_min_len = minimum_response_len(row.id);
        let attempts_total = self.config.retry_policy.max_attempts.max(1);

        let mut last_status = ResponseStatus::Malformed;
        let mut last_len = 0usize;

        for attempt in 1..=attempts_total {
            match self.read_response_reassembled(timeout_ms, expected_min_len) {
                Ok(raw) => {
                    let status = validate_response(row.id, &raw);
                    if status == ResponseStatus::Ok {
                        let report = CommandExecutionReport {
                            command: row.id,
                            attempts: attempt,
                            validator: self.validator_name(row),
                            status: ResponseStatus::Ok,
                            bytes_written,
                            bytes_read: raw.len(),
                            error_code: None,
                        };
                        self.record_execution(report);
                        return Ok(ResponseFrame {
                            parsed_fields: parse_fields(row.id, &raw),
                            raw,
                            status,
                        });
                    }
                    last_status = status;
                    last_len = raw.len();
                }
                Err(BitdoError::Timeout) => {
                    last_status = ResponseStatus::Malformed;
                    last_len = 0;
                }
                Err(err) => {
                    let report = CommandExecutionReport {
                        command: row.id,
                        attempts: attempt,
                        validator: self.validator_name(row),
                        status: ResponseStatus::Malformed,
                        bytes_written,
                        bytes_read: 0,
                        error_code: Some(err.code()),
                    };
                    self.record_execution(report);
                    return Err(err);
                }
            }

            if attempt < attempts_total && self.config.retry_policy.backoff_ms > 0 {
                thread::sleep(Duration::from_millis(self.config.retry_policy.backoff_ms));
            }
        }

        match last_status {
            ResponseStatus::Invalid => {
                let err = BitdoError::InvalidResponse {
                    command: row.id,
                    reason: "response signature mismatch".to_owned(),
                };
                let report = CommandExecutionReport {
                    command: row.id,
                    attempts: attempts_total,
                    validator: self.validator_name(row),
                    status: ResponseStatus::Invalid,
                    bytes_written,
                    bytes_read: last_len,
                    error_code: Some(err.code()),
                };
                self.record_execution(report);
                Err(err)
            }
            _ => {
                let err = BitdoError::MalformedResponse {
                    command: row.id,
                    len: last_len,
                };
                let report = CommandExecutionReport {
                    command: row.id,
                    attempts: attempts_total,
                    validator: self.validator_name(row),
                    status: ResponseStatus::Malformed,
                    bytes_written,
                    bytes_read: last_len,
                    error_code: Some(err.code()),
                };
                self.record_execution(report);
                Err(err)
            }
        }
    }

    fn read_response_reassembled(
        &mut self,
        timeout_ms: u64,
        expected_min_len: usize,
    ) -> Result<Vec<u8>> {
        let mut raw = Vec::new();

        // Some devices can split replies across multiple reads; reassemble bounded chunks.
        for _ in 0..3 {
            let chunk = self.transport.read(64, timeout_ms)?;
            if chunk.is_empty() {
                continue;
            }
            raw.extend_from_slice(&chunk);
            if raw.len() >= expected_min_len {
                break;
            }
        }

        if raw.is_empty() {
            return Err(BitdoError::Timeout);
        }
        Ok(raw)
    }

    fn record_execution(&mut self, report: CommandExecutionReport) {
        self.last_execution = Some(report.clone());
        if self.config.trace_enabled {
            self.trace.push(report);
        }
    }

    fn timeout_for_command(&self, row: &CommandRegistryRow) -> u64 {
        match row.safety_class {
            SafetyClass::UnsafeFirmware => self.config.timeout_profile.firmware_ms,
            SafetyClass::SafeRead => self.config.timeout_profile.probe_ms,
            SafetyClass::SafeWrite | SafetyClass::UnsafeBoot => self.config.timeout_profile.io_ms,
        }
    }

    fn validator_name(&self, row: &CommandRegistryRow) -> String {
        format!(
            "pid={:#06x};signature={}",
            self.target.pid, row.expected_response
        )
    }

    fn ensure_command_allowed(&self, command: CommandId) -> Result<&'static CommandRegistryRow> {
        let row = find_command(command).ok_or(BitdoError::UnknownCommand(command))?;

        // Gate 1: confidence/runtime policy.
        // We intentionally keep inferred write/unsafe paths non-executable until
        // they are upgraded to confirmed evidence.
        match row.runtime_policy() {
            CommandRuntimePolicy::EnabledDefault => {}
            CommandRuntimePolicy::ExperimentalGate => {
                if !self.config.experimental {
                    return Err(BitdoError::ExperimentalRequired { command });
                }
            }
            CommandRuntimePolicy::BlockedUntilConfirmed => {
                return Err(BitdoError::UnsupportedForPid {
                    command,
                    pid: self.target.pid,
                });
            }
        }

        // Gate 2: PID/family/capability applicability.
        if !is_command_allowed_by_family(self.profile.protocol_family, command)
            || !is_command_allowed_by_capability(self.profile.capability, command)
            || !command_applies_to_pid(row, self.target.pid)
        {
            return Err(BitdoError::UnsupportedForPid {
                command,
                pid: self.target.pid,
            });
        }

        // Gate 3: support-tier restrictions.
        if self.profile.support_tier == SupportTier::CandidateReadOnly
            && !is_command_allowed_for_candidate_pid(self.target.pid, command, row.safety_class)
        {
            return Err(BitdoError::UnsupportedForPid {
                command,
                pid: self.target.pid,
            });
        }

        // Gate 4: explicit unsafe confirmation requirements.
        if row.safety_class.is_unsafe() {
            if self.profile.support_tier != SupportTier::Full {
                return Err(BitdoError::UnsupportedForPid {
                    command,
                    pid: self.target.pid,
                });
            }
            if !(self.config.allow_unsafe && self.config.brick_risk_ack) {
                return Err(BitdoError::UnsafeCommandDenied { command });
            }
        }

        if row.safety_class == SafetyClass::SafeWrite
            && self.profile.support_tier != SupportTier::Full
        {
            return Err(BitdoError::UnsupportedForPid {
                command,
                pid: self.target.pid,
            });
        }

        Ok(row)
    }
}

fn classify_diag_failure(
    command: CommandId,
    runtime_policy: CommandRuntimePolicy,
    confidence: EvidenceConfidence,
    code: BitdoErrorCode,
    pid: u16,
) -> DiagSeverity {
    if runtime_policy != CommandRuntimePolicy::ExperimentalGate
        || confidence != EvidenceConfidence::Inferred
    {
        return DiagSeverity::Warning;
    }

    // Escalation is intentionally narrow for inferred checks:
    // - identity mismatch / impossible transitions
    // - command/schema applicability mismatch
    // - precondition/capability mismatches implied by unsupported errors
    let identity_or_transition_issue = matches!(
        (command, code),
        (CommandId::GetPid, BitdoErrorCode::InvalidResponse)
            | (CommandId::GetPid, BitdoErrorCode::MalformedResponse)
            | (CommandId::GetMode, BitdoErrorCode::InvalidResponse)
            | (CommandId::GetModeAlt, BitdoErrorCode::InvalidResponse)
            | (CommandId::ReadProfile, BitdoErrorCode::InvalidResponse)
            | (
                CommandId::GetControllerVersion,
                BitdoErrorCode::InvalidResponse
            )
            | (CommandId::Version, BitdoErrorCode::InvalidResponse)
    );
    if identity_or_transition_issue {
        return DiagSeverity::NeedsAttention;
    }

    if code == BitdoErrorCode::UnsupportedForPid
        && find_command(command)
            .map(|row| command_applies_to_pid(row, pid))
            .unwrap_or(false)
    {
        return DiagSeverity::NeedsAttention;
    }

    DiagSeverity::Warning
}

fn is_command_allowed_for_candidate_pid(pid: u16, command: CommandId, safety: SafetyClass) -> bool {
    if safety != SafetyClass::SafeRead {
        return false;
    }

    const BASE_DIAG_READS: &[CommandId] = &[
        CommandId::GetPid,
        CommandId::GetReportRevision,
        CommandId::GetControllerVersion,
        CommandId::Version,
        CommandId::Idle,
    ];
    const STANDARD_CANDIDATE_PIDS: &[u16] = &[
        0x6002, 0x6003, 0x3010, 0x3011, 0x3012, 0x3013, 0x3004, 0x3019, 0x3100, 0x3105, 0x2100,
        0x2101, 0x901a, 0x6006, 0x5203, 0x5204, 0x301a, 0x9028, 0x3026, 0x3027,
    ];
    const JP_CANDIDATE_PIDS: &[u16] = &[0x5200, 0x5201, 0x203a, 0x2049, 0x2028, 0x202e];

    if BASE_DIAG_READS.contains(&command) {
        return STANDARD_CANDIDATE_PIDS.contains(&pid) || JP_CANDIDATE_PIDS.contains(&pid);
    }

    if STANDARD_CANDIDATE_PIDS.contains(&pid) {
        return matches!(
            command,
            CommandId::GetMode | CommandId::GetModeAlt | CommandId::ReadProfile
        );
    }

    false
}

fn is_command_allowed_by_capability(cap: PidCapability, command: CommandId) -> bool {
    match command {
        CommandId::GetPid
        | CommandId::GetReportRevision
        | CommandId::GetControllerVersion
        | CommandId::Version
        | CommandId::Idle
        | CommandId::GetSuperButton => true,
        CommandId::GetMode | CommandId::GetModeAlt | CommandId::SetModeDInput => cap.supports_mode,
        CommandId::ReadProfile | CommandId::WriteProfile => cap.supports_profile_rw,
        CommandId::EnterBootloaderA
        | CommandId::EnterBootloaderB
        | CommandId::EnterBootloaderC
        | CommandId::ExitBootloader
        | CommandId::Jp108EnterBootloader
        | CommandId::Jp108ExitBootloader
        | CommandId::U2EnterBootloader
        | CommandId::U2ExitBootloader => cap.supports_boot,
        CommandId::FirmwareChunk
        | CommandId::FirmwareCommit
        | CommandId::Jp108FirmwareChunk
        | CommandId::Jp108FirmwareCommit
        | CommandId::U2FirmwareChunk
        | CommandId::U2FirmwareCommit => cap.supports_firmware,
        CommandId::Jp108ReadDedicatedMappings
        | CommandId::Jp108WriteDedicatedMapping
        | CommandId::Jp108ReadFeatureFlags
        | CommandId::Jp108WriteFeatureFlags
        | CommandId::Jp108ReadVoice
        | CommandId::Jp108WriteVoice => cap.supports_jp108_dedicated_map,
        CommandId::U2GetCurrentSlot
        | CommandId::U2ReadConfigSlot
        | CommandId::U2WriteConfigSlot => cap.supports_u2_slot_config,
        CommandId::U2ReadButtonMap | CommandId::U2WriteButtonMap | CommandId::U2SetMode => {
            cap.supports_u2_button_map
        }
    }
}

fn is_command_allowed_by_family(family: ProtocolFamily, command: CommandId) -> bool {
    match family {
        ProtocolFamily::Unknown => matches!(
            command,
            CommandId::GetPid
                | CommandId::GetReportRevision
                | CommandId::GetControllerVersion
                | CommandId::Version
                | CommandId::Idle
        ),
        ProtocolFamily::JpHandshake => !matches!(
            command,
            CommandId::SetModeDInput
                | CommandId::ReadProfile
                | CommandId::WriteProfile
                | CommandId::FirmwareChunk
                | CommandId::FirmwareCommit
                | CommandId::U2GetCurrentSlot
                | CommandId::U2ReadConfigSlot
                | CommandId::U2WriteConfigSlot
                | CommandId::U2ReadButtonMap
                | CommandId::U2WriteButtonMap
                | CommandId::U2SetMode
                | CommandId::U2EnterBootloader
                | CommandId::U2FirmwareChunk
                | CommandId::U2FirmwareCommit
                | CommandId::U2ExitBootloader
        ),
        ProtocolFamily::DS4Boot => matches!(
            command,
            CommandId::EnterBootloaderA
                | CommandId::EnterBootloaderB
                | CommandId::EnterBootloaderC
                | CommandId::ExitBootloader
                | CommandId::FirmwareChunk
                | CommandId::FirmwareCommit
                | CommandId::GetPid
        ),
        ProtocolFamily::Standard64 | ProtocolFamily::DInput => true,
    }
}

pub fn validate_response(command: CommandId, response: &[u8]) -> ResponseStatus {
    if response.len() < 2 {
        return ResponseStatus::Malformed;
    }

    match command {
        CommandId::GetPid => {
            if response.len() < 24 {
                return ResponseStatus::Malformed;
            }
            if response[0] == 0x02 && response[1] == 0x05 && response[4] == 0xC1 {
                ResponseStatus::Ok
            } else {
                ResponseStatus::Invalid
            }
        }
        CommandId::GetReportRevision => {
            if response.len() < 6 {
                return ResponseStatus::Malformed;
            }
            if response[0] == 0x02 && response[1] == 0x04 && response[5] == 0x01 {
                ResponseStatus::Ok
            } else {
                ResponseStatus::Invalid
            }
        }
        CommandId::GetMode | CommandId::GetModeAlt => {
            if response.len() < 6 {
                return ResponseStatus::Malformed;
            }
            if response[0] == 0x02 && response[1] == 0x05 {
                ResponseStatus::Ok
            } else {
                ResponseStatus::Invalid
            }
        }
        CommandId::Jp108ReadDedicatedMappings
        | CommandId::Jp108ReadFeatureFlags
        | CommandId::Jp108ReadVoice
        | CommandId::U2ReadConfigSlot
        | CommandId::U2ReadButtonMap
        | CommandId::U2GetCurrentSlot => {
            if response.len() < 6 {
                return ResponseStatus::Malformed;
            }
            if response[0] == 0x02 && response[1] == 0x05 {
                ResponseStatus::Ok
            } else {
                ResponseStatus::Invalid
            }
        }
        CommandId::GetControllerVersion | CommandId::Version => {
            if response.len() < 5 {
                return ResponseStatus::Malformed;
            }
            if response[0] == 0x02 && response[1] == 0x22 {
                ResponseStatus::Ok
            } else {
                ResponseStatus::Invalid
            }
        }
        CommandId::Idle => {
            if response[0] == 0x02 {
                ResponseStatus::Ok
            } else {
                ResponseStatus::Invalid
            }
        }
        CommandId::EnterBootloaderA
        | CommandId::EnterBootloaderB
        | CommandId::EnterBootloaderC
        | CommandId::ExitBootloader => ResponseStatus::Ok,
        _ => {
            if response[0] == 0x02 {
                ResponseStatus::Ok
            } else {
                ResponseStatus::Invalid
            }
        }
    }
}

fn minimum_response_len(command: CommandId) -> usize {
    match command {
        CommandId::GetPid => 24,
        CommandId::GetReportRevision => 6,
        CommandId::GetMode | CommandId::GetModeAlt => 6,
        CommandId::U2GetCurrentSlot => 6,
        CommandId::Jp108ReadDedicatedMappings
        | CommandId::Jp108ReadFeatureFlags
        | CommandId::Jp108ReadVoice
        | CommandId::U2ReadConfigSlot
        | CommandId::U2ReadButtonMap => 12,
        CommandId::GetControllerVersion | CommandId::Version => 5,
        _ => 2,
    }
}

fn parse_fields(command: CommandId, response: &[u8]) -> BTreeMap<String, u32> {
    let mut parsed = BTreeMap::new();
    match command {
        CommandId::GetPid if response.len() >= 24 => {
            let pid = u16::from_le_bytes([response[22], response[23]]);
            parsed.insert("detected_pid".to_owned(), pid as u32);
        }
        CommandId::GetMode | CommandId::GetModeAlt if response.len() >= 6 => {
            parsed.insert("mode".to_owned(), response[5] as u32);
        }
        CommandId::GetControllerVersion | CommandId::Version if response.len() >= 5 => {
            let fw = u16::from_le_bytes([response[2], response[3]]) as u32;
            parsed.insert("version_x100".to_owned(), fw);
            parsed.insert("beta".to_owned(), response[4] as u32);
        }
        CommandId::U2GetCurrentSlot if response.len() >= 6 => {
            parsed.insert("slot".to_owned(), response[5] as u32);
        }
        _ => {}
    }
    parsed
}

fn parse_indexed_u16_table(raw: &[u8], expected_items: usize) -> Vec<(u8, u16)> {
    let mut out = Vec::with_capacity(expected_items);
    let offset = if raw.len() >= 8 { 8 } else { 2 };

    for idx in 0..expected_items {
        let pos = offset + idx * 2;
        let usage = if pos + 1 < raw.len() {
            u16::from_le_bytes([raw[pos], raw[pos + 1]])
        } else {
            0
        };
        out.push((idx as u8, usage));
    }

    out
}
