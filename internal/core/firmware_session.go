package core

import (
	"sync"
	"time"
)

type firmwareSessionState int

const (
	stagePreflight firmwareSessionState = iota
	stageAwaitingConfirmation
	stageRunning
	stageCompleted
	stageCancelled
	stageFailed
)

// firmwareSessionHandle tracks one in-flight (or completed) firmware update
// from preflight through its terminal report.
type firmwareSessionHandle struct {
	request FirmwarePreflightRequest
	plan    FirmwareUpdatePlan
	events  *broadcaster

	mu              sync.Mutex
	state           firmwareSessionState
	sequence        uint64
	cancelRequested bool
	report          *FirmwareFinalReport
	startedAt       time.Time
	completedAt     time.Time
}

func (h *firmwareSessionHandle) cancelRequestedNow() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cancelRequested
}
