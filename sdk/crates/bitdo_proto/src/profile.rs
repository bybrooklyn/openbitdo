use crate::error::{BitdoError, Result};
use serde::{Deserialize, Serialize};

const MAGIC: &[u8; 4] = b"BDP1";

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct ProfileBlob {
    pub slot: u8,
    pub payload: Vec<u8>,
}

impl ProfileBlob {
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(4 + 1 + 2 + self.payload.len() + 4);
        out.extend_from_slice(MAGIC);
        out.push(self.slot);
        out.extend_from_slice(&(self.payload.len() as u16).to_le_bytes());
        out.extend_from_slice(&self.payload);
        let checksum = checksum(&out[4..]);
        out.extend_from_slice(&checksum.to_le_bytes());
        out
    }

    pub fn from_bytes(data: &[u8]) -> Result<Self> {
        if data.len() < 11 {
            return Err(BitdoError::InvalidInput(
                "profile blob too short".to_owned(),
            ));
        }
        if &data[0..4] != MAGIC {
            return Err(BitdoError::InvalidInput("invalid profile magic".to_owned()));
        }

        let slot = data[4];
        let len = u16::from_le_bytes([data[5], data[6]]) as usize;
        let payload_end = 7 + len;
        if payload_end + 4 > data.len() {
            return Err(BitdoError::InvalidInput(
                "profile length exceeds blob size".to_owned(),
            ));
        }

        let payload = data[7..payload_end].to_vec();
        let expected = u32::from_le_bytes([
            data[payload_end],
            data[payload_end + 1],
            data[payload_end + 2],
            data[payload_end + 3],
        ]);
        let actual = checksum(&data[4..payload_end]);
        if expected != actual {
            return Err(BitdoError::InvalidInput(format!(
                "checksum mismatch expected={expected:#x} actual={actual:#x}"
            )));
        }

        Ok(Self { slot, payload })
    }
}

fn checksum(data: &[u8]) -> u32 {
    data.iter().fold(0u32, |acc, b| acc.wrapping_add(*b as u32))
}
