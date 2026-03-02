# Add Device Support (Hardcoded Path)

This guide keeps device support simple and explicit: everything is added directly in Rust code.

## 1) Add/verify PID in hardcoded registry
File:
- `/Users/brooklyn/data/8bitdo/cleanroom/sdk/crates/bitdo_proto/src/pid_registry_table.rs`

Add a `PidRegistryRow` with:
- `name`
- `pid`
- `support_level`
- `support_tier`
- `protocol_family`

## 2) Update capability policy
File:
- `/Users/brooklyn/data/8bitdo/cleanroom/sdk/crates/bitdo_proto/src/registry.rs`

Update `default_capability_for(...)` and support-tier PID lists so capability flags match evidence.

## 3) Add/verify command declarations
File:
- `/Users/brooklyn/data/8bitdo/cleanroom/sdk/crates/bitdo_proto/src/command_registry_table.rs`

Add/verify command rows:
- `id`
- `safety_class`
- `confidence`
- `experimental_default`
- `report_id`
- `request`
- `expected_response`
- `applies_to`
- `operation_group`

## 4) Confirm runtime policy
Runtime policy is derived in code (not scripts):
- `confirmed` -> enabled by default
- inferred `SafeRead` -> experimental-gated
- inferred `SafeWrite`/unsafe -> blocked until confirmed

File:
- `/Users/brooklyn/data/8bitdo/cleanroom/sdk/crates/bitdo_proto/src/registry.rs`

## 5) Update candidate gating allowlists
File:
- `/Users/brooklyn/data/8bitdo/cleanroom/sdk/crates/bitdo_proto/src/session.rs`

Update `is_command_allowed_for_candidate_pid(...)` so detect/diag behavior for the new PID is explicit.

## 6) Keep spec artifacts in sync
Files:
- `/Users/brooklyn/data/8bitdo/cleanroom/spec/pid_matrix.csv`
- `/Users/brooklyn/data/8bitdo/cleanroom/spec/command_matrix.csv`
- `/Users/brooklyn/data/8bitdo/cleanroom/spec/evidence_index.csv`
- `/Users/brooklyn/data/8bitdo/cleanroom/spec/dossiers/...`

## 7) Add tests
- Extend candidate gating tests:
  - `/Users/brooklyn/data/8bitdo/cleanroom/sdk/tests/candidate_readonly_gating.rs`
- Extend runtime policy tests:
  - `/Users/brooklyn/data/8bitdo/cleanroom/sdk/tests/runtime_policy.rs`

## 8) Validation
From `/Users/brooklyn/data/8bitdo/cleanroom/sdk`:
- `cargo test --workspace --all-targets`
- `./scripts/cleanroom_guard.sh`
