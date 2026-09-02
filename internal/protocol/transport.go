package protocol

import "context"

// Transport is the wire-level HID connection a DeviceSession drives. Only
// Open/Close/Write/Read are ported: the Rust Transport trait also declared
// WriteFeature/ReadFeature, but nothing in the original implementation ever
// called them (verified across the whole workspace) — so this Go interface
// drops that dead surface rather than carrying it forward.
type Transport interface {
	Open(ctx context.Context, target VidPid) error
	Close() error
	Write(data []byte) (int, error)
	Read(ctx context.Context, length int, timeoutMs uint64) ([]byte, error)
}
