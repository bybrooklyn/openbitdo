use std::env;
use std::fs;
use std::path::{Path, PathBuf};

fn main() {
    let manifest_dir =
        PathBuf::from(env::var("CARGO_MANIFEST_DIR").expect("missing CARGO_MANIFEST_DIR"));
    let spec_dir = manifest_dir.join("../../../spec");
    let out_dir = PathBuf::from(env::var("OUT_DIR").expect("missing OUT_DIR"));

    let pid_csv = spec_dir.join("pid_matrix.csv");
    let command_csv = spec_dir.join("command_matrix.csv");

    println!("cargo:rerun-if-changed={}", pid_csv.display());
    println!("cargo:rerun-if-changed={}", command_csv.display());

    generate_pid_registry(&pid_csv, &out_dir.join("generated_pid_registry.rs"));
    generate_command_registry(&command_csv, &out_dir.join("generated_command_registry.rs"));
}

fn generate_pid_registry(csv_path: &Path, out_path: &Path) {
    let mut rdr = csv::Reader::from_path(csv_path).expect("failed to open pid_matrix.csv");
    let mut out = String::new();
    out.push_str("pub const PID_REGISTRY: &[crate::registry::PidRegistryRow] = &[\n");

    for rec in rdr.records() {
        let rec = rec.expect("invalid pid csv record");
        let name = rec.get(0).expect("pid_name");
        let pid: u16 = rec
            .get(1)
            .expect("pid_decimal")
            .parse()
            .expect("invalid pid decimal");
        let support_level = match rec.get(5).expect("support_level") {
            "full" => "crate::types::SupportLevel::Full",
            "detect-only" => "crate::types::SupportLevel::DetectOnly",
            other => panic!("unknown support_level {other}"),
        };
        let protocol_family = match rec.get(6).expect("protocol_family") {
            "Standard64" => "crate::types::ProtocolFamily::Standard64",
            "JpHandshake" => "crate::types::ProtocolFamily::JpHandshake",
            "DInput" => "crate::types::ProtocolFamily::DInput",
            "DS4Boot" => "crate::types::ProtocolFamily::DS4Boot",
            "Unknown" => "crate::types::ProtocolFamily::Unknown",
            other => panic!("unknown protocol_family {other}"),
        };

        out.push_str(&format!(
            "    crate::registry::PidRegistryRow {{ name: \"{name}\", pid: {pid}, support_level: {support_level}, protocol_family: {protocol_family} }},\n"
        ));
    }

    out.push_str("]\n;");
    fs::write(out_path, out).expect("failed writing generated_pid_registry.rs");
}

fn generate_command_registry(csv_path: &Path, out_path: &Path) {
    let mut rdr = csv::Reader::from_path(csv_path).expect("failed to open command_matrix.csv");
    let mut out = String::new();
    out.push_str("pub const COMMAND_REGISTRY: &[crate::registry::CommandRegistryRow] = &[\n");

    for rec in rdr.records() {
        let rec = rec.expect("invalid command csv record");
        let id = rec.get(0).expect("command_id");
        let safety_class = match rec.get(1).expect("safety_class") {
            "SafeRead" => "crate::types::SafetyClass::SafeRead",
            "SafeWrite" => "crate::types::SafetyClass::SafeWrite",
            "UnsafeBoot" => "crate::types::SafetyClass::UnsafeBoot",
            "UnsafeFirmware" => "crate::types::SafetyClass::UnsafeFirmware",
            other => panic!("unknown safety_class {other}"),
        };
        let confidence = match rec.get(2).expect("confidence") {
            "confirmed" => "crate::types::CommandConfidence::Confirmed",
            "inferred" => "crate::types::CommandConfidence::Inferred",
            other => panic!("unknown confidence {other}"),
        };
        let experimental_default = rec
            .get(3)
            .expect("experimental_default")
            .parse::<bool>()
            .expect("invalid experimental_default");
        let report_id = parse_u8(rec.get(4).expect("report_id"));
        let request_hex = rec.get(6).expect("request_hex");
        let request = hex_to_bytes(request_hex);
        let expected_response = rec.get(7).expect("expected_response");

        out.push_str(&format!(
            "    crate::registry::CommandRegistryRow {{ id: crate::command::CommandId::{id}, safety_class: {safety_class}, confidence: {confidence}, experimental_default: {experimental_default}, report_id: {report_id}, request: &{request:?}, expected_response: \"{expected_response}\" }},\n"
        ));
    }

    out.push_str("]\n;");
    fs::write(out_path, out).expect("failed writing generated_command_registry.rs");
}

fn parse_u8(value: &str) -> u8 {
    if let Some(stripped) = value.strip_prefix("0x") {
        u8::from_str_radix(stripped, 16).expect("invalid hex u8")
    } else {
        value.parse::<u8>().expect("invalid u8")
    }
}

fn hex_to_bytes(hex: &str) -> Vec<u8> {
    let hex = hex.trim();
    if hex.len() % 2 != 0 {
        panic!("hex length must be even: {hex}");
    }
    let mut bytes = Vec::with_capacity(hex.len() / 2);
    let raw = hex.as_bytes();
    for i in (0..raw.len()).step_by(2) {
        let hi = (raw[i] as char)
            .to_digit(16)
            .unwrap_or_else(|| panic!("invalid hex: {hex}"));
        let lo = (raw[i + 1] as char)
            .to_digit(16)
            .unwrap_or_else(|| panic!("invalid hex: {hex}"));
        bytes.push(((hi << 4) | lo) as u8);
    }
    bytes
}
