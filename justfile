# OpenBitdo development commands. Install `just`: https://github.com/casey/just
#
# Run `just` with no arguments to list all recipes.

default:
    @just --list

# Verify the exact release Go toolchain is active.
toolchain-check:
    ./scripts/check_go_toolchain.sh

# Build the binary into ./openbitdo with canonical version/build metadata.
build: toolchain-check
    go build -ldflags "$(./scripts/build_metadata.sh)" -o openbitdo ./cmd/openbitdo

# Run the unit/integration test suite.
test: toolchain-check
    go test ./...

# Run tests with the race detector (slower; this is what CI gates on).
test-race: toolchain-check
    go test -race ./...

# Compile manual hardware tests without executing any test body.
manual-compile: toolchain-check
    go test -tags manual ./... -run '^$'

# Launch the TUI against real, attached hardware.
run: toolchain-check
    go run ./cmd/openbitdo

# Launch the TUI against a mock device, no real hardware needed.
run-mock: toolchain-check
    go run ./cmd/openbitdo --mock

# Require and run the same golangci-lint release used by CI.
lint: toolchain-check
    golangci-lint version | grep -F 'version 2.13.1'
    golangci-lint run ./...

# Check the generated PID/command registry without modifying tracked files.
generate-check: toolchain-check
    ./scripts/check_generated_registry.sh

# Regenerate the PID/command registry after editing docs/spec/*.csv.
generate: toolchain-check
    go generate ./...

# List any files gofmt would reformat, without changing them.
fmt-check: toolchain-check
    @unformatted="$(gofmt -l .)"; if [ -n "$unformatted" ]; then printf 'files require gofmt:\n%s\n' "$unformatted" >&2; exit 1; fi

# Reformat every Go file in place.
fmt: toolchain-check
    gofmt -w .

# Run go vet with the pinned toolchain.
vet: toolchain-check
    go vet ./...

# Verify downloaded module content against go.sum.
mod-verify: toolchain-check
    go mod verify

# Run the pinned vulnerability scanner; reachable findings fail the recipe.
vulncheck: toolchain-check
    go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

# Validate package layouts and release-metadata rendering without publishing.
package-check:
    ./scripts/test_package_layouts.sh
    ./scripts/test_render_release_metadata.sh

# Run the release-safety shell/Python gates that do not need compilation.
policy-check:
    ./scripts/check_manual_hardware_test_tags.sh
    ./scripts/cleanroom_guard.sh
    python3 ./scripts/check_evidence_readiness.py
    ./scripts/check_docs_consistency.sh

# Run every source gate CI uses. This never rewrites generated or formatted files.
check: toolchain-check generate-check build fmt-check lint vet mod-verify vulncheck test-race manual-compile package-check policy-check
