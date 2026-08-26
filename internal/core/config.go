// Package core orchestrates device discovery, diagnostics, JP108/Ultimate2
// mapping, the candidate-readonly write probe, and firmware updates on top
// of internal/protocol. Ported from sdk/crates/bitdo_app_core.
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
)

const defaultManifestURL = "https://github.com/bybrooklyn/openbitdo/releases/latest/download/firmware-manifest.toml"

// Pinned Ed25519 public keys used to verify firmware artifact signatures.
// "Next" is currently identical to "active" — kept separate so a future key
// rotation can pin both during the transition window.
const (
	pinnedEd25519ActivePublicKeyHex = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
	pinnedEd25519NextPublicKeyHex   = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
)

// SigningKeyFingerprintActiveSHA256 returns the SHA-256 fingerprint of the
// active pinned signing key, for display in build/version info.
func SigningKeyFingerprintActiveSHA256() string {
	return signingKeyFingerprintSHA256(pinnedEd25519ActivePublicKeyHex)
}

// SigningKeyFingerprintNextSHA256 returns the SHA-256 fingerprint of the
// next pinned signing key.
func SigningKeyFingerprintNextSHA256() string {
	return signingKeyFingerprintSHA256(pinnedEd25519NextPublicKeyHex)
}

func signingKeyFingerprintSHA256(publicKeyHex string) string {
	raw, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return "unknown"
	}
	return sha256Hex(raw)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Config controls OpenBitdoCore's mode and defaults.
type Config struct {
	MockMode            bool
	AdvancedMode        bool
	DefaultChunkSize    int
	ProgressIntervalMs  uint64
	FirmwareManifestURL string
	// DebugLog, when set, makes every real device Open/Write/Read call
	// (across diagnostics, mapping, and firmware — every operation flows
	// through OpenBitdoCore.transport()) log its outcome, including raw
	// bytes, for troubleshooting a hard bug after the fact. Off (nil) by
	// default; never touched in mock mode.
	DebugLog *log.Logger
}

// DefaultConfig matches the Rust implementation's defaults.
func DefaultConfig() Config {
	return Config{
		DefaultChunkSize:    56,
		ProgressIntervalMs:  25,
		FirmwareManifestURL: defaultManifestURL,
	}
}
