package tui

import (
	"time"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

type devicesLoadedMsg struct {
	devices []core.AppDevice
	err     error
}

type diagResultMsg struct {
	result protocol.DiagProbeResult
	err    error
}

type jp108MappingLoadedMsg struct {
	mappings []core.DedicatedButtonMapping
	err      error
}

type jp108ApplyResultMsg struct {
	report core.WriteRecoveryReport
	err    error
}

type u2ProfileLoadedMsg struct {
	profile core.U2CoreProfile
	err     error
}

type u2ApplyResultMsg struct {
	report core.WriteRecoveryReport
	err    error
}

type candidateProbeResultMsg struct {
	report core.RuntimeUnlockReport
	err    error
}

// firmwareBeginMsg and candidateProbeBeginMsg are cross-screen transition
// triggers: either dispatched directly (risk already acknowledged this
// session) or as a modal's onConfirm (risk just acknowledged). Reaching
// their handler in app.go always means the ack gate is satisfied.
type firmwareBeginMsg struct {
	device core.AppDevice
}

type candidateProbeBeginMsg struct {
	device core.AppDevice
}

type restoreBackupResultMsg struct {
	err error
}

type firmwareDownloadedMsg struct {
	result core.FirmwareDownloadResult
	err    error
}

type firmwarePreflightMsg struct {
	result core.FirmwarePreflightResult
	err    error
}

type firmwareStartedMsg struct {
	plan core.FirmwareUpdatePlan
	err  error
}

type firmwareConfirmedMsg struct {
	plan core.FirmwareUpdatePlan
	err  error
}

type firmwareProgressMsg struct {
	event core.FirmwareProgressEvent
}

type firmwareEventsClosedMsg struct{}

type firmwareReportMsg struct {
	report core.FirmwareFinalReport
}

type navEventMsg struct {
	event input.NavEvent
}

type navClosedMsg struct{}

type reportSavedMsg struct {
	path string
	err  error
}

type settingsSavedMsg struct {
	err error
}

// blinkMsg drives the modal/status-line's lightweight animation tick.
type blinkMsg time.Time
