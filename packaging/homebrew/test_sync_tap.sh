#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

MOCK_BIN="$TMP/bin"
mkdir -p "$MOCK_BIN"

cat >"$MOCK_BIN/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

STATE_DIR="${MOCK_GH_STATE_DIR:?}"

if [[ "${1:-}" != "api" ]]; then
  echo "mock gh only supports api" >&2
  exit 1
fi
shift

method="GET"
if [[ "${1:-}" == "--method" ]]; then
  method="$2"
  shift 2
fi

endpoint="$1"
shift

case "${method}:${endpoint}" in
  GET:repos/*/contents/Formula/openbitdo.rb\?ref=main)
    if [[ -f "$STATE_DIR/remote_formula.rb" ]]; then
      content="$(base64 < "$STATE_DIR/remote_formula.rb" | tr -d '\n')"
      printf '{"sha":"remote-sha","content":"%s"}\n' "$content"
      exit 0
    fi
    exit 1
    ;;
  PUT:repos/*/contents/Formula/openbitdo.rb)
    content=""
    while [[ $# -gt 0 ]]; do
      if [[ "$1" == "-f" ]]; then
        case "$2" in
          content=*)
            content="${2#content=}"
            ;;
        esac
        shift 2
      else
        shift
      fi
    done
    printf '%s' "$content" | base64 --decode >"$STATE_DIR/updated_formula.rb"
    echo "put" >>"$STATE_DIR/requests.log"
    printf '{"content":{"sha":"new-sha"}}\n'
    ;;
  *)
    echo "unexpected mock gh call: ${method} ${endpoint}" >&2
    exit 1
    ;;
esac
EOF
chmod +x "$MOCK_BIN/gh"

FORMULA_SOURCE="$TMP/openbitdo.rb"
cat >"$FORMULA_SOURCE" <<'EOF'
class Openbitdo < Formula
  desc "OpenBitdo"
end
EOF

run_sync() {
  PATH="$MOCK_BIN:$PATH" \
    MOCK_GH_STATE_DIR="$TMP/mock-state" \
    GH_TOKEN="job-token" \
    HOMEBREW_TAP_TOKEN="tap-token" \
    HOMEBREW_TAP_REPO="bybrooklyn/homebrew-openbitdo" \
    HOMEBREW_PUBLISH_ENABLED="1" \
    FORMULA_SOURCE="$FORMULA_SOURCE" \
    bash "$ROOT/packaging/homebrew/sync_tap.sh"
}

mkdir -p "$TMP/mock-state"
cp "$FORMULA_SOURCE" "$TMP/mock-state/remote_formula.rb"
run_sync >"$TMP/noop.out"
grep -Fq "no formula changes to push" "$TMP/noop.out"
test ! -f "$TMP/mock-state/updated_formula.rb"

cat >"$TMP/mock-state/remote_formula.rb" <<'EOF'
class Openbitdo < Formula
  desc "Old formula"
end
EOF
run_sync >"$TMP/update.out"
grep -Fq "updated bybrooklyn/homebrew-openbitdo:Formula/openbitdo.rb" "$TMP/update.out"
cmp -s "$FORMULA_SOURCE" "$TMP/mock-state/updated_formula.rb"
