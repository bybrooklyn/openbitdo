use crate::AppDevice;
use anyhow::{anyhow, Result};
use bitdo_app_core::FirmwareFinalReport;
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
    diag: Option<DiagProbeResult>,
    firmware: Option<FirmwareFinalReport>,
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
) -> Result<PathBuf> {
    let now = Utc::now();
    let report = SupportReport {
        schema_version: 1,
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
        }),
        status: status.to_owned(),
        message,
        diag: diag.cloned(),
        firmware: firmware.cloned(),
    };

    let report_dir = default_report_directory();
    tokio::fs::create_dir_all(&report_dir).await?;

    let token = report_subject_token(device);
    let file_name = format!(
        "{}-{}-{}.toml",
        sanitize_token(operation),
        now.format("%Y%m%d-%H%M%S"),
        token
    );
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
