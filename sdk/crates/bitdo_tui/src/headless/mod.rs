use crate::support_report::persist_support_report;
use crate::{should_save_support_report, HeadlessOutputMode, RunLaunchOptions};
use anyhow::{anyhow, Result};
use bitdo_app_core::{
    FirmwareCancelRequest, FirmwareConfirmRequest, FirmwareFinalReport, FirmwareOutcome,
    FirmwarePreflightRequest, FirmwareStartRequest, OpenBitdoCore,
};
use serde::Serialize;
use std::path::Path;
use tokio::time::{sleep, Duration};

#[derive(Serialize)]
struct JsonProgress<'a> {
    r#type: &'static str,
    session_id: &'a str,
    sequence: u64,
    stage: &'a str,
    progress: u8,
    message: &'a str,
    timestamp: String,
}

#[derive(Serialize)]
struct JsonFinal<'a> {
    r#type: &'static str,
    session_id: &'a str,
    status: &'a str,
    chunks_sent: usize,
    chunks_total: usize,
    message: &'a str,
    error_code: Option<String>,
}

pub async fn run_headless(
    core: OpenBitdoCore,
    opts: RunLaunchOptions,
) -> Result<FirmwareFinalReport> {
    let downloaded_firmware = opts.firmware_path.is_none() && opts.use_recommended;
    let firmware_path = if let Some(path) = opts.firmware_path.clone() {
        path
    } else if opts.use_recommended {
        core.download_recommended_firmware(opts.vid_pid)
            .await
            .map(|d| d.firmware_path)
            .map_err(|err| anyhow!("recommended firmware unavailable: {err}"))?
    } else {
        return Err(anyhow!(
            "firmware path is required when --recommended is not used"
        ));
    };

    let preflight = match core
        .preflight_firmware(FirmwarePreflightRequest {
            vid_pid: opts.vid_pid,
            firmware_path: firmware_path.clone(),
            allow_unsafe: opts.allow_unsafe,
            brick_risk_ack: opts.brick_risk_ack,
            experimental: opts.experimental,
            chunk_size: opts.chunk_size,
        })
        .await
    {
        Ok(preflight) => preflight,
        Err(err) => {
            maybe_cleanup_downloaded_firmware(downloaded_firmware, &firmware_path).await;
            return Err(err.into());
        }
    };

    if !preflight.gate.allowed {
        let message = preflight
            .gate
            .message
            .unwrap_or_else(|| "policy denied".to_owned());
        emit_failed_final(&opts, "preflight", &message);
        maybe_cleanup_downloaded_firmware(downloaded_firmware, &firmware_path).await;
        return Err(anyhow!("preflight denied: {message}"));
    }

    let plan = match preflight.plan {
        Some(plan) => plan,
        None => {
            maybe_cleanup_downloaded_firmware(downloaded_firmware, &firmware_path).await;
            return Err(anyhow!("preflight allowed without transfer plan"));
        }
    };

    if let Err(err) = core
        .start_firmware(FirmwareStartRequest {
            session_id: plan.session_id.clone(),
        })
        .await
    {
        maybe_cleanup_downloaded_firmware(downloaded_firmware, &firmware_path).await;
        return Err(err.into());
    }

    if let Err(err) = core
        .confirm_firmware(FirmwareConfirmRequest {
            session_id: plan.session_id.clone(),
            acknowledged_risk: opts.acknowledged_risk,
        })
        .await
    {
        maybe_cleanup_downloaded_firmware(downloaded_firmware, &firmware_path).await;
        return Err(err.into());
    }

    let mut events = match core.subscribe_events(&plan.session_id.0).await {
        Ok(events) => events,
        Err(err) => {
            maybe_cleanup_downloaded_firmware(downloaded_firmware, &firmware_path).await;
            return Err(err.into());
        }
    };

    loop {
        tokio::select! {
            evt = events.recv() => {
                if let Ok(evt) = evt {
                    if opts.emit_events {
                        emit_progress(&opts, &evt.session_id.0, evt.sequence, &evt.stage, evt.progress, &evt.message, evt.timestamp.to_rfc3339());
                    }
                    if evt.terminal {
                        break;
                    }
                }
            }
            _ = sleep(Duration::from_millis(10)) => {
                let report = match core.firmware_report(&plan.session_id.0).await {
                    Ok(report) => report,
                    Err(err) => {
                        maybe_cleanup_downloaded_firmware(downloaded_firmware, &firmware_path)
                            .await;
                        return Err(err.into());
                    }
                };
                if let Some(report) = report {
                    emit_final(&opts, &report);
                    maybe_persist_report(&core, &opts, &report).await;
                    maybe_cleanup_downloaded_firmware(downloaded_firmware, &firmware_path).await;
                    return Ok(report);
                }
            }
        }
    }

    let report = match core.firmware_report(&plan.session_id.0).await {
        Ok(report) => report.unwrap_or(FirmwareFinalReport {
            session_id: plan.session_id,
            status: FirmwareOutcome::Failed,
            started_at: None,
            completed_at: None,
            bytes_total: 0,
            chunks_total: 0,
            chunks_sent: 0,
            error_code: None,
            message: "missing final report".to_owned(),
        }),
        Err(err) => {
            maybe_cleanup_downloaded_firmware(downloaded_firmware, &firmware_path).await;
            return Err(err.into());
        }
    };

    emit_final(&opts, &report);
    maybe_persist_report(&core, &opts, &report).await;
    maybe_cleanup_downloaded_firmware(downloaded_firmware, &firmware_path).await;
    Ok(report)
}

pub async fn cancel_headless(
    core: &OpenBitdoCore,
    session_id: &str,
) -> Result<FirmwareFinalReport> {
    core.cancel_firmware(FirmwareCancelRequest {
        session_id: bitdo_app_core::FirmwareUpdateSessionId(session_id.to_owned()),
    })
    .await
    .map_err(Into::into)
}

fn emit_progress(
    opts: &RunLaunchOptions,
    session_id: &str,
    sequence: u64,
    stage: &str,
    progress: u8,
    message: &str,
    timestamp: String,
) {
    match opts.output_mode {
        HeadlessOutputMode::Human => {
            println!("[{progress:>3}%] {stage}: {message}");
        }
        HeadlessOutputMode::Json => {
            let payload = JsonProgress {
                r#type: "progress",
                session_id,
                sequence,
                stage,
                progress,
                message,
                timestamp,
            };
            if let Ok(json) = serde_json::to_string(&payload) {
                println!("{json}");
            }
        }
    }
}

fn emit_final(opts: &RunLaunchOptions, report: &FirmwareFinalReport) {
    match opts.output_mode {
        HeadlessOutputMode::Human => {
            println!(
                "final: {:?} chunks={}/{} message={}",
                report.status, report.chunks_sent, report.chunks_total, report.message
            );
        }
        HeadlessOutputMode::Json => {
            let payload = JsonFinal {
                r#type: "final",
                session_id: &report.session_id.0,
                status: match report.status {
                    FirmwareOutcome::Completed => "Completed",
                    FirmwareOutcome::Cancelled => "Cancelled",
                    FirmwareOutcome::Failed => "Failed",
                },
                chunks_sent: report.chunks_sent,
                chunks_total: report.chunks_total,
                message: &report.message,
                error_code: report.error_code.map(|err| format!("{err:?}")),
            };
            if let Ok(json) = serde_json::to_string(&payload) {
                println!("{json}");
            }
        }
    }
}

fn emit_failed_final(opts: &RunLaunchOptions, session_id: &str, message: &str) {
    match opts.output_mode {
        HeadlessOutputMode::Human => {
            println!("final: Failed message={message}");
        }
        HeadlessOutputMode::Json => {
            let payload = JsonFinal {
                r#type: "final",
                session_id,
                status: "Failed",
                chunks_sent: 0,
                chunks_total: 0,
                message,
                error_code: None,
            };
            if let Ok(json) = serde_json::to_string(&payload) {
                println!("{json}");
            }
        }
    }
}

async fn maybe_persist_report(
    core: &OpenBitdoCore,
    opts: &RunLaunchOptions,
    report: &FirmwareFinalReport,
) {
    let is_failure = report.status != FirmwareOutcome::Completed;
    if !should_save_support_report(opts.report_save_mode, is_failure) {
        return;
    }

    let devices = core.list_devices().await.unwrap_or_default();
    let selected = devices.iter().find(|d| d.vid_pid == opts.vid_pid);
    let status = if is_failure { "failed" } else { "completed" };

    let _ = persist_support_report(
        "fw-write",
        selected,
        status,
        report.message.clone(),
        None,
        Some(report),
    )
    .await;
}

async fn maybe_cleanup_downloaded_firmware(downloaded_firmware: bool, firmware_path: &Path) {
    if downloaded_firmware {
        let _ = cleanup_temp_file(firmware_path).await;
    }
}

async fn cleanup_temp_file(path: &Path) -> std::io::Result<()> {
    match tokio::fs::remove_file(path).await {
        Ok(()) => Ok(()),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(err) => Err(err),
    }
}
