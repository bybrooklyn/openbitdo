//go:build linux

package hidraw

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// fakeNode builds the sysfs layout the kernel exposes for one hidraw node:
// <root>/class/<name>/device is a symlink to the HID device directory, whose
// parent is the USB interface directory.
func fakeNode(t *testing.T, root, name, usbInterface, uevent string, descriptor []byte) {
	t.Helper()
	hidDir := filepath.Join(root, "devices", usbInterface, "0003:2DC8:0000."+name)
	if err := os.MkdirAll(hidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidDir, "uevent"), []byte(uevent), 0o644); err != nil {
		t.Fatal(err)
	}
	if descriptor != nil {
		if err := os.WriteFile(filepath.Join(hidDir, "report_descriptor"), descriptor, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	classDir := filepath.Join(root, "class", name)
	if err := os.MkdirAll(classDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hidDir, filepath.Join(classDir, "device")); err != nil {
		t.Fatal(err)
	}
}

func TestEnumerateReadsIdentityInterfaceAndUsage(t *testing.T) {
	root := t.TempDir()
	fakeNode(t, root, "hidraw0", "1-1:1.0",
		"DRIVER=hid-generic\nHID_ID=0003:00002DC8:00006013\nHID_NAME=8BitDo Ultimate 2\nHID_UNIQ=22EC9EA4DF\n",
		ultimate2Descriptor)
	fakeNode(t, root, "hidraw7", "8-3:1.2",
		"HID_ID=0003:00002DC8:00005209\nHID_NAME=8BitDo 8BitDo Retro 108 Keyboard\nHID_UNIQ=\n",
		retro108VendorDescriptor)
	fakeNode(t, root, "hidraw8", "10-1:1.0", "HID_ID=0003:0000046D:00004074\nHID_NAME=Logitech G305\n", nil)

	got := enumerateAt(filepath.Join(root, "class"), "/dev", 0x2dc8, 0)
	if len(got) != 2 {
		t.Fatalf("expected the two 8BitDo nodes, got %+v", got)
	}
	pad, keyboard := got[0], got[1]
	if pad.Path != "/dev/hidraw0" || pad.ProductID != 0x6013 || pad.Serial != "22EC9EA4DF" ||
		pad.Product != "8BitDo Ultimate 2" || pad.Interface != 0 {
		t.Fatalf("unexpected controller node: %+v", pad)
	}
	if len(pad.Usages) != 1 || pad.Usages[0] != (Usage{Page: 0xffa0, Usage: 0x01}) {
		t.Fatalf("unexpected controller usages: %+v", pad.Usages)
	}
	if keyboard.Path != "/dev/hidraw7" || keyboard.ProductID != 0x5209 || keyboard.Interface != 2 {
		t.Fatalf("unexpected keyboard node: %+v", keyboard)
	}

	if only := enumerateAt(filepath.Join(root, "class"), "/dev", 0x2dc8, 0x5209); len(only) != 1 || only[0].ProductID != 0x5209 {
		t.Fatalf("product filter did not apply: %+v", only)
	}
}

func TestEnumerateKeepsNodeWithUnreadableDescriptor(t *testing.T) {
	root := t.TempDir()
	fakeNode(t, root, "hidraw1", "1-1:1.0", "HID_ID=0005:00002DC8:00006012\n", nil)
	got := enumerateAt(filepath.Join(root, "class"), "/dev", 0, 0)
	if len(got) != 1 || got[0].ProductID != 0x6012 || len(got[0].Usages) != 0 {
		t.Fatalf("expected one node with no usages, got %+v", got)
	}
}

func TestEnumerateSkipsMalformedUevent(t *testing.T) {
	root := t.TempDir()
	fakeNode(t, root, "hidraw0", "1-1:1.0", "HID_ID=garbage\n", nil)
	fakeNode(t, root, "hidraw1", "1-2:1.0", "HID_NAME=no id\n", nil)
	if got := enumerateAt(filepath.Join(root, "class"), "/dev", 0, 0); len(got) != 0 {
		t.Fatalf("expected malformed nodes to be skipped, got %+v", got)
	}
	if got := enumerateAt(filepath.Join(root, "missing"), "/dev", 0, 0); got != nil {
		t.Fatalf("expected nil for a missing sysfs directory, got %+v", got)
	}
}

func TestOpenKeepsTheOSError(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "hidraw99"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected a not-exist error, got %v", err)
	}
}
