use bitdo_proto::{
    find_command, CommandId, DeviceSession, MockTransport, SafetyClass, SessionConfig, VidPid,
};

#[test]
fn diag_probe_expands_to_safe_read_commands_and_parsed_facts() {
    let mut transport = MockTransport::default();
    push_diag_success_sequence_for_u2(&mut transport);

    let mut session = DeviceSession::new(
        transport,
        VidPid::new(0x2dc8, 0x6012),
        SessionConfig {
            experimental: true,
            ..Default::default()
        },
    )
    .expect("session init");

    let diag = session.diag_probe();

    assert_eq!(diag.command_checks.len(), 12);
    assert!(diag.transport_ready);
    assert!(diag.command_checks.iter().all(|check| {
        find_command(check.command)
            .map(|row| row.safety_class == SafetyClass::SafeRead)
            .unwrap_or(false)
    }));
    assert!(diag
        .command_checks
        .iter()
        .any(|check| check.command == CommandId::U2GetCurrentSlot));

    let pid_check = diag
        .command_checks
        .iter()
        .find(|check| check.command == CommandId::GetPid)
        .expect("pid check");
    assert_eq!(
        pid_check.parsed_facts.get("detected_pid").copied(),
        Some(0x6012)
    );
    assert_eq!(pid_check.response_status, bitdo_proto::ResponseStatus::Ok);

    let revision_check = diag
        .command_checks
        .iter()
        .find(|check| check.command == CommandId::GetReportRevision)
        .expect("revision check");
    assert_eq!(
        revision_check.parsed_facts.get("revision").copied(),
        Some(1)
    );

    let version_check = diag
        .command_checks
        .iter()
        .find(|check| check.command == CommandId::GetControllerVersion)
        .expect("version check");
    assert_eq!(
        version_check.parsed_facts.get("version_x100").copied(),
        Some(42)
    );
    assert_eq!(version_check.parsed_facts.get("beta").copied(), Some(1));

    let slot_check = diag
        .command_checks
        .iter()
        .find(|check| check.command == CommandId::U2GetCurrentSlot)
        .expect("slot check");
    assert_eq!(slot_check.parsed_facts.get("slot").copied(), Some(2));
}

#[test]
fn diag_probe_get_mode_falls_back_to_get_mode_alt() {
    let mut transport = MockTransport::default();
    push_diag_sequence_with_mode_fallback(&mut transport);

    let mut session = DeviceSession::new(
        transport,
        VidPid::new(0x2dc8, 0x6002),
        SessionConfig {
            experimental: true,
            ..Default::default()
        },
    )
    .expect("session init");

    let diag = session.diag_probe();
    let mode_check = diag
        .command_checks
        .iter()
        .find(|check| check.command == CommandId::GetMode)
        .expect("mode check");

    assert!(mode_check.ok);
    assert_eq!(mode_check.parsed_facts.get("mode").copied(), Some(7));
    assert!(mode_check.detail.contains("GetModeAlt fallback"));
    assert_eq!(mode_check.response_status, bitdo_proto::ResponseStatus::Ok);
}

fn push_diag_success_sequence_for_u2(transport: &mut MockTransport) {
    transport.push_read_data(pid_response(0x6012));
    transport.push_read_data(report_revision_response(1));
    transport.push_read_data(mode_response(2));
    transport.push_read_data(mode_response(2));
    transport.push_read_data(version_response(42, 1));
    transport.push_read_data(ok_read_response());
    transport.push_read_data(idle_response());
    transport.push_read_data(version_response(42, 1));
    transport.push_read_data(ok_read_response());
    transport.push_read_data(slot_response(2));
    transport.push_read_data(ok_read_response());
    transport.push_read_data(ok_read_response());
}

fn push_diag_sequence_with_mode_fallback(transport: &mut MockTransport) {
    transport.push_read_data(pid_response(0x6002));
    transport.push_read_data(report_revision_response(1));
    transport.push_read_data(invalid_mode_response());
    transport.push_read_data(invalid_mode_response());
    transport.push_read_data(invalid_mode_response());
    transport.push_read_data(mode_response(7));
    transport.push_read_data(mode_response(7));
    transport.push_read_data(version_response(99, 0));
    transport.push_read_data(idle_response());
    transport.push_read_data(version_response(99, 0));
    transport.push_read_data(ok_read_response());
}

fn pid_response(pid: u16) -> Vec<u8> {
    let mut response = vec![0u8; 64];
    response[0] = 0x02;
    response[1] = 0x05;
    response[4] = 0xC1;
    response[22] = (pid & 0x00ff) as u8;
    response[23] = (pid >> 8) as u8;
    response
}

fn report_revision_response(revision: u8) -> Vec<u8> {
    let mut response = vec![0u8; 64];
    response[0] = 0x02;
    response[1] = 0x04;
    response[5] = revision;
    response
}

fn mode_response(mode: u8) -> Vec<u8> {
    let mut response = vec![0u8; 64];
    response[0] = 0x02;
    response[1] = 0x05;
    response[5] = mode;
    response
}

fn invalid_mode_response() -> Vec<u8> {
    let mut response = vec![0u8; 64];
    response[0] = 0x00;
    response[1] = 0x00;
    response
}

fn version_response(version_x100: u16, beta: u8) -> Vec<u8> {
    let mut response = vec![0u8; 64];
    response[0] = 0x02;
    response[1] = 0x22;
    let bytes = version_x100.to_le_bytes();
    response[2] = bytes[0];
    response[3] = bytes[1];
    response[4] = beta;
    response
}

fn slot_response(slot: u8) -> Vec<u8> {
    let mut response = vec![0u8; 64];
    response[0] = 0x02;
    response[1] = 0x05;
    response[5] = slot;
    response
}

fn ok_read_response() -> Vec<u8> {
    let mut response = vec![0u8; 64];
    response[0] = 0x02;
    response[1] = 0x05;
    response
}

fn idle_response() -> Vec<u8> {
    let mut response = vec![0u8; 64];
    response[0] = 0x02;
    response
}
