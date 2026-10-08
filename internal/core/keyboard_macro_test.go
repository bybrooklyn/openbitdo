package core

import (
	"bytes"
	"strings"
	"testing"
)

func TestKeyMacroEncodesAsTheKeyboardStoresIt(t *testing.T) {
	// Ctrl+C, then a 300 ms pause, then V; played three times, 1 s apart.
	macro := KeyMacro{
		Key: 240, Name: "copy", Repeat: 3, IntervalMillis: 1000,
		Steps: []KeyMacroStep{
			{Kind: StepPress, Usage: 0xe0}, {Kind: StepPress, Usage: 0x06},
			{Kind: StepRelease, Usage: 0x06}, {Kind: StepRelease, Usage: 0xe0},
			{Kind: StepWait, Millis: 300},
			{Kind: StepPress, Usage: 0x19}, {Kind: StepRelease, Usage: 0x19},
		},
	}
	value, err := macro.encodeValue()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x01, 3, 0, 8, // lead, repeat, not forever, eight steps with the interval
		0x83, 0xe0, 0, 0x81, 0x06, 0, 0x01, 0x06, 0, 0x03, 0xe0, 0,
		0x0f, 0x2c, 0x01, // 300 ms
		0x81, 0x19, 0, 0x01, 0x19, 0,
		0x0f, 0xe8, 0x03, // the 1000 ms interval, as a final step
	}
	if !bytes.Equal(value, want) {
		t.Fatalf("encoded as % x\nwant       % x", value, want)
	}
	back, err := decodeKeyMacroValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if back.Repeat != 3 || back.IntervalMillis != 1000 || len(back.Steps) != 7 || back.Steps[4] != macro.Steps[4] {
		t.Fatalf("decoded as %+v", back)
	}
	if got := macro.Summary(); got != "Left Ctrl+C V" {
		t.Fatalf("summary = %q", got)
	}
}

func TestKeyMacroForeverAndOnce(t *testing.T) {
	forever := KeyMacro{Repeat: KeyMacroForever, Steps: TypeKeys(20, 0x04)}
	value, err := forever.encodeValue()
	if err != nil {
		t.Fatal(err)
	}
	if value[1] != 0 || value[2] != 0x20 || value[3] != 4 {
		t.Fatalf("forever header = % x", value[:4])
	}
	if back, _ := decodeKeyMacroValue(value); back.Repeat != KeyMacroForever || back.IntervalMillis != 0 || len(back.Steps) != 3 {
		t.Fatalf("forever decoded as %+v", back)
	}

	// Played once, a trailing pause is a step, not an interval.
	once := KeyMacro{Repeat: 1, Steps: TypeKeys(20, 0x04)}
	value, _ = once.encodeValue()
	if value[1] != 1 || value[3] != 3 {
		t.Fatalf("once header = % x", value[:4])
	}
}

func TestKeyMacroRefusesWhatWouldMisbehave(t *testing.T) {
	long := KeyMacro{Repeat: 1}
	for i := 0; i < 101; i++ {
		long.Steps = append(long.Steps, TypeKeys(0, 0x04)...)
	}
	for name, macro := range map[string]KeyMacro{
		"no steps":                {Repeat: 1},
		"a key never released":    {Repeat: 1, Steps: []KeyMacroStep{{Kind: StepPress, Usage: 0x04}}},
		"a release with no press": {Repeat: 1, Steps: []KeyMacroStep{{Kind: StepRelease, Usage: 0x04}}},
		"a zero-length pause":     {Repeat: 1, Steps: append(TypeKeys(0, 0x04), KeyMacroStep{Kind: StepWait})},
		"a pause over a minute":   {Repeat: 1, Steps: append(TypeKeys(0, 0x04), KeyMacroStep{Kind: StepWait, Millis: 60001})},
		"a repeat count of 100":   {Repeat: 100, Steps: TypeKeys(0, 0x04)},
		"a repeat count of 0":     {Repeat: 0, Steps: TypeKeys(0, 0x04)},
		"more than 200 steps":     long,
	} {
		if err := macro.Validate(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if _, err := decodeKeyMacroValue([]byte{1, 1, 0, 1, 0x55, 0, 0}); err == nil || !strings.Contains(err.Error(), "not understood") {
		t.Fatalf("an unknown opcode must stop the read, got %v", err)
	}
	if _, err := decodeKeyMacroValue([]byte{1, 1, 0, 3, 0x81, 4, 0}); err == nil {
		t.Fatal("a value shorter than its step count must be refused")
	}
}

func TestTypeTextBuildsAValidMacro(t *testing.T) {
	steps, skipped := TypeText("Hi, there!\n")
	if skipped != 0 {
		t.Fatalf("%d characters skipped", skipped)
	}
	macro := KeyMacro{Name: "hi", Repeat: 1, Steps: steps}
	if err := macro.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := macro.Summary(); got != "Left Shift+H I , Space T H E R E Left Shift+1 Enter" {
		t.Fatalf("summary = %q", got)
	}
	// Every printable ASCII character has a key, and they are all distinct strokes.
	seen := map[[2]int]rune{}
	for r := rune(' '); r <= '~'; r++ {
		usage, shift, ok := KeyStrokeForRune(r)
		if !ok {
			t.Errorf("%q has no key", r)
			continue
		}
		key := [2]int{int(usage), 0}
		if shift {
			key[1] = 1
		}
		if other, dup := seen[key]; dup {
			t.Errorf("%q and %q map to the same stroke", r, other)
		}
		seen[key] = r
	}
	if _, skipped := TypeText("é"); skipped != 1 {
		t.Fatal("a character with no US key should be skipped and counted")
	}
}
