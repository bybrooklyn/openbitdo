use bitdo_proto::ProfileBlob;
use std::fs;
use std::path::PathBuf;

#[test]
fn golden_profile_fixture_roundtrips() {
    let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let path = manifest.join("../../../harness/golden/profile_fixture.bin");
    let fixture = fs::read(path).expect("read fixture");

    let blob = ProfileBlob::from_bytes(&fixture).expect("parse fixture");
    assert_eq!(blob.slot, 2);
    assert_eq!(blob.payload.len(), 16);

    let serialized = blob.to_bytes();
    assert_eq!(serialized, fixture);
}
