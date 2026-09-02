# Dirty-Room Dossier Schema

Each dossier is a TOML file that captures sanitized protocol evidence for one PID and one operation group.

## Required Fields

- `dossier_id`
- `pid_hex`
- `operation_group`
- `command_id`
- `request_shape`
- `response_shape`
- `validator_rules`
- `retry_behavior`
- `failure_signatures`
- `evidence_source`
- `confidence`
- `requirement_ids`
- `state_machine`
- `runtime_placeholder`
- `hardware_placeholder`

## Optional Fields

- `class_family`
- `notes`

## Authoring Rule

Prefer short, structure-level descriptions over long prose. The dossier should be good enough to guide clean implementation and testing without embedding dirty-room source text.
