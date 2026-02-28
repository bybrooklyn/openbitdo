use bitdo_proto::{BitdoError, DeviceSession, MockTransport, SessionConfig, VidPid};

#[test]
fn unsafe_boot_requires_dual_ack() {
    let transport = MockTransport::default();
    let mut session = DeviceSession::new(
        transport,
        VidPid::new(0x2dc8, 24585),
        SessionConfig {
            allow_unsafe: true,
            brick_risk_ack: false,
            experimental: true,
            ..SessionConfig::default()
        },
    )
    .expect("session init");

    let err = session.enter_bootloader().expect_err("expected denial");
    match err {
        BitdoError::UnsafeCommandDenied { .. } => {}
        other => panic!("unexpected error: {other:?}"),
    }
}

#[test]
fn unsafe_boot_succeeds_with_dual_ack() {
    let transport = MockTransport::default();
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

    session.enter_bootloader().expect("boot sequence");
}
