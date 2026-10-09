package protocol

import (
	"bytes"
	"context"
	"testing"
)

// arcadeProSession is a session with an Arcade Controller Pro whose tier is
// set for the length of the test (none when tier is empty).
// docs/spec/pid_matrix.csv has no row for it, which is what keeps the real
// one unreachable.
func arcadeProSession(t *testing.T, pad *U2Simulator, tier SupportTier) *DeviceSession {
	t.Helper()
	shipped := PIDRegistry
	t.Cleanup(func() { PIDRegistry = shipped })
	var rows []PidRow
	for _, row := range shipped {
		if row.Pid != ArcadeProPID {
			rows = append(rows, row)
		}
	}
	if tier != "" {
		rows = append(rows, PidRow{Name: "PID_HitBox2", Pid: ArcadeProPID, SupportLevel: SupportFull, SupportTier: tier, ProtocolFamily: DInput})
	}
	PIDRegistry = rows
	config := fastRetryConfig()
	config.Experimental = true
	return openSession(t, pad, ArcadeProPID, config)
}

func TestArcadeProCommandsAndTheirGate(t *testing.T) {
	ctx := context.Background()
	pad := &U2Simulator{Physical: U2PlatformXInput, RecordSize: ArcadeProRecordSize, PlatformOffset: 0x0c}
	session := arcadeProSession(t, pad, TierFull)

	// Its profile is all it is granted.
	if capability := session.Profile().Capability; !capability.SupportsU2SlotConfig || capability.SupportsFirmware || capability.SupportsBoot || capability.SupportsMode {
		t.Fatalf("capability = %+v", capability)
	}
	if err := session.ArcadeProSetSync(ctx, true); err != nil || !pad.Sync {
		t.Fatalf("sync on: %v", err)
	}
	if last := pad.Frames[len(pad.Frames)-1]; !bytes.Equal(last[:8], []byte{0x81, 0x04, 0x16, 0x00, 0x01, 0x00, 0x00, 0x00}) || len(last) != 64 {
		t.Fatalf("sync request = % x", last[:8])
	}
	if err := session.ArcadeProSetSync(ctx, false); err != nil || pad.Sync {
		t.Fatalf("sync off: %v", err)
	}
	if err := session.ArcadeProSwitchReport(ctx, 1); err != nil || pad.SwitchReport != 1 {
		t.Fatalf("report switch: %v", err)
	}
	if last := pad.Frames[len(pad.Frames)-1]; !bytes.Equal(last[:8], []byte{0x81, 0x04, 0x06, 0x01, 0x01, 0x00, 0x00, 0x00}) {
		t.Fatalf("report switch request = % x", last[:8])
	}
	frames := len(pad.Frames)
	if err := session.ArcadeProSwitchReport(ctx, 2); err == nil || len(pad.Frames) != frames {
		t.Fatal("a report switch state other than 0 or 1 must be refused unsent")
	}

	// The mode it reports: 2 is XInput, 0 and 4 Switch, anything else
	// refused rather than guessed.
	if got, err := session.ArcadeProPlatform(ctx); err != nil || got != U2PlatformXInput {
		t.Fatalf("mode 2: platform=%d err=%v", got, err)
	}
	if last := pad.Frames[len(pad.Frames)-1]; !bytes.Equal(last[:4], []byte{0x81, 0x00, 0x52, 0x00}) {
		t.Fatalf("mode request = % x", last[:4])
	}
	pad.ArcadeMode = 4
	if got, err := session.ArcadeProPlatform(ctx); err != nil || got != U2PlatformSwitch {
		t.Fatalf("mode 4: platform=%d err=%v", got, err)
	}
	pad.ArcadeMode, pad.Physical = 0, U2PlatformSwitch
	if got, err := session.ArcadeProPlatform(ctx); err != nil || got != U2PlatformSwitch {
		t.Fatalf("mode 0: platform=%d err=%v", got, err)
	}
	pad.ArcadeMode = 7
	if _, err := session.ArcadeProPlatform(ctx); err == nil {
		t.Fatal("an unknown mode must be refused")
	}

	// Its record is read and written at its own size.
	record, err := session.U2ReadRecord(ctx, ArcadeProRecordSize)
	if err != nil || len(record) != 0xa68 {
		t.Fatalf("record read: %d bytes err=%v", len(record), err)
	}
	record[0x634] = 3
	if err := session.U2WriteRecordRange(ctx, record, 0x634, 1); err != nil {
		t.Fatal(err)
	}
	if last := pad.Frames[len(pad.Frames)-1]; last[10] != 0x68 || last[11] != 0x0a || last[14] != 0x34 || last[15] != 0x06 || last[18] != 3 {
		t.Fatalf("write request = % x", last[:20])
	}

	// The other controllers are not sent its commands.
	other := u2Session(t, &U2Simulator{})
	if err := other.ArcadeProSetSync(ctx, true); err == nil {
		t.Fatal("an Ultimate 2 must not be sent the Arcade Controller Pro's session mark")
	}
	if err := other.ArcadeProSwitchReport(ctx, 0); err == nil {
		t.Fatal("an Ultimate 2 must not be sent the Arcade Controller Pro's report switch")
	}

	// As shipped (no row), and at detect-only, it is sent nothing.
	for _, tier := range []SupportTier{"", TierDetectOnly} {
		quiet := &U2Simulator{RecordSize: ArcadeProRecordSize, PlatformOffset: 0x0c}
		session := arcadeProSession(t, quiet, tier)
		if _, err := session.U2ReadRecord(ctx, ArcadeProRecordSize); err == nil {
			t.Fatalf("tier %q: a record read must be refused", tier)
		}
		if err := session.ArcadeProSetSync(ctx, true); err == nil {
			t.Fatalf("tier %q: the session mark must be refused", tier)
		}
		if _, err := session.ArcadeProPlatform(ctx); err == nil {
			t.Fatalf("tier %q: the mode query must be refused", tier)
		}
		if len(quiet.Frames) != 0 {
			t.Fatalf("tier %q: %d frames were sent", tier, len(quiet.Frames))
		}
	}
}

func TestShippedRegistryGrantsTheArcadeProNothing(t *testing.T) {
	if _, ok := FindPID(ArcadeProPID); ok {
		t.Skip("the Arcade Controller Pro now has a row in pid_matrix.csv; its tier decides")
	}
	if capability := DeviceProfileFor(VidPid{VID: 0x2dc8, PID: ArcadeProPID}).Capability; capability != (PidCapability{}) && capability != IdentifyOnlyCapability() {
		t.Fatalf("capability without a row = %+v", capability)
	}
}
