//go:build manual

package core

import (
	"context"
	"testing"
	"time"
)

// TestManualKeyboardReadProfile reads a connected Retro 108's whole profile.
// It writes nothing.
func TestManualKeyboardReadProfile(t *testing.T) {
	c := New(Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	profile, err := c.KeyboardReadProfile(ctx, retro108Target)
	if err != nil {
		t.Skipf("no readable Retro 108: %v", err)
	}
	t.Logf("profile %q, volume %d, locks %+v", profile.Name, profile.Volume, profile.Locks)
	for id, target := range profile.Mappings {
		name := "?"
		if key, ok := KeyboardKeyByID(id); ok {
			name = key.Name
		}
		t.Logf("  %-10s (%d) -> %s", name, id, target)
	}
	if profile.Volume < 1 || profile.Volume > 5 {
		t.Fatalf("volume %d is outside 1-5", profile.Volume)
	}
}
