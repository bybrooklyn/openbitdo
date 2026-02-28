use crate::command::CommandId;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Report64(pub [u8; 64]);

impl Report64 {
    pub fn as_slice(&self) -> &[u8] {
        &self.0
    }
}

impl TryFrom<&[u8]> for Report64 {
    type Error = String;

    fn try_from(value: &[u8]) -> Result<Self, Self::Error> {
        if value.len() != 64 {
            return Err(format!("expected 64 bytes, got {}", value.len()));
        }
        let mut arr = [0u8; 64];
        arr.copy_from_slice(value);
        Ok(Self(arr))
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VariableReport(pub Vec<u8>);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CommandFrame {
    pub id: CommandId,
    pub payload: Vec<u8>,
    pub report_id: u8,
    pub expected_response: &'static str,
}

impl CommandFrame {
    pub fn encode(&self) -> Vec<u8> {
        self.payload.clone()
    }
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum ResponseStatus {
    Ok,
    Invalid,
    Malformed,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct ResponseFrame {
    pub raw: Vec<u8>,
    pub status: ResponseStatus,
    pub parsed_fields: BTreeMap<String, u32>,
}
