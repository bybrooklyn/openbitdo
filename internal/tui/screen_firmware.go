package tui

import (
	"fmt"
	"strings"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type fwStage int

const (
	fwStageDownloading fwStage = iota
	fwStagePreflighting
	fwStageDenied
	fwStageReadyToConfirm
	fwStageConfirming
	fwStageRunning
	fwStageDone
	fwStageError
)

type firmwareState struct {
	device      core.AppDevice
	stage       fwStage
	err         error
	deniedMsg   string
	download    core.FirmwareDownloadResult
	preflight   core.FirmwarePreflightResult
	sessionID   core.FirmwareUpdateSessionID
	eventsChan  <-chan core.FirmwareProgressEvent
	progress    int
	progressMsg string
	finalReport core.FirmwareFinalReport
}

func newFirmwareState() firmwareState { return firmwareState{} }

func (m Model) updateFirmware(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case firmwareDownloadedMsg:
		if msg.err != nil {
			m.fw.stage, m.fw.err = fwStageError, msg.err
			return m, nil
		}
		m.fw.download = msg.result
		m.fw.stage = fwStagePreflighting
		req := core.FirmwarePreflightRequest{
			VidPid: m.fw.device.VidPid, FirmwarePath: msg.result.FirmwarePath,
			AllowUnsafe: true, BrickRiskAck: true, Experimental: m.advancedMode,
		}
		return m, cmdPreflightFirmware(m.ctx, m.core, req)

	case firmwarePreflightMsg:
		if msg.err != nil {
			m.fw.stage, m.fw.err = fwStageError, msg.err
			return m, nil
		}
		if !msg.result.Gate.Allowed {
			m.fw.stage, m.fw.deniedMsg = fwStageDenied, msg.result.Gate.Message
			return m, nil
		}
		m.fw.preflight = msg.result
		m.fw.sessionID = msg.result.Plan.SessionID
		m.fw.stage = fwStageReadyToConfirm
		return m, nil

	case firmwareStartedMsg:
		if msg.err != nil {
			m.fw.stage, m.fw.err = fwStageError, msg.err
			return m, nil
		}
		return m, cmdConfirmFirmware(m.ctx, m.core, m.fw.sessionID)

	case firmwareConfirmedMsg:
		if msg.err != nil {
			m.fw.stage, m.fw.err = fwStageError, msg.err
			return m, nil
		}
		m.fw.stage = fwStageRunning
		ch, err := m.core.SubscribeEvents(m.fw.sessionID)
		if err != nil {
			m.fw.stage, m.fw.err = fwStageError, err
			return m, nil
		}
		m.fw.eventsChan = ch
		return m, cmdListenFirmwareEvents(ch)

	case firmwareProgressMsg:
		m.fw.progress = msg.event.Progress
		m.fw.progressMsg = msg.event.Message
		if !msg.event.Terminal {
			return m, cmdListenFirmwareEvents(m.fw.eventsChan)
		}
		report, _ := m.core.FirmwareReport(m.ctx, m.fw.sessionID)
		return m.finishFirmware(report)

	case firmwareEventsClosedMsg:
		return m, nil

	case firmwareReportMsg:
		return m.finishFirmware(&msg.report)

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if m.fw.stage == fwStageRunning {
				return m, nil // background transfer keeps running; esc doesn't abandon it
			}
			m.screen = screenDevices
			return m, nil
		case "enter":
			if m.fw.stage == fwStageReadyToConfirm {
				m.fw.stage = fwStageConfirming
				return m, cmdStartFirmware(m.ctx, m.core, m.fw.sessionID)
			}
			if m.fw.stage == fwStageDone || m.fw.stage == fwStageError || m.fw.stage == fwStageDenied {
				m.screen = screenDevices
			}
		case "c":
			if m.fw.stage == fwStageRunning {
				return m, cmdCancelFirmware(m.ctx, m.core, m.fw.sessionID)
			}
		}
	}
	return m, nil
}

func (m Model) finishFirmware(report *core.FirmwareFinalReport) (tea.Model, tea.Cmd) {
	m.fw.stage = fwStageDone
	if report != nil {
		m.fw.finalReport = *report
	}
	device := m.fw.device
	status := "ok"
	message := "Firmware update completed."
	if report != nil {
		switch report.Status {
		case core.OutcomeFailed:
			status, message = "attention", "Firmware update failed: "+report.Message
		case core.OutcomeCancelled:
			status, message = "cancelled", "Firmware update cancelled."
		case core.OutcomeCompletedUnverified:
			status, message = "attention", "Firmware update completed but could not be verified: "+report.Message
		}
	}
	return m, cmdSaveReport(m.settings.ReportSaveMode, m.settingsPath, "firmware-update", &device, status, message, nil, report, nil)
}

// fwSteps is the fixed sequence shown by the stage indicator. Denied/Error
// are exceptional exits, not steps in the happy path, so they're excluded
// here and handled as their own unstyled-breadcrumb case in viewFirmware.
var fwSteps = []string{"Download", "Verify", "Confirm", "Transfer", "Done"}

// currentFwStepIndex maps a fwStage to its 0-based index into fwSteps, or -1
// for the two stages (Denied, Error) that aren't part of the linear
// happy-path sequence a breadcrumb can meaningfully represent.
func currentFwStepIndex(stage fwStage) int {
	switch stage {
	case fwStageDownloading:
		return 0
	case fwStagePreflighting:
		return 1
	case fwStageReadyToConfirm:
		return 2
	case fwStageConfirming, fwStageRunning:
		return 3
	case fwStageDone:
		return 4
	default:
		return -1
	}
}

// renderFwStageIndicator gives the flow visual continuity across its six
// stages — without it, each stage's text abruptly replaces the last with no
// sense of progress through what can be a multi-second download+verify+
// transfer sequence, which is exactly the "disconnected" feeling reported
// against this screen. Completed steps get IconPass, the current step gets
// IconInProgress and the accent color, upcoming steps are faint.
func renderFwStageIndicator(stage fwStage) string {
	idx := currentFwStepIndex(stage)
	if idx < 0 {
		return ""
	}
	parts := make([]string, len(fwSteps))
	for i, label := range fwSteps {
		switch {
		// Reaching fwStageDone means the pipeline itself ran to completion --
		// the final "Done" step is a destination, not a task that can be "in
		// progress," so it gets IconPass like every earlier step rather than
		// the IconInProgress diamond (which would otherwise read as "still
		// working" on a screen that has, in fact, finished). Whether the
		// *outcome* was success/failure/cancelled is a separate signal,
		// carried by the IconPass/IconWarn/IconFail line in the body below --
		// this breadcrumb only tracks how far the flow got.
		case i < idx || (i == idx && stage == fwStageDone):
			parts[i] = stylePositive.Render(IconPass + " " + label)
		case i == idx:
			parts[i] = styleAccent.Render(IconInProgress + " " + label)
		default:
			parts[i] = styleFaint.Render(label)
		}
	}
	return strings.Join(parts, styleFaint.Render("  →  ")) + "\n\n"
}

func (m Model) viewFirmware(height int) string {
	var b strings.Builder
	b.WriteString(stylePanelTitle.Render("Firmware Update: "+m.fw.device.Name) + "\n\n")
	b.WriteString(renderFwStageIndicator(m.fw.stage))

	switch m.fw.stage {
	case fwStageDownloading:
		b.WriteString(styleFaint.Render("Downloading and verifying firmware…"))
	case fwStagePreflighting:
		b.WriteString(styleFaint.Render("Checking safety gates and computing transfer plan…"))
	case fwStageDenied:
		b.WriteString(styleDangerBlock.Render(styleDanger.Render(IconFail+" Blocked: ") + m.fw.deniedMsg))
	case fwStageError:
		b.WriteString(styleDangerBlock.Render(styleDanger.Render(fmt.Sprintf(IconFail+" Error: %v", m.fw.err))))
	case fwStageReadyToConfirm:
		plan := m.fw.preflight.Plan
		// Blocks joined by exactly one blank line each, rather than each
		// piece manually tracking its own leading/trailing newlines — the
		// latter previously produced a double blank line whenever
		// plan.Warnings was empty (both the Chunks line's own trailing
		// blank and the "Press enter" line's leading blank applied).
		//
		// Kept as an inline (not modal) confirmation deliberately: the
		// brick-risk acknowledgement modal already ran before this screen
		// was even entered (see riskAckModal in screen_devices.go) — this
		// step shows the actual computed plan (image size, chunk count,
		// per-warning detail), which needs more width/height than the
		// modal system's ~60-column cap comfortably gives. What was
		// missing wasn't "this should be a modal," it was that the content
		// rendered as bare unstyled text instead of using the same
		// left-bar block treatment every other confirmation-adjacent block
		// in this app uses — that inconsistency, not the inline placement,
		// was the actual source of the "disconnected" feeling.
		var blocks []string
		blocks = append(blocks, styleAccentBlock.Render(fmt.Sprintf("Image: %s (%d bytes, sha256 %s)\nChunks: %d × %d bytes  ·  estimated %ds",
			m.fw.download.Version, plan.BytesTotal, shortHash(plan.ImageSHA256),
			plan.ChunksTotal, plan.ChunkSize, plan.ExpectedSeconds)))
		if len(plan.Warnings) > 0 {
			var warnings strings.Builder
			for i, w := range plan.Warnings {
				if i > 0 {
					warnings.WriteString("\n")
				}
				warnings.WriteString(styleWarning.Render(IconWarn + " " + w))
			}
			blocks = append(blocks, styleWarningBlock.Render(warnings.String()))
		}
		blocks = append(blocks, stylePositiveBlock.Render(stylePositive.Render("Press enter to begin — do not disconnect the device.")))
		b.WriteString(strings.Join(blocks, "\n\n"))
	case fwStageConfirming:
		b.WriteString(styleFaint.Render("Starting transfer…"))
	case fwStageRunning:
		b.WriteString(progressBar(m.fw.progress, 40) + " " + styleFaint.Render(fmt.Sprintf("%d%%", m.fw.progress)) + "\n")
		b.WriteString(styleFaint.Render(m.fw.progressMsg) + "\n\n")
		b.WriteString(styleHelp.Render("c cancel · transfer continues even if you leave this screen"))
	case fwStageDone:
		report := m.fw.finalReport
		switch report.Status {
		case core.OutcomeCompleted:
			b.WriteString(stylePositive.Render(IconPass + " Update completed and verified."))
			if report.ObservedVersion != "" {
				b.WriteString("\n" + styleFaint.Render("Device now reports: "+report.ObservedVersion))
			}
		case core.OutcomeCancelled:
			b.WriteString(styleWarning.Render(IconWarn + " Update cancelled."))
		case core.OutcomeCompletedUnverified:
			b.WriteString(styleWarning.Render(IconWarn + " Update completed, but could not be verified."))
			b.WriteString("\n" + styleFaint.Render(report.Message))
			b.WriteString("\n" + styleWarning.Render("The transfer reported no error, but the device did not confirm it's running the new firmware — check that it powers on and responds normally before trusting this update."))
		default:
			b.WriteString(styleDanger.Render(IconFail + " Update failed: " + report.Message))
			if report.ErrorCode == protocol.CodeDeviceDisconnected {
				b.WriteString("\n" + styleFaint.Render("Reconnect the device, then go back and press r on the dashboard to rescan."))
			}
		}
		b.WriteString("\n" + styleFaint.Render(fmt.Sprintf("%d/%d chunks sent.", report.ChunksSent, report.ChunksTotal)) + "\n\n")
		b.WriteString(styleHelp.Render("enter/esc to return"))
	}

	return stylePanel.Width(m.width - 2).Height(height - 2).Render(b.String())
}

func progressBar(percent, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := width * percent / 100
	bar := strings.Repeat(IconProgressFilled, filled) + strings.Repeat(IconProgressEmpty, width-filled)
	return styleAccent.Render(bar)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
