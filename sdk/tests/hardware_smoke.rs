use bitdo_proto::{
    device_profile_for, enumerate_hid_devices, DeviceSession, HidTransport, ProtocolFamily,
    SessionConfig, VidPid,
};

fn hardware_enabled() -> bool {
    std::env::var("BITDO_HARDWARE").ok().as_deref() == Some("1")
}

fn parse_pid(input: &str) -> Option<u16> {
    let trimmed = input.trim();
    if let Some(hex) = trimmed
        .strip_prefix("0x")
        .or_else(|| trimmed.strip_prefix("0X"))
    {
        u16::from_str_radix(hex, 16).ok()
    } else {
        trimmed.parse::<u16>().ok()
    }
}

fn expected_pid(env_key: &str, family: &str) -> u16 {
    let raw = std::env::var(env_key)
        .unwrap_or_else(|_| panic!("missing required {env_key} for {family} family hardware gate"));
    parse_pid(&raw).unwrap_or_else(|| {
        panic!("invalid {env_key} value '{raw}' for {family} family hardware gate")
    })
}

fn attached_8bitdo_pids() -> Vec<u16> {
    enumerate_hid_devices()
        .expect("enumeration")
        .into_iter()
        .filter(|d| d.vid_pid.vid == 0x2dc8)
        .map(|d| d.vid_pid.pid)
        .collect()
}

fn assert_family_fixture(env_key: &str, family: &str, expected_family: ProtocolFamily) {
    if !hardware_enabled() {
        return;
    }

    let pid = expected_pid(env_key, family);
    let attached_pids = attached_8bitdo_pids();
    assert!(
        attached_pids.contains(&pid),
        "missing fixture for {family}: expected attached pid={pid:#06x}, attached={:?}",
        attached_pids
            .iter()
            .map(|value| format!("{value:#06x}"))
            .collect::<Vec<_>>()
    );

    let profile = device_profile_for(VidPid::new(0x2dc8, pid));
    assert_eq!(
        profile.protocol_family, expected_family,
        "expected {family} family for pid={pid:#06x}, got {:?}",
        profile.protocol_family
    );
}

fn assert_named_fixture(env_key: &str, name: &str, expected_family: ProtocolFamily) -> u16 {
    if !hardware_enabled() {
        return 0;
    }

    let pid = expected_pid(env_key, name);
    let attached_pids = attached_8bitdo_pids();
    assert!(
        attached_pids.contains(&pid),
        "missing fixture for {name}: expected attached pid={pid:#06x}, attached={:?}",
        attached_pids
            .iter()
            .map(|value| format!("{value:#06x}"))
            .collect::<Vec<_>>()
    );

    let profile = device_profile_for(VidPid::new(0x2dc8, pid));
    assert_eq!(
        profile.protocol_family, expected_family,
        "expected {name} family {:?} for pid={pid:#06x}, got {:?}",
        expected_family, profile.protocol_family
    );

    pid
}

#[test]
#[ignore = "requires lab hardware and BITDO_HARDWARE=1"]
fn hardware_smoke_detect_devices() {
    if !hardware_enabled() {
        eprintln!("BITDO_HARDWARE!=1, skipping");
        return;
    }

    let devices = enumerate_hid_devices().expect("enumeration");
    let eight_bitdo: Vec<_> = devices
        .into_iter()
        .filter(|d| d.vid_pid.vid == 0x2dc8)
        .collect();

    assert!(!eight_bitdo.is_empty(), "no 8BitDo devices detected");
}

#[test]
#[ignore = "requires lab hardware and BITDO_EXPECT_DINPUT_PID"]
fn hardware_smoke_dinput_family() {
    assert_family_fixture("BITDO_EXPECT_DINPUT_PID", "DInput", ProtocolFamily::DInput);
}

#[test]
#[ignore = "requires lab hardware and BITDO_EXPECT_STANDARD64_PID"]
fn hardware_smoke_standard64_family() {
    assert_family_fixture(
        "BITDO_EXPECT_STANDARD64_PID",
        "Standard64",
        ProtocolFamily::Standard64,
    );
}

#[test]
#[ignore = "requires lab hardware and BITDO_EXPECT_JPHANDSHAKE_PID"]
fn hardware_smoke_jphandshake_family() {
    assert_family_fixture(
        "BITDO_EXPECT_JPHANDSHAKE_PID",
        "JpHandshake",
        ProtocolFamily::JpHandshake,
    );
}

#[test]
#[ignore = "requires lab hardware and BITDO_EXPECT_ULTIMATE2_PID"]
fn hardware_smoke_ultimate2_core_ops() {
    if !hardware_enabled() {
        return;
    }

    let pid = assert_named_fixture(
        "BITDO_EXPECT_ULTIMATE2_PID",
        "Ultimate2",
        ProtocolFamily::DInput,
    );
    let profile = device_profile_for(VidPid::new(0x2dc8, pid));
    assert!(profile.capability.supports_u2_slot_config);
    assert!(profile.capability.supports_u2_button_map);

    let mut session = DeviceSession::new(
        HidTransport::new(),
        VidPid::new(0x2dc8, pid),
        SessionConfig {
            experimental: true,
            ..Default::default()
        },
    )
    .expect("open session");

    let mode_before = session.get_mode().expect("read mode").mode;
    session
        .u2_set_mode(mode_before)
        .expect("mode read/write/readback");
    let mode_after = session.get_mode().expect("read mode after write").mode;
    assert_eq!(mode_after, mode_before);

    let slot = session.u2_get_current_slot().expect("read current slot");
    let config_before = session.u2_read_config_slot(slot).expect("read config slot");
    session
        .u2_write_config_slot(slot, &config_before)
        .expect("write config slot");
    let config_after = session
        .u2_read_config_slot(slot)
        .expect("read config readback");
    assert!(!config_after.is_empty());

    let map_before = session.u2_read_button_map(slot).expect("read button map");
    session
        .u2_write_button_map(slot, &map_before)
        .expect("write button map");
    let map_after = session
        .u2_read_button_map(slot)
        .expect("read button map readback");
    assert_eq!(map_before.len(), map_after.len());

    // Firmware smoke is preflight-only in CI: dry_run avoids any transfer/write.
    session
        .firmware_transfer(&[0xAA; 128], 32, true)
        .expect("firmware preflight dry-run");

    let _ = session.close();
}

#[test]
#[ignore = "requires lab hardware and BITDO_EXPECT_108JP_PID"]
fn hardware_smoke_108jp_dedicated_ops() {
    if !hardware_enabled() {
        return;
    }

    let pid = assert_named_fixture(
        "BITDO_EXPECT_108JP_PID",
        "JP108",
        ProtocolFamily::JpHandshake,
    );
    let profile = device_profile_for(VidPid::new(0x2dc8, pid));
    assert!(profile.capability.supports_jp108_dedicated_map);

    let mut session = DeviceSession::new(
        HidTransport::new(),
        VidPid::new(0x2dc8, pid),
        SessionConfig {
            experimental: true,
            ..Default::default()
        },
    )
    .expect("open session");

    let mappings_before = session
        .jp108_read_dedicated_mappings()
        .expect("read dedicated mappings");
    assert!(mappings_before.len() >= 3);

    for idx in [0u8, 1u8, 2u8] {
        let usage = mappings_before
            .iter()
            .find(|(entry_idx, _)| *entry_idx == idx)
            .map(|(_, usage)| *usage)
            .unwrap_or(0);
        session
            .jp108_write_dedicated_mapping(idx, usage)
            .expect("write dedicated mapping");
    }

    let mappings_after = session
        .jp108_read_dedicated_mappings()
        .expect("read dedicated mappings readback");
    assert!(mappings_after.len() >= 3);

    session
        .firmware_transfer(&[0xBB; 128], 32, true)
        .expect("firmware preflight dry-run");

    let _ = session.close();
}
