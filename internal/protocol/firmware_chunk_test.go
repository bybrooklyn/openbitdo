package protocol

import (
	"bytes"
	"context"
	"testing"
)

// Ported from sdk/tests/firmware_chunk.rs.

func TestInferredFirmwareTransferIsBlockedUntilConfirmed(t *testing.T) {
	transport := &MockTransport{}
	for i := 0; i < 4; i++ {
		transport.PushReadData([]byte{0x02, 0x10, 0x00, 0x00})
	}

	config := cfgWith(func(c *SessionConfig) { c.AllowUnsafe = true; c.BrickRiskAck = true; c.Experimental = true })
	session := openSession(t, transport, 24585, config)
	image := bytes.Repeat([]byte{0xAB}, 120)
	_, err := session.FirmwareTransfer(context.Background(), image, 50, false)
	mustErrCode(t, err, CodeUnsupportedForPid)

	if len(transport.Writes()) != 0 {
		t.Fatalf("expected no writes, got %d", len(transport.Writes()))
	}
}

func TestFullSupportU2FirmwareTransferUsesPidSpecificFrames(t *testing.T) {
	transport := &MockTransport{}
	for i := 0; i < 4; i++ {
		transport.PushReadData([]byte{0x02, 0x10, 0x00, 0x00})
	}

	session := openSession(t, transport, 0x6012, cfgWith(func(c *SessionConfig) { c.AllowUnsafe = true; c.BrickRiskAck = true }))
	ctx := context.Background()

	if err := session.EnterBootloader(ctx); err != nil {
		t.Fatalf("enter bootloader: %v", err)
	}
	image := bytes.Repeat([]byte{0xAB}, 70)
	report, err := session.FirmwareTransfer(ctx, image, 32, false)
	if err != nil {
		t.Fatalf("firmware transfer: %v", err)
	}
	if report.ChunksSent != 3 {
		t.Fatalf("expected 3 chunks, got %d", report.ChunksSent)
	}
	if err := session.ExitBootloader(ctx); err != nil {
		t.Fatalf("exit bootloader: %v", err)
	}

	writes := transport.Writes()
	if len(writes) != 6 {
		t.Fatalf("expected 6 writes, got %d", len(writes))
	}
	if !bytes.Equal(writes[0], []byte{0x05, 0x00, 0x50, 0x01, 0x00, 0x00}) {
		t.Fatalf("unexpected enter-bootloader frame: %x", writes[0])
	}
	if !bytes.Equal(writes[1][:5], []byte{0x81, 0x60, 0x10, 0x60, 0x12}) {
		t.Fatalf("unexpected chunk header: %x", writes[1][:5])
	}
	if !bytes.Equal(writes[1][5:37], image[:32]) {
		t.Fatal("unexpected chunk payload")
	}
	if !bytes.Equal(writes[4][:5], []byte{0x81, 0x60, 0x11, 0x60, 0x12}) {
		t.Fatalf("unexpected commit header: %x", writes[4][:5])
	}
	if !bytes.Equal(writes[5], []byte{0x05, 0x00, 0x51, 0x01, 0x00, 0x00}) {
		t.Fatalf("unexpected exit-bootloader frame: %x", writes[5])
	}
}
