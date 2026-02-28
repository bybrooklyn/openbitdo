use crate::command::CommandId;
use crate::error::{BitdoError, BitdoErrorCode, Result};
use crate::frame::{CommandFrame, ResponseFrame, ResponseStatus};
use crate::profile::ProfileBlob;
use crate::registry::{device_profile_for, find_command, find_pid, CommandRegistryRow};
use crate::transport::Transport;
use crate::types::{
    CommandConfidence, DeviceProfile, PidCapability, ProtocolFamily, SafetyClass, SupportEvidence,
    SupportLevel, VidPid,
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
    pub error_code: Option<BitdoErrorCode>,
    pub detail: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct DiagProbeResult {
    pub target: VidPid,
    pub profile_name: String,
    pub support_level: SupportLevel,
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
            protocol_family: profile.protocol_family,
            capability: profile.capability,
            evidence: profile.evidence,
            detected_pid,
        })
    }

    pub fn diag_probe(&mut self) -> DiagProbeResult {
        let checks = [
            CommandId::GetPid,
            CommandId::GetReportRevision,
            CommandId::GetMode,
            CommandId::GetControllerVersion,
        ]
        .iter()
        .map(|cmd| match self.send_command(*cmd, None) {
            Ok(_) => DiagCommandStatus {
                command: *cmd,
                ok: true,
                error_code: None,
                detail: "ok".to_owned(),
            },
            Err(err) => DiagCommandStatus {
                command: *cmd,
                ok: false,
                error_code: Some(err.code()),
                detail: err.to_string(),
            },
        })
        .collect::<Vec<_>>();

        DiagProbeResult {
            target: self.target,
            profile_name: self.profile.name.clone(),
            support_level: self.profile.support_level,
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

        if row.confidence == CommandConfidence::Inferred && !self.config.experimental {
            return Err(BitdoError::ExperimentalRequired { command });
        }

        if !is_command_allowed_by_family(self.profile.protocol_family, command)
            || !is_command_allowed_by_capability(self.profile.capability, command)
        {
            return Err(BitdoError::UnsupportedForPid {
                command,
                pid: self.target.pid,
            });
        }

        if row.safety_class.is_unsafe() {
            if self.profile.support_level != SupportLevel::Full {
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
            && self.profile.support_level == SupportLevel::DetectOnly
        {
            return Err(BitdoError::UnsupportedForPid {
                command,
                pid: self.target.pid,
            });
        }

        Ok(row)
    }
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
        | CommandId::ExitBootloader => cap.supports_boot,
        CommandId::FirmwareChunk | CommandId::FirmwareCommit => cap.supports_firmware,
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
        _ => {}
    }
    parsed
}
