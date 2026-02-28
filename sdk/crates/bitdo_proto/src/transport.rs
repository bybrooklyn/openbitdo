use crate::error::{BitdoError, Result};
use crate::types::VidPid;
use std::collections::VecDeque;

pub trait Transport {
    fn open(&mut self, vid_pid: VidPid) -> Result<()>;
    fn close(&mut self) -> Result<()>;
    fn write(&mut self, data: &[u8]) -> Result<usize>;
    fn read(&mut self, len: usize, timeout_ms: u64) -> Result<Vec<u8>>;
    fn write_feature(&mut self, data: &[u8]) -> Result<usize>;
    fn read_feature(&mut self, len: usize) -> Result<Vec<u8>>;
}

impl<T: Transport + ?Sized> Transport for Box<T> {
    fn open(&mut self, vid_pid: VidPid) -> Result<()> {
        (**self).open(vid_pid)
    }

    fn close(&mut self) -> Result<()> {
        (**self).close()
    }

    fn write(&mut self, data: &[u8]) -> Result<usize> {
        (**self).write(data)
    }

    fn read(&mut self, len: usize, timeout_ms: u64) -> Result<Vec<u8>> {
        (**self).read(len, timeout_ms)
    }

    fn write_feature(&mut self, data: &[u8]) -> Result<usize> {
        (**self).write_feature(data)
    }

    fn read_feature(&mut self, len: usize) -> Result<Vec<u8>> {
        (**self).read_feature(len)
    }
}

#[derive(Clone, Debug)]
pub enum MockReadEvent {
    Data(Vec<u8>),
    Timeout,
    Error(String),
}

#[derive(Clone, Debug, Default)]
pub struct MockTransport {
    opened: Option<VidPid>,
    reads: VecDeque<MockReadEvent>,
    feature_reads: VecDeque<MockReadEvent>,
    writes: Vec<Vec<u8>>,
    feature_writes: Vec<Vec<u8>>,
}

impl MockTransport {
    pub fn push_read_data(&mut self, data: Vec<u8>) {
        self.reads.push_back(MockReadEvent::Data(data));
    }

    pub fn push_read_timeout(&mut self) {
        self.reads.push_back(MockReadEvent::Timeout);
    }

    pub fn push_read_error(&mut self, message: impl Into<String>) {
        self.reads.push_back(MockReadEvent::Error(message.into()));
    }

    pub fn push_feature_read_data(&mut self, data: Vec<u8>) {
        self.feature_reads.push_back(MockReadEvent::Data(data));
    }

    pub fn writes(&self) -> &[Vec<u8>] {
        &self.writes
    }

    pub fn feature_writes(&self) -> &[Vec<u8>] {
        &self.feature_writes
    }
}

impl Transport for MockTransport {
    fn open(&mut self, vid_pid: VidPid) -> Result<()> {
        self.opened = Some(vid_pid);
        Ok(())
    }

    fn close(&mut self) -> Result<()> {
        self.opened = None;
        Ok(())
    }

    fn write(&mut self, data: &[u8]) -> Result<usize> {
        if self.opened.is_none() {
            return Err(BitdoError::Transport("mock transport not open".to_owned()));
        }
        self.writes.push(data.to_vec());
        Ok(data.len())
    }

    fn read(&mut self, _len: usize, _timeout_ms: u64) -> Result<Vec<u8>> {
        match self.reads.pop_front() {
            Some(MockReadEvent::Data(d)) => Ok(d),
            Some(MockReadEvent::Timeout) => Err(BitdoError::Timeout),
            Some(MockReadEvent::Error(msg)) => Err(BitdoError::Transport(msg)),
            None => Err(BitdoError::Timeout),
        }
    }

    fn write_feature(&mut self, data: &[u8]) -> Result<usize> {
        if self.opened.is_none() {
            return Err(BitdoError::Transport("mock transport not open".to_owned()));
        }
        self.feature_writes.push(data.to_vec());
        Ok(data.len())
    }

    fn read_feature(&mut self, _len: usize) -> Result<Vec<u8>> {
        match self.feature_reads.pop_front() {
            Some(MockReadEvent::Data(d)) => Ok(d),
            Some(MockReadEvent::Timeout) => Err(BitdoError::Timeout),
            Some(MockReadEvent::Error(msg)) => Err(BitdoError::Transport(msg)),
            None => Err(BitdoError::Timeout),
        }
    }
}
