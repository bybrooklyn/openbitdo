package protocol

import "context"

// cfgWith starts from DefaultSessionConfig() and applies mutate — the Go
// equivalent of Rust's `SessionConfig { field: value, ..Default::default() }`,
// since a bare Go struct literal leaves unset fields at their zero value
// (e.g. RetryPolicy.MaxAttempts=0), not at DefaultSessionConfig()'s values.
func cfgWith(mutate func(*SessionConfig)) SessionConfig {
	cfg := DefaultSessionConfig()
	mutate(&cfg)
	return cfg
}

func openSession(t interface {
	Helper()
	Fatalf(string, ...any)
}, transport Transport, pid uint16, config SessionConfig) *DeviceSession {
	t.Helper()
	session, err := NewDeviceSession(context.Background(), transport, VidPid{VID: 0x2dc8, PID: pid}, config)
	if err != nil {
		t.Fatalf("session init: %v", err)
	}
	return session
}

func mustErrCode(t interface {
	Helper()
	Fatalf(string, ...any)
}, err error, code ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", code)
	}
	pe, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if pe.Code() != code {
		t.Fatalf("expected error code %s, got %s (%v)", code, pe.Code(), err)
	}
}

func pidResponse(pid uint16) []byte {
	resp := make([]byte, 64)
	resp[0], resp[1], resp[4] = 0x02, 0x05, 0xC1
	resp[22] = byte(pid)
	resp[23] = byte(pid >> 8)
	return resp
}

func reportRevisionResponse(revision byte) []byte {
	resp := make([]byte, 64)
	resp[0], resp[1], resp[5] = 0x02, 0x04, revision
	return resp
}

func modeResponse(mode byte) []byte {
	resp := make([]byte, 64)
	resp[0], resp[1], resp[5] = 0x02, 0x05, mode
	return resp
}

func invalidModeResponse() []byte {
	resp := make([]byte, 64)
	resp[0], resp[1] = 0x00, 0x00
	return resp
}

func versionResponse(versionX100 uint16, beta byte) []byte {
	resp := make([]byte, 64)
	resp[0], resp[1] = 0x02, 0x22
	resp[2] = byte(versionX100)
	resp[3] = byte(versionX100 >> 8)
	resp[4] = beta
	return resp
}

func slotResponse(slot byte) []byte {
	resp := make([]byte, 64)
	resp[0], resp[1], resp[5] = 0x02, 0x05, slot
	return resp
}

func okReadResponse() []byte {
	resp := make([]byte, 64)
	resp[0], resp[1] = 0x02, 0x05
	return resp
}

func idleResponse() []byte {
	resp := make([]byte, 64)
	resp[0] = 0x02
	return resp
}
