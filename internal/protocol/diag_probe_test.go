package protocol

import (
	"context"
	"strings"
	"testing"
)

// Ported from sdk/tests/diag_probe.rs.

func TestDiagProbeExpandsToSafeReadCommandsAndParsedFacts(t *testing.T) {
	transport := &MockTransport{}
	transport.PushReadData(pidResponse(0x6012))
	transport.PushReadData(reportRevisionResponse(1))
	transport.PushReadData(modeResponse(2))
	transport.PushReadData(modeResponse(2))
	transport.PushReadData(versionResponse(42, 1))
	transport.PushReadData(okReadResponse())
	transport.PushReadData(idleResponse())
	transport.PushReadData(versionResponse(42, 1))
	transport.PushReadData(okReadResponse())
	transport.PushReadData(slotResponse(2))
	transport.PushReadData(okReadResponse())
	transport.PushReadData(okReadResponse())

	session := openSession(t, transport, 0x6012, cfgWith(func(c *SessionConfig) { c.Experimental = true }))
	diag := session.DiagProbe(context.Background())

	if len(diag.CommandChecks) != 12 {
		t.Fatalf("expected 12 checks, got %d", len(diag.CommandChecks))
	}
	if !diag.TransportReady {
		t.Fatal("expected transport ready")
	}
	for _, check := range diag.CommandChecks {
		row, ok := FindCommand(check.Command)
		if !ok || row.SafetyClass != SafeRead {
			t.Errorf("check %s is not a SafeRead command", check.Command)
		}
	}

	find := func(id CommandID) DiagCommandStatus {
		for _, c := range diag.CommandChecks {
			if c.Command == id {
				return c
			}
		}
		t.Fatalf("check %s not found", id)
		return DiagCommandStatus{}
	}

	if find(CommandU2GetCurrentSlot).Command != CommandU2GetCurrentSlot {
		t.Fatal("expected U2GetCurrentSlot check present")
	}

	pidCheck := find(CommandGetPid)
	if pidCheck.ParsedFacts["detected_pid"] != 0x6012 {
		t.Fatalf("expected detected_pid=0x6012, got %v", pidCheck.ParsedFacts["detected_pid"])
	}
	if pidCheck.ResponseStatus != StatusOk {
		t.Fatalf("expected StatusOk, got %s", pidCheck.ResponseStatus)
	}

	revisionCheck := find(CommandGetReportRevision)
	if revisionCheck.ParsedFacts["revision"] != 1 {
		t.Fatalf("expected revision=1, got %v", revisionCheck.ParsedFacts["revision"])
	}

	versionCheck := find(CommandGetControllerVersion)
	if versionCheck.ParsedFacts["version_x100"] != 42 {
		t.Fatalf("expected version_x100=42, got %v", versionCheck.ParsedFacts["version_x100"])
	}
	if versionCheck.ParsedFacts["beta"] != 1 {
		t.Fatalf("expected beta=1, got %v", versionCheck.ParsedFacts["beta"])
	}

	slotCheck := find(CommandU2GetCurrentSlot)
	if slotCheck.ParsedFacts["slot"] != 2 {
		t.Fatalf("expected slot=2, got %v", slotCheck.ParsedFacts["slot"])
	}
}

func TestDiagProbeGetModeFallsBackToGetModeAlt(t *testing.T) {
	transport := &MockTransport{}
	transport.PushReadData(pidResponse(0x6002))
	transport.PushReadData(reportRevisionResponse(1))
	transport.PushReadData(invalidModeResponse())
	transport.PushReadData(invalidModeResponse())
	transport.PushReadData(invalidModeResponse())
	transport.PushReadData(modeResponse(7))
	transport.PushReadData(modeResponse(7))
	transport.PushReadData(versionResponse(99, 0))
	transport.PushReadData(idleResponse())
	transport.PushReadData(versionResponse(99, 0))
	transport.PushReadData(okReadResponse())

	session := openSession(t, transport, 0x6002, cfgWith(func(c *SessionConfig) { c.Experimental = true }))
	diag := session.DiagProbe(context.Background())

	var modeCheck *DiagCommandStatus
	for i := range diag.CommandChecks {
		if diag.CommandChecks[i].Command == CommandGetMode {
			modeCheck = &diag.CommandChecks[i]
			break
		}
	}
	if modeCheck == nil {
		t.Fatal("mode check not found")
	}
	if !modeCheck.OK {
		t.Fatal("expected mode check to succeed via fallback")
	}
	if modeCheck.ParsedFacts["mode"] != 7 {
		t.Fatalf("expected mode=7, got %v", modeCheck.ParsedFacts["mode"])
	}
	if !strings.Contains(modeCheck.Detail, "GetModeAlt fallback") {
		t.Fatalf("expected detail to mention GetModeAlt fallback, got %q", modeCheck.Detail)
	}
	if modeCheck.ResponseStatus != StatusOk {
		t.Fatalf("expected StatusOk, got %s", modeCheck.ResponseStatus)
	}
}
