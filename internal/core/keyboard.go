package core

import (
	"context"
	"fmt"
	"sort"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// This file is the keyboard side of the core: a Retro 108's whole profile
// (every key's assignment, the lock options, the volume level, the profile
// name) rather than only its ten dedicated buttons.

// KeyTargetKind is what kind of thing a key is assigned to.
type KeyTargetKind int

const (
	// TargetNone: the key does nothing.
	TargetNone KeyTargetKind = iota
	// TargetKey: a keyboard key, optionally with one modifier held.
	TargetKey
	// TargetMedia: a media or system key (volume, play, mail, ...).
	TargetMedia
	// TargetMouse: mouse buttons, or one step of the wheel.
	TargetMouse
)

// KeyTarget is what one keyboard key is assigned to.
type KeyTarget struct {
	Kind KeyTargetKind
	// Modifier and Key are HID keyboard usages, for TargetKey. Either may
	// be zero: a modifier alone, a key alone, or both (Shift+1).
	Modifier byte
	Key      byte
	// Media is a HID consumer-page usage, for TargetMedia.
	Media uint16
	// Buttons is a mouse-button bitmask and Wheel a signed scroll step, for
	// TargetMouse.
	Buttons byte
	Wheel   int8
}

// KeyTargetKeyOf is the plain key with usage u, or the modifier with usage u.
func KeyTargetKeyOf(usage byte) KeyTarget {
	if usage >= 0xe0 && usage <= 0xe7 {
		return KeyTarget{Kind: TargetKey, Modifier: usage}
	}
	return KeyTarget{Kind: TargetKey, Key: usage}
}

func (t KeyTarget) wire() protocol.JP108Mapping {
	switch t.Kind {
	case TargetKey:
		return protocol.JP108Mapping{Type: protocol.JP108TypeKeyboard, Value: [4]byte{t.Modifier, t.Key}}
	case TargetMedia:
		return protocol.JP108Mapping{Type: protocol.JP108TypeConsumer, Value: [4]byte{byte(t.Media), byte(t.Media >> 8)}}
	case TargetMouse:
		return protocol.JP108Mapping{Type: protocol.JP108TypeMouse, Value: [4]byte{t.Buttons, 0, 0, byte(t.Wheel)}}
	}
	return protocol.JP108Mapping{Type: protocol.JP108TypeKeyboard}
}

func keyTargetFromWire(m protocol.JP108Mapping) (KeyTarget, error) {
	if m.Unassigned() {
		return KeyTarget{}, nil
	}
	switch m.Type {
	case protocol.JP108TypeKeyboard:
		return KeyTarget{Kind: TargetKey, Modifier: m.Value[0], Key: m.Value[1]}, nil
	case protocol.JP108TypeConsumer:
		return KeyTarget{Kind: TargetMedia, Media: uint16(m.Value[0]) | uint16(m.Value[1])<<8}, nil
	case protocol.JP108TypeMouse:
		return KeyTarget{Kind: TargetMouse, Buttons: m.Value[0], Wheel: int8(m.Value[3])}, nil
	}
	return KeyTarget{}, fmt.Errorf("mapping type %#02x is not understood", m.Type)
}

// KeyboardLocks are key combinations the keyboard can be told to ignore, so
// they cannot interrupt a game.
type KeyboardLocks struct {
	WinKey bool
	AltTab bool
	AltF4  bool
}

func (l KeyboardLocks) flags() byte {
	var flags byte
	if l.WinKey {
		flags |= protocol.JP108LockWinKey
	}
	if l.AltTab {
		flags |= protocol.JP108LockAltTab
	}
	if l.AltF4 {
		flags |= protocol.JP108LockAltF4
	}
	return flags
}

func keyboardLocksFromFlags(flags byte) KeyboardLocks {
	return KeyboardLocks{
		WinKey: flags&protocol.JP108LockWinKey != 0,
		AltTab: flags&protocol.JP108LockAltTab != 0,
		AltF4:  flags&protocol.JP108LockAltF4 != 0,
	}
}

// KeyboardProfile is everything a keyboard's profile holds.
type KeyboardProfile struct {
	// Name is the profile's name; "" means the keyboard has no profile.
	Name string
	// Mappings holds, by key id, every key that is assigned to something
	// other than itself. A key not present behaves as printed on it (a
	// dedicated button not present does nothing).
	Mappings map[byte]KeyTarget
	Locks    KeyboardLocks
	// Volume is the keyboard's volume level, 1 (quietest) to 5.
	Volume int
}

// Target returns what key is assigned to, and whether that is an explicit
// assignment rather than the key's built-in behaviour.
func (p KeyboardProfile) Target(key KeyboardKey) (KeyTarget, bool) {
	if target, ok := p.Mappings[key.ID]; ok {
		return target, true
	}
	return key.Default(), false
}

// KeyboardChanges is a set of edits to apply to a keyboard's profile. A nil
// pointer leaves that setting alone.
type KeyboardChanges struct {
	// Mappings assigns keys, by key id.
	Mappings map[byte]KeyTarget
	Locks    *KeyboardLocks
	Volume   *int
	Name     *string
}

// Empty reports whether there is nothing to apply.
func (c KeyboardChanges) Empty() bool {
	return len(c.Mappings) == 0 && c.Locks == nil && c.Volume == nil && c.Name == nil
}

func supportsKeyboardProfile(vidPid protocol.VidPid) bool {
	return protocol.DeviceProfileFor(vidPid).Capability.SupportsJP108DedicatedMap
}

// KeyboardReadProfile reads a keyboard's whole profile.
func (c *OpenBitdoCore) KeyboardReadProfile(ctx context.Context, vidPid protocol.VidPid) (KeyboardProfile, error) {
	if !supportsKeyboardProfile(vidPid) {
		return KeyboardProfile{}, errPolicyDenied(ReasonUnsupportedPid, "keyboard profiles are not supported for %s", vidPid)
	}
	session, err := c.openSessionForOps(ctx, vidPid)
	if err != nil {
		return KeyboardProfile{}, err
	}
	defer func() { _ = session.Close() }()
	return readKeyboardProfile(ctx, session)
}

func readKeyboardProfile(ctx context.Context, session *protocol.DeviceSession) (KeyboardProfile, error) {
	profile := KeyboardProfile{Mappings: map[byte]KeyTarget{}}
	var err error
	if profile.Name, err = session.JP108ReadProfileName(ctx); err != nil {
		return KeyboardProfile{}, errProtocol(err)
	}
	mapped, err := session.JP108ReadMappedKeys(ctx)
	if err != nil {
		return KeyboardProfile{}, errProtocol(err)
	}
	for _, id := range mapped {
		wire, err := session.JP108ReadKey(ctx, id)
		if err != nil {
			return KeyboardProfile{}, errProtocol(err)
		}
		target, err := keyTargetFromWire(wire)
		if err != nil {
			// Refuse rather than drop it: this profile becomes the backup,
			// and a backup missing a mapping would erase it on restore.
			return KeyboardProfile{}, errProtocol(fmt.Errorf("key %d: %w", id, err))
		}
		profile.Mappings[id] = target
	}
	flags, err := session.JP108ReadFeatures(ctx)
	if err != nil {
		return KeyboardProfile{}, errProtocol(err)
	}
	profile.Locks = keyboardLocksFromFlags(flags)
	volume, err := session.JP108ReadVolume(ctx)
	if err != nil {
		return KeyboardProfile{}, errProtocol(err)
	}
	profile.Volume = int(volume)
	return profile, nil
}

// KeyboardApply writes changes to a keyboard with the same
// backup, write, read back, roll back discipline as every other write: the
// profile is read first and kept as a backup; each written key is read back;
// if anything fails or does not match, what was written is put back.
func (c *OpenBitdoCore) KeyboardApply(ctx context.Context, vidPid protocol.VidPid, changes KeyboardChanges) (WriteRecoveryReport, error) {
	if !supportsKeyboardProfile(vidPid) {
		return WriteRecoveryReport{}, errPolicyDenied(ReasonUnsupportedPid, "keyboard profiles are not supported for %s", vidPid)
	}
	if changes.Volume != nil && (*changes.Volume < 1 || *changes.Volume > 5) {
		return WriteRecoveryReport{}, errInvalidState("volume level %d is out of range 1-5", *changes.Volume)
	}
	session, err := c.openSessionForOps(ctx, vidPid)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	defer func() { _ = session.Close() }()

	before, err := readKeyboardProfile(ctx, session)
	if err != nil {
		return WriteRecoveryReport{}, err
	}
	backupID := c.storeBackup(vidPid, configBackupPayload{kind: backupKeyboard, keyboard: before})

	if changes.Name != nil {
		if *changes.Name == "" {
			return WriteRecoveryReport{}, errInvalidState("a profile name cannot be empty")
		}
		// Whether renaming keeps the stored mappings is the keyboard's
		// business; writing every existing mapping again after the name
		// makes the result the same either way.
		merged := make(map[byte]KeyTarget, len(before.Mappings)+len(changes.Mappings))
		for id, target := range before.Mappings {
			merged[id] = target
		}
		for id, target := range changes.Mappings {
			merged[id] = target
		}
		changes.Mappings = merged
	}

	applyErr := writeKeyboardChanges(ctx, session, changes)
	if applyErr == nil {
		return WriteRecoveryReport{BackupID: backupID, HasBackupID: true, WriteApplied: true}, nil
	}

	// Put back exactly the settings this call touched.
	report := WriteRecoveryReport{
		BackupID: backupID, HasBackupID: true, RollbackAttempted: true, WriteError: applyErr.Error(),
	}
	if rollbackErr := writeKeyboardChanges(ctx, session, undoOf(changes, before)); rollbackErr != nil {
		report.RollbackError = rollbackErr.Error()
		return report, nil
	}
	report.RollbackSucceeded = true
	return report, nil
}

// undoOf builds the changes that restore, from before, every setting that
// changes touches.
func undoOf(changes KeyboardChanges, before KeyboardProfile) KeyboardChanges {
	undo := KeyboardChanges{Mappings: map[byte]KeyTarget{}}
	for id := range changes.Mappings {
		if target, ok := before.Mappings[id]; ok {
			undo.Mappings[id] = target
		} else if key, known := KeyboardKeyByID(id); known {
			undo.Mappings[id] = key.Default()
		} else {
			undo.Mappings[id] = KeyTarget{}
		}
	}
	if changes.Locks != nil {
		locks := before.Locks
		undo.Locks = &locks
	}
	if changes.Volume != nil && before.Volume >= 1 && before.Volume <= 5 {
		volume := before.Volume
		undo.Volume = &volume
	}
	if changes.Name != nil && before.Name != "" {
		name := before.Name
		undo.Name = &name
	}
	return undo
}

// writeKeyboardChanges writes each change and reads it back. Keys are
// written in id order so a run is reproducible.
func writeKeyboardChanges(ctx context.Context, session *protocol.DeviceSession, changes KeyboardChanges) error {
	if changes.Name != nil {
		if err := session.JP108WriteProfileName(ctx, *changes.Name); err != nil {
			return fmt.Errorf("profile name: %w", err)
		}
	}
	ids := make([]int, 0, len(changes.Mappings))
	for id := range changes.Mappings {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	for _, id := range ids {
		want := changes.Mappings[byte(id)].wire()
		if err := session.JP108WriteKey(ctx, byte(id), want); err != nil {
			return fmt.Errorf("key %d: %w", id, err)
		}
		got, err := session.JP108ReadKey(ctx, byte(id))
		if err != nil {
			return fmt.Errorf("key %d: readback failed: %w", id, err)
		}
		if got != want && (!got.Unassigned() || !want.Unassigned()) {
			return fmt.Errorf("readback mismatch for key %d: wrote type %#02x value % x, keyboard holds type %#02x value % x",
				id, want.Type, want.Value, got.Type, got.Value)
		}
	}
	if changes.Locks != nil {
		want := changes.Locks.flags()
		if err := session.JP108WriteFeatures(ctx, want); err != nil {
			return fmt.Errorf("lock options: %w", err)
		}
		if got, err := session.JP108ReadFeatures(ctx); err != nil || got != want {
			return fmt.Errorf("readback mismatch for lock options: wrote %#02x, keyboard holds %#02x (%v)", want, got, err)
		}
	}
	if changes.Volume != nil {
		want := byte(*changes.Volume)
		if err := session.JP108WriteVolume(ctx, want); err != nil {
			return fmt.Errorf("volume: %w", err)
		}
		if got, err := session.JP108ReadVolume(ctx); err != nil || got != want {
			return fmt.Errorf("readback mismatch for volume: wrote %d, keyboard holds %d (%v)", want, got, err)
		}
	}
	return nil
}

// KeyboardClearProfile removes a keyboard's profile: its name and every
// mapping. The profile as it was is kept as a backup first.
func (c *OpenBitdoCore) KeyboardClearProfile(ctx context.Context, vidPid protocol.VidPid) (ConfigBackupID, error) {
	if !supportsKeyboardProfile(vidPid) {
		return "", errPolicyDenied(ReasonUnsupportedPid, "keyboard profiles are not supported for %s", vidPid)
	}
	session, err := c.openSessionForOps(ctx, vidPid)
	if err != nil {
		return "", err
	}
	defer func() { _ = session.Close() }()
	before, err := readKeyboardProfile(ctx, session)
	if err != nil {
		return "", err
	}
	backupID := c.storeBackup(vidPid, configBackupPayload{kind: backupKeyboard, keyboard: before})
	if err := session.JP108ClearProfile(ctx); err != nil {
		return backupID, errProtocol(err)
	}
	return backupID, nil
}

// restoreKeyboardBackup writes a backed-up profile back in full.
func restoreKeyboardBackup(ctx context.Context, session *protocol.DeviceSession, backup KeyboardProfile) error {
	current, err := readKeyboardProfile(ctx, session)
	if err != nil {
		return err
	}
	changes := KeyboardChanges{Mappings: map[byte]KeyTarget{}}
	// Keys mapped now but not in the backup go back to their defaults.
	for id := range current.Mappings {
		if key, known := KeyboardKeyByID(id); known {
			changes.Mappings[id] = key.Default()
		} else {
			changes.Mappings[id] = KeyTarget{}
		}
	}
	for id, target := range backup.Mappings {
		changes.Mappings[id] = target
	}
	locks := backup.Locks
	changes.Locks = &locks
	if backup.Volume >= 1 && backup.Volume <= 5 {
		volume := backup.Volume
		changes.Volume = &volume
	}
	if backup.Name != "" {
		name := backup.Name
		changes.Name = &name
	}
	if err := writeKeyboardChanges(ctx, session, changes); err != nil {
		return errProtocol(err)
	}
	return nil
}
