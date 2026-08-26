package core

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// OpenBitdoCore is the façade every UI (mock or real) drives: device
// discovery, diagnostics, JP108/Ultimate2 mapping, the candidate-readonly
// write probe, and firmware updates.
type OpenBitdoCore struct {
	config       Config
	advancedMode atomic.Bool

	sessionsMu sync.RWMutex
	sessions   map[FirmwareUpdateSessionID]*firmwareSessionHandle

	backupsMu sync.RWMutex
	backups   map[ConfigBackupID]configBackup

	http *http.Client

	// transportOverride lets tests inject a protocol.MockTransport in place
	// of real HID access, without needing physical hardware. Unset in normal
	// use, where transport() falls back to protocol.NewHidTransport().
	transportOverride protocol.Transport
}

func (c *OpenBitdoCore) transport() protocol.Transport {
	if c.transportOverride != nil {
		return c.transportOverride
	}
	return protocol.NewHidTransport()
}

// New constructs an OpenBitdoCore from config.
func New(config Config) *OpenBitdoCore {
	c := &OpenBitdoCore{
		config:   config,
		sessions: make(map[FirmwareUpdateSessionID]*firmwareSessionHandle),
		backups:  make(map[ConfigBackupID]configBackup),
		http:     &http.Client{},
	}
	c.advancedMode.Store(config.AdvancedMode)
	return c
}

// SetAdvancedMode toggles advanced mode. Advanced mode enables inferred
// SafeRead commands only — write/unsafe inferred commands stay blocked by
// runtime policy regardless.
func (c *OpenBitdoCore) SetAdvancedMode(enabled bool) { c.advancedMode.Store(enabled) }

// AdvancedMode reports whether advanced mode is enabled.
func (c *OpenBitdoCore) AdvancedMode() bool { return c.advancedMode.Load() }

// ListDevices enumerates connected 8BitDo devices (or fixed mock devices in
// mock mode).
func (c *OpenBitdoCore) ListDevices(ctx context.Context) ([]AppDevice, error) {
	if c.config.MockMode {
		return []AppDevice{
			mockDevice(protocol.VidPid{VID: 0x2dc8, PID: 0x5209}, true),
			mockDevice(protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, true),
			mockDevice(protocol.VidPid{VID: 0x2dc8, PID: 0x2100}, false),
		}, nil
	}

	devices := protocol.EnumerateHIDDevices()
	out := make([]AppDevice, 0, len(devices))
	for _, d := range devices {
		if d.VidPid.VID != 0x2dc8 {
			continue
		}
		p := protocol.DeviceProfileFor(d.VidPid)
		out = append(out, AppDevice{
			VidPid: d.VidPid, Name: p.Name, SupportLevel: p.SupportLevel, SupportTier: p.SupportTier,
			ProtocolFamily: p.ProtocolFamily, Capability: p.Capability, Evidence: p.Evidence,
			Serial: d.Serial, Connected: true,
		})
	}
	return out, nil
}

// DiagProbe runs every applicable safe-read diagnostic check against target.
func (c *OpenBitdoCore) DiagProbe(ctx context.Context, target protocol.VidPid) (protocol.DiagProbeResult, error) {
	if c.config.MockMode {
		return mockDiagProbe(target), nil
	}

	// Diagnostics always execute inferred SafeRead checks; those checks are
	// explicitly marked experimental in their result metadata so users can
	// distinguish confidence levels.
	session, err := protocol.NewDeviceSession(ctx, c.transport(), target,
		protocol.SessionConfig{Experimental: true, RetryPolicy: protocol.DefaultRetryPolicy(), TimeoutProfile: protocol.DefaultTimeoutProfile(), TraceEnabled: true})
	if err != nil {
		return protocol.DiagProbeResult{}, errProtocol(err)
	}
	defer func() { _ = session.Close() }() // best-effort; diagnostics already have their result
	return session.DiagProbe(ctx), nil
}

// BeginnerDiagSummary produces a human-readable status string for a
// completed diagnostic probe.
func (c *OpenBitdoCore) BeginnerDiagSummary(device AppDevice, diag protocol.DiagProbeResult) string {
	passed, total := 0, len(diag.CommandChecks)
	confirmedTotal, confirmedOK := 0, 0
	issueTotal, experimentalTotal, experimentalOK, needsAttention := 0, 0, 0, 0
	for _, c := range diag.CommandChecks {
		if c.OK {
			passed++
		}
		if c.Confidence == protocol.EvidenceConfirmed {
			confirmedTotal++
			if c.OK {
				confirmedOK++
			}
		}
		if !c.OK || c.Severity != protocol.SeverityOK {
			issueTotal++
		}
		if c.IsExperimental {
			experimentalTotal++
			if c.OK {
				experimentalOK++
			}
		}
		if c.Severity == protocol.SeverityNeedsAttention {
			needsAttention++
		}
	}

	familyHint := map[protocol.ProtocolFamily]string{
		protocol.Standard64:    "Standard64 diagnostics are available. Read checks are safe while writes stay blocked until hardware confirmation.",
		protocol.JpHandshake:   "JP-handshake diagnostics are available. Handshake/version checks are the safe default path.",
		protocol.DInput:        "DInput diagnostics are available. Read checks are safe; write paths remain policy-gated.",
		protocol.DS4Boot:       "Boot-mode diagnostics are limited. Keep the device in normal mode for beginner-safe checks.",
		protocol.FamilyUnknown: "Only basic identify diagnostics are available for unknown protocol family devices.",
	}[device.ProtocolFamily]

	statusHint := "Issues: none."
	if issueTotal > 0 {
		statusHint = fmt.Sprintf("Issues: %d total, %d need attention.", issueTotal, needsAttention)
	}
	experimentalHint := fmt.Sprintf("Experimental checks: %d/%d passed.", experimentalOK, experimentalTotal)
	transportHint := "Transport ready: no successful safe-read responses yet."
	if diag.TransportReady {
		transportHint = "Transport ready: yes."
	}
	blockedHint := fmt.Sprintf("Blocked operations: %s.", blockedOperationSummary(device))

	base := fmt.Sprintf("Checks: %d/%d passed. Confirmed checks: %d/%d passed. %s %s %s %s %s",
		passed, total, confirmedOK, confirmedTotal, experimentalHint, statusHint, transportHint, blockedHint, familyHint)

	switch device.SupportTier {
	case protocol.TierFull:
		return base + " This device is full-support."
	case protocol.TierCandidateReadOnly:
		return base + " This device is candidate-readonly: update and mapping stay blocked until runtime + hardware confirmation."
	default:
		return base + " This device is detect-only: use diagnostics only."
	}
}

// GuidedButtonTest returns beginner guidance text for a guided button-test
// walkthrough; always "passes" since it's a UI-driven visual confirmation,
// not a protocol check.
func (c *OpenBitdoCore) GuidedButtonTest(ctx context.Context, kind DeviceKind, expectedInputs []string) (GuidedButtonTestResult, error) {
	guidance := "Press each remapped Ultimate2 core button once and verify it matches the expected action."
	if kind == KindJP108 {
		guidance = "Press each mapped JP108 dedicated key once and verify it matches the on-screen expected input."
	}
	return GuidedButtonTestResult{DeviceKind: kind, ExpectedInputs: expectedInputs, Passed: true, Guidance: guidance}, nil
}

func (c *OpenBitdoCore) openSessionForOps(ctx context.Context, target protocol.VidPid) (*protocol.DeviceSession, error) {
	config := protocol.SessionConfig{
		AllowUnsafe: true, BrickRiskAck: true, Experimental: c.AdvancedMode(),
		RetryPolicy: protocol.DefaultRetryPolicy(), TimeoutProfile: protocol.DefaultTimeoutProfile(), TraceEnabled: true,
	}
	session, err := protocol.NewDeviceSession(ctx, c.transport(), target, config)
	if err != nil {
		return nil, errProtocol(err)
	}
	return session, nil
}

func (c *OpenBitdoCore) storeBackup(target protocol.VidPid, payload configBackupPayload) ConfigBackupID {
	id := ConfigBackupID(newID())
	c.backupsMu.Lock()
	c.backups[id] = configBackup{createdAt: time.Now(), target: target, payload: payload}
	c.backupsMu.Unlock()
	return id
}

func (c *OpenBitdoCore) sessionHandle(id FirmwareUpdateSessionID) (*firmwareSessionHandle, error) {
	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()
	handle, ok := c.sessions[id]
	if !ok {
		return nil, errNotFound("unknown session id: %s", id)
	}
	return handle, nil
}
