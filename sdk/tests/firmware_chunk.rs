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

#[test]
fn full_support_u2_firmware_transfer_uses_pid_specific_frames() {
    let mut transport = MockTransport::default();
    for _ in 0..4 {
        transport.push_read_data(vec![0x02, 0x10, 0x00, 0x00]);
    }

    let mut session = DeviceSession::new(
        transport,
        VidPid::new(0x2dc8, 0x6012),
        SessionConfig {
            allow_unsafe: true,
            brick_risk_ack: true,
            ..SessionConfig::default()
        },
    )
    .expect("session init");

    session.enter_bootloader().expect("enter bootloader");
    let image = vec![0xAB; 70];
    let report = session
        .firmware_transfer(&image, 32, false)
        .expect("firmware transfer");
    assert_eq!(report.chunks_sent, 3);
    session.exit_bootloader().expect("exit bootloader");

    let transport = session.into_transport();
    assert_eq!(transport.writes().len(), 6);
    assert_eq!(transport.writes()[0], vec![0x05, 0x00, 0x50, 0x01, 0x00, 0x00]);
    assert_eq!(&transport.writes()[1][..5], &[0x81, 0x60, 0x10, 0x60, 0x12]);
    assert_eq!(&transport.writes()[1][5..37], &image[..32]);
    assert_eq!(&transport.writes()[4][..5], &[0x81, 0x60, 0x11, 0x60, 0x12]);
    assert_eq!(transport.writes()[5], vec![0x05, 0x00, 0x51, 0x01, 0x00, 0x00]);
}
