use bitdo_proto::{BitdoError, BitdoErrorCode};

#[test]
fn bitdo_error_maps_to_stable_codes() {
    let err = BitdoError::InvalidInput("bad".to_owned());
    assert_eq!(err.code(), BitdoErrorCode::InvalidInput);

    let err = BitdoError::Timeout;
    assert_eq!(err.code(), BitdoErrorCode::Timeout);
}
