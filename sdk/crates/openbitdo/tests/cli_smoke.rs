use assert_cmd::cargo::cargo_bin_cmd;
use predicates::prelude::*;

#[test]
fn help_mentions_single_command_surface() {
    let mut cmd = cargo_bin_cmd!("openbitdo");
    cmd.arg("--help")
        .assert()
        .success()
        .stdout(predicate::str::contains("Usage: openbitdo [OPTIONS]"))
        .stdout(predicate::str::contains("--mock"))
        .stdout(predicate::str::contains("Examples:"))
        .stdout(predicate::str::contains(
            "Homebrew: brew tap bybrooklyn/openbitdo && brew install openbitdo",
        ))
        .stdout(predicate::str::contains(
            "macOS packages are currently unsigned and non-notarized.",
        ))
        .stdout(predicate::str::contains("Commands:").not())
        .stdout(predicate::str::contains("ui").not())
        .stdout(predicate::str::contains("run [OPTIONS]").not());
}

#[test]
fn rejects_ui_subcommand_form() {
    let mut cmd = cargo_bin_cmd!("openbitdo");
    cmd.args(["ui", "--mock"]).assert().failure();
}

#[test]
fn rejects_run_subcommand_form() {
    let mut cmd = cargo_bin_cmd!("openbitdo");
    cmd.args(["run", "--vidpid", "2dc8:6009", "--recommended"])
        .assert()
        .failure();
}
