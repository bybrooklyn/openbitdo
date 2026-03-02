#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
LAB_CONFIG="${ROOT}/../harness/lab/device_lab.yaml"

if [[ ! -f "$LAB_CONFIG" ]]; then
  echo "missing lab config: $LAB_CONFIG" >&2
  exit 1
fi

REPORT_DIR="${ROOT}/../harness/reports"
mkdir -p "$REPORT_DIR"
TS="$(date +%Y%m%d-%H%M%S)"
REPORT_PATH="${1:-$REPORT_DIR/hardware_smoke_${TS}.json}"

SUITE="${BITDO_REQUIRED_SUITE:-family}"
REQUIRED_FAMILIES="${BITDO_REQUIRED_FAMILIES:-Standard64,DInput}"

PARSE_OUTPUT="$(mktemp)"
set +e
python3 - <<'PY' "$LAB_CONFIG" "$SUITE" "$REQUIRED_FAMILIES" >"$PARSE_OUTPUT"
import pathlib
import re
import sys

config_path = pathlib.Path(sys.argv[1])
suite = sys.argv[2].strip()
required_families = [item.strip() for item in sys.argv[3].split(",") if item.strip()]
lines = config_path.read_text().splitlines()

devices = []
current = None
in_devices = False

def parse_scalar(text: str):
    value = text.split("#", 1)[0].strip()
    if not value:
        return value
    if value.startswith(("0x", "0X")):
        return int(value, 16)
    try:
        return int(value)
    except ValueError:
        return value

for line in lines:
    stripped = line.strip()
    if stripped.startswith("devices:"):
        in_devices = True
        continue
    if not in_devices:
        continue
    if stripped.startswith("policies:"):
        if current:
            devices.append(current)
        current = None
        break

    if re.match(r"^\s*-\s+", line):
        if current:
            devices.append(current)
        current = {}
        continue

    if current is None:
        continue

    field_match = re.match(r"^\s*([A-Za-z0-9_]+)\s*:\s*(.+)$", line)
    if not field_match:
        continue

    key = field_match.group(1)
    value = parse_scalar(field_match.group(2))
    current[key] = value

if current:
    devices.append(current)

if not devices:
    sys.stderr.write(f"no devices found in {config_path}\n")
    sys.exit(1)

family_to_pid = {}
fixture_to_pid = {}
for device in devices:
    family = device.get("protocol_family")
    pid = device.get("pid")
    fixture_id = device.get("fixture_id")
    if isinstance(family, str) and isinstance(pid, int) and family not in family_to_pid:
        family_to_pid[family] = pid
    if isinstance(fixture_id, str) and isinstance(pid, int) and fixture_id not in fixture_to_pid:
        fixture_to_pid[fixture_id] = pid

if suite == "family":
    missing = [fam for fam in required_families if fam not in family_to_pid]
    if missing:
        available = ", ".join(sorted(family_to_pid.keys())) if family_to_pid else "none"
        sys.stderr.write(
            f"missing required family fixtures in {config_path}: {', '.join(missing)}; available: {available}\n"
        )
        sys.exit(1)
    for fam in required_families:
        print(f"FAMILY:{fam}={family_to_pid[fam]:#06x}")
elif suite == "ultimate2":
    if "ultimate2" not in fixture_to_pid:
        available = ", ".join(sorted(fixture_to_pid.keys())) if fixture_to_pid else "none"
        sys.stderr.write(
            f"missing fixture_id=ultimate2 in {config_path}; available fixture_ids: {available}\n"
        )
        sys.exit(1)
    print(f"FIXTURE:ultimate2={fixture_to_pid['ultimate2']:#06x}")
elif suite == "108jp":
    if "108jp" not in fixture_to_pid:
        available = ", ".join(sorted(fixture_to_pid.keys())) if fixture_to_pid else "none"
        sys.stderr.write(
            f"missing fixture_id=108jp in {config_path}; available fixture_ids: {available}\n"
        )
        sys.exit(1)
    print(f"FIXTURE:108jp={fixture_to_pid['108jp']:#06x}")
else:
    sys.stderr.write(f"unsupported BITDO_REQUIRED_SUITE value: {suite}\n")
    sys.exit(1)
PY
PARSE_STATUS=$?
set -e

if [[ $PARSE_STATUS -ne 0 ]]; then
  rm -f "$PARSE_OUTPUT"
  exit $PARSE_STATUS
fi

while IFS='=' read -r key pid_hex; do
  [[ -z "$key" ]] && continue
  if [[ "$key" == FAMILY:* ]]; then
    family="${key#FAMILY:}"
    case "$family" in
      DInput) export BITDO_EXPECT_DINPUT_PID="$pid_hex" ;;
      Standard64) export BITDO_EXPECT_STANDARD64_PID="$pid_hex" ;;
      JpHandshake) export BITDO_EXPECT_JPHANDSHAKE_PID="$pid_hex" ;;
      *)
        echo "unsupported family in parsed lab config: $family" >&2
        rm -f "$PARSE_OUTPUT"
        exit 1
        ;;
    esac
  elif [[ "$key" == FIXTURE:* ]]; then
    fixture="${key#FIXTURE:}"
    case "$fixture" in
      ultimate2) export BITDO_EXPECT_ULTIMATE2_PID="$pid_hex" ;;
      108jp) export BITDO_EXPECT_108JP_PID="$pid_hex" ;;
      *)
        echo "unsupported fixture in parsed lab config: $fixture" >&2
        rm -f "$PARSE_OUTPUT"
        exit 1
        ;;
    esac
  fi
done <"$PARSE_OUTPUT"
rm -f "$PARSE_OUTPUT"

TEST_OUTPUT_FILE="$(mktemp)"
TEST_STATUS=0

run_test() {
  local test_name="$1"
  set +e
  BITDO_HARDWARE=1 cargo test --workspace --test hardware_smoke -- --ignored --exact "$test_name" >>"$TEST_OUTPUT_FILE" 2>&1
  local status=$?
  set -e
  if [[ $status -ne 0 ]]; then
    TEST_STATUS=$status
  fi
}

run_test "hardware_smoke_detect_devices"

case "$SUITE" in
  family)
    IFS=',' read -r -a FAMILY_LIST <<<"$REQUIRED_FAMILIES"
    for family in "${FAMILY_LIST[@]}"; do
      case "$family" in
        DInput) run_test "hardware_smoke_dinput_family" ;;
        Standard64) run_test "hardware_smoke_standard64_family" ;;
        JpHandshake) run_test "hardware_smoke_jphandshake_family" ;;
        *)
          echo "unsupported required family for tests: $family" >>"$TEST_OUTPUT_FILE"
          TEST_STATUS=1
          ;;
      esac
    done
    ;;
  ultimate2)
    run_test "hardware_smoke_ultimate2_core_ops"
    ;;
  108jp)
    run_test "hardware_smoke_108jp_dedicated_ops"
    ;;
  *)
    echo "unsupported suite: $SUITE" >>"$TEST_OUTPUT_FILE"
    TEST_STATUS=1
    ;;
esac

python3 - <<'PY' "$REPORT_PATH" "$TEST_STATUS" "$TEST_OUTPUT_FILE" "$SUITE" "$REQUIRED_FAMILIES" "${BITDO_EXPECT_STANDARD64_PID:-}" "${BITDO_EXPECT_DINPUT_PID:-}" "${BITDO_EXPECT_JPHANDSHAKE_PID:-}" "${BITDO_EXPECT_ULTIMATE2_PID:-}" "${BITDO_EXPECT_108JP_PID:-}"
import json, sys, pathlib, datetime
report_path = pathlib.Path(sys.argv[1])
test_status = int(sys.argv[2])
output_file = pathlib.Path(sys.argv[3])
suite = sys.argv[4]
required_families = [x for x in sys.argv[5].split(",") if x]
expected_standard64 = sys.argv[6]
expected_dinput = sys.argv[7]
expected_jphandshake = sys.argv[8]
expected_ultimate2 = sys.argv[9]
expected_108jp = sys.argv[10]

report = {
    "timestamp_utc": datetime.datetime.utcnow().isoformat() + "Z",
    "suite": suite,
    "test_status": test_status,
    "tests_passed": test_status == 0,
    "required_families": required_families,
    "required_family_fixtures": {
        "Standard64": expected_standard64,
        "DInput": expected_dinput,
        "JpHandshake": expected_jphandshake,
    },
    "required_device_fixtures": {
        "ultimate2": expected_ultimate2,
        "108jp": expected_108jp,
    },
    "raw_test_output": output_file.read_text(errors="replace"),
}

report_path.write_text(json.dumps(report, indent=2))
print(report_path)
PY

rm -f "$TEST_OUTPUT_FILE"
echo "hardware smoke report written: $REPORT_PATH"
exit "$TEST_STATUS"
