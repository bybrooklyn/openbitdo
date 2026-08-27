package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

func TestHasDiagnosedFalseUntilFirstProbe(t *testing.T) {
	c := New(Config{MockMode: true})
	device := mockDevice(protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, true)

	if c.HasDiagnosed(device) {
		t.Fatal("expected HasDiagnosed to be false before any probe has run")
	}
	if _, ok := c.CachedDiag(device); ok {
		t.Fatal("expected CachedDiag to report no entry before any probe has run")
	}

	if _, err := c.DiagProbeFresh(context.Background(), device); err != nil {
		t.Fatalf("DiagProbeFresh: %v", err)
	}

	if !c.HasDiagnosed(device) {
		t.Fatal("expected HasDiagnosed to be true after a probe has run")
	}
}

func TestDiagProbeCachedDoesNotRerunOnHit(t *testing.T) {
	c := New(Config{MockMode: true})
	device := mockDevice(protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, true)

	first, err := c.DiagProbeCached(context.Background(), device)
	if err != nil {
		t.Fatalf("first DiagProbeCached: %v", err)
	}

	time.Sleep(2 * time.Millisecond) // guarantee a distinguishable RanAt if this rerun (it shouldn't)

	second, err := c.DiagProbeCached(context.Background(), device)
	if err != nil {
		t.Fatalf("second DiagProbeCached: %v", err)
	}

	if !second.RanAt.Equal(first.RanAt) {
		t.Fatalf("expected the second cached call to reuse the same result (RanAt %v), got a fresh RanAt %v",
			first.RanAt, second.RanAt)
	}
}

func TestDiagProbeFreshAlwaysReplacesCacheEntry(t *testing.T) {
	c := New(Config{MockMode: true})
	device := mockDevice(protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, true)

	first, err := c.DiagProbeFresh(context.Background(), device)
	if err != nil {
		t.Fatalf("first DiagProbeFresh: %v", err)
	}

	time.Sleep(2 * time.Millisecond)

	second, err := c.DiagProbeFresh(context.Background(), device)
	if err != nil {
		t.Fatalf("second DiagProbeFresh: %v", err)
	}

	if !second.RanAt.After(first.RanAt) {
		t.Fatalf("expected DiagProbeFresh to always run a new probe with a later RanAt, got first=%v second=%v",
			first.RanAt, second.RanAt)
	}

	cached, ok := c.CachedDiag(device)
	if !ok {
		t.Fatal("expected a cache entry after DiagProbeFresh")
	}
	if !cached.RanAt.Equal(second.RanAt) {
		t.Fatalf("expected the cache to hold the most recent run (%v), got %v", second.RanAt, cached.RanAt)
	}
}

func TestDiagCacheDistinguishesDevicesBySerial(t *testing.T) {
	c := New(Config{MockMode: true})
	vidPid := protocol.VidPid{VID: 0x2dc8, PID: 0x6012}
	deviceA := mockDevice(vidPid, true)
	deviceA.Serial = "SERIAL-A"
	deviceB := mockDevice(vidPid, true)
	deviceB.Serial = "SERIAL-B"

	if _, err := c.DiagProbeFresh(context.Background(), deviceA); err != nil {
		t.Fatalf("DiagProbeFresh(deviceA): %v", err)
	}

	if c.HasDiagnosed(deviceB) {
		t.Fatal("expected a same-VidPid, different-serial device to be unaffected by deviceA's cache entry")
	}
	if !c.HasDiagnosed(deviceA) {
		t.Fatal("expected deviceA to still be cached")
	}
}

func TestDiagCacheEntryAgeReflectsElapsedTime(t *testing.T) {
	c := New(Config{MockMode: true})
	device := mockDevice(protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, true)

	entry, err := c.DiagProbeFresh(context.Background(), device)
	if err != nil {
		t.Fatalf("DiagProbeFresh: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	if entry.Age() < 5*time.Millisecond {
		t.Fatalf("expected Age() to reflect at least 5ms elapsed, got %v", entry.Age())
	}
}

// TestDiagCacheConcurrentAccessIsRaceFree hammers CachedDiag/DiagProbeCached/
// DiagProbeFresh/HasDiagnosed from many goroutines against a small pool of
// devices at once -- meant to be run with -race, where it's the actual
// assertion: reads from the TUI's event loop racing writes from async
// diagnostic completions is exactly the shape production usage will take.
func TestDiagCacheConcurrentAccessIsRaceFree(t *testing.T) {
	c := New(Config{MockMode: true})
	devices := []AppDevice{
		mockDevice(protocol.VidPid{VID: 0x2dc8, PID: 0x6012}, true),
		mockDevice(protocol.VidPid{VID: 0x2dc8, PID: 0x5209}, true),
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		device := devices[i%len(devices)]
		wg.Add(4)
		go func() {
			defer wg.Done()
			_, _ = c.CachedDiag(device)
		}()
		go func() {
			defer wg.Done()
			_ = c.HasDiagnosed(device)
		}()
		go func() {
			defer wg.Done()
			_, _ = c.DiagProbeCached(context.Background(), device)
		}()
		go func() {
			defer wg.Done()
			_, _ = c.DiagProbeFresh(context.Background(), device)
		}()
	}
	wg.Wait()

	for _, device := range devices {
		if !c.HasDiagnosed(device) {
			t.Fatalf("expected %s to be diagnosed after concurrent probes", device.VidPid)
		}
	}
}
