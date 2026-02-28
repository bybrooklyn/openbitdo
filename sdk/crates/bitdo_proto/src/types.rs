use serde::{Deserialize, Serialize};
use std::fmt::{Display, Formatter};
use std::str::FromStr;

#[derive(Clone, Copy, Debug, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub struct VidPid {
    pub vid: u16,
    pub pid: u16,
}

impl VidPid {
    pub const fn new(vid: u16, pid: u16) -> Self {
        Self { vid, pid }
    }
}

impl Display for VidPid {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(f, "{:04x}:{:04x}", self.vid, self.pid)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum ProtocolFamily {
    Standard64,
    JpHandshake,
    DInput,
    DS4Boot,
    Unknown,
}

impl FromStr for ProtocolFamily {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "Standard64" => Ok(Self::Standard64),
            "JpHandshake" => Ok(Self::JpHandshake),
            "DInput" => Ok(Self::DInput),
            "DS4Boot" => Ok(Self::DS4Boot),
            "Unknown" => Ok(Self::Unknown),
            _ => Err(format!("unsupported protocol family: {s}")),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum SupportLevel {
    Full,
    DetectOnly,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum SafetyClass {
    SafeRead,
    SafeWrite,
    UnsafeBoot,
    UnsafeFirmware,
}

impl SafetyClass {
    pub fn is_unsafe(self) -> bool {
        matches!(self, Self::UnsafeBoot | Self::UnsafeFirmware)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum CommandConfidence {
    Confirmed,
    Inferred,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum SupportEvidence {
    Confirmed,
    Inferred,
    Untested,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct PidCapability {
    pub supports_mode: bool,
    pub supports_profile_rw: bool,
    pub supports_boot: bool,
    pub supports_firmware: bool,
}

impl PidCapability {
    pub const fn full() -> Self {
        Self {
            supports_mode: true,
            supports_profile_rw: true,
            supports_boot: true,
            supports_firmware: true,
        }
    }

    pub const fn identify_only() -> Self {
        Self {
            supports_mode: false,
            supports_profile_rw: false,
            supports_boot: false,
            supports_firmware: false,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct DeviceProfile {
    pub vid_pid: VidPid,
    pub name: String,
    pub support_level: SupportLevel,
    pub protocol_family: ProtocolFamily,
    pub capability: PidCapability,
    pub evidence: SupportEvidence,
}
