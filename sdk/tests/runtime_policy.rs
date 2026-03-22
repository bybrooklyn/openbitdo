use bitdo_proto::{
    find_command, BitdoError, CommandId, CommandRuntimePolicy, DeviceSession, DiagSeverity,
    EvidenceConfidence, MockTransport, ResponseStatus, SessionConfig, VidPid,
};

#[test]
fn inferred_safe_read_requires_experimental_mode() {
    let row = find_command(CommandId::GetSuperButton).expect("command present");
    assert_eq!(row.runtime_policy(), CommandRuntimePolicy::ExperimentalGate);

    let mut session = DeviceSession::new(
        MockTransport::default(),
        VidPid::new(0x2dc8, 0x6012),
        SessionConfig::default(),
    )
    .expect("session opens");

    let err = session
        .send_command(CommandId::GetSuperButton, None)
        .expect_err("experimental gate must deny inferred safe-read by default");
    assert!(matches!(err, BitdoError::ExperimentalRequired { .. }));
}

#[test]
fn inferred_write_is_blocked_until_confirmed() {
    let row = find_command(CommandId::WriteProfile).expect("command present");
    assert_eq!(
        row.runtime_policy(),
        CommandRuntimePolicy::BlockedUntilConfirmed
    );

    let mut session = DeviceSession::new(
        MockTransport::default(),
        VidPid::new(0x2dc8, 0x6012),
        SessionConfig {
            experimental: true,
            ..Default::default()
        },
    )
    .expect("session opens");

    let err = session
        .send_command(CommandId::WriteProfile, Some(&[1, 2, 3]))
        .expect_err("inferred writes remain blocked even in experimental mode");
    assert!(matches!(err, BitdoError::UnsupportedForPid { .. }));
}

#[test]
fn confirmed_read_remains_enabled_default() {
    let row = find_command(CommandId::GetPid).expect("command present");
    assert_eq!(row.runtime_policy(), CommandRuntimePolicy::EnabledDefault);
}

#[test]
fn diag_probe_marks_inferred_reads_as_experimental() {
    let mut session = DeviceSession::new(
        MockTransport::default(),
        VidPid::new(0x2dc8, 0x6012),
        SessionConfig {
            experimental: true,
            ..Default::default()
        },
    )
    .expect("session opens");

    let diag = session.diag_probe();
    let inferred = diag
        .command_checks
        .iter()
        .find(|c| c.command == CommandId::GetSuperButton)
        .expect("inferred check present");
    assert!(inferred.is_experimental);
    assert_eq!(inferred.confidence, EvidenceConfidence::Inferred);
    assert!(inferred.attempts >= 1);
    assert_eq!(inferred.response_status, ResponseStatus::Malformed);
    assert!(inferred.bytes_written > 0);
    assert!(matches!(
        inferred.severity,
        DiagSeverity::Ok | DiagSeverity::Warning | DiagSeverity::NeedsAttention
    ));
}

#[test]
fn full_support_pid_scoped_commands_work_without_experimental_mode() {
    let mut transport = MockTransport::default();
    transport.push_read_data(vec![0x02, 0x05, 0x00, 0x00, 0x00, 0x02]);
    transport.push_read_data(vec![0x02, 0x00]);

    let mut session = DeviceSession::new(
        transport,
        VidPid::new(0x2dc8, 0x6012),
        SessionConfig::default(),
    )
    .expect("session opens");

    let slot = session
        .u2_get_current_slot()
        .expect("pid-scoped read should be available");
    assert_eq!(slot, 2);

    let mode = session
        .u2_set_mode(3)
        .expect("pid-scoped write should be available");
    assert_eq!(mode.mode, 3);
}
