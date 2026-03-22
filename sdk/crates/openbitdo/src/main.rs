
use anyhow::Result;
use bitdo_app_core::{OpenBitdoCore, OpenBitdoCoreConfig};
use bitdo_tui::{run_ui, UiLaunchOptions};
use clap::Parser;
use openbitdo::{load_user_settings, user_settings_path, BuildInfo, UserSettings};

const CLI_AFTER_HELP: &str = "\
Examples:
  openbitdo
  openbitdo --mock

Install:
  Homebrew: brew tap bybrooklyn/openbitdo && brew install openbitdo
  AUR:      paru -S openbitdo-bin
  Releases: download a tarball, then run bin/openbitdo

Notes:
  --mock starts the app without real hardware.
  macOS packages are currently unsigned and non-notarized.
";

#[derive(Debug, Parser)]
#[command(name = "openbitdo")]
#[command(about = "Beginner-first 8BitDo controller utility")]
#[command(after_help = CLI_AFTER_HELP)]
struct Cli {
    #[arg(long, help = "Use mock transport/devices")]
    mock: bool,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt::init();

    let cli = Cli::parse();
    let settings_path = user_settings_path();
    let settings = match load_user_settings(&settings_path) {
        Ok(settings) => settings,
        Err(err) => {
            eprintln!(
                "warning: failed to load settings from {}: {err}; using defaults",
                settings_path.display()
            );
            UserSettings::default()
        }
    };

    let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: cli.mock,
        advanced_mode: settings.advanced_mode,
        progress_interval_ms: 5,
        ..Default::default()
    });

    run_ui(
        core,
        UiLaunchOptions {
            build_info: BuildInfo::current().to_tui_info(),
            advanced_mode: settings.advanced_mode,
            report_save_mode: settings.report_save_mode,
            // The TUI is the interactive acknowledgement surface for unsafe firmware flows.
            allow_unsafe: true,
            brick_risk_ack: true,
            settings_path: Some(settings_path),
            ..Default::default()
        },
    )
    .await?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use clap::error::ErrorKind;

    #[test]
    fn cli_supports_mock_only() {
        let cli = Cli::parse_from(["openbitdo", "--mock"]);
        assert!(cli.mock);
    }

    #[test]
    fn cli_rejects_ui_subcommand_form() {
        let err = Cli::try_parse_from(["openbitdo", "ui", "--mock"]).expect_err("must reject ui");
        assert_eq!(err.kind(), ErrorKind::UnknownArgument);
    }

    #[test]
    fn cli_rejects_run_subcommand_form() {
        let err =
            Cli::try_parse_from(["openbitdo", "run", "--vidpid", "2dc8:6009", "--recommended"])
                .expect_err("must reject run");
        assert_eq!(err.kind(), ErrorKind::UnknownArgument);
    }

    #[test]
    fn cli_rejects_legacy_cmd_subcommand() {
        let err = Cli::try_parse_from(["openbitdo", "cmd"]).expect_err("must reject cmd");
        assert_eq!(err.kind(), ErrorKind::UnknownArgument);
    }
}
