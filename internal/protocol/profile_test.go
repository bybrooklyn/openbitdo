package protocol

import (
	"bytes"
	"os"
	"testing"
)

// Ported from sdk/tests/profile_serialization.rs.
func TestGoldenProfileFixtureRoundtrips(t *testing.T) {
	fixture, err := os.ReadFile("../../harness/golden/profile_fixture.bin")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	blob, err := ProfileBlobFromBytes(fixture)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if blob.Slot != 2 {
		t.Fatalf("expected slot 2, got %d", blob.Slot)
	}
	if len(blob.Payload) != 16 {
		t.Fatalf("expected payload len 16, got %d", len(blob.Payload))
	}

	serialized := blob.ToBytes()
	if !bytes.Equal(serialized, fixture) {
		t.Fatalf("roundtrip mismatch:\n got=%x\nwant=%x", serialized, fixture)
	}
}
