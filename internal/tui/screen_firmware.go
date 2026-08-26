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

func (m Model) viewFirmware(height int) string {
	var b strings.Builder
	b.WriteString(stylePanelTitle.Render("Firmware Update: "+m.fw.device.Name) + "\n\n")

	switch m.fw.stage {
	case fwStageDownloading:
		b.WriteString(styleFaint.Render("Downloading and verifying firmware…"))
	case fwStagePreflighting:
		b.WriteString(styleFaint.Render("Checking safety gates and computing transfer plan…"))
	case fwStageDenied:
		b.WriteString(styleDanger.Render("Blocked: ") + m.fw.deniedMsg)
	case fwStageError:
		b.WriteString(styleDanger.Render(fmt.Sprintf("Error: %v", m.fw.err)))
	case fwStageReadyToConfirm:
		plan := m.fw.preflight.Plan
		fmt.Fprintf(&b, "Image: %s (%d bytes, sha256 %s)\n", m.fw.download.Version, plan.BytesTotal, shortHash(plan.ImageSHA256))
		fmt.Fprintf(&b, "Chunks: %d × %d bytes  ·  estimated %ds\n\n", plan.ChunksTotal, plan.ChunkSize, plan.ExpectedSeconds)
		for _, w := range plan.Warnings {
			b.WriteString(styleWarning.Render("⚠ "+w) + "\n")
		}
		b.WriteString("\n" + stylePositive.Render("Press enter to begin — do not disconnect the device."))
	case fwStageConfirming:
		b.WriteString(styleFaint.Render("Starting transfer…"))
	case fwStageRunning:
		b.WriteString(progressBar(m.fw.progress, 40) + fmt.Sprintf(" %d%%\n", m.fw.progress))
		b.WriteString(styleFaint.Render(m.fw.progressMsg) + "\n\n")
		b.WriteString(styleHelp.Render("c cancel · transfer continues even if you leave this screen"))
	case fwStageDone:
		report := m.fw.finalReport
		switch report.Status {
		case core.OutcomeCompleted:
			b.WriteString(stylePositive.Render("Update completed and verified."))
			if report.ObservedVersion != "" {
				b.WriteString("\n" + styleFaint.Render("Device now reports: "+report.ObservedVersion))
			}
		case core.OutcomeCancelled:
			b.WriteString(styleWarning.Render("Update cancelled."))
		case core.OutcomeCompletedUnverified:
			b.WriteString(styleWarning.Render("Update completed, but could not be verified."))
			b.WriteString("\n" + styleFaint.Render(report.Message))
			b.WriteString("\n" + styleWarning.Render("The transfer reported no error, but the device did not confirm it's running the new firmware — check that it powers on and responds normally before trusting this update."))
		default:
			b.WriteString(styleDanger.Render("Update failed: " + report.Message))
			if report.ErrorCode == protocol.CodeDeviceDisconnected {
				b.WriteString("\n" + styleFaint.Render("Reconnect the device, then go back and press r on the dashboard to rescan."))
			}
		}
		fmt.Fprintf(&b, "\n%d/%d chunks sent.\n\n", report.ChunksSent, report.ChunksTotal)
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
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return styleAccent.Render(bar)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
