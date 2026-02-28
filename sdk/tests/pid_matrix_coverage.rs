use bitdo_proto::pid_registry;
use std::fs;
use std::path::PathBuf;

#[test]
fn pid_registry_matches_spec_rows() {
    let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let csv_path = manifest.join("../../../spec/pid_matrix.csv");
    let content = fs::read_to_string(csv_path).expect("read pid_matrix.csv");
    let rows = content
        .lines()
        .skip(1)
        .filter(|l| !l.trim().is_empty())
        .count();
    assert_eq!(rows, pid_registry().len());
}
