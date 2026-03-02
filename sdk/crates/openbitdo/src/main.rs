use anyhow::Result;
use bitdo_app_core::{OpenBitdoCore, OpenBitdoCoreConfig};
use bitdo_tui::{run_tui_app, TuiLaunchOptions};
use clap::Parser;
use openbitdo::{load_user_settings, user_settings_path, BuildInfo};

#[derive(Debug, Parser)]
#[command(name = "openbitdo")]
#[command(about = "OpenBitdo beginner-first launcher")]
struct Cli {
    #[arg(long, help = "Use mock transport/devices")]
    mock: bool,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt::init();
    let cli = Cli::parse();
    let settings_path = user_settings_path();
    let settings = load_user_settings(&settings_path);

    let core = OpenBitdoCore::new(OpenBitdoCoreConfig {
        mock_mode: cli.mock,
        advanced_mode: settings.advanced_mode,
        progress_interval_ms: 5,
        ..Default::default()
    });
    run_tui_app(
        core,
        TuiLaunchOptions {
            build_info: BuildInfo::current().to_tui_info(),
            advanced_mode: settings.advanced_mode,
            report_save_mode: settings.report_save_mode,
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
    fn cli_rejects_cmd_subcommand() {
        let err = Cli::try_parse_from(["openbitdo", "cmd"]).expect_err("must reject cmd");
        assert_eq!(err.kind(), ErrorKind::UnknownArgument);
    }
}
