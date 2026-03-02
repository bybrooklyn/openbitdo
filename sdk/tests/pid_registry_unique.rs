use bitdo_proto::pid_registry;
use std::collections::HashSet;

#[test]
fn pid_registry_contains_unique_pid_values() {
    let mut seen = HashSet::new();
    for row in pid_registry() {
        assert!(
            seen.insert(row.pid),
            "duplicate pid in runtime registry: {:#06x} ({})",
            row.pid,
            row.name
        );
    }
}
