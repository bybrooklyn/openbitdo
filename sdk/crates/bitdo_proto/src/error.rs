use crate::command::CommandId;
use crate::types::VidPid;
use serde::{Deserialize, Serialize};
use thiserror::Error;

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum BitdoErrorCode {
    Transport,
    Timeout,
    InvalidResponse,
    MalformedResponse,
    UnsupportedForPid,
    ExperimentalRequired,
    UnsafeCommandDenied,
    UnknownPid,
    InvalidInput,
    UnknownCommand,
    DeviceNotOpen,
}

#[derive(Debug, Error)]
pub enum BitdoError {
    #[error("transport error: {0}")]
    Transport(String),
    #[error("timeout while waiting for device response")]
    Timeout,
    #[error("invalid response for {command:?}: {reason}")]
    InvalidResponse { command: CommandId, reason: String },
    #[error("malformed response for {command:?}: len={len}")]
    MalformedResponse { command: CommandId, len: usize },
    #[error("unsupported command {command:?} for PID {pid:#06x}")]
    UnsupportedForPid { command: CommandId, pid: u16 },
    #[error("inferred command {command:?} requires --experimental")]
    ExperimentalRequired { command: CommandId },
    #[error("unsafe command {command:?} requires --unsafe and --i-understand-brick-risk")]
    UnsafeCommandDenied { command: CommandId },
    #[error("unknown PID {0:#06x}")]
    UnknownPid(u16),
    #[error("invalid input: {0}")]
    InvalidInput(String),
    #[error("command definition not found: {0:?}")]
    UnknownCommand(CommandId),
    #[error("device not open for {0}")]
    DeviceNotOpen(VidPid),
}

impl BitdoError {
    pub fn code(&self) -> BitdoErrorCode {
        match self {
            BitdoError::Transport(_) => BitdoErrorCode::Transport,
            BitdoError::Timeout => BitdoErrorCode::Timeout,
            BitdoError::InvalidResponse { .. } => BitdoErrorCode::InvalidResponse,
            BitdoError::MalformedResponse { .. } => BitdoErrorCode::MalformedResponse,
            BitdoError::UnsupportedForPid { .. } => BitdoErrorCode::UnsupportedForPid,
            BitdoError::ExperimentalRequired { .. } => BitdoErrorCode::ExperimentalRequired,
            BitdoError::UnsafeCommandDenied { .. } => BitdoErrorCode::UnsafeCommandDenied,
            BitdoError::UnknownPid(_) => BitdoErrorCode::UnknownPid,
            BitdoError::InvalidInput(_) => BitdoErrorCode::InvalidInput,
            BitdoError::UnknownCommand(_) => BitdoErrorCode::UnknownCommand,
            BitdoError::DeviceNotOpen(_) => BitdoErrorCode::DeviceNotOpen,
        }
    }
}

pub type Result<T> = std::result::Result<T, BitdoError>;
