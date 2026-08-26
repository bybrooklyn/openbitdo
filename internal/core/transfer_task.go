package core

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// runTransferTask drives one confirmed firmware transfer end to end: enter
// bootloader, send chunks (checking for cancellation between each), commit,
// exit bootloader, verify. Runs in its own goroutine, started from
// ConfirmFirmware — detached from Bubbletea's Cmd system, so a panic here
// would otherwise crash the whole process uncaught (Bubbletea's own panic
// recovery only wraps goroutines it spawns itself) and leave the firmware
// session hung forever waiting for a progress event that will never come.
// The recover() below ensures a panic is instead reported as a normal
// (if unexpected) transfer failure.
func runTransferTask(ctx context.Context, handle *firmwareSessionHandle, intervalMs uint64, mockMode bool, transport protocol.Transport) {
	// chunksSent is declared here (rather than at its original point of first
	// use, further down) so the panic recovery below can report how far the
	// transfer actually got, not always 0.
	chunksSent := 0
	defer func() {
		if r := recover(); r != nil {
			finalizeFailure(handle, protocol.CodeInvalidInput, chunksSent,
				fmt.Sprintf("internal error during firmware transfer: %v\n%s", r, debug.Stack()))
		}
	}()

	bytes, err := os.ReadFile(handle.request.FirmwarePath)
	if err != nil {
		finalizeFailure(handle, protocol.CodeInvalidInput, 0, fmt.Sprintf("Failed to read firmware image: %v", err))
		return
	}

	if mockMode {
		runMockTransferTask(ctx, handle, intervalMs, bytes)
		return
	}

	config := protocol.SessionConfig{
		AllowUnsafe: true, BrickRiskAck: true, Experimental: handle.request.Experimental,
		RetryPolicy: protocol.DefaultRetryPolicy(), TimeoutProfile: protocol.DefaultTimeoutProfile(), TraceEnabled: true,
	}
	session, err := protocol.NewDeviceSession(ctx, transport, handle.request.VidPid, config)
	if err != nil {
		finalizeFailure(handle, transferFailureCode(handle.request.VidPid, err), 0, fmt.Sprintf("Failed to open device session: %v", err))
		return
	}

	totalChunks := max(handle.plan.ChunksTotal, 1)

	handle.eventsPublish("bootloader", 0, "Entering bootloader", false)
	if err := session.EnterBootloader(ctx); err != nil {
		finalizeFailure(handle, transferFailureCode(handle.request.VidPid, err), 0, fmt.Sprintf("Failed to enter bootloader: %v", err))
		_ = session.Close()
		return
	}

	for offset, idx := 0, 0; offset < len(bytes); offset, idx = offset+handle.plan.ChunkSize, idx+1 {
		if handle.cancelRequestedNow() {
			cancelRunningTransfer(ctx, handle, session, chunksSent)
			_ = session.Close()
			return
		}

		end := min(offset+handle.plan.ChunkSize, len(bytes))
		if _, err := session.SendFirmwareChunk(ctx, bytes[offset:end]); err != nil {
			code := transferFailureCode(handle.request.VidPid, err)
			var message string
			if code == protocol.CodeDeviceDisconnected {
				// Attempting a recovery exit-bootloader write against a
				// device that's confirmably gone would just fail again --
				// skip straight to reporting.
				message = fmt.Sprintf("Device disconnected during chunk %d transfer", idx+1)
			} else {
				message = firmwareFailureMessage(ctx, session, fmt.Sprintf("Failed to transfer chunk %d: %v", idx+1, err))
			}
			finalizeFailure(handle, code, chunksSent, message)
			_ = session.Close()
			return
		}

		chunksSent = idx + 1
		progress := (chunksSent * 100) / totalChunks
		handle.eventsPublish("transfer", progress, fmt.Sprintf("Transferred chunk %d/%d", chunksSent, totalChunks), false)
		if err := ctxSleepCore(ctx, time.Duration(intervalMs)*time.Millisecond); err != nil {
			_ = session.Close()
			return
		}
	}

	handle.eventsPublish("commit", 99, "Committing firmware", false)
	if err := session.FirmwareCommit(ctx); err != nil {
		code := transferFailureCode(handle.request.VidPid, err)
		var message string
		if code == protocol.CodeDeviceDisconnected {
			message = "Device disconnected during firmware commit"
		} else {
			message = firmwareFailureMessage(ctx, session, fmt.Sprintf("Firmware commit failed: %v", err))
		}
		finalizeFailure(handle, code, chunksSent, message)
		_ = session.Close()
		return
	}

	handle.eventsPublish("exit", 99, "Leaving bootloader", false)
	if err := session.ExitBootloader(ctx); err != nil {
		finalizeFailure(handle, transferFailureCode(handle.request.VidPid, err), chunksSent, fmt.Sprintf("Firmware applied but bootloader exit failed: %v", err))
		_ = session.Close()
		return
	}

	_ = session.Close()
	handle.eventsPublish("verify", 99, "Verifying firmware", false)
	if err := ctxSleepCore(ctx, time.Duration(intervalMs)*time.Millisecond); err != nil {
		return
	}

	finalizeCompleted(handle, chunksSent)
	handle.eventsPublish("completed", 100, "Firmware update completed", true)
}

func runMockTransferTask(ctx context.Context, handle *firmwareSessionHandle, intervalMs uint64, bytes []byte) {
	chunksSent := 0
	totalChunks := max(handle.plan.ChunksTotal, 1)

	for offset, idx := 0, 0; offset < len(bytes); offset, idx = offset+handle.plan.ChunkSize, idx+1 {
		if handle.cancelRequestedNow() {
			finalizeCancelled(handle, chunksSent)
			return
		}

		chunksSent = idx + 1
		progress := (chunksSent * 100) / totalChunks
		handle.eventsPublish("transfer", progress, fmt.Sprintf("Transferred chunk %d/%d", chunksSent, totalChunks), false)
		if err := ctxSleepCore(ctx, time.Duration(intervalMs)*time.Millisecond); err != nil {
			return
		}
	}

	handle.eventsPublish("verify", 99, "Verifying firmware", false)
	if err := ctxSleepCore(ctx, time.Duration(intervalMs)*time.Millisecond); err != nil {
		return
	}

	finalizeCompleted(handle, chunksSent)
	handle.eventsPublish("completed", 100, "Firmware update completed", true)
}

func finalizeCompleted(handle *firmwareSessionHandle, chunksSent int) {
	handle.mu.Lock()
	handle.state = stageCompleted
	handle.completedAt = time.Now()
	report := FirmwareFinalReport{
		SessionID: handle.plan.SessionID, Status: OutcomeCompleted, StartedAt: handle.startedAt,
		CompletedAt: handle.completedAt, BytesTotal: handle.plan.BytesTotal, ChunksTotal: handle.plan.ChunksTotal,
		ChunksSent: chunksSent, Message: "Firmware update completed",
	}
	handle.report = &report
	handle.mu.Unlock()
}

func finalizeFailure(handle *firmwareSessionHandle, code protocol.ErrorCode, chunksSent int, message string) {
	handle.mu.Lock()
	handle.state = stageFailed
	handle.completedAt = time.Now()
	report := FirmwareFinalReport{
		SessionID: handle.plan.SessionID, Status: OutcomeFailed, StartedAt: handle.startedAt,
		CompletedAt: handle.completedAt, BytesTotal: handle.plan.BytesTotal, ChunksTotal: handle.plan.ChunksTotal,
		ChunksSent: chunksSent, ErrorCode: code, Message: message,
	}
	handle.report = &report
	handle.mu.Unlock()
	handle.eventsPublish("failed", 100, message, true)
}

func finalizeCancelled(handle *firmwareSessionHandle, chunksSent int) {
	handle.mu.Lock()
	handle.state = stageCancelled
	handle.completedAt = time.Now()
	report := FirmwareFinalReport{
		SessionID: handle.plan.SessionID, Status: OutcomeCancelled, StartedAt: handle.startedAt,
		CompletedAt: handle.completedAt, BytesTotal: handle.plan.BytesTotal, ChunksTotal: handle.plan.ChunksTotal,
		ChunksSent: chunksSent, Message: "Firmware update cancelled",
	}
	handle.report = &report
	handle.mu.Unlock()
	handle.eventsPublish("cancelled", 100, "Firmware update cancelled", true)
}

func cancelRunningTransfer(ctx context.Context, handle *firmwareSessionHandle, session *protocol.DeviceSession, chunksSent int) {
	handle.eventsPublish("cancel_recovery", 100, "Cancelling transfer and leaving bootloader", false)

	if err := session.ExitBootloader(ctx); err != nil {
		finalizeFailure(handle, transferFailureCode(handle.request.VidPid, err), chunksSent, fmt.Sprintf("Firmware update cancelled but bootloader exit failed: %v", err))
		return
	}
	finalizeCancelled(handle, chunksSent)
}

// firmwareFailureMessage attempts a recovery exit-bootloader before
// reporting a mid-transfer failure, appending whether that recovery itself
// succeeded — mirrors the Rust implementation's best-effort recovery.
func firmwareFailureMessage(ctx context.Context, session *protocol.DeviceSession, base string) string {
	if err := session.ExitBootloader(ctx); err != nil {
		return fmt.Sprintf("%s; recovery exit failed: %v", base, err)
	}
	return base
}

func errorCodeOf(err error) protocol.ErrorCode {
	if pe, ok := err.(*protocol.Error); ok {
		return pe.Code()
	}
	return ""
}

// transferFailureCode classifies a firmware-transfer-step failure, checking
// whether the device is still physically present (via a fresh
// protocol.IsDevicePresent re-enumeration, not error-string guessing) before
// falling back to the underlying error's own code. A confirmed disconnect
// takes priority: it's more actionable and more honest than whatever
// low-level error a session write happens to surface once the device is
// already gone.
func transferFailureCode(target protocol.VidPid, err error) protocol.ErrorCode {
	return transferFailureCodeWithPresence(target, err, protocol.IsDevicePresent)
}

// transferFailureCodeWithPresence is transferFailureCode's testable core —
// the presence check is injected so tests can exercise both branches
// (device gone vs. device still there but some other error occurred)
// without needing real hardware to attach or detach for either case.
func transferFailureCodeWithPresence(target protocol.VidPid, err error, isPresent func(protocol.VidPid) bool) protocol.ErrorCode {
	if !isPresent(target) {
		return protocol.CodeDeviceDisconnected
	}
	return errorCodeOf(err)
}

// eventsPublish increments the sequence counter and publishes one progress
// event — the goroutine-side counterpart to OpenBitdoCore.emitEvent, used
// by code that only has the handle (not the core) in scope.
func (h *firmwareSessionHandle) eventsPublish(stage string, progress int, message string, terminal bool) {
	h.mu.Lock()
	h.sequence++
	event := FirmwareProgressEvent{
		SessionID: h.plan.SessionID, Sequence: h.sequence, Stage: stage,
		Progress: progress, Message: message, Terminal: terminal, Timestamp: time.Now(),
	}
	h.mu.Unlock()
	h.events.publish(event)
}

// ctxSleepCore sleeps for d or returns ctx.Err() early if ctx is cancelled.
func ctxSleepCore(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
