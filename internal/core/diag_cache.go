package core

import (
	"context"
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
		return entry, nil
	}
	return c.DiagProbeFresh(ctx, device)
}

// DiagProbeFresh runs a new diagnostic probe against device, unconditionally
// replacing any cached result for it.
func (c *OpenBitdoCore) DiagProbeFresh(ctx context.Context, device AppDevice) (DiagCacheEntry, error) {
	result, err := c.DiagProbe(ctx, device.VidPid)
	if err != nil {
		return DiagCacheEntry{}, err
	}
	entry := DiagCacheEntry{Result: result, RanAt: time.Now()}
	c.diagCacheMu.Lock()
	c.diagCache[diagCacheKeyFor(device)] = entry
	c.diagCacheMu.Unlock()
	return entry, nil
}
