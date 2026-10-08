package core

import (
	"context"
	"errors"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// diagCacheKey identifies one physical device for diagnostic-result
// caching. VidPid alone isn't enough to distinguish two identical
// controllers connected at once, so this matches AppDevice's own identity
// (VidPid + Serial) rather than DiagProbe's narrower VidPid-only targeting.
type diagCacheKey struct {
	vidPid protocol.VidPid
	serial string
}

func diagCacheKeyFor(device AppDevice) diagCacheKey {
	return diagCacheKey{vidPid: device.VidPid, serial: device.Serial}
}

// DiagCacheEntry is one session-cached diagnostic result, plus when it was
// produced.
type DiagCacheEntry struct {
	Result protocol.DiagProbeResult
	RanAt  time.Time
	// Err is set when the probe could not run at all (the device could not
	// be opened). It is cached like a result: the failure is what is known
	// about the device until the next probe.
	Err error
}

// Age reports how long ago this entry's diagnostic run completed --
// intended for a "last run: Xs ago" staleness indicator.
func (e DiagCacheEntry) Age() time.Duration { return time.Since(e.RanAt) }

// CachedDiag returns device's most recent diagnostic result from this
// session without running a new probe. ok is false if device has never
// been diagnosed this session.
func (c *OpenBitdoCore) CachedDiag(device AppDevice) (entry DiagCacheEntry, ok bool) {
	c.diagCacheMu.RLock()
	defer c.diagCacheMu.RUnlock()
	entry, ok = c.diagCache[diagCacheKeyFor(device)]
	return entry, ok
}

// HasDiagnosed reports whether device has a cached diagnostic result from
// this session.
func (c *OpenBitdoCore) HasDiagnosed(device AppDevice) bool {
	_, ok := c.CachedDiag(device)
	return ok
}

// DiagProbeCached returns device's cached diagnostic result if this session
// already has one, or runs DiagProbe and caches the result if not. This is
// an opt-in layer over DiagProbe -- DiagProbe itself is unchanged and still
// always runs a fresh probe, exactly as its existing callers expect. Use
// DiagProbeFresh to force a new run regardless of what's cached.
func (c *OpenBitdoCore) DiagProbeCached(ctx context.Context, device AppDevice) (DiagCacheEntry, error) {
	if entry, ok := c.CachedDiag(device); ok {
		return entry, entry.Err
	}
	return c.DiagProbeFresh(ctx, device)
}

// DiagProbeFresh runs a new diagnostic probe against device, unconditionally
// replacing any cached result for it.
func (c *OpenBitdoCore) DiagProbeFresh(ctx context.Context, device AppDevice) (DiagCacheEntry, error) {
	result, err := c.DiagProbe(ctx, device.VidPid)
	if err != nil && ctx.Err() != nil {
		// Shutting down, not a fact about the device.
		return DiagCacheEntry{}, err
	}
	entry := DiagCacheEntry{Result: result, RanAt: time.Now(), Err: err}
	c.diagCacheMu.Lock()
	c.diagCache[diagCacheKeyFor(device)] = entry
	c.diagCacheMu.Unlock()
	return entry, err
}

// HealthState is what is currently known about whether a connected device
// can actually be talked to, as opposed to its listed support tier.
type HealthState int

const (
	// HealthUnknown: no probe has finished yet.
	HealthUnknown HealthState = iota
	// HealthResponding: at least one diagnostic read was answered.
	HealthResponding
	// HealthSilent: the device opened but answered nothing.
	HealthSilent
	// HealthNoChannel: no configuration interface to talk through.
	HealthNoChannel
	// HealthNoPermission: the OS refused access to the device.
	HealthNoPermission
	// HealthDisconnected: the device went away.
	HealthDisconnected
	// HealthError: the probe failed for another reason.
	HealthError
)

// DeviceHealth is a device's live state: what the last probe found.
type DeviceHealth struct {
	State HealthState
	// Answered and Total count diagnostic reads; both zero unless a probe ran.
	Answered, Total int
	Err             error
}

// Reachable reports whether configuration commands can currently reach the
// device. Unknown counts as reachable: nothing has ruled it out yet.
func (h DeviceHealth) Reachable() bool {
	return h.State == HealthUnknown || h.State == HealthResponding
}

// Health reports device's live state from what enumeration saw and the last
// diagnostic probe this session, without touching the device.
func (c *OpenBitdoCore) Health(device AppDevice) DeviceHealth {
	entry, probed := c.CachedDiag(device)
	if probed && entry.Err != nil {
		health := DeviceHealth{State: HealthError, Err: entry.Err}
		var coreErr *Error
		if errors.As(entry.Err, &coreErr) {
			switch coreErr.Kind {
			case KindPermissionDenied:
				health.State = HealthNoPermission
			case KindNoConfigChannel:
				health.State = HealthNoChannel
			case KindDeviceDisconnected:
				health.State = HealthDisconnected
			}
		}
		return health
	}
	if device.ConfigChannel == ChannelAbsent {
		return DeviceHealth{State: HealthNoChannel}
	}
	if !probed {
		return DeviceHealth{State: HealthUnknown}
	}
	health := DeviceHealth{State: HealthSilent, Total: len(entry.Result.CommandChecks)}
	for _, check := range entry.Result.CommandChecks {
		if check.OK {
			health.Answered++
		}
	}
	if health.Answered > 0 {
		health.State = HealthResponding
	}
	return health
}

// ForgetFailedDiags drops every cached probe that could not run, so the next
// probe retries those devices. Call it when the cause may have changed: the
// user asked for a rescan, or a device was plugged in or removed.
func (c *OpenBitdoCore) ForgetFailedDiags() {
	c.diagCacheMu.Lock()
	defer c.diagCacheMu.Unlock()
	for key, entry := range c.diagCache {
		if entry.Err != nil {
			delete(c.diagCache, key)
		}
	}
}
