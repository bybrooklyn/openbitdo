package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/bybrooklyn/openbitdo/internal/core"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

// TOML support reports are the tool's only user-facing report format —
// there is no JSON output path. Field names mirror the prior Rust TUI's
// schema_version=2 shape (same snake_case keys) so existing shared reports
// stay legible; the schema is authored fresh in Go rather than sharing a
// struct with internal/core, matching the original design where reporting
// lived in the UI layer, not the core façade.

const (
	reportSchemaVersion = 2
	reportMaxCount      = 20
	reportMaxAgeDays    = 30
)

type supportReport struct {
	SchemaVersion  int                  `toml:"schema_version"`
	GeneratedAtUTC string               `toml:"generated_at_utc"`
	Operation      string               `toml:"operation"`
	Status         string               `toml:"status"`
	Message        string               `toml:"message"`
	Device         *reportDevice        `toml:"device,omitempty"`
	Scorecard      *reportScorecard     `toml:"scorecard,omitempty"`
	Diag           *reportDiag          `toml:"diag,omitempty"`
	Firmware       *reportFirmware      `toml:"firmware,omitempty"`
	RuntimeUnlock  *reportRuntimeUnlock `toml:"runtime_unlock,omitempty"`
}

type reportDevice struct {
	VID               uint16   `toml:"vid"`
	PID               uint16   `toml:"pid"`
	Name              string   `toml:"name"`
	CanonicalID       string   `toml:"canonical_id"`
	RuntimeLabel      string   `toml:"runtime_label"`
	Serial            string   `toml:"serial,omitempty"`
	SupportLevel      string   `toml:"support_level"`
	SupportTier       string   `toml:"support_tier"`
	ProtocolFamily    string   `toml:"protocol_family"`
	Evidence          string   `toml:"evidence"`
	WorksNow          []string `toml:"works_now"`
	BlockedOperations []string `toml:"blocked_operations"`
	MissingEvidence   []string `toml:"missing_evidence"`
}

type reportVidPid struct {
	VID uint16 `toml:"vid"`
	PID uint16 `toml:"pid"`
}

type reportScorecard struct {
	SupportTier             string       `toml:"support_tier"`
	StaticEvidence          string       `toml:"static_evidence"`
	RuntimeEvidence         string       `toml:"runtime_evidence"`
	HardwareConfirmation    string       `toml:"hardware_confirmation"`
	SafeReadCoverage        string       `toml:"safe_read_coverage"`
	SafeWriteReadiness      string       `toml:"safe_write_readiness"`
	BackupReadbackReadiness string       `toml:"backup_readback_readiness"`
	FirmwareStatus          string       `toml:"firmware_status"`
	ScorePercent            int          `toml:"score_percent"`
	PromotionReady          bool         `toml:"promotion_ready"`
	MissingEvidence         []string     `toml:"missing_evidence"`
	VidPid                  reportVidPid `toml:"vid_pid"`
}

type reportDiagCapability struct {
	SupportsMode              bool `toml:"supports_mode"`
	SupportsProfileRW         bool `toml:"supports_profile_rw"`
	SupportsBoot              bool `toml:"supports_boot"`
	SupportsFirmware          bool `toml:"supports_firmware"`
	SupportsJP108DedicatedMap bool `toml:"supports_jp108_dedicated_map"`
	SupportsU2SlotConfig      bool `toml:"supports_u2_slot_config"`
	SupportsU2ButtonMap       bool `toml:"supports_u2_button_map"`
}

type reportCommandCheck struct {
	Command        string            `toml:"command"`
	OK             bool              `toml:"ok"`
	Confidence     string            `toml:"confidence"`
	IsExperimental bool              `toml:"is_experimental"`
	Severity       string            `toml:"severity"`
	Attempts       uint8             `toml:"attempts"`
	Validator      string            `toml:"validator"`
	ResponseStatus string            `toml:"response_status"`
	BytesWritten   int               `toml:"bytes_written"`
	BytesRead      int               `toml:"bytes_read"`
	ErrorCode      string            `toml:"error_code,omitempty"`
	Detail         string            `toml:"detail"`
	ParsedFacts    map[string]uint32 `toml:"parsed_facts"`
}

type reportDiag struct {
	ProfileName    string               `toml:"profile_name"`
	SupportLevel   string               `toml:"support_level"`
	SupportTier    string               `toml:"support_tier"`
	ProtocolFamily string               `toml:"protocol_family"`
	Evidence       string               `toml:"evidence"`
	TransportReady bool                 `toml:"transport_ready"`
	Target         reportVidPid         `toml:"target"`
	Capability     reportDiagCapability `toml:"capability"`
	CommandChecks  []reportCommandCheck `toml:"command_checks"`
}

type reportFirmware struct {
	SessionID   string `toml:"session_id"`
	Status      string `toml:"status"`
	BytesTotal  int    `toml:"bytes_total"`
	ChunksTotal int    `toml:"chunks_total"`
	ChunksSent  int    `toml:"chunks_sent"`
	ErrorCode   string `toml:"error_code,omitempty"`
	Message     string `toml:"message"`
}

type reportRuntimeUnlock struct {
	Allowed           bool     `toml:"allowed"`
	Operation         string   `toml:"operation"`
	CommandsAttempted []string `toml:"commands_attempted"`
	WriteApplied      bool     `toml:"write_applied"`
	ReadbackVerified  bool     `toml:"readback_verified"`
	WriteLockRequired bool     `toml:"write_lock_required"`
	Message           string   `toml:"message"`
}

func deviceToReport(d core.AppDevice) *reportDevice {
	status := d.SupportStatus()
	return &reportDevice{
		VID: d.VidPid.VID, PID: d.VidPid.PID, Name: d.Name, CanonicalID: d.Name,
		RuntimeLabel: string(status), Serial: d.Serial,
		SupportLevel: string(d.SupportLevel), SupportTier: string(d.SupportTier),
		ProtocolFamily: string(d.ProtocolFamily), Evidence: string(d.Evidence),
		WorksNow:          worksNowFor(d),
		BlockedOperations: blockedOperationsFor(d),
		MissingEvidence:   d.Scorecard().MissingEvidence,
	}
}

func worksNowFor(d core.AppDevice) []string {
	out := []string{"safe diagnostics", "support report generation", "device identification"}
	if d.Capability.SupportsMode {
		out = append(out, "mode read/switch where policy allows")
	}
	if d.Capability.SupportsProfileRW {
		out = append(out, "profile read/write where policy allows")
	}
	if d.SupportTier == protocol.TierFull {
		if d.Capability.SupportsJP108DedicatedMap || (d.Capability.SupportsU2ButtonMap && d.Capability.SupportsU2SlotConfig) {
			out = append(out, "confirmed mapping editor")
		}
		if d.Capability.SupportsFirmware {
			out = append(out, "firmware update")
		}
	}
	return out
}

func blockedOperationsFor(d core.AppDevice) []string {
	switch d.SupportTier {
	case protocol.TierCandidateReadOnly:
		return []string{
			"firmware writes blocked until runtime traces are confirmed",
			"mapping/profile writes blocked until hardware read/write/readback passes",
		}
	case protocol.TierDetectOnly:
		return []string{"diagnostics beyond identification, firmware, mapping, and writes are unavailable"}
	default:
		var out []string
		if !d.Capability.SupportsFirmware {
			out = append(out, "firmware: no verified path for this PID")
		}
		if !(d.Capability.SupportsJP108DedicatedMap || (d.Capability.SupportsU2ButtonMap && d.Capability.SupportsU2SlotConfig)) {
			out = append(out, "mapping: no confirmed editor for this PID")
		}
		return out
	}
}

func scorecardToReport(s core.SupportScorecard) *reportScorecard {
	return &reportScorecard{
		SupportTier: string(s.SupportTier), StaticEvidence: string(s.StaticEvidence),
		RuntimeEvidence: string(s.RuntimeEvidence), HardwareConfirmation: string(s.HardwareConfirmation),
		SafeReadCoverage: string(s.SafeReadCoverage), SafeWriteReadiness: string(s.SafeWriteReadiness),
		BackupReadbackReadiness: string(s.BackupReadbackReadiness), FirmwareStatus: string(s.FirmwareStatus),
		ScorePercent: s.ScorePercent, PromotionReady: s.PromotionReady, MissingEvidence: s.MissingEvidence,
		VidPid: reportVidPid{VID: s.VidPid.VID, PID: s.VidPid.PID},
	}
}

func diagToReport(d protocol.DiagProbeResult) *reportDiag {
	checks := make([]reportCommandCheck, 0, len(d.CommandChecks))
	for _, c := range d.CommandChecks {
		checks = append(checks, reportCommandCheck{
			Command: string(c.Command), OK: c.OK, Confidence: string(c.Confidence),
			IsExperimental: c.IsExperimental, Severity: string(c.Severity), Attempts: c.Attempts,
			Validator: c.Validator, ResponseStatus: string(c.ResponseStatus), BytesWritten: c.BytesWritten,
			BytesRead: c.BytesRead, ErrorCode: string(c.ErrorCode), Detail: c.Detail, ParsedFacts: c.ParsedFacts,
		})
	}
	return &reportDiag{
		ProfileName: d.ProfileName, SupportLevel: string(d.SupportLevel), SupportTier: string(d.SupportTier),
		ProtocolFamily: string(d.ProtocolFamily), Evidence: string(d.Evidence), TransportReady: d.TransportReady,
		Target:        reportVidPid{VID: d.Target.VID, PID: d.Target.PID},
		Capability:    reportDiagCapability(d.Capability),
		CommandChecks: checks,
	}
}

func firmwareToReport(f core.FirmwareFinalReport) *reportFirmware {
	return &reportFirmware{
		SessionID: string(f.SessionID), Status: string(f.Status), BytesTotal: f.BytesTotal,
		ChunksTotal: f.ChunksTotal, ChunksSent: f.ChunksSent, ErrorCode: string(f.ErrorCode), Message: f.Message,
	}
}

func runtimeUnlockToReport(r core.RuntimeUnlockReport) *reportRuntimeUnlock {
	return &reportRuntimeUnlock{
		Allowed: r.Allowed, Operation: r.Operation, CommandsAttempted: r.CommandsAttempted,
		WriteApplied: r.WriteApplied, ReadbackVerified: r.ReadbackVerified,
		WriteLockRequired: r.WriteLockRequired, Message: r.Message,
	}
}

// reportsDir mirrors the settings config directory's parent — reports live
// alongside config.toml under a "reports" subdirectory. Takes settingsPath
// explicitly (see unlock.go) so it follows the same injectable path the
// rest of the app uses instead of always resolving to the real OS config
// directory.
func reportsDir(settingsPath string) string {
	return filepath.Join(filepath.Dir(settingsPath), "reports")
}

// persistSupportReport writes a TOML report and prunes old ones (cap 20
// files / 30 days, same retention as the prior Rust TUI), gated by mode.
func persistSupportReport(mode ReportSaveMode, settingsPath, operation string, device *core.AppDevice, status, message string,
	diag *protocol.DiagProbeResult, firmware *core.FirmwareFinalReport, runtimeUnlock *core.RuntimeUnlockReport) (string, error) {

	failed := status != "ok" && status != "passed"
	switch mode {
	case ReportSaveOff:
		return "", nil
	case ReportSaveFailureOnly:
		if !failed {
			return "", nil
		}
	}

	report := supportReport{
		SchemaVersion: reportSchemaVersion, GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		Operation: operation, Status: status, Message: message,
	}
	if device != nil {
		report.Device = deviceToReport(*device)
		sc := device.Scorecard()
		report.Scorecard = scorecardToReport(sc)
	}
	if diag != nil {
		report.Diag = diagToReport(*diag)
	}
	if firmware != nil {
		report.Firmware = firmwareToReport(*firmware)
	}
	if runtimeUnlock != nil {
		report.RuntimeUnlock = runtimeUnlockToReport(*runtimeUnlock)
	}

	dir := reportsDir(settingsPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	token := "unknown"
	if device != nil {
		if device.Serial != "" {
			token = device.Serial
		} else {
			token = fmt.Sprintf("%04x_%04x", device.VidPid.VID, device.VidPid.PID)
		}
	}
	filename := fmt.Sprintf("%s_%s_%s.toml", time.Now().UTC().Format("20060102T150405Z"), operation, sanitizeToken(token))
	path := filepath.Join(dir, filename)

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if err := toml.NewEncoder(f).Encode(report); err != nil {
		return "", err
	}

	pruneReports(dir)
	return path, nil
}

func sanitizeToken(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// pruneReports enforces the 20-file / 30-day retention window, deleting the
// oldest reports first once either limit is exceeded.
func pruneReports(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	files := make([]fileInfo, 0, len(entries))
	cutoff := time.Now().Add(-reportMaxAgeDays * 24 * time.Hour)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
			continue
		}
		files = append(files, fileInfo{path: filepath.Join(dir, e.Name()), modTime: info.ModTime()})
	}
	if len(files) <= reportMaxCount {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	for _, f := range files[:len(files)-reportMaxCount] {
		_ = os.Remove(f.path)
	}
}
