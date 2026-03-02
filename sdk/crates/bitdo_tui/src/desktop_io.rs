use anyhow::{anyhow, Result};
use std::path::Path;
use std::process::{Command, Stdio};

/// Open a file or directory with the user's default desktop application.
pub(crate) fn open_path_with_default_app(path: &Path) -> Result<()> {
    let mut cmd = if cfg!(target_os = "macos") {
        let mut c = Command::new("open");
        c.arg(path);
        c
    } else {
        let mut c = Command::new("xdg-open");
        c.arg(path);
        c
    };
    let status = cmd.status()?;
    if status.success() {
        Ok(())
    } else {
        Err(anyhow!(
            "failed to open path with default app: {}",
            path.to_string_lossy()
        ))
    }
}

/// Copy text into the system clipboard using platform-appropriate commands.
pub(crate) fn copy_text_to_clipboard(text: &str) -> Result<()> {
    if cfg!(target_os = "macos") {
        return copy_via_command("pbcopy", &[], text);
    }

    if command_exists("wl-copy") {
        return copy_via_command("wl-copy", &[], text);
    }
    if command_exists("xclip") {
        return copy_via_command("xclip", &["-selection", "clipboard"], text);
    }

    Err(anyhow!(
        "no clipboard utility found (tried pbcopy/wl-copy/xclip)"
    ))
}

fn command_exists(name: &str) -> bool {
    Command::new("sh")
        .arg("-c")
        .arg(format!("command -v {name} >/dev/null 2>&1"))
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

fn copy_via_command(command: &str, args: &[&str], text: &str) -> Result<()> {
    let mut child = Command::new(command)
        .args(args)
        .stdin(Stdio::piped())
        .spawn()?;
    if let Some(stdin) = child.stdin.as_mut() {
        use std::io::Write as _;
        stdin.write_all(text.as_bytes())?;
    }
    let status = child.wait()?;
    if status.success() {
        Ok(())
    } else {
        Err(anyhow!("clipboard command failed: {command}"))
    }
}
