use bitdo_proto::{DeviceSession, MockTransport, SessionConfig, VidPid};

#[test]
fn firmware_transfer_chunks_and_commit() {
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
    let report = session
        .firmware_transfer(&image, 50, false)
        .expect("fw transfer");
    assert_eq!(report.chunks_sent, 3);

    let transport = session.into_transport();
    assert_eq!(transport.writes().len(), 4);
}
