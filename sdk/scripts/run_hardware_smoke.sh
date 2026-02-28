#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

REPORT_DIR="${ROOT}/../harness/reports"
mkdir -p "$REPORT_DIR"
TS="$(date +%Y%m%d-%H%M%S)"
REPORT_PATH="${1:-$REPORT_DIR/hardware_smoke_${TS}.json}"

LIST_JSON="$(cargo run -q -p bitdoctl -- --json list 2>/dev/null || echo '[]')"

TEST_OUTPUT_FILE="$(mktemp)"
set +e
BITDO_HARDWARE=1 cargo test --workspace --test hardware_smoke -- --ignored >"$TEST_OUTPUT_FILE" 2>&1
TEST_STATUS=$?
set -e

python3 - <<'PY' "$REPORT_PATH" "$TEST_STATUS" "$TEST_OUTPUT_FILE" "$LIST_JSON"
import json, sys, pathlib, datetime
report_path = pathlib.Path(sys.argv[1])
test_status = int(sys.argv[2])
output_file = pathlib.Path(sys.argv[3])
list_json_raw = sys.argv[4]

try:
    devices = json.loads(list_json_raw)
except Exception:
    devices = []

report = {
    "timestamp_utc": datetime.datetime.utcnow().isoformat() + "Z",
    "test_status": test_status,
    "tests_passed": test_status == 0,
    "devices": devices,
    "raw_test_output": output_file.read_text(errors="replace"),
}

report_path.write_text(json.dumps(report, indent=2))
print(report_path)
PY

rm -f "$TEST_OUTPUT_FILE"
echo "hardware smoke report written: $REPORT_PATH"
