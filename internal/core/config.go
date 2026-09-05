// Package core orchestrates device discovery, diagnostics, JP108/Ultimate2
// mapping, the candidate-readonly write probe, and firmware updates on top
// of internal/protocol. Ported from sdk/crates/bitdo_app_core.
package core

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"log"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Config controls OpenBitdoCore's mode and defaults.
type Config struct {
	MockMode           bool
	AdvancedMode       bool
	DefaultChunkSize   int
	ProgressIntervalMs uint64

	// FirmwareUpdatesEnabled is an internal runtime feature gate. Production
	// builds for v0.0.3 leave it false and expose no CLI override. Tests that
	// exercise downloads must opt in explicitly and inject a local feed plus
	// ephemeral trusted keys below.
	FirmwareUpdatesEnabled bool
	FirmwareManifestURL    string
	FirmwareTrustedKeys    []ed25519.PublicKey
	// DebugLog, when set, makes every real device Open/Write/Read call
	// (across diagnostics, mapping, and firmware — every operation flows
	// through OpenBitdoCore.transport()) log its outcome, including raw
	// bytes, for troubleshooting a hard bug after the fact. Off (nil) by
	// default; never touched in mock mode.
	DebugLog *log.Logger
}

// DefaultConfig returns the safe production defaults for v0.0.3.
func DefaultConfig() Config {
	return Config{
		DefaultChunkSize:   56,
		ProgressIntervalMs: 25,
	}
}
