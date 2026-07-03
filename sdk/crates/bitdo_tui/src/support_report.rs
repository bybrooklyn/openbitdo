use crate::AppDevice;
use anyhow::{Result, anyhow};
use bitdo_app_core::{FirmwareFinalReport, RuntimeUnlockReport, SupportScorecard};
use bitdo_proto::{DiagProbeResult, SupportLevel, SupportTier};
use chrono::Utc;
use serde::Serialize;
use std::path::PathBuf;
use std::time::{Duration, SystemTime};

const REPORT_MAX_COUNT: usize = 20;
const REPORT_MAX_AGE_DAYS: u64 = 30;

#[derive(Clone, Debug, Serialize)]
struct SupportReport {
    schema_version: u32,
    generated_at_utc: String,
    operation: String,
    device: Option<SupportReportDevice>,
    status: String,
    message: String,
    scorecard: Option<SupportScorecard>,
    diag: Option<DiagProbeResult>,
    firmware: Option<FirmwareFinalReport>,
    runtime_unlock: Option<RuntimeUnlockReport>,
}

#[derive(Clone, Debug, Serialize)]
struct SupportReportDevice {
    vid: u16,
    pid: u16,
    name: String,
    canonical_id: String,
    runtime_label: String,
    serial: Option<String>,
    support_level: String,
    support_tier: String,
    protocol_family: String,
    evidence: String,
    works_now: Vec<String>,
    blocked_operations: Vec<String>,
    missing_evidence: Vec<String>,
}

/// Persist a troubleshooting report as TOML.
///
/// Reports are intended for failure/support paths and are named with a timestamp plus
/// a serial-or-VID/PID token so users can share deterministic artifacts with support.
pub(crate) async fn persist_support_report(
    operation: &str,
    device: Option<&AppDevice>,
    status: &str,
    message: String,
    diag: Option<&DiagProbeResult>,
    firmware: Option<&FirmwareFinalReport>,
    runtime_unlock: Option<&RuntimeUnlockReport>,
) -> Result<PathBuf> {
    let now = Utc::now();
    let report = SupportReport {
        schema_version: 2,
        generated_at_utc: now.to_rfc3339(),
        operation: operation.to_owned(),
        device: device.map(|d| SupportReportDevice {
            vid: d.vid_pid.vid,
            pid: d.vid_pid.pid,
            name: d.name.clone(),
            canonical_id: d.name.clone(),
            runtime_label: d.support_status().as_str().to_owned(),
            serial: d.serial.clone(),
            support_level: match d.support_level {
                SupportLevel::Full => "full".to_owned(),
                SupportLevel::DetectOnly => "detect-only".to_owned(),
            },
            support_tier: match d.support_tier {
                SupportTier::Full => "full".to_owned(),
                SupportTier::CandidateReadOnly => "candidate-readonly".to_owned(),
                SupportTier::DetectOnly => "detect-only".to_owned(),
            },
            protocol_family: format!("{:?}", d.protocol_family),
            evidence: format!("{:?}", d.evidence),
            works_now: report_works_now(d),
            blocked_operations: report_blocked_operations(d),
            missing_evidence: report_missing_evidence(d),
        }),
        status: status.to_owned(),
        message,
        scorecard: device.map(|d| d.scorecard()),
        diag: diag.cloned(),
        firmware: firmware.cloned(),
        runtime_unlock: runtime_unlock.cloned(),
    };

    let report_dir = default_report_directory();
    tokio::fs::create_dir_all(&report_dir).await?;

    let token = report_subject_token(device);
    let file_name = support_report_file_name(now, operation, &token);
    let path = report_dir.join(file_name);

    let body = toml::to_string_pretty(&report)
        .map_err(|err| anyhow!("failed to serialize support report: {err}"))?;
    tokio::fs::write(&path, body).await?;
    let _ = prune_reports_on_write().await;

    Ok(path)
}

/// Startup pruning is age-based to keep stale files out of user systems.
pub(crate) async fn prune_reports_on_startup() -> Result<()> {
    prune_reports_by_age().await
}

/// Write-time pruning is count-based to keep growth bounded deterministically.
async fn prune_reports_on_write() -> Result<()> {
    prune_reports_by_count().await
}

pub(crate) fn report_subject_token(device: Option<&AppDevice>) -> String {
    if let Some(device) = device {
        if let Some(serial) = device.serial.as_deref() {
            let cleaned = sanitize_token(serial);
            if !cleaned.is_empty() {
                return cleaned;
            }
        }

        return format!("{:04x}{:04x}", device.vid_pid.vid, device.vid_pid.pid);
    }

    "unknown".to_owned()
}

fn sanitize_token(value: &str) -> String {
    let mut out = String::with_capacity(value.len());
    for ch in value.chars() {
        if ch.is_ascii_alphanumeric() || ch == '-' || ch == '_' {
            out.push(ch);
        } else {
            out.push('_');
        }
    }

    out.trim_matches('_').to_owned()
}

fn report_works_now(device: &AppDevice) -> Vec<String> {
    let mut out = vec![
        "safe diagnostics".to_owned(),
        "support report generation".to_owned(),
        "device identification".to_owned(),
    ];
    if device.capability.supports_mode {
        out.push("mode read/switch where policy allows".to_owned());
    }
    if device.capability.supports_profile_rw {
        out.push("profile read/write where policy allows".to_owned());
    }
    if device.capability.supports_firmware {
        out.push("verified firmware preflight/update".to_owned());
    }
    if device.capability.supports_jp108_dedicated_map {
        out.push("JP108 dedicated mapping".to_owned());
    }
    if device.capability.supports_u2_button_map || device.capability.supports_u2_slot_config {
        out.push("Ultimate 2 slot and mapping".to_owned());
    }
    out
}

fn report_blocked_operations(device: &AppDevice) -> Vec<String> {
    match device.support_tier {
        SupportTier::Full => {
            let mut out = Vec::new();
            if !device.capability.supports_firmware {
                out.push("firmware update: no verified path for this PID".to_owned());
            }
            if !(device.capability.supports_jp108_dedicated_map
                || (device.capability.supports_u2_button_map
                    && device.capability.supports_u2_slot_config))
            {
                out.push("mapping editor: no confirmed mapping surface".to_owned());
            }
            if out.is_empty() {
                out.push("none for confirmed capabilities".to_owned());
            }
            out
        }
        SupportTier::CandidateReadOnly => vec![
            "firmware writes blocked until runtime traces are confirmed".to_owned(),
            "mapping/profile writes blocked until hardware read/write/readback passes".to_owned(),
        ],
        SupportTier::DetectOnly => vec![
            "diagnostics beyond identification are limited".to_owned(),
            "firmware, mapping, profile, and mode writes are unavailable".to_owned(),
        ],
    }
}

fn report_missing_evidence(device: &AppDevice) -> Vec<String> {
    let missing = device.scorecard().missing_evidence;
    if missing.is_empty() {
        vec!["no blocking evidence gaps for current support tier".to_owned()]
    } else {
        missing
    }
}

fn support_report_file_name(now: chrono::DateTime<Utc>, operation: &str, token: &str) -> String {
    format!(
        "{}-{}-{:09}-{}.toml",
        sanitize_token(operation),
        now.format("%Y%m%d-%H%M%S"),
        now.timestamp_subsec_nanos(),
        token
    )
}

fn default_report_directory() -> PathBuf {
    if cfg!(target_os = "macos") {
        return home_directory()
            .join("Library")
            .join("Application Support")
            .join("OpenBitdo")
            .join("reports");
    }

    if cfg!(target_os = "linux") {
        if let Some(xdg_data_home) = std::env::var_os("XDG_DATA_HOME") {
            return PathBuf::from(xdg_data_home)
                .join("openbitdo")
                .join("reports");
        }

        return home_directory()
            .join(".local")
            .join("share")
            .join("openbitdo")
            .join("reports");
    }

    std::env::temp_dir().join("openbitdo").join("reports")
}

async fn list_report_files() -> Result<Vec<PathBuf>> {
    let report_dir = default_report_directory();
    let mut out = Vec::new();
    let mut entries = match tokio::fs::read_dir(&report_dir).await {
        Ok(entries) => entries,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(out),
        Err(err) => return Err(err.into()),
    };

    while let Some(entry) = entries.next_entry().await? {
        let path = entry.path();
        if path.extension().and_then(|e| e.to_str()) == Some("toml") {
            out.push(path);
        }
    }

    Ok(out)
}

async fn prune_reports_by_count() -> Result<()> {
    let mut files = list_report_files().await?;
    files.sort_by_key(|path| {
        std::fs::metadata(path)
            .and_then(|meta| meta.modified())
            .unwrap_or(SystemTime::UNIX_EPOCH)
    });
    files.reverse();

    for path in files.into_iter().skip(REPORT_MAX_COUNT) {
        let _ = tokio::fs::remove_file(path).await;
    }

    Ok(())
}

async fn prune_reports_by_age() -> Result<()> {
    let now = SystemTime::now();
    let max_age = Duration::from_secs(REPORT_MAX_AGE_DAYS * 24 * 60 * 60);
    for path in list_report_files().await? {
        let Ok(meta) = std::fs::metadata(&path) else {
            continue;
        };
        let Ok(modified) = meta.modified() else {
            continue;
        };
        if now.duration_since(modified).unwrap_or_default() > max_age {
            let _ = tokio::fs::remove_file(path).await;
        }
    }
    Ok(())
}

fn home_directory() -> PathBuf {
    std::env::var_os("HOME")
        .map(PathBuf::from)
        .unwrap_or_else(std::env::temp_dir)
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::{TimeZone, Timelike, Utc};

    #[test]
    fn support_report_file_names_do_not_collide_within_same_second() {
        let first = Utc
            .with_ymd_and_hms(2026, 3, 20, 12, 34, 56)
            .single()
            .expect("valid datetime")
            .with_nanosecond(123)
            .expect("valid nanos");
        let second = first.with_nanosecond(456).expect("valid nanos");

        let first_name = support_report_file_name(first, "diag-probe", "2dc86012");
        let second_name = support_report_file_name(second, "diag-probe", "2dc86012");

        assert_ne!(first_name, second_name);
        assert!(first_name.ends_with(".toml"));
    }
}
