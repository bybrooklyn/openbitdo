use std::env;
use std::process::Command;

fn main() {
    println!("cargo:rerun-if-changed=build.rs");

    emit("OPENBITDO_APP_VERSION", env::var("CARGO_PKG_VERSION").ok());
    emit("OPENBITDO_TARGET_TRIPLE", env::var("TARGET").ok());
    emit(
        "OPENBITDO_GIT_COMMIT_FULL",
        run_cmd("git", &["rev-parse", "HEAD"]),
    );
    emit(
        "OPENBITDO_GIT_COMMIT_SHORT",
        run_cmd("git", &["rev-parse", "--short=12", "HEAD"]),
    );
    emit(
        "OPENBITDO_BUILD_DATE_UTC",
        run_cmd("date", &["-u", "+%Y-%m-%dT%H:%M:%SZ"]),
    );
}

fn emit(key: &str, value: Option<String>) {
    let normalized = value
        .as_deref()
        .map(str::trim)
        .filter(|v| !v.is_empty())
        .unwrap_or("unknown");
    println!("cargo:rustc-env={key}={normalized}");
}

fn run_cmd(program: &str, args: &[&str]) -> Option<String> {
    let output = Command::new(program).args(args).output().ok()?;
    if !output.status.success() {
        return None;
    }

    String::from_utf8(output.stdout)
        .ok()
        .map(|v| v.trim().to_owned())
        .filter(|v| !v.is_empty())
}
