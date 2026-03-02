use bitdo_proto::{
    device_profile_for, BitdoError, DeviceSession, MockTransport, SessionConfig, SupportLevel,
    SupportTier, VidPid,
};

const CANDIDATE_READONLY_PIDS: &[u16] = &[
    0x6002, 0x6003, 0x3010, 0x3011, 0x3012, 0x3013, 0x5200, 0x5201, 0x203a, 0x2049, 0x2028, 0x202e,
    0x3004, 0x3019, 0x3100, 0x3105, 0x2100, 0x2101, 0x901a, 0x6006, 0x5203, 0x5204, 0x301a, 0x9028,
    0x3026, 0x3027,
];

#[test]
fn candidate_targets_are_candidate_readonly() {
    for pid in CANDIDATE_READONLY_PIDS {
        let profile = device_profile_for(VidPid::new(0x2dc8, *pid));
        assert_eq!(
            profile.support_tier,
            SupportTier::CandidateReadOnly,
            "expected candidate-readonly for pid={pid:#06x}"
        );
        assert_eq!(
            profile.support_level,
            SupportLevel::DetectOnly,
            "support_level remains detect-only until full promotion"
        );
    }
}

#[test]
fn candidate_standard_pid_allows_diag_read_but_blocks_write_and_unsafe() {
    let pid = 0x6002;
    let mut transport = MockTransport::default();
    // get_mode issues up to 3 reads; allow timeout outcome to prove it was permitted by policy.
    transport.push_read_timeout();
    transport.push_read_timeout();
    transport.push_read_timeout();

    let mut session = DeviceSession::new(
        transport,
        VidPid::new(0x2dc8, pid),
        SessionConfig {
            experimental: true,
            ..Default::default()
        },
    )
    .expect("open session");

    let mode_err = session
        .get_mode()
        .expect_err("candidate get_mode should execute and fail only at transport/response stage");
    assert!(matches!(
        mode_err,
        BitdoError::Timeout | BitdoError::MalformedResponse { .. }
    ));

    let write_err = session
        .set_mode(1)
        .expect_err("candidate safe-write must be blocked");
    assert!(matches!(write_err, BitdoError::UnsupportedForPid { .. }));

    let unsafe_err = session
        .enter_bootloader()
        .expect_err("candidate unsafe command must be blocked");
    assert!(matches!(unsafe_err, BitdoError::UnsupportedForPid { .. }));

    let _ = session.close();
}

#[test]
fn candidate_jp_pid_remains_diag_only() {
    let pid = 0x5200;
    let mut transport = MockTransport::default();
    transport.push_read_data({
        let mut response = vec![0u8; 64];
        response[0] = 0x02;
        response[1] = 0x05;
        response[4] = 0xC1;
        response
    });

    let mut session = DeviceSession::new(
        transport,
        VidPid::new(0x2dc8, pid),
        SessionConfig {
            experimental: true,
            ..Default::default()
        },
    )
    .expect("open session");

    let identify = session.identify().expect("identify allowed");
    assert_eq!(identify.target.pid, pid);
    let profile = device_profile_for(VidPid::new(0x2dc8, pid));
    assert_eq!(profile.support_tier, SupportTier::CandidateReadOnly);

    let mode_err = session
        .get_mode()
        .expect_err("jp candidate should not expose mode read path");
    assert!(matches!(mode_err, BitdoError::UnsupportedForPid { .. }));

    let _ = session.close();
}

#[test]
fn wave2_candidate_standard_pid_allows_safe_reads_only() {
    let pid = 0x3100;
    let mut transport = MockTransport::default();
    transport.push_read_timeout();
    transport.push_read_timeout();
    transport.push_read_timeout();

    let mut session = DeviceSession::new(
        transport,
        VidPid::new(0x2dc8, pid),
        SessionConfig {
            experimental: true,
            ..Default::default()
        },
    )
    .expect("open session");

    let mode_err = session.get_mode().expect_err(
        "wave2 candidate get_mode should be permitted and fail at transport/response stage",
    );
    assert!(matches!(
        mode_err,
        BitdoError::Timeout | BitdoError::MalformedResponse { .. }
    ));

    let write_err = session
        .set_mode(1)
        .expect_err("wave2 candidate safe-write must be blocked");
    assert!(matches!(write_err, BitdoError::UnsupportedForPid { .. }));

    let _ = session.close();
}
