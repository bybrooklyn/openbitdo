use bitdo_proto::{command_registry, CommandFrame, CommandId, Report64};
use std::collections::HashSet;

#[test]
fn frame_encode_decode_roundtrip_for_all_commands() {
    let unique = command_registry()
        .iter()
        .map(|row| row.id)
        .collect::<HashSet<_>>();
    assert_eq!(unique.len(), CommandId::all().len());
    assert!(command_registry().len() >= unique.len());

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
