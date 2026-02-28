use anyhow::{anyhow, Result};
use bitdo_proto::{
    command_registry, device_profile_for, enumerate_hid_devices, BitdoErrorCode, CommandId,
    DeviceSession, FirmwareTransferReport, HidTransport, MockTransport, ProfileBlob, RetryPolicy,
    SessionConfig, TimeoutProfile, Transport, VidPid,
};
use clap::{Parser, Subcommand};
use serde_json::json;
use std::fs;
use std::path::PathBuf;

#[derive(Debug, Parser)]
#[command(name = "bitdoctl")]
#[command(about = "OpenBitdo clean-room protocol CLI")]
struct Cli {
    #[arg(long)]
    vid: Option<String>,
    #[arg(long)]
    pid: Option<String>,
    #[arg(long)]
    json: bool,
    #[arg(long = "unsafe")]
    allow_unsafe: bool,
    #[arg(long = "i-understand-brick-risk")]
    brick_risk_ack: bool,
    #[arg(long)]
    experimental: bool,
    #[arg(long)]
    mock: bool,
    #[arg(long, default_value_t = 3)]
    max_attempts: u8,
    #[arg(long, default_value_t = 10)]
    backoff_ms: u64,
    #[arg(long, default_value_t = 200)]
    probe_timeout_ms: u64,
    #[arg(long, default_value_t = 400)]
    io_timeout_ms: u64,
    #[arg(long, default_value_t = 1200)]
    firmware_timeout_ms: u64,
    #[command(subcommand)]
    command: Commands,
}

#[derive(Debug, Subcommand)]
enum Commands {
    List,
    Identify,
    Diag {
        #[command(subcommand)]
        command: DiagCommand,
    },
    Profile {
        #[command(subcommand)]
        command: ProfileCommand,
    },
    Mode {
        #[command(subcommand)]
        command: ModeCommand,
    },
    Boot {
        #[command(subcommand)]
        command: BootCommand,
    },
    Fw {
        #[command(subcommand)]
        command: FwCommand,
    },
}

#[derive(Debug, Subcommand)]
enum DiagCommand {
    Probe,
}

#[derive(Debug, Subcommand)]
enum ProfileCommand {
    Dump {
        #[arg(long)]
        slot: u8,
    },
    Apply {
        #[arg(long)]
        slot: u8,
        #[arg(long)]
        file: PathBuf,
    },
}

#[derive(Debug, Subcommand)]
enum ModeCommand {
    Get,
    Set {
        #[arg(long)]
        mode: u8,
    },
}

#[derive(Debug, Subcommand)]
enum BootCommand {
    Enter,
    Exit,
}

#[derive(Debug, Subcommand)]
enum FwCommand {
    Write {
        #[arg(long)]
        file: PathBuf,
        #[arg(long, default_value_t = 56)]
        chunk_size: usize,
        #[arg(long)]
        dry_run: bool,
    },
}

fn main() -> Result<()> {
    let cli = Cli::parse();
    if let Err(err) = run(cli) {
        eprintln!("error: {err}");
        return Err(err);
    }
    Ok(())
}

fn run(cli: Cli) -> Result<()> {
    match &cli.command {
        Commands::List => handle_list(&cli),
        Commands::Identify
        | Commands::Diag { .. }
        | Commands::Profile { .. }
        | Commands::Mode { .. }
        | Commands::Boot { .. }
        | Commands::Fw { .. } => {
            let target = resolve_target(&cli)?;
            let transport: Box<dyn Transport> = if cli.mock {
                Box::new(mock_transport_for(&cli.command, target)?)
            } else {
                Box::new(HidTransport::new())
            };

            let config = SessionConfig {
                retry_policy: RetryPolicy {
                    max_attempts: cli.max_attempts,
                    backoff_ms: cli.backoff_ms,
                },
                timeout_profile: TimeoutProfile {
                    probe_ms: cli.probe_timeout_ms,
                    io_ms: cli.io_timeout_ms,
                    firmware_ms: cli.firmware_timeout_ms,
                },
                allow_unsafe: cli.allow_unsafe,
                brick_risk_ack: cli.brick_risk_ack,
                experimental: cli.experimental,
                trace_enabled: true,
            };
            let mut session = DeviceSession::new(transport, target, config)?;

            match &cli.command {
                Commands::Identify => {
                    let info = session.identify()?;
                    if cli.json {
                        println!("{}", serde_json::to_string_pretty(&info)?);
                    } else {
                        println!(
                            "target={} profile={} support={:?} family={:?} evidence={:?} capability={:?} detected_pid={}",
                            info.target,
                            info.profile_name,
                            info.support_level,
                            info.protocol_family,
                            info.evidence,
                            info.capability,
                            info.detected_pid
                                .map(|v| format!("{v:#06x}"))
                                .unwrap_or_else(|| "none".to_owned())
                        );
                    }
                }
                Commands::Diag { command } => match command {
                    DiagCommand::Probe => {
                        let diag = session.diag_probe();
                        if cli.json {
                            println!("{}", serde_json::to_string_pretty(&diag)?);
                        } else {
                            println!(
                                "diag target={} profile={} family={:?}",
                                diag.target, diag.profile_name, diag.protocol_family
                            );
                            for check in diag.command_checks {
                                println!(
                                    "  {:?}: ok={} code={}",
                                    check.command,
                                    check.ok,
                                    check
                                        .error_code
                                        .map(|c| format!("{c:?}"))
                                        .unwrap_or_else(|| "none".to_owned())
                                );
                            }
                        }
                    }
                },
                Commands::Mode { command } => match command {
                    ModeCommand::Get => {
                        let mode = session.get_mode()?;
                        print_mode(mode.mode, &mode.source, cli.json);
                    }
                    ModeCommand::Set { mode } => {
                        let mode_state = session.set_mode(*mode)?;
                        print_mode(mode_state.mode, &mode_state.source, cli.json);
                    }
                },
                Commands::Profile { command } => match command {
                    ProfileCommand::Dump { slot } => {
                        let profile = session.read_profile(*slot)?;
                        if cli.json {
                            println!(
                                "{}",
                                serde_json::to_string_pretty(&json!({
                                    "slot": profile.slot,
                                    "payload_hex": hex::encode(&profile.payload),
                                }))?
                            );
                        } else {
                            println!(
                                "slot={} payload_hex={}",
                                profile.slot,
                                hex::encode(&profile.payload)
                            );
                        }
                    }
                    ProfileCommand::Apply { slot, file } => {
                        let bytes = fs::read(file)?;
                        let parsed = ProfileBlob::from_bytes(&bytes)?;
                        let blob = ProfileBlob {
                            slot: *slot,
                            payload: parsed.payload,
                        };
                        session.write_profile(*slot, &blob)?;
                        if cli.json {
                            println!(
                                "{}",
                                serde_json::to_string_pretty(&json!({
                                    "applied": true,
                                    "slot": slot,
                                }))?
                            );
                        } else {
                            println!("applied profile to slot={slot}");
                        }
                    }
                },
                Commands::Boot { command } => {
                    match command {
                        BootCommand::Enter => session.enter_bootloader()?,
                        BootCommand::Exit => session.exit_bootloader()?,
                    }
                    if cli.json {
                        println!(
                            "{}",
                            serde_json::to_string_pretty(&json!({
                                "ok": true,
                                "command": format!("{:?}", command),
                            }))?
                        );
                    } else {
                        println!("{:?} completed", command);
                    }
                }
                Commands::Fw { command } => match command {
                    FwCommand::Write {
                        file,
                        chunk_size,
                        dry_run,
                    } => {
                        let image = fs::read(file)?;
                        let report = session.firmware_transfer(&image, *chunk_size, *dry_run)?;
                        print_fw_report(report, cli.json)?;
                    }
                },
                Commands::List => unreachable!(),
            }

            session.close()?;
            Ok(())
        }
    }
}

fn handle_list(cli: &Cli) -> Result<()> {
    if cli.mock {
        let profile = device_profile_for(VidPid::new(0x2dc8, 0x6009));
        if cli.json {
            println!(
                "{}",
                serde_json::to_string_pretty(&vec![json!({
                    "vid": "0x2dc8",
                    "pid": "0x6009",
                    "product": "Mock 8BitDo Device",
                    "support_level": format!("{:?}", profile.support_level),
                    "protocol_family": format!("{:?}", profile.protocol_family),
                    "capability": profile.capability,
                    "evidence": format!("{:?}", profile.evidence),
                })])?
            );
        } else {
            println!("2dc8:6009 Mock 8BitDo Device");
        }
        return Ok(());
    }

    let devices = enumerate_hid_devices()?;
    let filtered: Vec<_> = devices
        .into_iter()
        .filter(|d| d.vid_pid.vid == 0x2dc8)
        .collect();

    if cli.json {
        let out: Vec<_> = filtered
            .iter()
            .map(|d| {
                let profile = device_profile_for(d.vid_pid);
                json!({
                    "vid": format!("{:#06x}", d.vid_pid.vid),
                    "pid": format!("{:#06x}", d.vid_pid.pid),
                    "product": d.product,
                    "manufacturer": d.manufacturer,
                    "serial": d.serial,
                    "path": d.path,
                    "support_level": format!("{:?}", profile.support_level),
                    "protocol_family": format!("{:?}", profile.protocol_family),
                    "capability": profile.capability,
                    "evidence": format!("{:?}", profile.evidence),
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&out)?);
    } else {
        for d in &filtered {
            println!(
                "{} {}",
                d.vid_pid,
                d.product.as_deref().unwrap_or("(unknown product)")
            );
        }
    }
    Ok(())
}

fn resolve_target(cli: &Cli) -> Result<VidPid> {
    let vid = cli
        .vid
        .as_deref()
        .map(parse_u16)
        .transpose()?
        .unwrap_or(0x2dc8);
    let pid_str = cli
        .pid
        .as_deref()
        .ok_or_else(|| anyhow!("--pid is required for this command"))?;
    let pid = parse_u16(pid_str)?;
    Ok(VidPid::new(vid, pid))
}

fn parse_u16(input: &str) -> Result<u16> {
    if let Some(hex) = input
        .strip_prefix("0x")
        .or_else(|| input.strip_prefix("0X"))
    {
        return Ok(u16::from_str_radix(hex, 16)?);
    }
    Ok(input.parse::<u16>()?)
}

fn mock_transport_for(command: &Commands, target: VidPid) -> Result<MockTransport> {
    let mut t = MockTransport::default();
    match command {
        Commands::Identify => {
            t.push_read_data(build_pid_response(target.pid));
        }
        Commands::Diag { command } => match command {
            DiagCommand::Probe => {
                t.push_read_data(build_pid_response(target.pid));
                t.push_read_data(build_rr_response());
                t.push_read_data(build_mode_response(2));
                t.push_read_data(build_version_response());
            }
        },
        Commands::Mode { command } => match command {
            ModeCommand::Get => t.push_read_data(build_mode_response(2)),
            ModeCommand::Set { mode } => {
                t.push_read_data(build_ack_response());
                t.push_read_data(build_mode_response(*mode));
            }
        },
        Commands::Profile { command } => match command {
            ProfileCommand::Dump { slot } => {
                let mut raw = vec![0x02, 0x06, 0x00, *slot];
                raw.extend_from_slice(&[1, 2, 3, 4, 5, 6, 7, 8]);
                t.push_read_data(raw);
            }
            ProfileCommand::Apply { .. } => {
                t.push_read_data(build_ack_response());
            }
        },
        Commands::Boot { .. } => {}
        Commands::Fw { command } => {
            let chunks = match command {
                FwCommand::Write {
                    file,
                    chunk_size,
                    dry_run,
                } => {
                    if *dry_run {
                        0
                    } else {
                        let sz = fs::metadata(file).map(|m| m.len() as usize).unwrap_or(0);
                        sz.div_ceil(*chunk_size) + 1
                    }
                }
            };
            for _ in 0..chunks {
                t.push_read_data(build_ack_response());
            }
        }
        Commands::List => {}
    }

    if matches!(command, Commands::Profile { .. } | Commands::Fw { .. })
        && !command_registry()
            .iter()
            .any(|c| c.id == CommandId::ReadProfile)
    {
        return Err(anyhow!("command registry is empty"));
    }

    Ok(t)
}

fn build_ack_response() -> Vec<u8> {
    vec![0x02, 0x01, 0x00, 0x00]
}

fn build_mode_response(mode: u8) -> Vec<u8> {
    let mut out = vec![0u8; 64];
    out[0] = 0x02;
    out[1] = 0x05;
    out[5] = mode;
    out
}

fn build_rr_response() -> Vec<u8> {
    let mut out = vec![0u8; 64];
    out[0] = 0x02;
    out[1] = 0x04;
    out[5] = 0x01;
    out
}

fn build_version_response() -> Vec<u8> {
    let mut out = vec![0u8; 64];
    out[0] = 0x02;
    out[1] = 0x22;
    out[2] = 0x2A;
    out[3] = 0x00;
    out[4] = 0x01;
    out
}

fn build_pid_response(pid: u16) -> Vec<u8> {
    let mut out = vec![0u8; 64];
    out[0] = 0x02;
    out[1] = 0x05;
    out[4] = 0xC1;
    let [lo, hi] = pid.to_le_bytes();
    out[22] = lo;
    out[23] = hi;
    out
}

fn print_mode(mode: u8, source: &str, as_json: bool) {
    if as_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&json!({
                "mode": mode,
                "source": source,
            }))
            .expect("json serialization")
        );
    } else {
        println!("mode={} source={}", mode, source);
    }
}

fn print_fw_report(report: FirmwareTransferReport, as_json: bool) -> Result<()> {
    if as_json {
        println!("{}", serde_json::to_string_pretty(&report)?);
    } else {
        println!(
            "bytes_total={} chunk_size={} chunks_sent={} dry_run={}",
            report.bytes_total, report.chunk_size, report.chunks_sent, report.dry_run
        );
    }
    Ok(())
}

#[allow(dead_code)]
fn print_error_code(code: BitdoErrorCode, as_json: bool) {
    if as_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&json!({ "error_code": format!("{:?}", code) }))
                .expect("json serialization")
        );
    } else {
        println!("error_code={:?}", code);
    }
}
