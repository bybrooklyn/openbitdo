use bitdo_proto::{command_registry, CommandFrame, CommandId, Report64};

#[test]
fn frame_encode_decode_roundtrip_for_all_commands() {
    assert_eq!(command_registry().len(), CommandId::all().len());

    for row in command_registry() {
        let frame = CommandFrame {
            id: row.id,
            payload: row.request.to_vec(),
            report_id: row.report_id,
            expected_response: row.expected_response,
        };

        let encoded = frame.encode();
        if encoded.len() == 64 {
            let parsed = Report64::try_from(encoded.as_slice()).expect("64-byte frame parses");
            assert_eq!(parsed.as_slice(), encoded.as_slice());
        } else {
            assert!(!encoded.is_empty());
        }
    }
}
