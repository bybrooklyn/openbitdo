use std::path::PathBuf;
use std::process::Command;

#[test]
fn guard_script_passes() {
    let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let sdk_root = manifest.join("../..");
    let script = sdk_root.join("scripts/cleanroom_guard.sh");

    let status = Command::new("bash")
        .arg(script)
        .current_dir(&sdk_root)
        .status()
        .expect("run cleanroom_guard.sh");

    assert!(status.success());
}
