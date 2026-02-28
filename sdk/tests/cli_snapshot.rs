use assert_cmd::cargo::cargo_bin_cmd;
use predicates::prelude::*;
use std::fs;

#[test]
fn list_mock_text_snapshot() {
    let mut cmd = cargo_bin_cmd!("bitdoctl");
    cmd.args(["--mock", "list"])
        .assert()
        .success()
        .stdout(predicate::str::contains("2dc8:6009"));
}

#[test]
fn identify_mock_json_snapshot() {
    let mut cmd = cargo_bin_cmd!("bitdoctl");
    cmd.args(["--mock", "--json", "--pid", "24585", "identify"])
        .assert()
        .success()
        .stdout(predicate::str::contains("\"capability\""));
}

#[test]
fn mode_get_mock_json_snapshot() {
    let mut cmd = cargo_bin_cmd!("bitdoctl");
    cmd.args(["--mock", "--json", "--pid", "24585", "mode", "get"])
        .assert()
        .success()
        .stdout(predicate::str::contains("\"mode\": 2"));
}

#[test]
fn diag_probe_mock_json_snapshot() {
    let mut cmd = cargo_bin_cmd!("bitdoctl");
    cmd.args(["--mock", "--json", "--pid", "24585", "diag", "probe"])
        .assert()
        .success()
        .stdout(predicate::str::contains("\"command_checks\""));
}

#[test]
fn firmware_dry_run_snapshot() {
    let tmp = std::env::temp_dir().join("bitdoctl-fw-test.bin");
    fs::write(&tmp, vec![0xAA; 128]).expect("write temp fw");

    let mut cmd = cargo_bin_cmd!("bitdoctl");
    cmd.args([
        "--mock",
        "--json",
        "--pid",
        "24585",
        "--unsafe",
        "--i-understand-brick-risk",
        "--experimental",
        "fw",
        "write",
        "--file",
        tmp.to_str().expect("path"),
        "--dry-run",
    ])
    .assert()
    .success()
    .stdout(predicate::str::contains("\"dry_run\": true"));

    let _ = fs::remove_file(tmp);
}
