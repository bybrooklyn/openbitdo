use bitdo_proto::{
    DeviceSession, MockTransport, RetryPolicy, SessionConfig, TimeoutProfile, VidPid,
};

#[test]
fn retries_after_timeout_then_succeeds() {
    let mut transport = MockTransport::default();
    transport.push_read_timeout();
    let mut good = vec![0u8; 64];
    good[0] = 0x02;
    good[1] = 0x05;
    good[4] = 0xC1;
    good[22] = 0x09;
    good[23] = 0x60;
    transport.push_read_data(good);

    let config = SessionConfig {
        retry_policy: RetryPolicy {
            max_attempts: 3,
            backoff_ms: 0,
        },
        timeout_profile: TimeoutProfile {
            probe_ms: 1,
            io_ms: 1,
            firmware_ms: 1,
        },
        allow_unsafe: false,
        brick_risk_ack: false,
        experimental: false,
        trace_enabled: true,
    };
    let mut session =
        DeviceSession::new(transport, VidPid::new(0x2dc8, 24585), config).expect("session init");

    let response = session
        .send_command(bitdo_proto::CommandId::GetPid, None)
        .expect("response");
    assert_eq!(
        response.parsed_fields.get("detected_pid").copied(),
        Some(24585)
    );
}

#[test]
fn retries_after_malformed_then_succeeds() {
    let mut transport = MockTransport::default();
    let mut malformed = vec![0u8; 64];
    malformed[0] = 0x00;
    malformed[1] = 0x05;
    malformed[4] = 0xC1;
    transport.push_read_data(malformed);

    let mut good = vec![0u8; 64];
    good[0] = 0x02;
    good[1] = 0x05;
    good[4] = 0xC1;
    good[22] = 0x09;
    good[23] = 0x60;
    transport.push_read_data(good);

    let config = SessionConfig {
        retry_policy: RetryPolicy {
            max_attempts: 3,
            backoff_ms: 0,
        },
        timeout_profile: TimeoutProfile {
            probe_ms: 1,
            io_ms: 1,
            firmware_ms: 1,
        },
        allow_unsafe: false,
        brick_risk_ack: false,
        experimental: false,
        trace_enabled: true,
    };
    let mut session =
        DeviceSession::new(transport, VidPid::new(0x2dc8, 24585), config).expect("session init");

    let response = session
        .send_command(bitdo_proto::CommandId::GetPid, None)
        .expect("response");
    assert_eq!(
        response.parsed_fields.get("detected_pid").copied(),
        Some(24585)
    );
}
