use bitdo_proto::{BitdoError, DeviceSession, MockTransport, SessionConfig, VidPid};

#[test]
fn inferred_firmware_transfer_is_blocked_until_confirmed() {
    let mut transport = MockTransport::default();
    for _ in 0..4 {
        transport.push_read_data(vec![0x02, 0x10, 0x00, 0x00]);
    }

    let mut session = DeviceSession::new(
        transport,
        VidPid::new(0x2dc8, 24585),
        SessionConfig {
            allow_unsafe: true,
            brick_risk_ack: true,
            experimental: true,
            ..SessionConfig::default()
        },
    )
    .expect("session init");

    let image = vec![0xAB; 120];
    let err = session
        .firmware_transfer(&image, 50, false)
        .expect_err("inferred firmware chunk/commit must remain blocked");
    assert!(matches!(err, BitdoError::UnsupportedForPid { .. }));

    let transport = session.into_transport();
    assert_eq!(transport.writes().len(), 0);
}
