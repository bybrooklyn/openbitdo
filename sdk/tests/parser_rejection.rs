use bitdo_proto::{validate_response, CommandId, ResponseStatus};

#[test]
fn malformed_response_is_rejected() {
    let status = validate_response(CommandId::GetPid, &[0x02]);
    assert_eq!(status, ResponseStatus::Malformed);
}

#[test]
fn invalid_signature_is_rejected() {
    let mut bad = vec![0u8; 64];
    bad[0] = 0x00;
    bad[1] = 0x05;
    bad[4] = 0xC1;
    let status = validate_response(CommandId::GetPid, &bad);
    assert_eq!(status, ResponseStatus::Invalid);
}

#[test]
fn valid_signature_is_accepted() {
    let mut good = vec![0u8; 64];
    good[0] = 0x02;
    good[1] = 0x05;
    good[4] = 0xC1;
    good[22] = 0x09;
    good[23] = 0x60;
    let status = validate_response(CommandId::GetPid, &good);
    assert_eq!(status, ResponseStatus::Ok);
}
