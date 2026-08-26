package protocol

import (
	"context"
	"testing"
)

// Ported from sdk/tests/candidate_readonly_gating.rs.

var candidateReadOnlyPIDs = []uint16{
	0x6002, 0x6003, 0x3010, 0x3011, 0x3012, 0x3013, 0x5200, 0x5201, 0x203a, 0x2049, 0x2028, 0x202e,
	0x3004, 0x3019, 0x3100, 0x3105, 0x2100, 0x2101, 0x901a, 0x6006, 0x5203, 0x5204, 0x301a, 0x9028,
	0x3026, 0x3027,
}

func TestCandidateTargetsAreCandidateReadOnly(t *testing.T) {
	for _, pid := range candidateReadOnlyPIDs {
		profile := DeviceProfileFor(VidPid{VID: 0x2dc8, PID: pid})
		if profile.SupportTier != TierCandidateReadOnly {
			t.Errorf("pid=%#04x: expected candidate-readonly, got %s", pid, profile.SupportTier)
		}
		if profile.SupportLevel != SupportDetectOnly {
			t.Errorf("pid=%#04x: support_level should remain detect-only until full promotion, got %s", pid, profile.SupportLevel)
		}
	}
}

func TestCandidateStandardPidAllowsDiagReadButBlocksWriteAndUnsafe(t *testing.T) {
	pid := uint16(0x6002)
	transport := &MockTransport{}
	transport.PushReadTimeout()
	transport.PushReadTimeout()
	transport.PushReadTimeout()

	session := openSession(t, transport, pid, cfgWith(func(c *SessionConfig) { c.Experimental = true }))
	ctx := context.Background()

	_, err := session.GetMode(ctx)
	if err == nil {
		t.Fatal("candidate get_mode should execute and fail only at transport/response stage")
	}
	if pe, ok := err.(*Error); !ok || (pe.Code() != CodeTimeout && pe.Code() != CodeMalformedResponse) {
		t.Fatalf("expected Timeout or MalformedResponse, got %v", err)
	}

	_, err = session.SetMode(ctx, 1)
	mustErrCode(t, err, CodeUnsupportedForPid)

	err = session.EnterBootloader(ctx)
	mustErrCode(t, err, CodeUnsupportedForPid)
}

func TestCandidateStandardPidAllowsSafeWriteOnlyWithRuntimeUnlock(t *testing.T) {
	pid := uint16(0x6002)
	transport := &MockTransport{}
	r1 := make([]byte, 64)
	r1[0] = 0x02
	transport.PushReadData(r1)
	r2 := make([]byte, 64)
	r2[0], r2[1], r2[5] = 0x02, 0x05, 1
	transport.PushReadData(r2)

	session := openSession(t, transport, pid, cfgWith(func(c *SessionConfig) { c.Experimental = true; c.CandidateWriteUnlock = true }))
	ctx := context.Background()

	mode, err := session.SetMode(ctx, 1)
	if err != nil {
		t.Fatalf("candidate mode write should execute with runtime unlock: %v", err)
	}
	if mode.Mode != 1 {
		t.Fatalf("expected mode 1, got %d", mode.Mode)
	}

	err = session.EnterBootloader(ctx)
	mustErrCode(t, err, CodeUnsupportedForPid)
}

func TestCandidateJPPidRemainsDiagOnly(t *testing.T) {
	pid := uint16(0x5200)
	transport := &MockTransport{}
	r := make([]byte, 64)
	r[0], r[1], r[4] = 0x02, 0x05, 0xC1
	transport.PushReadData(r)

	session := openSession(t, transport, pid, cfgWith(func(c *SessionConfig) { c.Experimental = true }))
	ctx := context.Background()

	identify, err := session.Identify(ctx)
	if err != nil {
		t.Fatalf("identify should be allowed: %v", err)
	}
	if identify.Target.PID != pid {
		t.Fatalf("expected target pid %#04x, got %#04x", pid, identify.Target.PID)
	}
	profile := DeviceProfileFor(VidPid{VID: 0x2dc8, PID: pid})
	if profile.SupportTier != TierCandidateReadOnly {
		t.Fatalf("expected candidate-readonly, got %s", profile.SupportTier)
	}

	_, err = session.GetMode(ctx)
	mustErrCode(t, err, CodeUnsupportedForPid)
}

func TestWave2CandidateStandardPidAllowsSafeReadsOnly(t *testing.T) {
	pid := uint16(0x3100)
	transport := &MockTransport{}
	transport.PushReadTimeout()
	transport.PushReadTimeout()
	transport.PushReadTimeout()

	session := openSession(t, transport, pid, cfgWith(func(c *SessionConfig) { c.Experimental = true }))
	ctx := context.Background()

	_, err := session.GetMode(ctx)
	if err == nil {
		t.Fatal("wave2 candidate get_mode should be permitted and fail at transport/response stage")
	}
	if pe, ok := err.(*Error); !ok || (pe.Code() != CodeTimeout && pe.Code() != CodeMalformedResponse) {
		t.Fatalf("expected Timeout or MalformedResponse, got %v", err)
	}

	_, err = session.SetMode(ctx, 1)
	mustErrCode(t, err, CodeUnsupportedForPid)
}

func TestCandidateUltimate2AllowsSlotReadAndConditionalWrite(t *testing.T) {
	pid := uint16(0x3105)
	transport := &MockTransport{}
	r1 := make([]byte, 64)
	r1[0], r1[1], r1[5] = 0x02, 0x05, 1
	transport.PushReadData(r1)
	r2 := make([]byte, 64)
	r2[0], r2[1], r2[5], r2[6], r2[7] = 0x02, 0x05, 1, 128, 128
	transport.PushReadData(r2)

	session := openSession(t, transport, pid, cfgWith(func(c *SessionConfig) { c.Experimental = true }))
	ctx := context.Background()

	slot, err := session.U2GetCurrentSlot(ctx)
	if err != nil {
		t.Fatalf("u2_get_current_slot allowed: %v", err)
	}
	if slot != 1 {
		t.Fatalf("expected slot 1, got %d", slot)
	}
	config, err := session.U2ReadConfigSlot(ctx, slot)
	if err != nil {
		t.Fatalf("u2_read_config_slot allowed: %v", err)
	}
	if len(config) == 0 {
		t.Fatal("expected non-empty config")
	}

	err = session.U2WriteConfigSlot(ctx, slot, config)
	mustErrCode(t, err, CodeUnsupportedForPid)

	transportUnlocked := &MockTransport{}
	ru := make([]byte, 64)
	ru[0] = 0x02
	transportUnlocked.PushReadData(ru)
	sessionUnlocked := openSession(t, transportUnlocked, pid, cfgWith(func(c *SessionConfig) { c.Experimental = true; c.CandidateWriteUnlock = true }))

	if err := sessionUnlocked.U2WriteConfigSlot(ctx, slot, config); err != nil {
		t.Fatalf("u2_write_config_slot allowed with write unlock: %v", err)
	}
}
