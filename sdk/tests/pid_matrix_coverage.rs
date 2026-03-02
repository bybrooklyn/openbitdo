use bitdo_proto::{find_pid, pid_registry, ProtocolFamily, SupportLevel, SupportTier};
use std::collections::HashSet;
use std::fs;
use std::path::PathBuf;

#[test]
fn pid_registry_matches_spec_rows() {
    let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let csv_path = manifest.join("../../../spec/pid_matrix.csv");
    let content = fs::read_to_string(csv_path).expect("read pid_matrix.csv");
    let mut lines = content.lines();
    let header = lines.next().expect("pid matrix header");
    let columns = header.split(',').collect::<Vec<_>>();
    let idx_name = col_index(&columns, "pid_name");
    let idx_pid = col_index(&columns, "pid_hex");
    let idx_level = col_index(&columns, "support_level");
    let idx_tier = col_index(&columns, "support_tier");
    let idx_family = col_index(&columns, "protocol_family");

    let rows = lines.filter(|l| !l.trim().is_empty()).count();
    assert_eq!(rows, pid_registry().len());

    let mut seen = HashSet::new();
    for row in content.lines().skip(1).filter(|l| !l.trim().is_empty()) {
        let fields = row.split(',').collect::<Vec<_>>();
        let name = fields[idx_name];
        let pid_hex = fields[idx_pid];
        let level = fields[idx_level];
        let tier = fields[idx_tier];
        let family = fields[idx_family];

        let pid = parse_hex_u16(pid_hex);
        assert!(
            seen.insert(pid),
            "duplicate PID found in pid_matrix.csv: {pid_hex} (name={name})"
        );
        let reg = find_pid(pid).unwrap_or_else(|| panic!("missing pid in registry: {pid_hex}"));
        assert_eq!(reg.name, name, "name mismatch for pid={pid_hex}");
        assert_eq!(
            reg.support_level,
            parse_support_level(level),
            "support_level mismatch for pid={pid_hex}"
        );
        assert_eq!(
            reg.support_tier,
            parse_support_tier(tier),
            "support_tier mismatch for pid={pid_hex}"
        );
        assert_eq!(
            reg.protocol_family,
            parse_family(family),
            "protocol_family mismatch for pid={pid_hex}"
        );
    }
}

fn col_index(columns: &[&str], name: &str) -> usize {
    columns
        .iter()
        .position(|c| *c == name)
        .unwrap_or_else(|| panic!("missing column: {name}"))
}

fn parse_hex_u16(v: &str) -> u16 {
    u16::from_str_radix(v.trim_start_matches("0x"), 16).expect("valid pid hex")
}

fn parse_support_level(v: &str) -> SupportLevel {
    match v {
        "full" => SupportLevel::Full,
        "detect-only" => SupportLevel::DetectOnly,
        other => panic!("unknown support_level: {other}"),
    }
}

fn parse_support_tier(v: &str) -> SupportTier {
    match v {
        "full" => SupportTier::Full,
        "candidate-readonly" => SupportTier::CandidateReadOnly,
        "detect-only" => SupportTier::DetectOnly,
        other => panic!("unknown support_tier: {other}"),
    }
}

fn parse_family(v: &str) -> ProtocolFamily {
    match v {
        "Standard64" => ProtocolFamily::Standard64,
        "JpHandshake" => ProtocolFamily::JpHandshake,
        "DInput" => ProtocolFamily::DInput,
        "DS4Boot" => ProtocolFamily::DS4Boot,
        "Unknown" => ProtocolFamily::Unknown,
        other => panic!("unknown protocol_family: {other}"),
    }
}
