package protocol

import (
	"encoding/csv"
	"os"
	"testing"
)

// Regression guards for the spec-generated registries: catches drift between
// spec/*.csv and the generated tables without needing to re-run go generate.
// (The generator itself already fails loudly on unrecognized values and
// duplicate PIDs at generation time — this is defense in depth.)

func csvRowCount(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }() // read-only fd; close error is not actionable
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return len(rows) - 1 // minus header
}

func TestPIDRegistryMatchesSpecRowCount(t *testing.T) {
	want := csvRowCount(t, "../../spec/pid_matrix.csv")
	if len(PIDRegistry) != want {
		t.Fatalf("PIDRegistry has %d rows, spec/pid_matrix.csv has %d", len(PIDRegistry), want)
	}
}

func TestCommandRegistryMatchesSpecRowCount(t *testing.T) {
	want := csvRowCount(t, "../../spec/command_matrix.csv")
	if len(CommandRegistry) != want {
		t.Fatalf("CommandRegistry has %d rows, spec/command_matrix.csv has %d", len(CommandRegistry), want)
	}
}

func TestPIDRegistryHasNoDuplicates(t *testing.T) {
	seen := map[uint16]bool{}
	for _, row := range PIDRegistry {
		if seen[row.Pid] {
			t.Errorf("duplicate pid %#04x (%s)", row.Pid, row.Name)
		}
		seen[row.Pid] = true
	}
}

func TestUnknownPidResolvesToDetectOnly(t *testing.T) {
	profile := DeviceProfileFor(VidPid{VID: 0x2dc8, PID: 0xffff})
	if profile.SupportTier != TierDetectOnly {
		t.Fatalf("expected detect-only for unknown pid, got %s", profile.SupportTier)
	}
	if profile.Evidence != EvidenceUntested {
		t.Fatalf("expected untested evidence, got %s", profile.Evidence)
	}
}
