package core

import (
	"context"
	"log"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// recordingTransport is a minimal protocol.Transport for exercising
// loggingTransport's decoration without needing real hardware.
type recordingTransport struct{}

func (recordingTransport) Open(context.Context, protocol.VidPid) error { return nil }
func (recordingTransport) Close() error                                { return nil }
func (recordingTransport) Write(data []byte) (int, error)              { return len(data), nil }
func (recordingTransport) Read(context.Context, int, uint64) ([]byte, error) {
	return []byte{0xAB, 0xCD}, nil
}

func TestLoggingTransportLogsRawBytesForEveryCall(t *testing.T) {
	var sb strings.Builder
	logger := log.New(&sb, "", 0)
	lt := newLoggingTransport(recordingTransport{}, logger.Printf)

	target := protocol.VidPid{VID: 0x2dc8, PID: 0x6009}
	if err := lt.Open(context.Background(), target); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := lt.Write([]byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := lt.Read(context.Background(), 2, 100); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := lt.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	out := sb.String()
	for _, want := range []string{"open ", "010203", "read ", "abcd", "close "} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected log output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestCoreTransportWrapsWithLoggingOnlyWhenConfigured(t *testing.T) {
	plain := New(Config{}).transport()
	if _, ok := plain.(*loggingTransport); ok {
		t.Fatal("expected no logging wrapper when DebugLog is unset")
	}

	logger := log.New(&strings.Builder{}, "", 0)
	c := New(Config{DebugLog: logger})
	c.transportOverride = recordingTransport{}
	wrapped := c.transport()
	if _, ok := wrapped.(*loggingTransport); !ok {
		t.Fatalf("expected transport() to return a *loggingTransport when DebugLog is set, got %T", wrapped)
	}
}
