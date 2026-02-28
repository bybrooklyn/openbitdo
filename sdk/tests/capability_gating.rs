use bitdo_proto::{BitdoError, DeviceSession, MockTransport, SessionConfig, VidPid};

#[test]
fn detect_only_pid_blocks_unsafe_operations() {
    let transport = MockTransport::default();
    let config = SessionConfig {
        allow_unsafe: true,
        brick_risk_ack: true,
        experimental: true,
        ..SessionConfig::default()
    };

    let mut session =
        DeviceSession::new(transport, VidPid::new(0x2dc8, 8448), config).expect("session init");

    let err = session
        .enter_bootloader()
        .expect_err("must reject unsafe op");
    match err {
        BitdoError::UnsupportedForPid { .. } => {}
        other => panic!("unexpected error: {other:?}"),
    }
}
