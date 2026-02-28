mod command;
mod error;
mod frame;
#[cfg(feature = "hidapi-backend")]
mod hid_transport;
mod profile;
mod registry;
mod session;
mod transport;
mod types;

pub use command::{CommandDefinition, CommandId};
pub use error::{BitdoError, BitdoErrorCode, Result};
pub use frame::{CommandFrame, Report64, ResponseFrame, ResponseStatus, VariableReport};
#[cfg(feature = "hidapi-backend")]
pub use hid_transport::{enumerate_hid_devices, EnumeratedDevice, HidTransport};
pub use profile::ProfileBlob;
pub use registry::{
    command_registry, device_profile_for, find_command, find_pid, pid_registry, CommandRegistryRow,
    PidRegistryRow,
};
pub use session::{
    validate_response, CommandExecutionReport, DeviceSession, DiagCommandStatus, DiagProbeResult,
    FirmwareTransferReport, IdentifyResult, ModeState, RetryPolicy, SessionConfig, TimeoutProfile,
};
pub use transport::{MockTransport, Transport};
pub use types::{
    CommandConfidence, DeviceProfile, PidCapability, ProtocolFamily, SafetyClass, SupportEvidence,
    SupportLevel, VidPid,
};
