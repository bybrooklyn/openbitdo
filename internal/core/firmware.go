package core

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/bybrooklyn/openbitdo/internal/protocol"
)

const firmwareDeferredMessage = "Firmware updates are deferred in 0.1.0."

func firmwareDisabledError() *Error {
	return errPolicyDenied(ReasonFeatureUnavailable, firmwareDeferredMessage)
}

func (c *OpenBitdoCore) validateFirmwareDownloadConfig() error {
	if c.config.FirmwareManifestURL == "" {
		return errManifest("firmware manifest URL is not configured")
	}
	if len(c.config.FirmwareTrustedKeys) == 0 {
		return errManifest("no trusted firmware signing keys configured")
	}
	for _, key := range c.config.FirmwareTrustedKeys {
		if len(key) != ed25519.PublicKeySize {
			return errManifest("trusted key length must be %d bytes", ed25519.PublicKeySize)
		}
	}
	return nil
}

// DownloadRecommendedFirmware fetches, hash-verifies, and signature-verifies
// the stable-channel firmware artifact for target, writing it to a temp file.
func (c *OpenBitdoCore) DownloadRecommendedFirmware(ctx context.Context, target protocol.VidPid) (FirmwareDownloadResult, error) {
	if !c.FirmwareEnabled() {
		return FirmwareDownloadResult{}, firmwareDisabledError()
	}
	if err := c.validateFirmwareDownloadConfig(); err != nil {
		return FirmwareDownloadResult{}, err
	}

	manifestRaw, err := c.fetchBytes(ctx, c.config.FirmwareManifestURL, "manifest")
	if err != nil {
		return FirmwareDownloadResult{}, err
	}
	var manifest FirmwareManifest
	if _, err := toml.Decode(string(manifestRaw), &manifest); err != nil {
		return FirmwareDownloadResult{}, errManifest("invalid manifest TOML: %v", err)
	}

	artifact, ok := manifest.recommendedFor(target)
	if !ok {
		return FirmwareDownloadResult{}, errDownload("no stable firmware artifact for pid=%#04x", target.PID)
	}

	artifactBytes, err := c.fetchBytes(ctx, artifact.URL, "artifact")
	if err != nil {
		return FirmwareDownloadResult{}, err
	}

	actualHash := sha256Hex(artifactBytes)
	if !strings.EqualFold(actualHash, artifact.SHA256) {
		return FirmwareDownloadResult{}, errPolicyDenied(ReasonImageValidationFailed,
			"downloaded firmware hash mismatch: expected=%s actual=%s", artifact.SHA256, actualHash)
	}

	if err := c.verifyArtifactSignature(ctx, artifact, artifactBytes); err != nil {
		return FirmwareDownloadResult{}, err
	}

	out := filepath.Join(os.TempDir(), fmt.Sprintf("openbitdo-fw-%04x-%s.bin", artifact.PID, newID()))
	if err := os.WriteFile(out, artifactBytes, 0o644); err != nil {
		return FirmwareDownloadResult{}, errIO(err)
	}

	return FirmwareDownloadResult{
		FirmwarePath: out, Version: artifact.Version, SourceURL: artifact.URL,
		SHA256: actualHash, VerifiedSignature: true,
	}, nil
}

func (c *OpenBitdoCore) fetchBytes(ctx context.Context, url, what string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errDownload("%s request failed: %v", what, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errDownload("%s request failed: %v", what, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errDownload("%s download failed: HTTP %d", what, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errDownload("%s read failed: %v", what, err)
	}
	return body, nil
}

func (c *OpenBitdoCore) verifyArtifactSignature(ctx context.Context, artifact FirmwareArtifact, artifactBytes []byte) error {
	if !c.FirmwareEnabled() {
		return firmwareDisabledError()
	}
	if !strings.EqualFold(artifact.Signature.Algorithm, "ed25519") {
		return errManifest("unsupported signature algorithm: %s", artifact.Signature.Algorithm)
	}
	if len(c.config.FirmwareTrustedKeys) == 0 {
		return errManifest("no trusted firmware signing keys configured")
	}
	for _, key := range c.config.FirmwareTrustedKeys {
		if len(key) != ed25519.PublicKeySize {
			return errManifest("trusted key length must be %d bytes", ed25519.PublicKeySize)
		}
	}

	sigBody, err := c.fetchBytes(ctx, artifact.Signature.URL, "signature")
	if err != nil {
		return err
	}

	sigBytes := sigBody
	if len(sigBody) != ed25519.SignatureSize {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigBody)))
		if err != nil {
			return errManifest("invalid signature base64: %v", err)
		}
		sigBytes = decoded
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return errManifest("invalid signature format: expected %d bytes, got %d", ed25519.SignatureSize, len(sigBytes))
	}

	for _, key := range c.config.FirmwareTrustedKeys {
		if ed25519.Verify(key, artifactBytes, sigBytes) {
			return nil
		}
	}

	return errPolicyDenied(ReasonImageValidationFailed, "signature verification failed for all trusted keys")
}

// PreflightFirmware validates the firmware image and gates, and computes a
// transfer plan, without sending anything to the device yet.
func (c *OpenBitdoCore) PreflightFirmware(ctx context.Context, request FirmwarePreflightRequest) (FirmwarePreflightResult, error) {
	if !c.FirmwareEnabled() {
		return deniedPreflight(ReasonFeatureUnavailable, firmwareDeferredMessage), nil
	}
	p := protocol.DeviceProfileFor(request.VidPid)
	if p.SupportTier != protocol.TierFull {
		return deniedPreflight(ReasonNotHardwareConfirmed, "Firmware updates are available only after per-PID hardware confirmation."), nil
	}
	if !request.AllowUnsafe || !request.BrickRiskAck {
		return deniedPreflight(ReasonUnsafeFlagsMissing, "Safety acknowledgement is required before firmware update"), nil
	}

	imageMeta, err := validateFirmwareImage(request.FirmwarePath)
	if err != nil {
		return FirmwarePreflightResult{}, err
	}
	chunkSize := request.ChunkSize
	if chunkSize <= 0 {
		chunkSize = c.config.DefaultChunkSize
	}
	if chunkSize < 8 {
		chunkSize = 8
	}
	chunksTotal := (imageMeta.bytesTotal + chunkSize - 1) / chunkSize
	expectedSeconds := max(uint64(chunksTotal)*c.config.ProgressIntervalMs/1000, 1)
	sessionID := FirmwareUpdateSessionID(newID())

	warnings := []string{"Do not disconnect device during transfer", "Use only validated firmware images"}
	if hasUnusualFirmwareExtension(request.FirmwarePath) {
		warnings = append(warnings, "Firmware filename extension is unusual. Continuing with strict content/hash validation.")
	}

	plan := FirmwareUpdatePlan{
		SessionID: sessionID, ChunkSize: chunkSize, BytesTotal: imageMeta.bytesTotal, ChunksTotal: chunksTotal,
		ExpectedSeconds: expectedSeconds, Warnings: warnings, ImageSHA256: imageMeta.sha256,
		CurrentVersion: "unknown", TargetVersion: "unspecified",
	}

	handle := &firmwareSessionHandle{request: request, plan: plan, events: newBroadcaster(), state: stagePreflight}
	c.sessionsMu.Lock()
	c.sessions[sessionID] = handle
	c.sessionsMu.Unlock()

	handle.eventsPublish("preflight", 0, "Preflight complete", false)

	return FirmwarePreflightResult{
		Gate: AppPolicyGateResult{Allowed: true}, Plan: plan, HasPlan: true,
		Capability: p.Capability, Evidence: p.Evidence,
	}, nil
}

// StartFirmware moves a preflighted session to AwaitingConfirmation.
func (c *OpenBitdoCore) StartFirmware(ctx context.Context, request FirmwareStartRequest) (FirmwareUpdatePlan, error) {
	if !c.FirmwareEnabled() {
		return FirmwareUpdatePlan{}, firmwareDisabledError()
	}
	handle, err := c.sessionHandle(request.SessionID)
	if err != nil {
		return FirmwareUpdatePlan{}, err
	}
	handle.mu.Lock()
	if handle.state != stagePreflight {
		handle.mu.Unlock()
		return FirmwareUpdatePlan{}, errInvalidState("Firmware session must be in preflight state")
	}
	handle.state = stageAwaitingConfirmation
	handle.mu.Unlock()

	handle.eventsPublish("awaiting_confirmation", 0, "Awaiting explicit confirmation", false)
	return handle.plan, nil
}

// ConfirmFirmware starts the actual transfer in the background.
func (c *OpenBitdoCore) ConfirmFirmware(ctx context.Context, request FirmwareConfirmRequest) (FirmwareUpdatePlan, error) {
	if !c.FirmwareEnabled() {
		return FirmwareUpdatePlan{}, firmwareDisabledError()
	}
	if !request.AcknowledgedRisk {
		return FirmwareUpdatePlan{}, errPolicyDenied(ReasonUnsafeFlagsMissing, "You must acknowledge firmware risk before continuing")
	}
	handle, err := c.sessionHandle(request.SessionID)
	if err != nil {
		return FirmwareUpdatePlan{}, err
	}
	handle.mu.Lock()
	if handle.state != stageAwaitingConfirmation {
		handle.mu.Unlock()
		return FirmwareUpdatePlan{}, errInvalidState("Firmware session is not awaiting confirmation")
	}
	handle.state = stageRunning
	handle.startedAt = time.Now()
	handle.cancelRequested = false
	handle.mu.Unlock()

	interval := c.config.ProgressIntervalMs
	mockMode := c.config.MockMode
	// The transfer runs detached from the request's context: cancellation is
	// signalled cooperatively via handle.cancelRequested (checked between
	// chunks and honored promptly), matching the original design where a
	// confirmed transfer keeps running in the background even if the UI
	// navigates away — only an explicit CancelFirmware call stops it early.
	go runTransferTask(context.Background(), handle, interval, mockMode, c.transport())

	return handle.plan, nil
}

// CancelFirmware requests cancellation and waits for a terminal report.
func (c *OpenBitdoCore) CancelFirmware(ctx context.Context, request FirmwareCancelRequest) (FirmwareFinalReport, error) {
	if !c.FirmwareEnabled() {
		return FirmwareFinalReport{}, firmwareDisabledError()
	}
	handle, err := c.sessionHandle(request.SessionID)
	if err != nil {
		return FirmwareFinalReport{}, err
	}

	handle.mu.Lock()
	if handle.state == stageCompleted || handle.state == stageCancelled || handle.state == stageFailed {
		report := handle.report
		handle.mu.Unlock()
		if report != nil {
			return *report, nil
		}
	} else {
		handle.mu.Unlock()
	}

	handle.eventsPublish("cancel_requested", 0, "Cancellation requested", false)

	handle.mu.Lock()
	if handle.state == stagePreflight || handle.state == stageAwaitingConfirmation {
		handle.state = stageCancelled
		handle.completedAt = time.Now()
		report := FirmwareFinalReport{
			SessionID: handle.plan.SessionID, Status: OutcomeCancelled, StartedAt: handle.startedAt,
			CompletedAt: handle.completedAt, BytesTotal: handle.plan.BytesTotal, ChunksTotal: handle.plan.ChunksTotal,
			Message: "Firmware update cancelled before transfer",
		}
		handle.report = &report
		handle.mu.Unlock()
		handle.eventsPublish("cancelled", 100, "Update cancelled", true)
		return report, nil
	}
	handle.cancelRequested = true
	handle.mu.Unlock()

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if report, err := c.FirmwareReport(ctx, request.SessionID); err == nil && report != nil {
			return *report, nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return FirmwareFinalReport{}, ctx.Err()
		}
	}
}

// FirmwareReport returns the terminal report for a session, or nil if the
// transfer hasn't finished yet.
func (c *OpenBitdoCore) FirmwareReport(ctx context.Context, sessionID FirmwareUpdateSessionID) (*FirmwareFinalReport, error) {
	if !c.FirmwareEnabled() {
		return nil, firmwareDisabledError()
	}
	handle, err := c.sessionHandle(sessionID)
	if err != nil {
		return nil, err
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	return handle.report, nil
}

// SubscribeEvents returns a channel receiving progress events for a session,
// published from the moment of subscription onward.
func (c *OpenBitdoCore) SubscribeEvents(sessionID FirmwareUpdateSessionID) (<-chan FirmwareProgressEvent, error) {
	if !c.FirmwareEnabled() {
		return nil, firmwareDisabledError()
	}
	handle, err := c.sessionHandle(sessionID)
	if err != nil {
		return nil, err
	}
	return handle.events.subscribe(), nil
}

type firmwareImageMeta struct {
	bytesTotal int
	sha256     string
}

func validateFirmwareImage(path string) (firmwareImageMeta, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return firmwareImageMeta{}, errIO(err)
	}
	if len(bytes) == 0 {
		return firmwareImageMeta{}, errPolicyDenied(ReasonImageValidationFailed, "Firmware image is empty")
	}
	if len(bytes) > 64*1024*1024 {
		return firmwareImageMeta{}, errPolicyDenied(ReasonImageValidationFailed, "Firmware image exceeds 64MB limit")
	}
	return firmwareImageMeta{bytesTotal: len(bytes), sha256: sha256Hex(bytes)}, nil
}

func hasUnusualFirmwareExtension(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	return ext != "bin" && ext != "fw"
}
