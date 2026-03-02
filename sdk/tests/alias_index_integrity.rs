use bitdo_proto::pid_registry;
use std::fs;
use std::path::PathBuf;

#[test]
fn alias_index_matches_unique_registry_policy() {
    let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let alias_path = manifest.join("../../../spec/alias_index.md");
    let body = fs::read_to_string(alias_path).expect("read alias_index.md");

    assert!(body.contains("PID_Pro2_OLD"));
    assert!(body.contains("PID_Pro2"));
    assert!(body.contains("0x6003"));
    assert!(body.contains("PID_ASLGMouse"));
    assert!(body.contains("PID_Mouse"));
    assert!(body.contains("0x5205"));

    let names = pid_registry()
        .iter()
        .map(|row| row.name)
        .collect::<Vec<_>>();
    assert!(names.contains(&"PID_Pro2"));
    assert!(names.contains(&"PID_Mouse"));
    assert!(!names.contains(&"PID_Pro2_OLD"));
    assert!(!names.contains(&"PID_ASLGMouse"));
}
