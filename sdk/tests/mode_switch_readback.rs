use bitdo_proto::{
    DeviceSession, MockTransport, RetryPolicy, SessionConfig, TimeoutProfile, VidPid,
};

#[test]
fn set_mode_reads_back_latest_mode() {
    let mut transport = MockTransport::default();
    transport.push_read_data(vec![0x02, 0x01, 0x00, 0x00]);

    let mut mode = vec![0u8; 64];
    mode[0] = 0x02;
    mode[1] = 0x05;
    mode[5] = 3;
    transport.push_read_data(mode);

    let config = SessionConfig {
        retry_policy: RetryPolicy {
            max_attempts: 2,
            backoff_ms: 0,
        },
        timeout_profile: TimeoutProfile {
            probe_ms: 10,
            io_ms: 10,
            firmware_ms: 10,
        },
        ..SessionConfig::default()
    };

    let mut session =
        DeviceSession::new(transport, VidPid::new(0x2dc8, 24585), config).expect("session init");

    let mode_state = session.set_mode(3).expect("set mode");
    assert_eq!(mode_state.mode, 3);
}
