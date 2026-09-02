package tui

import (
	"context"

	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/input"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// Every command below wraps one internal/core (or internal/input) call as a
// tea.Cmd. The context passed in is the app's root context, cancelled on
// quit — long-running operations (firmware transfer) are intentionally
// detached inside internal/core itself, matching the documented behavior
// that a confirmed transfer keeps running even if the UI navigates away.

func cmdLoadDevices(ctx context.Context, c *core.OpenBitdoCore) tea.Cmd {
	return func() tea.Msg {
		devices, err := c.ListDevices(ctx)
		return devicesLoadedMsg{devices: devices, err: err}
	}
}

// cmdDiagProbeCached returns device's cached diagnostic result if this
// session already has one, or runs and caches a fresh probe if not — used
// whenever the user navigates to Diagnostics for a device, so revisiting one
// already probed this session shows instantly instead of re-running.
func cmdDiagProbeCached(ctx context.Context, c *core.OpenBitdoCore, device core.AppDevice) tea.Cmd {
	return func() tea.Msg {
		entry, err := c.DiagProbeCached(ctx, device)
		return diagResultMsg{result: entry.Result, ranAt: entry.RanAt, err: err}
	}
}

// cmdDiagProbeFresh always runs a new probe and replaces the cache entry —
// used for the Diagnostics screen's explicit "r" rerun, so caching never
// blocks getting a genuinely fresh result on demand.
func cmdDiagProbeFresh(ctx context.Context, c *core.OpenBitdoCore, device core.AppDevice) tea.Cmd {
	return func() tea.Msg {
		entry, err := c.DiagProbeFresh(ctx, device)
		return diagResultMsg{result: entry.Result, ranAt: entry.RanAt, err: err}
	}
}

// cmdAutoDiagnose runs in the background (never user-triggered directly)
// whenever a device is loaded or freshly hotplug-connected and hasn't been
// diagnosed yet this session — DiagProbe/its wrappers only issue read-only
// SafeRead HID commands, so this is safe to run without confirmation, unlike
// anything write/firmware-tier. Reports via autoDiagResultMsg rather than
// diagResultMsg so its handler can tell it apart from a user-triggered probe
// and only update the live Diagnostics view if that's actually what's shown.
func cmdAutoDiagnose(ctx context.Context, c *core.OpenBitdoCore, device core.AppDevice) tea.Cmd {
	return func() tea.Msg {
		entry, err := c.DiagProbeCached(ctx, device)
		return autoDiagResultMsg{device: device, result: entry.Result, ranAt: entry.RanAt, err: err}
	}
}

func cmdJP108ReadMapping(ctx context.Context, c *core.OpenBitdoCore, target protocol.VidPid) tea.Cmd {
	return func() tea.Msg {
		mappings, err := c.JP108ReadDedicatedMapping(ctx, target)
		return jp108MappingLoadedMsg{mappings: mappings, err: err}
	}
}

func cmdJP108Apply(ctx context.Context, c *core.OpenBitdoCore, target protocol.VidPid, changes []core.DedicatedButtonMapping) tea.Cmd {
	return func() tea.Msg {
		report, err := c.JP108ApplyDedicatedMappingWithRecovery(ctx, target, changes, true)
		return jp108ApplyResultMsg{report: report, err: err}
	}
}

func cmdU2ReadProfile(ctx context.Context, c *core.OpenBitdoCore, target protocol.VidPid, slot core.U2SlotID) tea.Cmd {
	return func() tea.Msg {
		profile, err := c.U2ReadCoreProfile(ctx, target, slot)
		return u2ProfileLoadedMsg{profile: profile, err: err}
	}
}

func cmdU2PreviewSlot(ctx context.Context, c *core.OpenBitdoCore, target protocol.VidPid, slot core.U2SlotID) tea.Cmd {
	return func() tea.Msg {
		profile, err := c.U2PreviewSlot(ctx, target, slot)
		return u2SlotPreviewMsg{slot: slot, profile: profile, err: err}
	}
}

func cmdU2Apply(ctx context.Context, c *core.OpenBitdoCore, target protocol.VidPid, slot core.U2SlotID, mode byte, changes []core.U2ButtonMapping, l2, r2 float32) tea.Cmd {
	return func() tea.Msg {
		report, err := c.U2ApplyCoreProfileWithRecovery(ctx, target, slot, mode, changes, l2, r2, true)
		return u2ApplyResultMsg{report: report, err: err}
	}
}

func cmdCandidateProbe(ctx context.Context, c *core.OpenBitdoCore, device core.AppDevice, policy core.RuntimeUnlockPolicy) tea.Cmd {
	return func() tea.Msg {
		report, err := c.CandidateWriteProbe(ctx, device.VidPid, policy)
		return candidateProbeResultMsg{device: device, report: report, err: err}
	}
}

func cmdRestoreBackup(ctx context.Context, c *core.OpenBitdoCore, id core.ConfigBackupID) tea.Cmd {
	return func() tea.Msg {
		err := c.RestoreBackup(ctx, id)
		return restoreBackupResultMsg{err: err}
	}
}

func cmdDownloadFirmware(ctx context.Context, c *core.OpenBitdoCore, target protocol.VidPid) tea.Cmd {
	return func() tea.Msg {
		result, err := c.DownloadRecommendedFirmware(ctx, target)
		return firmwareDownloadedMsg{result: result, err: err}
	}
}

func cmdPreflightFirmware(ctx context.Context, c *core.OpenBitdoCore, req core.FirmwarePreflightRequest) tea.Cmd {
	return func() tea.Msg {
		result, err := c.PreflightFirmware(ctx, req)
		return firmwarePreflightMsg{result: result, err: err}
	}
}

func cmdStartFirmware(ctx context.Context, c *core.OpenBitdoCore, sessionID core.FirmwareUpdateSessionID) tea.Cmd {
	return func() tea.Msg {
		plan, err := c.StartFirmware(ctx, core.FirmwareStartRequest{SessionID: sessionID})
		return firmwareStartedMsg{plan: plan, err: err}
	}
}

func cmdConfirmFirmware(ctx context.Context, c *core.OpenBitdoCore, sessionID core.FirmwareUpdateSessionID) tea.Cmd {
	return func() tea.Msg {
		plan, err := c.ConfirmFirmware(ctx, core.FirmwareConfirmRequest{SessionID: sessionID, AcknowledgedRisk: true})
		return firmwareConfirmedMsg{plan: plan, err: err}
	}
}

func cmdCancelFirmware(ctx context.Context, c *core.OpenBitdoCore, sessionID core.FirmwareUpdateSessionID) tea.Cmd {
	return func() tea.Msg {
		report, err := c.CancelFirmware(ctx, core.FirmwareCancelRequest{SessionID: sessionID})
		if err != nil {
			return nil
		}
		return firmwareReportMsg{report: report}
	}
}

// cmdListenFirmwareEvents re-arms itself on every message so the caller
// only needs to issue it once per subscription.
func cmdListenFirmwareEvents(ch <-chan core.FirmwareProgressEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return firmwareEventsClosedMsg{}
		}
		return firmwareProgressMsg{event: event}
	}
}

// cmdListenNav bridges internal/input's gamepad nav event channel into
// Bubbletea messages; re-arms itself the same way.
func cmdListenNav(events <-chan input.NavEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return navClosedMsg{}
		}
		return navEventMsg{event: event}
	}
}

func cmdSaveSettings(path string, s Settings) tea.Cmd {
	return func() tea.Msg {
		return settingsSavedMsg{err: saveSettings(path, s)}
	}
}

func cmdSaveReport(mode ReportSaveMode, settingsPath, operation string, device *core.AppDevice, status, message string,
	diag *protocol.DiagProbeResult, firmware *core.FirmwareFinalReport, runtimeUnlock *core.RuntimeUnlockReport) tea.Cmd {
	return func() tea.Msg {
		path, err := persistSupportReport(mode, settingsPath, operation, device, status, message, diag, firmware, runtimeUnlock)
		return reportSavedMsg{path: path, err: err}
	}
}
