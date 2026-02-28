#![cfg(feature = "hidapi-backend")]

use crate::error::{BitdoError, Result};
use crate::transport::Transport;
use crate::types::VidPid;
use hidapi::{HidApi, HidDevice};

#[derive(Clone, Debug)]
pub struct EnumeratedDevice {
    pub vid_pid: VidPid,
    pub product: Option<String>,
    pub manufacturer: Option<String>,
    pub serial: Option<String>,
    pub path: String,
}

pub fn enumerate_hid_devices() -> Result<Vec<EnumeratedDevice>> {
    let api = HidApi::new().map_err(|e| BitdoError::Transport(e.to_string()))?;
    let mut devices = Vec::new();
    for dev in api.device_list() {
        devices.push(EnumeratedDevice {
            vid_pid: VidPid::new(dev.vendor_id(), dev.product_id()),
            product: dev.product_string().map(ToOwned::to_owned),
            manufacturer: dev.manufacturer_string().map(ToOwned::to_owned),
            serial: dev.serial_number().map(ToOwned::to_owned),
            path: dev.path().to_string_lossy().to_string(),
        });
    }
    Ok(devices)
}

pub struct HidTransport {
    api: Option<HidApi>,
    device: Option<HidDevice>,
    target: Option<VidPid>,
}

impl HidTransport {
    pub fn new() -> Self {
        Self {
            api: None,
            device: None,
            target: None,
        }
    }
}

impl Default for HidTransport {
    fn default() -> Self {
        Self::new()
    }
}

impl Transport for HidTransport {
    fn open(&mut self, vid_pid: VidPid) -> Result<()> {
        let api = HidApi::new().map_err(|e| BitdoError::Transport(e.to_string()))?;
        let device = api
            .open(vid_pid.vid, vid_pid.pid)
            .map_err(|e| BitdoError::Transport(format!("open failed for {}: {}", vid_pid, e)))?;
        self.target = Some(vid_pid);
        self.device = Some(device);
        self.api = Some(api);
        Ok(())
    }

    fn close(&mut self) -> Result<()> {
        self.device = None;
        self.api = None;
        self.target = None;
        Ok(())
    }

    fn write(&mut self, data: &[u8]) -> Result<usize> {
        let device = self
            .device
            .as_ref()
            .ok_or_else(|| BitdoError::Transport("HID transport not open".to_owned()))?;
        device
            .write(data)
            .map_err(|e| BitdoError::Transport(e.to_string()))
    }

    fn read(&mut self, len: usize, timeout_ms: u64) -> Result<Vec<u8>> {
        let device = self
            .device
            .as_ref()
            .ok_or_else(|| BitdoError::Transport("HID transport not open".to_owned()))?;
        let mut buf = vec![0u8; len];
        let read = device
            .read_timeout(&mut buf, timeout_ms as i32)
            .map_err(|e| BitdoError::Transport(e.to_string()))?;
        if read == 0 {
            return Err(BitdoError::Timeout);
        }
        buf.truncate(read);
        Ok(buf)
    }

    fn write_feature(&mut self, data: &[u8]) -> Result<usize> {
        let device = self
            .device
            .as_ref()
            .ok_or_else(|| BitdoError::Transport("HID transport not open".to_owned()))?;
        device
            .send_feature_report(data)
            .map_err(|e| BitdoError::Transport(e.to_string()))?;
        Ok(data.len())
    }

    fn read_feature(&mut self, len: usize) -> Result<Vec<u8>> {
        let device = self
            .device
            .as_ref()
            .ok_or_else(|| BitdoError::Transport("HID transport not open".to_owned()))?;
        let mut buf = vec![0u8; len];
        let read = device
            .get_feature_report(&mut buf)
            .map_err(|e| BitdoError::Transport(e.to_string()))?;
        if read == 0 {
            return Err(BitdoError::Timeout);
        }
        buf.truncate(read);
        Ok(buf)
    }
}
