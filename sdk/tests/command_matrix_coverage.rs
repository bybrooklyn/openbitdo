use bitdo_proto::{command_registry, CommandRuntimePolicy};
use std::fs;
use std::path::PathBuf;

#[test]
fn command_registry_matches_spec_rows_and_runtime_policy() {
    let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let csv_path = manifest.join("../../../spec/command_matrix.csv");
    let content = fs::read_to_string(csv_path).expect("read command_matrix.csv");

    let mut lines = content.lines();
    let header = lines.next().expect("command matrix header");
    let columns = header.split(',').collect::<Vec<_>>();
    let idx_command = col_index(&columns, "command_id");
    let idx_safety = col_index(&columns, "safety_class");
    let idx_confidence = col_index(&columns, "confidence");

    let spec_rows = content
        .lines()
        .skip(1)
        .filter(|row| !row.trim().is_empty() && !row.starts_with("command_id,"))
        .collect::<Vec<_>>();
    assert_eq!(
        spec_rows.len(),
        command_registry().len(),
        "command registry size mismatch vs command_matrix.csv"
    );

    for row in spec_rows {
        let fields = row.split(',').collect::<Vec<_>>();
        let command_name = fields[idx_command];
        let safety = fields[idx_safety];
        let confidence = fields[idx_confidence];
        let reg = command_registry()
            .iter()
            .find(|entry| format!("{:?}", entry.id) == command_name)
            .unwrap_or_else(|| panic!("missing command in registry: {command_name}"));

        let expected_policy = match (confidence, safety) {
            ("confirmed", _) => CommandRuntimePolicy::EnabledDefault,
            ("inferred", "SafeRead") => CommandRuntimePolicy::ExperimentalGate,
            ("inferred", _) => CommandRuntimePolicy::BlockedUntilConfirmed,
            other => panic!("unknown confidence/safety tuple: {other:?}"),
        };

        assert_eq!(
            reg.runtime_policy(),
            expected_policy,
            "runtime policy mismatch for command={command_name}"
        );
    }
}

fn col_index(columns: &[&str], name: &str) -> usize {
    columns
        .iter()
        .position(|c| *c == name)
        .unwrap_or_else(|| panic!("missing column: {name}"))
}
