package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

func sampleKeyboardProfile() KeyboardProfile {
	return KeyboardProfile{
		Name: "Work", Volume: 4, Locks: KeyboardLocks{WinKey: true},
		Mappings: map[byte]KeyTarget{
			233:  KeyTargetKeyOf(0x68),
			232:  {Kind: TargetKey, Modifier: 0xe1, Key: 0x1e},
			0x39: KeyTargetKeyOf(0xe0),
			240:  {Kind: TargetMedia, Media: 0x018a},
			241:  {Kind: TargetMouse, Wheel: -2},
			238:  {},
		},
		Macros: map[byte]KeyMacro{
			236: {Key: 236, Name: "copy", Repeat: KeyMacroForever, IntervalMillis: 500, Steps: []KeyMacroStep{
				{Kind: StepPress, Usage: 0xe0}, {Kind: StepPress, Usage: 0x06}, {Kind: StepWait, Millis: 30},
				{Kind: StepRelease, Usage: 0x06}, {Kind: StepRelease, Usage: 0xe0},
			}},
		},
	}
}

func TestKeyboardProfileFileRoundTrips(t *testing.T) {
	want := sampleKeyboardProfile()
	data, err := EncodeKeyboardProfile(want)
	if err != nil {
		t.Fatal(err)
	}
	// A person can read what each entry does.
	for _, text := range []string{`key_name = "A button"`, `does = "F13"`, `does = "Left Shift+1"`, `"press 224 Left Ctrl"`, `"wait 30"`} {
		if !strings.Contains(string(data), text) {
			t.Errorf("expected the file to contain %s:\n%s", text, data)
		}
	}
	got, err := DecodeKeyboardProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed the profile:\n got %+v\nwant %+v", got, want)
	}
}

func TestKeyboardProfileFileRefusesWhatAKeyboardCannotHold(t *testing.T) {
	good, _ := EncodeKeyboardProfile(sampleKeyboardProfile())
	for name, edit := range map[string][2]string{
		"an unknown key":                  {"key = 233", "key = 250"},
		"a volume out of range":           {"volume = 4", "volume = 9"},
		"an unknown mapping kind":         {`kind = "media"`, `kind = "laser"`},
		"an unknown setting":              {"volume = 4", "volume = 4\nturbo = true"},
		"a macro leaving a key held":      {`"release 224 Left Ctrl"`, `"wait 5"`},
		"a macro step that is not a step": {`"wait 30"`, `"dance 30"`},
		"another device's file":           {`device = "retro-108"`, `device = "ultimate-2"`},
	} {
		if !strings.Contains(string(good), edit[0]) {
			t.Fatalf("%s: the sample does not contain %q", name, edit[0])
		}
		if _, err := DecodeKeyboardProfile([]byte(strings.Replace(string(good), edit[0], edit[1], 1))); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if _, err := DecodeKeyboardProfile([]byte("this is not toml = = =")); err == nil {
		t.Error("a file that is not TOML must be refused")
	}
}

func TestPadSlotFileRoundTripsAndIsPlatformBound(t *testing.T) {
	slot := defaultPadSlot(protocol.U2PlatformDInput)
	slot.InUse, slot.Name = true, "Racing"
	slot.Buttons[18], slot.Buttons[19] = PadA, PadLSUp
	slot.LeftStick, slot.RightTrigger = PadRange{10, 120}, PadRange{0, 200}
	slot.VibrationLeft, slot.Options = 2, PadInvertRightY|PadSwapTriggers

	data, err := EncodePadSlot(protocol.U2PlatformDInput, slot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `input = "Back paddle P1"`) || !strings.Contains(string(data), `does = "Left Stick Up"`) {
		t.Fatalf("expected a readable file:\n%s", data)
	}
	got, err := DecodePadSlot(data, protocol.U2PlatformDInput)
	if err != nil || got != slot {
		t.Fatalf("round trip: err=%v\n got %+v\nwant %+v", err, got, slot)
	}
	if _, err := DecodePadSlot(data, protocol.U2PlatformXInput); err == nil {
		t.Fatal("a slot saved for one mode-switch position must not load on the other")
	}
	bad := strings.Replace(string(data), "target = 8192", "target = 48", 1)
	if _, err := DecodePadSlot([]byte(bad), protocol.U2PlatformDInput); err == nil {
		t.Fatal("a function the controller does not have must be refused")
	}
}

func TestProfileFilesOnDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profiles", "retro-108")
	for name, want := range map[string]string{
		"Work": "Work.toml", "my profile 2": "my-profile-2.toml", "../../etc/passwd": "etcpasswd.toml", "配置": "profile.toml", "": "profile.toml",
	} {
		if got := ProfileFileName(name); got != want {
			t.Errorf("ProfileFileName(%q) = %q, want %q", name, got, want)
		}
	}
	path, err := WriteProfileFile(dir, "Work", []byte("a"), false)
	if err != nil || filepath.Dir(path) != dir {
		t.Fatalf("write: %q err=%v", path, err)
	}
	if _, err := WriteProfileFile(dir, "Work", []byte("b"), false); err == nil {
		t.Fatal("an existing file must not be replaced silently")
	}
	if _, err := WriteProfileFile(dir, "Work", []byte("b"), true); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "b" {
		t.Fatalf("overwrite left %q", data)
	}
	if got := ListProfileFiles(dir); len(got) != 1 || got[0] != "Work.toml" {
		t.Fatalf("list = %v", got)
	}
	if got := ListProfileFiles(filepath.Join(dir, "missing")); got != nil {
		t.Fatalf("a missing directory should list nothing, got %v", got)
	}
}
