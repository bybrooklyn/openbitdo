package core

import (
	"crypto/rand"
	"encoding/hex"
)

// newID returns a random 128-bit hex identifier, used for backup and
// firmware-session IDs in place of Rust's Uuid::new_v4() — a full UUID
// dependency isn't needed for an opaque local identifier.
func newID() string {
	var buf [16]byte
	// crypto/rand.Read never returns a short read or a non-nil error on any
	// platform Go supports; the error return exists only for the io.Reader
	// interface's sake.
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
