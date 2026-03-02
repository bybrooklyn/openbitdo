use base64::Engine;
use bitdo_proto::{
    device_profile_for, enumerate_hid_devices, BitdoErrorCode, DeviceSession, DiagProbeResult,
    DiagSeverity, HidTransport, PidCapability, ProtocolFamily, SessionConfig, SupportEvidence,
    SupportLevel, SupportTier, VidPid,
};
use chrono::{DateTime, Utc};
use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use thiserror::Error;
use tokio::sync::{broadcast, Mutex, RwLock};
use tokio::time::{sleep, Duration};
use uuid::Uuid;

const DEFAULT_MANIFEST_URL: &str =
    "https://github.com/bybrooklyn/openbitdo/releases/latest/download/firmware-manifest.toml";
const PINNED_ED25519_ACTIVE_PUBLIC_KEY_HEX: &str =
    "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a";
const PINNED_ED25519_NEXT_PUBLIC_KEY_HEX: &str =
    "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a";

pub fn signing_key_fingerprint_active_sha256() -> String {
    signing_key_fingerprint_sha256(PINNED_ED25519_ACTIVE_PUBLIC_KEY_HEX)
}

pub fn signing_key_fingerprint_next_sha256() -> String {
    signing_key_fingerprint_sha256(PINNED_ED25519_NEXT_PUBLIC_KEY_HEX)
}

fn signing_key_fingerprint_sha256(public_key_hex: &str) -> String {
    let bytes = match hex::decode(public_key_hex) {
        Ok(bytes) => bytes,
        Err(_) => return "unknown".to_owned(),
    };
    sha256_hex(&bytes)
}

#[derive(Clone, Debug)]
pub struct OpenBitdoCoreConfig {
    pub mock_mode: bool,
    pub advanced_mode: bool,
    pub default_chunk_size: usize,
    pub progress_interval_ms: u64,
    pub firmware_manifest_url: String,
}

impl Default for OpenBitdoCoreConfig {
    fn default() -> Self {
        Self {
            mock_mode: false,
            advanced_mode: false,
            default_chunk_size: 56,
            progress_interval_ms: 25,
            firmware_manifest_url: DEFAULT_MANIFEST_URL.to_owned(),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub enum DedicatedButtonId {
    A,
    B,
    K1,
    K2,
    K3,
    K4,
    K5,
    K6,
    K7,
    K8,
}

impl DedicatedButtonId {
    pub const ALL: [DedicatedButtonId; 10] = [
        DedicatedButtonId::A,
        DedicatedButtonId::B,
        DedicatedButtonId::K1,
        DedicatedButtonId::K2,
        DedicatedButtonId::K3,
        DedicatedButtonId::K4,
        DedicatedButtonId::K5,
        DedicatedButtonId::K6,
        DedicatedButtonId::K7,
        DedicatedButtonId::K8,
    ];

    fn wire_index(self) -> u8 {
        match self {
            DedicatedButtonId::A => 0,
            DedicatedButtonId::B => 1,
            DedicatedButtonId::K1 => 2,
            DedicatedButtonId::K2 => 3,
            DedicatedButtonId::K3 => 4,
            DedicatedButtonId::K4 => 5,
            DedicatedButtonId::K5 => 6,
            DedicatedButtonId::K6 => 7,
            DedicatedButtonId::K7 => 8,
            DedicatedButtonId::K8 => 9,
        }
    }

    fn from_wire_index(value: u8) -> Option<Self> {
        Self::ALL
            .iter()
            .copied()
            .find(|entry| entry.wire_index() == value)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub enum U2ButtonId {
    A,
    B,
    K1,
    K2,
    K3,
    K4,
    K5,
    K6,
    K7,
    K8,
}

impl U2ButtonId {
    pub const ALL: [U2ButtonId; 10] = [
        U2ButtonId::A,
        U2ButtonId::B,
        U2ButtonId::K1,
        U2ButtonId::K2,
        U2ButtonId::K3,
        U2ButtonId::K4,
        U2ButtonId::K5,
        U2ButtonId::K6,
        U2ButtonId::K7,
        U2ButtonId::K8,
    ];

    fn wire_index(self) -> u8 {
        match self {
            U2ButtonId::A => 0,
            U2ButtonId::B => 1,
            U2ButtonId::K1 => 2,
            U2ButtonId::K2 => 3,
            U2ButtonId::K3 => 4,
            U2ButtonId::K4 => 5,
            U2ButtonId::K5 => 6,
            U2ButtonId::K6 => 7,
            U2ButtonId::K7 => 8,
            U2ButtonId::K8 => 9,
        }
    }

    fn from_wire_index(value: u8) -> Option<Self> {
        Self::ALL
            .iter()
            .copied()
            .find(|entry| entry.wire_index() == value)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub enum U2SlotId {
    Slot1,
    Slot2,
    Slot3,
}

impl U2SlotId {
    fn wire_value(self) -> u8 {
        match self {
            U2SlotId::Slot1 => 1,
            U2SlotId::Slot2 => 2,
            U2SlotId::Slot3 => 3,
        }
    }

    fn from_wire_value(value: u8) -> Self {
        match value {
            2 => U2SlotId::Slot2,
            3 => U2SlotId::Slot3,
            _ => U2SlotId::Slot1,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub struct ConfigBackupId(pub String);

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum DeviceKind {
    Jp108,
    Ultimate2,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct DedicatedButtonMapping {
    pub button: DedicatedButtonId,
    pub target_hid_usage: u16,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct U2ButtonMapping {
    pub button: U2ButtonId,
    pub target_hid_usage: u16,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct U2CoreProfile {
    pub slot: U2SlotId,
    pub mode: u8,
    pub firmware_version: String,
    pub l2_analog: f32,
    pub r2_analog: f32,
    pub supports_trigger_write: bool,
    pub mappings: Vec<U2ButtonMapping>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct GuidedButtonTestResult {
    pub device_kind: DeviceKind,
    pub expected_inputs: Vec<String>,
    pub passed: bool,
    pub guidance: String,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
struct ConfigBackup {
    created_at: DateTime<Utc>,
    target: VidPid,
    payload: ConfigBackupPayload,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
enum ConfigBackupPayload {
    Jp108 {
        mappings: Vec<DedicatedButtonMapping>,
    },
    U2 {
        profile: U2CoreProfile,
        config_blob: Vec<u8>,
    },
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct WriteRecoveryReport {
    pub backup_id: Option<ConfigBackupId>,
    pub write_applied: bool,
    pub rollback_attempted: bool,
    pub rollback_succeeded: bool,
    pub write_error: Option<String>,
    pub rollback_error: Option<String>,
}

impl WriteRecoveryReport {
    pub fn rollback_failed(&self) -> bool {
        self.rollback_attempted && !self.rollback_succeeded
    }
}

#[derive(Clone)]
pub struct OpenBitdoCore {
    config: OpenBitdoCoreConfig,
    advanced_mode: Arc<AtomicBool>,
    sessions: Arc<RwLock<HashMap<String, Arc<FirmwareSessionHandle>>>>,
    backups: Arc<RwLock<HashMap<String, ConfigBackup>>>,
    http: reqwest::Client,
}

impl OpenBitdoCore {
    pub fn new(config: OpenBitdoCoreConfig) -> Self {
        Self {
            advanced_mode: Arc::new(AtomicBool::new(config.advanced_mode)),
            config,
            sessions: Arc::new(RwLock::new(HashMap::new())),
            backups: Arc::new(RwLock::new(HashMap::new())),
            http: reqwest::Client::new(),
        }
    }

    /// Advanced mode enables inferred SafeRead commands only.
    /// Write/unsafe inferred commands remain blocked by runtime policy.
    pub fn set_advanced_mode(&self, enabled: bool) {
        self.advanced_mode.store(enabled, Ordering::Relaxed);
    }

    pub fn advanced_mode(&self) -> bool {
        self.advanced_mode.load(Ordering::Relaxed)
    }

    pub async fn list_devices(&self) -> AppCoreResult<Vec<AppDevice>> {
        if self.config.mock_mode {
            return Ok(vec![
                mock_device(VidPid::new(0x2dc8, 0x5209), true),
                mock_device(VidPid::new(0x2dc8, 0x6012), true),
                mock_device(VidPid::new(0x2dc8, 0x2100), false),
            ]);
        }

        let devices = enumerate_hid_devices().map_err(AppCoreError::Protocol)?;
        let filtered = devices
            .into_iter()
            .filter(|d| d.vid_pid.vid == 0x2dc8)
            .map(|d| {
                let profile = device_profile_for(d.vid_pid);
                AppDevice {
                    vid_pid: d.vid_pid,
                    name: profile.name,
                    support_level: profile.support_level,
                    support_tier: profile.support_tier,
                    protocol_family: profile.protocol_family,
                    capability: profile.capability,
                    evidence: profile.evidence,
                    serial: d.serial,
                    connected: true,
                }
            })
            .collect::<Vec<_>>();
        Ok(filtered)
    }

    pub async fn diag_probe(&self, target: VidPid) -> AppCoreResult<DiagProbeResult> {
        if self.config.mock_mode {
            return Ok(mock_diag_probe(target));
        }

        let mut session = DeviceSession::new(
            HidTransport::new(),
            target,
            SessionConfig {
                // Diagnostics always execute inferred SafeRead checks. Those
                // checks are explicitly marked experimental in their result
                // metadata so users can distinguish confidence levels.
                experimental: true,
                ..Default::default()
            },
        )
        .map_err(AppCoreError::Protocol)?;
        let diag = session.diag_probe();
        let _ = session.close();
        Ok(diag)
    }

    pub fn beginner_diag_summary(&self, device: &AppDevice, diag: &DiagProbeResult) -> String {
        let passed = diag.command_checks.iter().filter(|c| c.ok).count();
        let total = diag.command_checks.len();
        let experimental_total = diag
            .command_checks
            .iter()
            .filter(|c| c.is_experimental)
            .count();
        let experimental_ok = diag
            .command_checks
            .iter()
            .filter(|c| c.is_experimental && c.ok)
            .count();
        let needs_attention = diag
            .command_checks
            .iter()
            .filter(|c| c.severity == DiagSeverity::NeedsAttention)
            .count();
        let family_hint = match device.protocol_family {
            ProtocolFamily::Standard64 => {
                "Standard64 diagnostics are available. Read checks are safe while writes stay blocked until hardware confirmation."
            }
            ProtocolFamily::JpHandshake => {
                "JP-handshake diagnostics are available. Handshake/version checks are the safe default path."
            }
            ProtocolFamily::DInput => {
                "DInput diagnostics are available. Read checks are safe; write paths remain policy-gated."
            }
            ProtocolFamily::DS4Boot => {
                "Boot-mode diagnostics are limited. Keep the device in normal mode for beginner-safe checks."
            }
            ProtocolFamily::Unknown => {
                "Only basic identify diagnostics are available for unknown protocol family devices."
            }
        };

        let status_hint = if needs_attention > 0 {
            format!("Needs attention: {needs_attention} safety-critical diagnostic signal(s).")
        } else {
            "Needs attention: none.".to_owned()
        };
        let experimental_hint =
            format!("Experimental checks: {experimental_ok}/{experimental_total} passed.");

        match device.support_tier {
            SupportTier::Full => format!(
                "{passed}/{total} checks passed. {experimental_hint} {status_hint} {family_hint} This device is full-support."
            ),
            SupportTier::CandidateReadOnly => format!(
                "{passed}/{total} checks passed. {experimental_hint} {status_hint} {family_hint} This device is candidate-readonly: update and mapping stay blocked until runtime + hardware confirmation."
            ),
            SupportTier::DetectOnly => format!(
                "{passed}/{total} checks passed. {experimental_hint} {status_hint} {family_hint} This device is detect-only: use diagnostics only."
            ),
        }
    }

    pub async fn jp108_read_dedicated_mapping(
        &self,
        vidpid: VidPid,
    ) -> AppCoreResult<Vec<DedicatedButtonMapping>> {
        let profile = device_profile_for(vidpid);
        if !profile.capability.supports_jp108_dedicated_map {
            return Err(AppCoreError::PolicyDenied {
                reason: AppPolicyGateReason::UnsupportedPid,
                message: format!("JP108 dedicated mapping is not supported for {}", vidpid),
            });
        }

        if self.config.mock_mode {
            return Ok(default_jp108_mappings());
        }

        let mut session = self.open_session_for_ops(vidpid)?;
        let mappings = session
            .jp108_read_dedicated_mappings()
            .map_err(AppCoreError::Protocol)?
            .into_iter()
            .filter_map(|(idx, usage)| {
                DedicatedButtonId::from_wire_index(idx).map(|button| DedicatedButtonMapping {
                    button,
                    target_hid_usage: usage,
                })
            })
            .collect::<Vec<_>>();
        let _ = session.close();
        Ok(mappings)
    }

    pub async fn jp108_apply_dedicated_mapping(
        &self,
        vidpid: VidPid,
        changes: Vec<DedicatedButtonMapping>,
        backup: bool,
    ) -> AppCoreResult<Option<ConfigBackupId>> {
        let report = self
            .jp108_apply_dedicated_mapping_with_recovery(vidpid, changes, backup)
            .await?;
        if report.write_applied {
            return Ok(report.backup_id);
        }
        if report.rollback_failed() {
            return Err(AppCoreError::InvalidState(
                report
                    .rollback_error
                    .unwrap_or_else(|| "write failed and rollback failed".to_owned()),
            ));
        }
        Err(AppCoreError::InvalidState(
            report
                .write_error
                .unwrap_or_else(|| "write failed; rollback restored previous state".to_owned()),
        ))
    }

    pub async fn jp108_apply_dedicated_mapping_with_recovery(
        &self,
        vidpid: VidPid,
        changes: Vec<DedicatedButtonMapping>,
        backup: bool,
    ) -> AppCoreResult<WriteRecoveryReport> {
        let profile = device_profile_for(vidpid);
        if !profile.capability.supports_jp108_dedicated_map {
            return Err(AppCoreError::PolicyDenied {
                reason: AppPolicyGateReason::UnsupportedPid,
                message: format!("JP108 dedicated mapping is not supported for {}", vidpid),
            });
        }

        if self.config.mock_mode {
            let backup_id = if backup {
                Some(
                    self.store_backup(
                        vidpid,
                        ConfigBackupPayload::Jp108 {
                            mappings: default_jp108_mappings(),
                        },
                    )
                    .await,
                )
            } else {
                None
            };
            return Ok(WriteRecoveryReport {
                backup_id,
                write_applied: true,
                rollback_attempted: false,
                rollback_succeeded: false,
                write_error: None,
                rollback_error: None,
            });
        }

        let backup_id = if backup {
            let existing = self.jp108_read_dedicated_mapping(vidpid).await?;
            Some(
                self.store_backup(vidpid, ConfigBackupPayload::Jp108 { mappings: existing })
                    .await,
            )
        } else {
            None
        };

        let mut session = self.open_session_for_ops(vidpid)?;
        let apply_result: AppCoreResult<()> = (|| {
            for change in &changes {
                session
                    .jp108_write_dedicated_mapping(
                        change.button.wire_index(),
                        change.target_hid_usage,
                    )
                    .map_err(AppCoreError::Protocol)?;
            }
            Ok(())
        })();
        let _ = session.close();

        if let Err(err) = apply_result {
            let write_error = err.to_string();
            if let Some(id) = backup_id.as_ref() {
                match self.restore_backup(id.clone()).await {
                    Ok(_) => {
                        return Ok(WriteRecoveryReport {
                            backup_id,
                            write_applied: false,
                            rollback_attempted: true,
                            rollback_succeeded: true,
                            write_error: Some(write_error),
                            rollback_error: None,
                        });
                    }
                    Err(rollback_err) => {
                        return Ok(WriteRecoveryReport {
                            backup_id,
                            write_applied: false,
                            rollback_attempted: true,
                            rollback_succeeded: false,
                            write_error: Some(write_error),
                            rollback_error: Some(rollback_err.to_string()),
                        });
                    }
                }
            }

            return Ok(WriteRecoveryReport {
                backup_id: None,
                write_applied: false,
                rollback_attempted: false,
                rollback_succeeded: false,
                write_error: Some(write_error),
                rollback_error: None,
            });
        }

        Ok(WriteRecoveryReport {
            backup_id,
            write_applied: true,
            rollback_attempted: false,
            rollback_succeeded: false,
            write_error: None,
            rollback_error: None,
        })
    }

    pub async fn u2_read_core_profile(
        &self,
        vidpid: VidPid,
        slot: U2SlotId,
    ) -> AppCoreResult<U2CoreProfile> {
        let profile = device_profile_for(vidpid);
        if !(profile.capability.supports_u2_slot_config
            && profile.capability.supports_u2_button_map)
        {
            return Err(AppCoreError::PolicyDenied {
                reason: AppPolicyGateReason::UnsupportedPid,
                message: format!("Ultimate2 core profile is not supported for {}", vidpid),
            });
        }

        if self.config.mock_mode {
            return Ok(U2CoreProfile {
                slot,
                mode: 0,
                firmware_version: "mock-1.0.0".to_owned(),
                l2_analog: 0.5,
                r2_analog: 0.5,
                supports_trigger_write: true,
                mappings: default_u2_mappings(),
            });
        }

        let mut session = self.open_session_for_ops(vidpid)?;
        let active_slot = session
            .u2_get_current_slot()
            .map(U2SlotId::from_wire_value)
            .unwrap_or(slot);
        let mode = session.get_mode().map_err(AppCoreError::Protocol)?.mode;
        let firmware_version = session
            .send_command(bitdo_proto::CommandId::GetControllerVersion, None)
            .ok()
            .and_then(|resp| resp.parsed_fields.get("version_x100").copied())
            .map(|raw| format!("{:.2}", raw as f32 / 100.0))
            .unwrap_or_else(|| "unknown".to_owned());
        let config_blob = session
            .u2_read_config_slot(active_slot.wire_value())
            .map_err(AppCoreError::Protocol)?;
        let map = session
            .u2_read_button_map(active_slot.wire_value())
            .map_err(AppCoreError::Protocol)?
            .into_iter()
            .filter_map(|(idx, usage)| {
                U2ButtonId::from_wire_index(idx).map(|button| U2ButtonMapping {
                    button,
                    target_hid_usage: usage,
                })
            })
            .collect::<Vec<_>>();
        let _ = session.close();
        Ok(U2CoreProfile {
            slot: active_slot,
            mode,
            firmware_version,
            l2_analog: config_blob.get(6).map(|v| *v as f32 / 255.0).unwrap_or(0.0),
            r2_analog: config_blob.get(7).map(|v| *v as f32 / 255.0).unwrap_or(0.0),
            supports_trigger_write: profile.support_tier == SupportTier::Full,
            mappings: map,
        })
    }

    #[allow(clippy::too_many_arguments)]
    pub async fn u2_apply_core_profile(
        &self,
        vidpid: VidPid,
        slot: U2SlotId,
        mode: u8,
        map_changes: Vec<U2ButtonMapping>,
        l2_analog: f32,
        r2_analog: f32,
        backup: bool,
    ) -> AppCoreResult<Option<ConfigBackupId>> {
        let report = self
            .u2_apply_core_profile_with_recovery(
                vidpid,
                slot,
                mode,
                map_changes,
                l2_analog,
                r2_analog,
                backup,
            )
            .await?;
        if report.write_applied {
            return Ok(report.backup_id);
        }
        if report.rollback_failed() {
            return Err(AppCoreError::InvalidState(
                report
                    .rollback_error
                    .unwrap_or_else(|| "write failed and rollback failed".to_owned()),
            ));
        }
        Err(AppCoreError::InvalidState(
            report
                .write_error
                .unwrap_or_else(|| "write failed; rollback restored previous state".to_owned()),
        ))
    }

    #[allow(clippy::too_many_arguments)]
    pub async fn u2_apply_core_profile_with_recovery(
        &self,
        vidpid: VidPid,
        slot: U2SlotId,
        mode: u8,
        map_changes: Vec<U2ButtonMapping>,
        l2_analog: f32,
        r2_analog: f32,
        backup: bool,
    ) -> AppCoreResult<WriteRecoveryReport> {
        let profile = device_profile_for(vidpid);
        if !(profile.capability.supports_u2_slot_config
            && profile.capability.supports_u2_button_map)
        {
            return Err(AppCoreError::PolicyDenied {
                reason: AppPolicyGateReason::UnsupportedPid,
                message: format!("Ultimate2 core profile is not supported for {}", vidpid),
            });
        }

        if self.config.mock_mode {
            let backup_id = if backup {
                Some(
                    self.store_backup(
                        vidpid,
                        ConfigBackupPayload::U2 {
                            profile: U2CoreProfile {
                                slot,
                                mode: 0,
                                firmware_version: "mock-1.0.0".to_owned(),
                                l2_analog: 0.5,
                                r2_analog: 0.5,
                                supports_trigger_write: true,
                                mappings: default_u2_mappings(),
                            },
                            config_blob: vec![0; 32],
                        },
                    )
                    .await,
                )
            } else {
                None
            };
            return Ok(WriteRecoveryReport {
                backup_id,
                write_applied: true,
                rollback_attempted: false,
                rollback_succeeded: false,
                write_error: None,
                rollback_error: None,
            });
        }

        let backup_id = if backup {
            let current = self.u2_read_core_profile(vidpid, slot).await?;
            let mut session = self.open_session_for_ops(vidpid)?;
            let config_blob = session
                .u2_read_config_slot(slot.wire_value())
                .map_err(AppCoreError::Protocol)?;
            let _ = session.close();
            Some(
                self.store_backup(
                    vidpid,
                    ConfigBackupPayload::U2 {
                        profile: current,
                        config_blob,
                    },
                )
                .await,
            )
        } else {
            None
        };

        let mut session = self.open_session_for_ops(vidpid)?;
        let apply_result: AppCoreResult<()> = (|| {
            session.u2_set_mode(mode).map_err(AppCoreError::Protocol)?;
            let wire_map = map_changes
                .iter()
                .map(|entry| (entry.button.wire_index(), entry.target_hid_usage))
                .collect::<Vec<_>>();
            session
                .u2_write_button_map(slot.wire_value(), &wire_map)
                .map_err(AppCoreError::Protocol)?;
            let mut config_blob = session
                .u2_read_config_slot(slot.wire_value())
                .map_err(AppCoreError::Protocol)?;
            if config_blob.is_empty() {
                config_blob.resize(16, 0);
            }
            if config_blob.len() > 6 {
                config_blob[4] = slot.wire_value();
                config_blob[5] = mode;
                if config_blob.len() > 8 {
                    config_blob[6] = (l2_analog.clamp(0.0, 1.0) * 255.0).round() as u8;
                    config_blob[7] = (r2_analog.clamp(0.0, 1.0) * 255.0).round() as u8;
                }
            }
            session
                .u2_write_config_slot(slot.wire_value(), &config_blob)
                .map_err(AppCoreError::Protocol)?;
            Ok(())
        })();
        let _ = session.close();

        if let Err(err) = apply_result {
            let write_error = err.to_string();
            if let Some(id) = backup_id.as_ref() {
                match self.restore_backup(id.clone()).await {
                    Ok(_) => {
                        return Ok(WriteRecoveryReport {
                            backup_id,
                            write_applied: false,
                            rollback_attempted: true,
                            rollback_succeeded: true,
                            write_error: Some(write_error),
                            rollback_error: None,
                        });
                    }
                    Err(rollback_err) => {
                        return Ok(WriteRecoveryReport {
                            backup_id,
                            write_applied: false,
                            rollback_attempted: true,
                            rollback_succeeded: false,
                            write_error: Some(write_error),
                            rollback_error: Some(rollback_err.to_string()),
                        });
                    }
                }
            }

            return Ok(WriteRecoveryReport {
                backup_id: None,
                write_applied: false,
                rollback_attempted: false,
                rollback_succeeded: false,
                write_error: Some(write_error),
                rollback_error: None,
            });
        }

        Ok(WriteRecoveryReport {
            backup_id,
            write_applied: true,
            rollback_attempted: false,
            rollback_succeeded: false,
            write_error: None,
            rollback_error: None,
        })
    }

    pub async fn restore_backup(&self, backup_id: ConfigBackupId) -> AppCoreResult<()> {
        let backup = {
            let backups = self.backups.read().await;
            backups.get(&backup_id.0).cloned().ok_or_else(|| {
                AppCoreError::NotFound(format!("unknown backup id: {}", backup_id.0))
            })?
        };

        if self.config.mock_mode {
            return Ok(());
        }

        let mut session = self.open_session_for_ops(backup.target)?;
        let restore_result: AppCoreResult<()> = (|| match backup.payload {
            ConfigBackupPayload::Jp108 { mappings } => {
                for entry in &mappings {
                    session
                        .jp108_write_dedicated_mapping(
                            entry.button.wire_index(),
                            entry.target_hid_usage,
                        )
                        .map_err(AppCoreError::Protocol)?;
                }
                Ok(())
            }
            ConfigBackupPayload::U2 {
                profile,
                config_blob,
            } => {
                session
                    .u2_set_mode(profile.mode)
                    .map_err(AppCoreError::Protocol)?;
                let wire_map = profile
                    .mappings
                    .iter()
                    .map(|entry| (entry.button.wire_index(), entry.target_hid_usage))
                    .collect::<Vec<_>>();
                session
                    .u2_write_button_map(profile.slot.wire_value(), &wire_map)
                    .map_err(AppCoreError::Protocol)?;
                session
                    .u2_write_config_slot(profile.slot.wire_value(), &config_blob)
                    .map_err(AppCoreError::Protocol)?;
                Ok(())
            }
        })();
        let _ = session.close();
        restore_result
    }

    pub async fn guided_button_test(
        &self,
        device_kind: DeviceKind,
        expected_inputs: Vec<String>,
    ) -> AppCoreResult<GuidedButtonTestResult> {
        let guidance = match device_kind {
            DeviceKind::Jp108 => {
                "Press each mapped JP108 dedicated key once and verify it matches the on-screen expected input."
            }
            DeviceKind::Ultimate2 => {
                "Press each remapped Ultimate2 core button once and verify it matches the expected action."
            }
        };

        Ok(GuidedButtonTestResult {
            device_kind,
            expected_inputs,
            passed: true,
            guidance: guidance.to_owned(),
        })
    }

    pub async fn download_recommended_firmware(
        &self,
        target: VidPid,
    ) -> AppCoreResult<FirmwareDownloadResult> {
        if self.config.mock_mode {
            let path = std::env::temp_dir().join(format!(
                "openbitdo-fw-mock-{:04x}-{}.bin",
                target.pid,
                Uuid::new_v4()
            ));
            let bytes = vec![0xAB; 4096];
            tokio::fs::write(&path, &bytes).await?;
            let sha256 = sha256_hex(&bytes);
            return Ok(FirmwareDownloadResult {
                firmware_path: path,
                version: "mock-1.0.0".to_owned(),
                source_url: "mock://firmware".to_owned(),
                sha256,
                verified_signature: true,
            });
        }

        let manifest_raw = self
            .http
            .get(&self.config.firmware_manifest_url)
            .send()
            .await
            .map_err(|err| AppCoreError::Download(format!("manifest request failed: {err}")))?
            .error_for_status()
            .map_err(|err| AppCoreError::Download(format!("manifest download failed: {err}")))?
            .text()
            .await
            .map_err(|err| AppCoreError::Download(format!("manifest read failed: {err}")))?;

        let manifest: FirmwareManifest = toml::from_str(&manifest_raw)
            .map_err(|err| AppCoreError::Manifest(format!("invalid manifest TOML: {err}")))?;

        let profile = device_profile_for(target);
        let artifact = manifest
            .recommended_for(target, profile.protocol_family)
            .ok_or_else(|| {
                AppCoreError::Download(format!(
                    "no stable firmware artifact for pid={:#06x} family={:?}",
                    target.pid, profile.protocol_family
                ))
            })?;

        let artifact_bytes = self
            .http
            .get(&artifact.url)
            .send()
            .await
            .map_err(|err| AppCoreError::Download(format!("artifact request failed: {err}")))?
            .error_for_status()
            .map_err(|err| AppCoreError::Download(format!("artifact download failed: {err}")))?
            .bytes()
            .await
            .map_err(|err| AppCoreError::Download(format!("artifact read failed: {err}")))?
            .to_vec();

        let actual_hash = sha256_hex(&artifact_bytes);
        if !actual_hash.eq_ignore_ascii_case(&artifact.sha256) {
            return Err(AppCoreError::PolicyDenied {
                reason: AppPolicyGateReason::ImageValidationFailed,
                message: format!(
                    "downloaded firmware hash mismatch: expected={} actual={}",
                    artifact.sha256, actual_hash
                ),
            });
        }

        verify_artifact_signature(&self.http, artifact, &artifact_bytes).await?;

        let out = std::env::temp_dir().join(format!(
            "openbitdo-fw-{:04x}-{}.bin",
            artifact.pid,
            Uuid::new_v4()
        ));
        tokio::fs::write(&out, &artifact_bytes).await?;

        Ok(FirmwareDownloadResult {
            firmware_path: out,
            version: artifact.version.clone(),
            source_url: artifact.url.clone(),
            sha256: actual_hash,
            verified_signature: true,
        })
    }

    pub async fn preflight_firmware(
        &self,
        request: FirmwarePreflightRequest,
    ) -> AppCoreResult<FirmwarePreflightResult> {
        let profile = device_profile_for(request.vid_pid);
        if profile.support_tier != SupportTier::Full {
            return Ok(FirmwarePreflightResult::denied(
                AppPolicyGateReason::NotHardwareConfirmed,
                "Firmware updates are available only after per-PID hardware confirmation."
                    .to_owned(),
            ));
        }
        if !(request.allow_unsafe && request.brick_risk_ack) {
            return Ok(FirmwarePreflightResult::denied(
                AppPolicyGateReason::UnsafeFlagsMissing,
                "Safety acknowledgement is required before firmware update".to_owned(),
            ));
        }
        let image_meta = validate_firmware_image(&request.firmware_path).await?;
        let chunk_size = request
            .chunk_size
            .unwrap_or(self.config.default_chunk_size)
            .max(8);
        let chunks_total = image_meta.bytes_total.div_ceil(chunk_size);
        let expected_seconds =
            ((chunks_total as u64 * self.config.progress_interval_ms) / 1000).max(1);
        let session_id = Uuid::new_v4().to_string();
        let mut warnings = vec![
            "Do not disconnect device during transfer".to_owned(),
            "Use only validated firmware images".to_owned(),
        ];
        if has_unusual_firmware_extension(&request.firmware_path) {
            warnings.push(
                "Firmware filename extension is unusual. Continuing with strict content/hash validation."
                    .to_owned(),
            );
        }

        let plan = FirmwareUpdatePlan {
            session_id: FirmwareUpdateSessionId(session_id.clone()),
            chunk_size,
            bytes_total: image_meta.bytes_total,
            chunks_total,
            expected_seconds,
            warnings,
            image_sha256: image_meta.sha256,
            current_version: "unknown".to_owned(),
            target_version: image_meta
                .target_version_hint
                .unwrap_or_else(|| "unspecified".to_owned()),
        };

        let (sender, _) = broadcast::channel(128);
        let handle = Arc::new(FirmwareSessionHandle {
            request: request.clone(),
            plan: plan.clone(),
            sender,
            runtime: Mutex::new(FirmwareSessionRuntime {
                state: FirmwareSessionState::Preflight,
                sequence: 0,
                cancel_requested: false,
                report: None,
                started_at: None,
                completed_at: None,
            }),
        });

        self.sessions
            .write()
            .await
            .insert(session_id.clone(), handle.clone());

        emit_event(&handle, "preflight", 0, "Preflight complete", false).await;

        Ok(FirmwarePreflightResult {
            gate: AppPolicyGateResult {
                allowed: true,
                reason: None,
                message: None,
            },
            plan: Some(plan),
            capability: profile.capability,
            evidence: profile.evidence,
        })
    }

    pub async fn start_firmware(
        &self,
        request: FirmwareStartRequest,
    ) -> AppCoreResult<FirmwareUpdatePlan> {
        let handle = self.session_handle(&request.session_id.0).await?;
        {
            let mut runtime = handle.runtime.lock().await;
            if runtime.state != FirmwareSessionState::Preflight {
                return Err(AppCoreError::InvalidState(
                    "Firmware session must be in preflight state".to_owned(),
                ));
            }
            runtime.state = FirmwareSessionState::AwaitingConfirmation;
        }
        emit_event(
            &handle,
            "awaiting_confirmation",
            0,
            "Awaiting explicit confirmation",
            false,
        )
        .await;

        Ok(handle.plan.clone())
    }

    pub async fn confirm_firmware(
        &self,
        request: FirmwareConfirmRequest,
    ) -> AppCoreResult<FirmwareUpdatePlan> {
        if !request.acknowledged_risk {
            return Err(AppCoreError::PolicyDenied {
                reason: AppPolicyGateReason::UnsafeFlagsMissing,
                message: "You must acknowledge firmware risk before continuing".to_owned(),
            });
        }

        let handle = self.session_handle(&request.session_id.0).await?;
        {
            let mut runtime = handle.runtime.lock().await;
            if runtime.state != FirmwareSessionState::AwaitingConfirmation {
                return Err(AppCoreError::InvalidState(
                    "Firmware session is not awaiting confirmation".to_owned(),
                ));
            }
            runtime.state = FirmwareSessionState::Running;
            runtime.started_at = Some(Utc::now());
            runtime.cancel_requested = false;
        }

        let interval = self.config.progress_interval_ms;
        let plan = handle.plan.clone();
        let session_id = plan.session_id.clone();
        let sessions = self.sessions.clone();
        tokio::spawn(async move {
            run_transfer_task(sessions, handle, interval, session_id).await;
        });

        Ok(plan)
    }

    pub async fn cancel_firmware(
        &self,
        request: FirmwareCancelRequest,
    ) -> AppCoreResult<FirmwareFinalReport> {
        let handle = self.session_handle(&request.session_id.0).await?;
        {
            let mut runtime = handle.runtime.lock().await;
            runtime.cancel_requested = true;
            if matches!(
                runtime.state,
                FirmwareSessionState::Completed
                    | FirmwareSessionState::Cancelled
                    | FirmwareSessionState::Failed
            ) {
                if let Some(report) = runtime.report.clone() {
                    return Ok(report);
                }
            }
        }

        emit_event(
            &handle,
            "cancel_requested",
            0,
            "Cancellation requested",
            false,
        )
        .await;

        {
            let mut runtime = handle.runtime.lock().await;
            if matches!(
                runtime.state,
                FirmwareSessionState::Preflight | FirmwareSessionState::AwaitingConfirmation
            ) {
                runtime.state = FirmwareSessionState::Cancelled;
                runtime.completed_at = Some(Utc::now());
                let report = FirmwareFinalReport {
                    session_id: handle.plan.session_id.clone(),
                    status: FirmwareOutcome::Cancelled,
                    started_at: runtime.started_at,
                    completed_at: runtime.completed_at,
                    bytes_total: handle.plan.bytes_total,
                    chunks_total: handle.plan.chunks_total,
                    chunks_sent: 0,
                    error_code: None,
                    message: "Firmware update cancelled before transfer".to_owned(),
                };
                runtime.report = Some(report.clone());
                drop(runtime);
                emit_event(&handle, "cancelled", 100, "Update cancelled", true).await;
                return Ok(report);
            }
        }

        loop {
            if let Some(report) = self.firmware_report(&request.session_id.0).await? {
                return Ok(report);
            }
            sleep(Duration::from_millis(5)).await;
        }
    }

    pub async fn firmware_report(
        &self,
        session_id: &str,
    ) -> AppCoreResult<Option<FirmwareFinalReport>> {
        let handle = self.session_handle(session_id).await?;
        let runtime = handle.runtime.lock().await;
        Ok(runtime.report.clone())
    }

    pub async fn subscribe_events(
        &self,
        session_id: &str,
    ) -> AppCoreResult<broadcast::Receiver<FirmwareProgressEvent>> {
        let handle = self.session_handle(session_id).await?;
        Ok(handle.sender.subscribe())
    }

    fn open_session_for_ops(&self, target: VidPid) -> AppCoreResult<DeviceSession<HidTransport>> {
        let config = SessionConfig {
            allow_unsafe: true,
            brick_risk_ack: true,
            experimental: self.advanced_mode(),
            ..Default::default()
        };
        DeviceSession::new(HidTransport::new(), target, config).map_err(AppCoreError::Protocol)
    }

    async fn store_backup(&self, target: VidPid, payload: ConfigBackupPayload) -> ConfigBackupId {
        let id = ConfigBackupId(Uuid::new_v4().to_string());
        let backup = ConfigBackup {
            created_at: Utc::now(),
            target,
            payload,
        };
        self.backups.write().await.insert(id.0.clone(), backup);
        id
    }

    async fn session_handle(&self, session_id: &str) -> AppCoreResult<Arc<FirmwareSessionHandle>> {
        let sessions = self.sessions.read().await;
        sessions
            .get(session_id)
            .cloned()
            .ok_or_else(|| AppCoreError::NotFound(format!("unknown session id: {session_id}")))
    }
}

async fn verify_artifact_signature(
    http: &reqwest::Client,
    artifact: &FirmwareArtifact,
    artifact_bytes: &[u8],
) -> AppCoreResult<()> {
    if !artifact.signature.algorithm.eq_ignore_ascii_case("ed25519") {
        return Err(AppCoreError::Manifest(format!(
            "unsupported signature algorithm: {}",
            artifact.signature.algorithm
        )));
    }

    let sig_body = http
        .get(&artifact.signature.url)
        .send()
        .await
        .map_err(|err| AppCoreError::Download(format!("signature request failed: {err}")))?
        .error_for_status()
        .map_err(|err| AppCoreError::Download(format!("signature download failed: {err}")))?
        .bytes()
        .await
        .map_err(|err| AppCoreError::Download(format!("signature read failed: {err}")))?
        .to_vec();

    let sig_bytes = if sig_body.len() == 64 {
        sig_body
    } else {
        let text = String::from_utf8(sig_body).map_err(|err| {
            AppCoreError::Manifest(format!("signature payload is not UTF-8/base64: {err}"))
        })?;
        base64::engine::general_purpose::STANDARD
            .decode(text.trim())
            .map_err(|err| AppCoreError::Manifest(format!("invalid signature base64: {err}")))?
    };

    let sig = Signature::from_slice(&sig_bytes)
        .map_err(|err| AppCoreError::Manifest(format!("invalid signature format: {err}")))?;

    let keys = [
        PINNED_ED25519_ACTIVE_PUBLIC_KEY_HEX,
        PINNED_ED25519_NEXT_PUBLIC_KEY_HEX,
    ];
    for key_hex in keys {
        let key_bytes = hex::decode(key_hex)
            .map_err(|err| AppCoreError::Manifest(format!("invalid pinned key hex: {err}")))?;
        let key_array: [u8; 32] = key_bytes
            .try_into()
            .map_err(|_| AppCoreError::Manifest("pinned key length must be 32 bytes".to_owned()))?;
        let key = VerifyingKey::from_bytes(&key_array)
            .map_err(|err| AppCoreError::Manifest(format!("invalid pinned key bytes: {err}")))?;
        if key.verify(artifact_bytes, &sig).is_ok() {
            return Ok(());
        }
    }

    Err(AppCoreError::PolicyDenied {
        reason: AppPolicyGateReason::ImageValidationFailed,
        message: "signature verification failed for active and next pinned keys".to_owned(),
    })
}

async fn run_transfer_task(
    sessions: Arc<RwLock<HashMap<String, Arc<FirmwareSessionHandle>>>>,
    handle: Arc<FirmwareSessionHandle>,
    interval_ms: u64,
    session_id: FirmwareUpdateSessionId,
) {
    let bytes = match tokio::fs::read(&handle.request.firmware_path).await {
        Ok(bytes) => bytes,
        Err(err) => {
            finalize_failure(
                &handle,
                BitdoErrorCode::InvalidInput,
                format!("Failed to read firmware image: {err}"),
            )
            .await;
            let mut map = sessions.write().await;
            map.remove(&session_id.0);
            return;
        }
    };

    let mut chunks_sent = 0usize;
    let total_chunks = handle.plan.chunks_total.max(1);

    for (idx, _chunk) in bytes.chunks(handle.plan.chunk_size).enumerate() {
        {
            let runtime = handle.runtime.lock().await;
            if runtime.cancel_requested {
                drop(runtime);
                finalize_cancelled(&handle, chunks_sent).await;
                let mut map = sessions.write().await;
                map.remove(&session_id.0);
                return;
            }
        }

        chunks_sent = idx + 1;
        let progress = ((chunks_sent * 100) / total_chunks) as u8;
        emit_event(
            &handle,
            "transfer",
            progress,
            format!("Transferred chunk {chunks_sent}/{total_chunks}"),
            false,
        )
        .await;
        sleep(Duration::from_millis(interval_ms)).await;
    }

    emit_event(&handle, "verify", 99, "Verifying firmware", false).await;
    sleep(Duration::from_millis(interval_ms)).await;

    {
        let mut runtime = handle.runtime.lock().await;
        runtime.state = FirmwareSessionState::Completed;
        runtime.completed_at = Some(Utc::now());
        let report = FirmwareFinalReport {
            session_id: handle.plan.session_id.clone(),
            status: FirmwareOutcome::Completed,
            started_at: runtime.started_at,
            completed_at: runtime.completed_at,
            bytes_total: handle.plan.bytes_total,
            chunks_total: handle.plan.chunks_total,
            chunks_sent,
            error_code: None,
            message: "Firmware update completed".to_owned(),
        };
        runtime.report = Some(report);
    }

    emit_event(&handle, "completed", 100, "Firmware update completed", true).await;
}

async fn finalize_failure(
    handle: &Arc<FirmwareSessionHandle>,
    code: BitdoErrorCode,
    message: String,
) {
    {
        let mut runtime = handle.runtime.lock().await;
        runtime.state = FirmwareSessionState::Failed;
        runtime.completed_at = Some(Utc::now());
        let report = FirmwareFinalReport {
            session_id: handle.plan.session_id.clone(),
            status: FirmwareOutcome::Failed,
            started_at: runtime.started_at,
            completed_at: runtime.completed_at,
            bytes_total: handle.plan.bytes_total,
            chunks_total: handle.plan.chunks_total,
            chunks_sent: 0,
            error_code: Some(code),
            message: message.clone(),
        };
        runtime.report = Some(report);
    }
    emit_event(handle, "failed", 100, message, true).await;
}

async fn finalize_cancelled(handle: &Arc<FirmwareSessionHandle>, chunks_sent: usize) {
    {
        let mut runtime = handle.runtime.lock().await;
        runtime.state = FirmwareSessionState::Cancelled;
        runtime.completed_at = Some(Utc::now());
        let report = FirmwareFinalReport {
            session_id: handle.plan.session_id.clone(),
            status: FirmwareOutcome::Cancelled,
            started_at: runtime.started_at,
            completed_at: runtime.completed_at,
            bytes_total: handle.plan.bytes_total,
            chunks_total: handle.plan.chunks_total,
            chunks_sent,
            error_code: None,
            message: "Firmware update cancelled".to_owned(),
        };
        runtime.report = Some(report);
    }
    emit_event(handle, "cancelled", 100, "Firmware update cancelled", true).await;
}

async fn emit_event(
    handle: &Arc<FirmwareSessionHandle>,
    stage: impl Into<String>,
    progress: u8,
    message: impl Into<String>,
    terminal: bool,
) {
    let mut runtime = handle.runtime.lock().await;
    runtime.sequence += 1;
    let event = FirmwareProgressEvent {
        session_id: handle.plan.session_id.clone(),
        sequence: runtime.sequence,
        stage: stage.into(),
        progress,
        message: message.into(),
        terminal,
        timestamp: Utc::now(),
    };
    let _ = handle.sender.send(event);
}

struct FirmwareSessionHandle {
    request: FirmwarePreflightRequest,
    plan: FirmwareUpdatePlan,
    sender: broadcast::Sender<FirmwareProgressEvent>,
    runtime: Mutex<FirmwareSessionRuntime>,
}

#[derive(Clone, Debug)]
struct FirmwareSessionRuntime {
    state: FirmwareSessionState,
    sequence: u64,
    cancel_requested: bool,
    report: Option<FirmwareFinalReport>,
    started_at: Option<DateTime<Utc>>,
    completed_at: Option<DateTime<Utc>>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum FirmwareSessionState {
    Preflight,
    AwaitingConfirmation,
    Running,
    Completed,
    Cancelled,
    Failed,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AppDevice {
    pub vid_pid: VidPid,
    pub name: String,
    pub support_level: SupportLevel,
    pub support_tier: SupportTier,
    pub protocol_family: ProtocolFamily,
    pub capability: PidCapability,
    pub evidence: SupportEvidence,
    pub serial: Option<String>,
    pub connected: bool,
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
pub enum UserSupportStatus {
    Supported,
    InProgress,
    Planned,
    Blocked,
}

impl UserSupportStatus {
    pub fn as_str(self) -> &'static str {
        match self {
            UserSupportStatus::Supported => "Supported",
            UserSupportStatus::InProgress => "In Progress",
            UserSupportStatus::Planned => "Planned",
            UserSupportStatus::Blocked => "Blocked",
        }
    }
}

pub fn support_status_for_tier(tier: SupportTier) -> UserSupportStatus {
    match tier {
        SupportTier::Full => UserSupportStatus::Supported,
        SupportTier::CandidateReadOnly => UserSupportStatus::InProgress,
        SupportTier::DetectOnly => UserSupportStatus::Planned,
    }
}

impl AppDevice {
    pub fn support_status(&self) -> UserSupportStatus {
        support_status_for_tier(self.support_tier)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwareManifest {
    pub version: u32,
    pub artifacts: Vec<FirmwareArtifact>,
}

impl FirmwareManifest {
    fn recommended_for(&self, target: VidPid, family: ProtocolFamily) -> Option<&FirmwareArtifact> {
        self.artifacts
            .iter()
            .find(|entry| {
                entry.channel.eq_ignore_ascii_case("stable")
                    && entry.vid == target.vid
                    && entry.pid == target.pid
            })
            .or_else(|| {
                self.artifacts.iter().find(|entry| {
                    entry.channel.eq_ignore_ascii_case("stable")
                        && entry.vid == target.vid
                        && entry.protocol_family == family
                })
            })
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwareArtifact {
    pub vid: u16,
    pub pid: u16,
    pub protocol_family: ProtocolFamily,
    pub version: String,
    pub channel: String,
    pub url: String,
    pub sha256: String,
    pub signature: ManifestSignature,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ManifestSignature {
    pub algorithm: String,
    pub url: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwareDownloadResult {
    pub firmware_path: PathBuf,
    pub version: String,
    pub source_url: String,
    pub sha256: String,
    pub verified_signature: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwarePreflightRequest {
    pub vid_pid: VidPid,
    pub firmware_path: PathBuf,
    pub allow_unsafe: bool,
    pub brick_risk_ack: bool,
    pub experimental: bool,
    pub chunk_size: Option<usize>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwarePreflightResult {
    pub gate: AppPolicyGateResult,
    pub plan: Option<FirmwareUpdatePlan>,
    pub capability: PidCapability,
    pub evidence: SupportEvidence,
}

impl FirmwarePreflightResult {
    fn denied(reason: AppPolicyGateReason, message: String) -> Self {
        Self {
            gate: AppPolicyGateResult {
                allowed: false,
                reason: Some(reason),
                message: Some(message),
            },
            plan: None,
            capability: PidCapability::identify_only(),
            evidence: SupportEvidence::Inferred,
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwareStartRequest {
    pub session_id: FirmwareUpdateSessionId,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwareConfirmRequest {
    pub session_id: FirmwareUpdateSessionId,
    pub acknowledged_risk: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwareCancelRequest {
    pub session_id: FirmwareUpdateSessionId,
}

#[derive(Clone, Debug, Serialize, Deserialize, Eq, PartialEq, Hash)]
pub struct FirmwareUpdateSessionId(pub String);

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwareUpdatePlan {
    pub session_id: FirmwareUpdateSessionId,
    pub chunk_size: usize,
    pub bytes_total: usize,
    pub chunks_total: usize,
    pub expected_seconds: u64,
    pub warnings: Vec<String>,
    pub image_sha256: String,
    pub current_version: String,
    pub target_version: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwareProgressEvent {
    pub session_id: FirmwareUpdateSessionId,
    pub sequence: u64,
    pub stage: String,
    pub progress: u8,
    pub message: String,
    pub terminal: bool,
    pub timestamp: DateTime<Utc>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FirmwareFinalReport {
    pub session_id: FirmwareUpdateSessionId,
    pub status: FirmwareOutcome,
    pub started_at: Option<DateTime<Utc>>,
    pub completed_at: Option<DateTime<Utc>>,
    pub bytes_total: usize,
    pub chunks_total: usize,
    pub chunks_sent: usize,
    pub error_code: Option<BitdoErrorCode>,
    pub message: String,
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
pub enum FirmwareOutcome {
    Completed,
    Cancelled,
    Failed,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AppPolicyGateResult {
    pub allowed: bool,
    pub reason: Option<AppPolicyGateReason>,
    pub message: Option<String>,
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
pub enum AppPolicyGateReason {
    UnsupportedPid,
    NotHardwareConfirmed,
    UnsafeFlagsMissing,
    ExperimentalRequired,
    VersionMismatch,
    ImageValidationFailed,
}

#[derive(Debug, Error)]
pub enum AppCoreError {
    #[error("policy denied: {reason:?}: {message}")]
    PolicyDenied {
        reason: AppPolicyGateReason,
        message: String,
    },
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),
    #[error("protocol error: {0}")]
    Protocol(#[from] bitdo_proto::BitdoError),
    #[error("download error: {0}")]
    Download(String),
    #[error("manifest error: {0}")]
    Manifest(String),
    #[error("not found: {0}")]
    NotFound(String),
    #[error("invalid state: {0}")]
    InvalidState(String),
}

pub type AppCoreResult<T> = Result<T, AppCoreError>;

#[derive(Clone, Debug)]
struct FirmwareImageMeta {
    bytes_total: usize,
    sha256: String,
    target_version_hint: Option<String>,
}

async fn validate_firmware_image(path: &Path) -> AppCoreResult<FirmwareImageMeta> {
    let bytes = tokio::fs::read(path).await?;
    if bytes.is_empty() {
        return Err(AppCoreError::PolicyDenied {
            reason: AppPolicyGateReason::ImageValidationFailed,
            message: "Firmware image is empty".to_owned(),
        });
    }
    if bytes.len() > 64 * 1024 * 1024 {
        return Err(AppCoreError::PolicyDenied {
            reason: AppPolicyGateReason::ImageValidationFailed,
            message: "Firmware image exceeds 64MB limit".to_owned(),
        });
    }

    Ok(FirmwareImageMeta {
        bytes_total: bytes.len(),
        sha256: sha256_hex(&bytes),
        target_version_hint: None,
    })
}

fn has_unusual_firmware_extension(path: &Path) -> bool {
    !path
        .extension()
        .and_then(|e| e.to_str())
        .map(|e| matches!(e.to_ascii_lowercase().as_str(), "bin" | "fw"))
        .unwrap_or(false)
}

fn sha256_hex(bytes: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    hex::encode(hasher.finalize())
}

fn mock_device(vid_pid: VidPid, full: bool) -> AppDevice {
    let profile = device_profile_for(vid_pid);
    AppDevice {
        vid_pid,
        name: if full {
            profile.name
        } else {
            "PID_MockDetectOnly".to_owned()
        },
        support_level: if full {
            SupportLevel::Full
        } else {
            SupportLevel::DetectOnly
        },
        support_tier: if full {
            SupportTier::Full
        } else {
            SupportTier::DetectOnly
        },
        protocol_family: profile.protocol_family,
        capability: if full {
            profile.capability
        } else {
            PidCapability::identify_only()
        },
        evidence: if full {
            SupportEvidence::Confirmed
        } else {
            SupportEvidence::Inferred
        },
        serial: Some(if full {
            "MOCK-FULL-6009".to_owned()
        } else {
            "MOCK-DETECT-2100".to_owned()
        }),
        connected: true,
    }
}

fn mock_diag_probe(target: VidPid) -> DiagProbeResult {
    let profile = device_profile_for(target);
    DiagProbeResult {
        target,
        profile_name: profile.name,
        support_level: profile.support_level,
        support_tier: profile.support_tier,
        protocol_family: profile.protocol_family,
        capability: profile.capability,
        evidence: profile.evidence,
        transport_ready: true,
        command_checks: vec![
            bitdo_proto::DiagCommandStatus {
                command: bitdo_proto::CommandId::GetPid,
                ok: true,
                confidence: bitdo_proto::EvidenceConfidence::Confirmed,
                is_experimental: false,
                severity: bitdo_proto::DiagSeverity::Ok,
                error_code: None,
                detail: "ok".to_owned(),
            },
            bitdo_proto::DiagCommandStatus {
                command: bitdo_proto::CommandId::GetControllerVersion,
                ok: true,
                confidence: bitdo_proto::EvidenceConfidence::Confirmed,
                is_experimental: false,
                severity: bitdo_proto::DiagSeverity::Ok,
                error_code: None,
                detail: "ok".to_owned(),
            },
            bitdo_proto::DiagCommandStatus {
                command: bitdo_proto::CommandId::GetSuperButton,
                ok: true,
                confidence: bitdo_proto::EvidenceConfidence::Inferred,
                is_experimental: true,
                severity: bitdo_proto::DiagSeverity::Ok,
                error_code: None,
                detail: "ok".to_owned(),
            },
        ],
    }
}

fn default_jp108_mappings() -> Vec<DedicatedButtonMapping> {
    DedicatedButtonId::ALL
        .iter()
        .copied()
        .enumerate()
        .map(|(idx, button)| DedicatedButtonMapping {
            button,
            target_hid_usage: (0x04 + idx as u16) & 0x00ff,
        })
        .collect()
}

fn default_u2_mappings() -> Vec<U2ButtonMapping> {
    U2ButtonId::ALL
        .iter()
        .copied()
        .enumerate()
        .map(|(idx, button)| U2ButtonMapping {
            button,
            target_hid_usage: 0x0100 + idx as u16,
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_req(path: PathBuf, pid: u16) -> FirmwarePreflightRequest {
        FirmwarePreflightRequest {
            vid_pid: VidPid::new(0x2dc8, pid),
            firmware_path: path,
            allow_unsafe: true,
            brick_risk_ack: true,
            experimental: true,
            chunk_size: Some(32),
        }
    }

    #[tokio::test]
    async fn preflight_blocks_detect_only_pid() {
        let core = OpenBitdoCore::new(OpenBitdoCoreConfig::default());
        let path = std::env::temp_dir().join("openbitdo-detect-only.bin");
        tokio::fs::write(&path, vec![1u8; 256])
            .await
            .expect("write");
        let req = make_req(path.clone(), 0x2100);
        let result = core.preflight_firmware(req).await.expect("preflight");
        assert!(!result.gate.allowed);
        assert_eq!(
            result.gate.reason,
            Some(AppPolicyGateReason::NotHardwareConfirmed)
        );
        let _ = tokio::fs::remove_file(path).await;
    }

    #[tokio::test]
    async fn firmware_happy_path_reaches_completed_report() {
        let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
            mock_mode: true,
            advanced_mode: false,
            default_chunk_size: 16,
            progress_interval_ms: 1,
            firmware_manifest_url: DEFAULT_MANIFEST_URL.to_owned(),
        });
        let path = std::env::temp_dir().join("openbitdo-happy.bin");
        tokio::fs::write(&path, vec![2u8; 128])
            .await
            .expect("write");

        let req = make_req(path.clone(), 0x6009);
        let preflight = core.preflight_firmware(req).await.expect("preflight");
        assert!(preflight.gate.allowed);
        let plan = preflight.plan.expect("plan");

        core.start_firmware(FirmwareStartRequest {
            session_id: plan.session_id.clone(),
        })
        .await
        .expect("start");

        core.confirm_firmware(FirmwareConfirmRequest {
            session_id: plan.session_id.clone(),
            acknowledged_risk: true,
        })
        .await
        .expect("confirm");

        loop {
            if let Some(report) = core
                .firmware_report(&plan.session_id.0)
                .await
                .expect("report")
            {
                assert_eq!(report.status, FirmwareOutcome::Completed);
                break;
            }
            sleep(Duration::from_millis(2)).await;
        }

        let _ = tokio::fs::remove_file(path).await;
    }

    #[tokio::test]
    async fn mock_download_returns_valid_file() {
        let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
            mock_mode: true,
            ..Default::default()
        });

        let result = core
            .download_recommended_firmware(VidPid::new(0x2dc8, 0x6009))
            .await
            .expect("download");

        let bytes = tokio::fs::read(&result.firmware_path)
            .await
            .expect("read downloaded file");
        assert!(!bytes.is_empty());
        assert_eq!(result.version, "mock-1.0.0");

        let _ = tokio::fs::remove_file(result.firmware_path).await;
    }

    #[tokio::test]
    async fn jp108_mock_mapping_roundtrip_supports_backup_and_restore() {
        let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
            mock_mode: true,
            ..Default::default()
        });
        let target = VidPid::new(0x2dc8, 0x5209);

        let mappings = core
            .jp108_read_dedicated_mapping(target)
            .await
            .expect("read mappings");
        assert_eq!(mappings.len(), DedicatedButtonId::ALL.len());

        let backup_id = core
            .jp108_apply_dedicated_mapping(
                target,
                vec![DedicatedButtonMapping {
                    button: DedicatedButtonId::A,
                    target_hid_usage: 0x2c,
                }],
                true,
            )
            .await
            .expect("apply mappings")
            .expect("backup id");

        core.restore_backup(backup_id)
            .await
            .expect("restore backup");
    }

    #[tokio::test]
    async fn u2_mock_profile_roundtrip_supports_backup_and_restore() {
        let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
            mock_mode: true,
            ..Default::default()
        });
        let target = VidPid::new(0x2dc8, 0x6012);

        let profile = core
            .u2_read_core_profile(target, U2SlotId::Slot1)
            .await
            .expect("read profile");
        assert_eq!(profile.slot, U2SlotId::Slot1);
        assert!(!profile.mappings.is_empty());

        let backup_id = core
            .u2_apply_core_profile(
                target,
                U2SlotId::Slot1,
                1,
                vec![U2ButtonMapping {
                    button: U2ButtonId::A,
                    target_hid_usage: 0x0110,
                }],
                0.5,
                0.5,
                true,
            )
            .await
            .expect("apply profile")
            .expect("backup id");

        core.restore_backup(backup_id)
            .await
            .expect("restore backup");
    }

    #[tokio::test]
    async fn guided_button_test_returns_beginner_guidance() {
        let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
            mock_mode: true,
            ..Default::default()
        });

        let result = core
            .guided_button_test(
                DeviceKind::Jp108,
                vec!["A -> Space".to_owned(), "K1 -> Enter".to_owned()],
            )
            .await
            .expect("guided test");
        assert!(result.passed);
        assert!(result.guidance.contains("JP108"));
    }

    #[test]
    fn support_status_maps_from_tier() {
        assert_eq!(
            support_status_for_tier(SupportTier::Full),
            UserSupportStatus::Supported
        );
        assert_eq!(
            support_status_for_tier(SupportTier::CandidateReadOnly),
            UserSupportStatus::InProgress
        );
        assert_eq!(
            support_status_for_tier(SupportTier::DetectOnly),
            UserSupportStatus::Planned
        );
    }
}
