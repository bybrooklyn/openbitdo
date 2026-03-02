use bitdo_proto::{DeviceSession, MockTransport, SessionConfig, VidPid};

#[test]
fn diag_probe_returns_command_checks() {
    let mut transport = MockTransport::default();

    let mut pid = vec![0u8; 64];
    pid[0] = 0x02;
    pid[1] = 0x05;
    pid[4] = 0xC1;
    pid[22] = 0x09;
    pid[23] = 0x60;
    transport.push_read_data(pid);

    let mut rr = vec![0u8; 64];
    rr[0] = 0x02;
    rr[1] = 0x04;
    rr[5] = 0x01;
    transport.push_read_data(rr);

    let mut mode = vec![0u8; 64];
    mode[0] = 0x02;
    mode[1] = 0x05;
    mode[5] = 2;
    transport.push_read_data(mode);

    let mut ver = vec![0u8; 64];
    ver[0] = 0x02;
    ver[1] = 0x22;
    ver[2] = 0x2A;
    ver[3] = 0x00;
    ver[4] = 1;
    transport.push_read_data(ver);

    let mut super_button = vec![0u8; 64];
    super_button[0] = 0x02;
    super_button[1] = 0x05;
    transport.push_read_data(super_button);

    let mut profile = vec![0u8; 64];
    profile[0] = 0x02;
    profile[1] = 0x05;
    transport.push_read_data(profile);

    let mut session = DeviceSession::new(
        transport,
        VidPid::new(0x2dc8, 24585),
        SessionConfig {
            experimental: true,
            ..Default::default()
        },
    )
    .expect("session init");

    let diag = session.diag_probe();
    assert_eq!(diag.command_checks.len(), 6);
    assert!(diag.command_checks.iter().all(|c| c.ok));
}
