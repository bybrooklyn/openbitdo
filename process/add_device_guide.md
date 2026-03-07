# Add Device Support

This guide describes the clean-room path for adding or promoting a device.

## Update The Runtime Catalog

1. Add or verify the PID row in `sdk/crates/bitdo_proto/src/pid_registry_table.rs`.
2. Update capability defaults and support-tier policy in `sdk/crates/bitdo_proto/src/registry.rs`.
3. Add or verify command rows in `sdk/crates/bitdo_proto/src/command_registry_table.rs`.
4. Update candidate-readonly gating in `sdk/crates/bitdo_proto/src/session.rs` when the new PID needs safe-read diagnostics.

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

From `cleanroom/sdk`:

```bash
./scripts/cleanroom_guard.sh
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace --all-targets
```
