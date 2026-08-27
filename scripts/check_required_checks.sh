#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 ]]; then
  echo "usage: $0 <repository> <commit-sha> <required-check>..." >&2
  exit 2
fi
if ! command -v gh >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
  echo "gh and jq are required to validate commit checks" >&2
  exit 1
fi
if [[ -z "${GH_TOKEN:-}" ]]; then
  echo "GH_TOKEN is required to validate commit checks" >&2
  exit 1
fi

repository="$1"
commit_sha="$2"
shift 2
required_checks=("$@")
max_attempts="${OPENBITDO_CHECK_ATTEMPTS:-180}"
sleep_seconds="${OPENBITDO_CHECK_INTERVAL_SECONDS:-10}"

final_states=()
for attempt in $(seq 1 "$max_attempts"); do
  if ! check_runs_json="$(gh api --paginate \
    -H 'Accept: application/vnd.github+json' \
    "repos/${repository}/commits/${commit_sha}/check-runs?per_page=100" \
    --slurp)"; then
    echo "attempt ${attempt}/${max_attempts}: unable to fetch check runs" >&2
    if [[ "$attempt" -lt "$max_attempts" ]]; then
      sleep "$sleep_seconds"
    fi
    continue
  fi

  all_success=1
  terminal_failure=0
  final_states=()
  echo "required-check attempt ${attempt}/${max_attempts} for ${commit_sha}"
  for check in "${required_checks[@]}"; do
    state="$(jq -r --arg name "$check" '
      [.[].check_runs[] | select(.name == $name)] as $runs
      | if ($runs | length) == 0 then
          "missing"
        elif any($runs[]; .conclusion == "success") then
          "success"
        elif any($runs[];
          .conclusion == "failure" or
          .conclusion == "timed_out" or
          .conclusion == "action_required" or
          .conclusion == "startup_failure" or
          .conclusion == "stale" or
          .conclusion == "cancelled" or
          .conclusion == "skipped" or
          .conclusion == "neutral") then
          "failing"
        else
          "pending"
        end
    ' <<<"$check_runs_json")"
    final_states+=("${check}=${state}")
    echo " - ${check}: ${state}"
    if [[ "$state" != "success" ]]; then
      all_success=0
    fi
    if [[ "$state" == "failing" ]]; then
      terminal_failure=1
    fi
  done

  if [[ "$all_success" -eq 1 ]]; then
    echo "all required checks succeeded on exact commit ${commit_sha}"
    exit 0
  fi
  if [[ "$terminal_failure" -eq 1 ]]; then
    echo "a required check reached a failing terminal state on ${commit_sha}" >&2
    exit 1
  fi
  if [[ "$attempt" -lt "$max_attempts" ]]; then
    sleep "$sleep_seconds"
  fi
done

echo "timed out waiting for required checks on ${commit_sha}" >&2
for entry in "${final_states[@]}"; do
  echo " - ${entry%%=*}: ${entry#*=}" >&2
done
exit 1
