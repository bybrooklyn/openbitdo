# Add Device Support

This guide describes the clean-room path for adding or promoting a device.

## Update The Runtime Catalog

1. Add or verify the PID row in `spec/pid_matrix.csv` and the command rows in
   `spec/command_matrix.csv` — these CSVs are the source of truth; the Go PID/command registry
   (`internal/protocol/registry_generated.go`) is generated from them via `go generate ./...`
   (see the `go:generate` directive at the top of `internal/protocol/registry.go`, implemented in
   `internal/protocol/gen/main.go`). Do not hand-edit `registry_generated.go`.
2. Update capability defaults and support-tier policy in `internal/protocol/registry.go`
   (`DefaultCapabilityFor`).
3. Update candidate-readonly gating in `internal/protocol/session.go` when the new PID needs
   safe-read diagnostics.

## Update The Sanitized Evidence

Keep the spec and evidence artifacts aligned:

- `spec/device_name_catalog.md`
- `spec/protocol_spec.md`
- `process/device_name_sources.md`
- dossier and matrix artifacts where applicable

## Update Tests

At minimum, touch the tests that prove:

- support-tier gating is correct
- command/runtime policy is correct
- diagnostics or mapping behavior is correct for the new device family

## Validation

From the repository root:

```bash
go generate ./...
./scripts/cleanroom_guard.sh
golangci-lint run ./...
go test -race ./...
```
