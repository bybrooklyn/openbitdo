package protocol

import "context"

// MockReadEventKind distinguishes the three scripted outcomes a queued mock
// read can produce.
type MockReadEventKind int

const (
	MockData MockReadEventKind = iota
	MockTimeout
	MockError
)

// MockReadEvent is one scripted response for MockTransport.Read.
type MockReadEvent struct {
	Kind    MockReadEventKind
	Data    []byte
	ErrText string
}

// MockTransport is a scripted Transport used by tests: queue up reads with
// PushReadData/PushReadTimeout/PushReadError, then drive a DeviceSession
// against it. This is the seam nearly every protocol test in this package
// hangs off, mirroring the Rust implementation's MockTransport.
type MockTransport struct {
	opened *VidPid
	reads  []MockReadEvent
	writes [][]byte
}

// PushReadData queues a successful read returning data.
func (m *MockTransport) PushReadData(data []byte) {
	m.reads = append(m.reads, MockReadEvent{Kind: MockData, Data: data})
}

// PushReadTimeout queues a timeout on the next read.
func (m *MockTransport) PushReadTimeout() {
	m.reads = append(m.reads, MockReadEvent{Kind: MockTimeout})
}

// PushReadError queues a transport error on the next read.
func (m *MockTransport) PushReadError(message string) {
	m.reads = append(m.reads, MockReadEvent{Kind: MockError, ErrText: message})
}

// Writes returns every payload written so far, for assertions.
func (m *MockTransport) Writes() [][]byte { return m.writes }

func (m *MockTransport) Open(_ context.Context, target VidPid) error {
	t := target
	m.opened = &t
	return nil
}

func (m *MockTransport) Close() error {
	m.opened = nil
	return nil
}

func (m *MockTransport) Write(data []byte) (int, error) {
	if m.opened == nil {
		return 0, errTransport("mock transport not open")
	}
	m.writes = append(m.writes, append([]byte(nil), data...))
	return len(data), nil
}

func (m *MockTransport) Read(_ context.Context, _ int, _ uint64) ([]byte, error) {
	if len(m.reads) == 0 {
		return nil, ErrTimeout
	}
	event := m.reads[0]
	m.reads = m.reads[1:]
	switch event.Kind {
	case MockData:
		return event.Data, nil
	case MockTimeout:
		return nil, ErrTimeout
	default:
		return nil, errTransport("%s", event.ErrText)
	}
}
