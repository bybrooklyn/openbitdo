package core

import (
	"context"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

func keyboardCore(keyboard *protocol.JP108Simulator) *OpenBitdoCore {
	c := New(Config{})
	c.transportOverride = keyboard
	return c
}

func TestKeyboardProfileRoundTripsEveryTargetKind(t *testing.T) {
	keyboard := &protocol.JP108Simulator{Volume: 2}
	c := keyboardCore(keyboard)
	ctx := context.Background()

	volume := 4
	locks := KeyboardLocks{WinKey: true, AltF4: true}
	changes := KeyboardChanges{
		Mappings: map[byte]KeyTarget{
			233:  KeyTargetKeyOf(0x68),                         // A button -> F13
			232:  {Kind: TargetKey, Modifier: 0xe1, Key: 0x1e}, // B button -> Shift+1
			0x39: KeyTargetKeyOf(0xe0),                         // Caps Lock -> Left Ctrl
			0x48: {Kind: TargetMedia, Media: 0x018a},           // Pause -> Mail (two-byte usage)
			240:  {Kind: TargetMouse, Buttons: MouseLeft},      // K1 -> left click
			241:  {Kind: TargetMouse, Wheel: -2},               // K2 -> wheel down
		},
		Locks: &locks, Volume: &volume,
	}
	report, err := c.KeyboardApply(ctx, retro108Target, changes)
	if err != nil || !report.WriteApplied {
		t.Fatalf("apply: %+v err=%v", report, err)
	}

	// What reached the keyboard, in its own encoding.
	wire := map[byte][5]byte{
		233: {0x07, 0x00, 0x68}, 232: {0x07, 0xe1, 0x1e}, 0x39: {0x07, 0xe0, 0x00},
		0x48: {0x0c, 0x8a, 0x01}, 240: {0x01, 0x01}, 241: {0x01, 0x00, 0x00, 0x00, 0xfe},
	}
	for id, want := range wire {
		if got := keyboard.Mappings[id]; got != want {
			t.Errorf("key %d stored as % x, want % x", id, got, want)
		}
	}
	if keyboard.Features != 0x05 || keyboard.Volume != 4 {
		t.Errorf("features=%#02x volume=%d, want 0x05 and 4", keyboard.Features, keyboard.Volume)
	}

	got, err := c.KeyboardReadProfile(ctx, retro108Target)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "OpenBitdo" || got.Volume != 4 || got.Locks != locks || len(got.Mappings) != len(changes.Mappings) {
		t.Fatalf("read back %+v", got)
	}
	for id, want := range changes.Mappings {
		if got.Mappings[id] != want {
			t.Errorf("key %d read back as %+v, want %+v", id, got.Mappings[id], want)
		}
	}

	// Restoring the backup undoes all of it, including keys that had no
	// mapping before: an ordinary key goes back to itself.
	if err := c.RestoreBackup(ctx, report.BackupID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if keyboard.Mappings[0x39] != [5]byte{0x07, 0x00, 0x39} || keyboard.Mappings[233] != [5]byte{0x07} {
		t.Fatalf("after restore: caps=% x a=% x", keyboard.Mappings[0x39], keyboard.Mappings[233])
	}
	if keyboard.Features != 0 || keyboard.Volume != 2 {
		t.Fatalf("after restore: features=%#02x volume=%d", keyboard.Features, keyboard.Volume)
	}
}

func TestKeyboardApplyRollsBackWhatItWrote(t *testing.T) {
	keyboard := &protocol.JP108Simulator{Name: []byte("P\x00"), Mappings: map[byte][5]byte{233: {0x07, 0x00, 0x04}}}
	c := keyboardCore(keyboard)
	keyboard.IgnoreWrites = true // acknowledges, stores nothing

	report, err := c.KeyboardApply(context.Background(), retro108Target,
		KeyboardChanges{Mappings: map[byte]KeyTarget{233: KeyTargetKeyOf(0x05)}})
	if err != nil {
		t.Fatal(err)
	}
	if report.WriteApplied || !report.RollbackAttempted {
		t.Fatalf("an unverified write must not be reported as applied: %+v", report)
	}
	if keyboard.Mappings[233] != [5]byte{0x07, 0x00, 0x04} {
		t.Fatalf("the key must hold what it held before: % x", keyboard.Mappings[233])
	}
}

func TestKeyboardReadRefusesAnUnknownMappingType(t *testing.T) {
	keyboard := &protocol.JP108Simulator{Mappings: map[byte][5]byte{0x04: {0x55, 0x01}}}
	if _, err := keyboardCore(keyboard).KeyboardReadProfile(context.Background(), retro108Target); err == nil {
		t.Fatal("a mapping type that is not understood must stop the read, not be dropped from the backup")
	}
}

func TestKeyboardClearProfileKeepsABackup(t *testing.T) {
	keyboard := &protocol.JP108Simulator{Name: []byte("P\x00"), Mappings: map[byte][5]byte{233: {0x07, 0x00, 0x68}}}
	c := keyboardCore(keyboard)
	backupID, err := c.KeyboardClearProfile(context.Background(), retro108Target)
	if err != nil {
		t.Fatal(err)
	}
	if len(keyboard.Name) != 0 || len(keyboard.Mappings) != 0 {
		t.Fatalf("expected an empty keyboard, got name=%q mappings=%v", keyboard.Name, keyboard.Mappings)
	}
	if err := c.RestoreBackup(context.Background(), backupID); err != nil {
		t.Fatal(err)
	}
	if keyboard.Mappings[233] != [5]byte{0x07, 0x00, 0x68} || string(keyboard.Name) != "P\x00" {
		t.Fatalf("restore did not bring the profile back: name=%q mappings=%v", keyboard.Name, keyboard.Mappings)
	}
}

func TestRetro108KeyTableIsConsistent(t *testing.T) {
	seen := map[byte]bool{}
	for _, key := range Retro108Keys {
		if key.Name == "" {
			t.Errorf("key %#02x has no name", key.ID)
		}
		if seen[key.ID] {
			t.Errorf("key id %#02x listed twice", key.ID)
		}
		seen[key.ID] = true
	}
	if len(Retro108Keys) != 104 {
		t.Fatalf("expected 10 dedicated buttons and 94 remappable keys, got %d", len(Retro108Keys))
	}
	// The A and B buttons are ids 233 and 232, in that order.
	if Retro108Keys[0].ID != 233 || Retro108Keys[1].ID != 232 {
		t.Fatal("A and B button ids are wrong")
	}
	names := map[string]KeyTarget{}
	for _, choice := range KeyTargetChoices() {
		name := choice.Target.String()
		if prior, dup := names[name]; dup && prior != choice.Target {
			t.Errorf("two different targets are both named %q", name)
		}
		names[name] = choice.Target
	}
	for _, want := range []string{"(none)", "F13", "Left Ctrl", "Volume Up", "Mail", "Left Click", "Wheel Down"} {
		if _, ok := names[want]; !ok {
			t.Errorf("expected a target named %q", want)
		}
	}
	if got := (KeyTarget{Kind: TargetKey, Modifier: 0xe1, Key: 0x1e}).String(); got != "Left Shift+1" {
		t.Errorf("combined target named %q", got)
	}
}

func TestMockModeHasAWorkingKeyboard(t *testing.T) {
	c := New(Config{MockMode: true})
	ctx := context.Background()
	if _, err := c.KeyboardApply(ctx, retro108Target, KeyboardChanges{Mappings: map[byte]KeyTarget{233: KeyTargetKeyOf(0x68)}}); err != nil {
		t.Fatal(err)
	}
	profile, err := c.KeyboardReadProfile(ctx, retro108Target)
	if err != nil || profile.Mappings[233] != KeyTargetKeyOf(0x68) {
		t.Fatalf("mock keyboard did not keep the edit: %+v err=%v", profile, err)
	}
}
