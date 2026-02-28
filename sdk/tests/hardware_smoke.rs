use bitdo_proto::{device_profile_for, enumerate_hid_devices, ProtocolFamily, VidPid};

fn hardware_enabled() -> bool {
    std::env::var("BITDO_HARDWARE").ok().as_deref() == Some("1")
}

fn expected_pid(env_key: &str) -> Option<u16> {
    std::env::var(env_key).ok().and_then(|v| {
        let trimmed = v.trim();
        if let Some(hex) = trimmed
            .strip_prefix("0x")
            .or_else(|| trimmed.strip_prefix("0X"))
        {
            u16::from_str_radix(hex, 16).ok()
        } else {
            trimmed.parse::<u16>().ok()
        }
    })
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
#[ignore = "optional family check; set BITDO_EXPECT_DINPUT_PID"]
fn hardware_smoke_dinput_family() {
    if !hardware_enabled() {
        return;
    }
    let Some(pid) = expected_pid("BITDO_EXPECT_DINPUT_PID") else {
        eprintln!("BITDO_EXPECT_DINPUT_PID not set, skipping DInput family check");
        return;
    };

    let profile = device_profile_for(VidPid::new(0x2dc8, pid));
    assert_eq!(
        profile.protocol_family,
        ProtocolFamily::DInput,
        "expected DInput family for pid={pid:#06x}, got {:?}",
        profile.protocol_family
    );
}

#[test]
#[ignore = "optional family check; set BITDO_EXPECT_STANDARD64_PID"]
fn hardware_smoke_standard64_family() {
    if !hardware_enabled() {
        return;
    }
    let Some(pid) = expected_pid("BITDO_EXPECT_STANDARD64_PID") else {
        eprintln!("BITDO_EXPECT_STANDARD64_PID not set, skipping Standard64 family check");
        return;
    };

    let profile = device_profile_for(VidPid::new(0x2dc8, pid));
    assert_eq!(
        profile.protocol_family,
        ProtocolFamily::Standard64,
        "expected Standard64 family for pid={pid:#06x}, got {:?}",
        profile.protocol_family
    );
}

#[test]
#[ignore = "optional family check; set BITDO_EXPECT_JPHANDSHAKE_PID"]
fn hardware_smoke_jphandshake_family() {
    if !hardware_enabled() {
        return;
    }
    let Some(pid) = expected_pid("BITDO_EXPECT_JPHANDSHAKE_PID") else {
        eprintln!("BITDO_EXPECT_JPHANDSHAKE_PID not set, skipping JpHandshake family check");
        return;
    };

    let profile = device_profile_for(VidPid::new(0x2dc8, pid));
    assert_eq!(
        profile.protocol_family,
        ProtocolFamily::JpHandshake,
        "expected JpHandshake family for pid={pid:#06x}, got {:?}",
        profile.protocol_family
    );
}
