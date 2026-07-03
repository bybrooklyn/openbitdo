#!/usr/bin/env python3
"""Validate candidate-readonly evidence artifacts stay promotion-safe."""

from __future__ import annotations

import csv
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SPEC = ROOT / "spec"

DOSSIER_REQUIRED_FIELDS = {
    "dossier_id",
    "pid_hex",
    "operation_group",
    "command_id",
    "request_shape",
    "response_shape",
    "validator_rules",
    "retry_behavior",
    "failure_signatures",
    "evidence_source",
    "confidence",
    "requirement_ids",
    "state_machine",
    "runtime_placeholder",
    "hardware_placeholder",
}
DOSSIER_BASE_FIELDS = DOSSIER_REQUIRED_FIELDS.difference(
    {"state_machine", "runtime_placeholder", "hardware_placeholder"}
)


def read_csv(path: Path) -> list[dict[str, str]]:
    with path.open(newline="", encoding="utf-8") as handle:
        return list(csv.DictReader(handle))


def normalize_pid(value: str) -> str:
    value = value.strip().lower()
    if value.startswith("0x"):
        return f"0x{int(value, 16):04x}"
    return f"0x{int(value):04x}"


def check_dossier(path: Path, expected_pid: str) -> list[str]:
    errors: list[str] = []
    raw = path.read_text(encoding="utf-8")
    top_level: set[str] = set()
    tables: dict[str, set[str]] = {}
    values: dict[str, str] = {}
    current_table: str | None = None

    for line in raw.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        table_match = re.fullmatch(r"\[([A-Za-z0-9_]+)\]", stripped)
        if table_match:
            current_table = table_match.group(1)
            top_level.add(current_table)
            tables.setdefault(current_table, set())
            continue
        field_match = re.match(r"([A-Za-z0-9_]+)\s*=\s*(.+)", stripped)
        if not field_match:
            continue
        key, value = field_match.groups()
        if current_table:
            tables.setdefault(current_table, set()).add(key)
            values[f"{current_table}.{key}"] = value.strip()
        else:
            top_level.add(key)
            values[key] = value.strip()

    uses_placeholder_schema = bool(
        {"state_machine", "runtime_placeholder", "hardware_placeholder"}.intersection(top_level)
    )
    required_fields = DOSSIER_REQUIRED_FIELDS if uses_placeholder_schema else DOSSIER_BASE_FIELDS
    missing = sorted(required_fields.difference(top_level))
    if missing:
        errors.append(f"{path}: missing required fields: {', '.join(missing)}")

    pid = values.get("pid_hex", "").strip('"')
    if pid and normalize_pid(pid) != expected_pid:
        errors.append(f"{path}: pid_hex {pid} does not match directory {expected_pid}")

    if uses_placeholder_schema:
        for table_name in ("runtime_placeholder", "hardware_placeholder"):
            table = tables.get(table_name)
            if table is None:
                errors.append(f"{path}: [{table_name}] must be present")
                continue
            if values.get(f"{table_name}.required") != "true":
                errors.append(f"{path}: [{table_name}].required must be true")
            needed = values.get(f"{table_name}.evidence_needed", "")
            if not (needed.startswith("[") and needed.endswith("]") and len(needed) > 2):
                errors.append(
                    f"{path}: [{table_name}].evidence_needed must be non-empty"
                )

    return errors


def main() -> int:
    errors: list[str] = []
    pid_rows = read_csv(SPEC / "pid_matrix.csv")
    evidence_rows = read_csv(SPEC / "evidence_index.csv")
    command_rows = read_csv(SPEC / "command_matrix.csv")

    candidates = {
        normalize_pid(row["pid_hex"])
        for row in pid_rows
        if row["support_tier"].strip() == "candidate-readonly"
    }
    evidence_pids = {normalize_pid(row["pid_hex"]) for row in evidence_rows}

    for pid in sorted(candidates):
        if pid not in evidence_pids:
            errors.append(f"{pid}: missing evidence_index.csv row")

        dossier_dir = SPEC / "dossiers" / pid.removeprefix("0x")
        dossier_paths = sorted(dossier_dir.glob("*.toml"))
        if not dossier_paths:
            errors.append(f"{pid}: missing sanitized dossier TOML files")
        for path in dossier_paths:
            errors.extend(check_dossier(path, pid))

    for row in command_rows:
        if row["promotion_gate"].strip() != "blocked/no_runtime":
            continue
        if (
            row["evidence_static"].strip() != "yes"
            or row["evidence_runtime"].strip() != "no"
            or row["evidence_hardware"].strip() != "no"
        ):
            errors.append(
                f"{row['command_id']}/{row['applies_to']}: blocked/no_runtime rows "
                "must be static-only evidence"
            )

    if errors:
        print("evidence readiness failed", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    print(
        f"evidence readiness passed: {len(candidates)} candidate-readonly PIDs checked"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
