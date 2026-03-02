# Dirty-Room Dossier Schema

Each dossier file is TOML and must include these fields.

## Required Fields
- `dossier_id`: stable identifier, e.g. `DOS-5200-CORE`.
- `pid_hex`: target PID in hex (`0xNNNN`).
- `operation_group`: logical grouping (`CoreDiag`, `ModeProfileRead`, `FirmwarePreflight`, etc).
- `command_id`: array of command IDs scoped by this dossier.
- `request_shape`: sanitized request structure summary.
- `response_shape`: sanitized response structure summary.
- `validator_rules`: array of response validation constraints.
- `retry_behavior`: retry and timeout behavior summary.
- `failure_signatures`: array of known failure signatures.
- `evidence_source`: `static` for this wave.
- `confidence`: `inferred` or `confirmed`.
- `requirement_ids`: array of linked requirement IDs.
- `state_machine`: table with `pre_state`, `action`, `post_state`, and `invalid_transitions`.
- `runtime_placeholder`: table with `required` and `evidence_needed`.
- `hardware_placeholder`: table with `required` and `evidence_needed`.

## Optional Fields
- `class_family`: static class-family grouping hints.
- `notes`: additional sanitized context.

## Example
```toml
dossier_id = "DOS-5200-CORE"
pid_hex = "0x5200"
operation_group = "CoreDiag"
command_id = ["GetPid", "GetReportRevision", "GetControllerVersion", "Version", "Idle"]
request_shape = "64-byte HID report, command byte in report[1], PID-specific gating outside payload"
response_shape = "short status header plus optional payload bytes"
validator_rules = ["byte0 == 0x02", "response length >= 4"]
retry_behavior = "retry up to configured max attempts on timeout/malformed response"
failure_signatures = ["timeout", "malformed response", "unsupported command for pid"]
evidence_source = "static"
confidence = "inferred"
requirement_ids = ["REQ-DR-001", "REQ-PROM-001", "REQ-PID-002"]
class_family = "JP/Handshake path"
notes = "candidate-readonly in this wave"

[state_machine]
pre_state = "DeviceConnected"
action = "Run core diagnostics reads"
post_state = "DeviceIdentified"
invalid_transitions = ["NoDevice", "TransportClosed", "BootloaderOnly"]

[runtime_placeholder]
required = true
evidence_needed = ["runtime request/response captures", "error signature examples"]

[hardware_placeholder]
required = true
evidence_needed = ["physical read validation", "repeatability checks"]
```
