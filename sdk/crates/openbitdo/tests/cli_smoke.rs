use assert_cmd::cargo::cargo_bin_cmd;
use predicates::prelude::*;

#[test]
fn help_mentions_beginner_flow() {
    let mut cmd = cargo_bin_cmd!("openbitdo");
    cmd.arg("--help")
        .assert()
        .success()
        .stdout(predicate::str::contains("beginner-first"))
        .stdout(predicate::str::contains("--mock"))
        .stdout(predicate::str::contains("cmd").not());
}
