package core

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// loggingTransport decorates a protocol.Transport, logging every call
// (including raw bytes as hex) to Config.DebugLog. This is the one choke
// point every real operation's session goes through
// (OpenBitdoCore.transport()), so wrapping here — rather than adding
// logging calls at each of the ~15 session.Close() sites scattered across
// diagnostics/mapping/firmware — covers all of them with one small type.
type loggingTransport struct {
	inner protocol.Transport
	log   func(format string, v ...any)
}

func newLoggingTransport(inner protocol.Transport, log func(format string, v ...any)) *loggingTransport {
	return &loggingTransport{inner: inner, log: log}
}

func (t *loggingTransport) Open(ctx context.Context, target protocol.VidPid) error {
	start := time.Now()
	err := t.inner.Open(ctx, target)
	t.log("open target=%s duration=%s err=%v", target, time.Since(start), err)
	return err
}

func (t *loggingTransport) Close() error {
	err := t.inner.Close()
	t.log("close err=%v", err)
	return err
}

func (t *loggingTransport) Write(data []byte) (int, error) {
	start := time.Now()
	n, err := t.inner.Write(data)
	t.log("write bytes=%s duration=%s n=%d err=%v", hex.EncodeToString(data), time.Since(start), n, err)
	return n, err
}

func (t *loggingTransport) Read(ctx context.Context, length int, timeoutMs uint64) ([]byte, error) {
	start := time.Now()
	buf, err := t.inner.Read(ctx, length, timeoutMs)
	t.log("read length=%d timeout_ms=%d duration=%s bytes=%s err=%v",
		length, timeoutMs, time.Since(start), hex.EncodeToString(buf), err)
	return buf, err
}
