# OpenBitdo dev commands. Install `just`: https://github.com/casey/just
#
# Run `just` with no arguments to list all recipes.

default:
    @just --list

# Build the binary into ./openbitdo.
build:
    go build -o openbitdo ./cmd/openbitdo

# Run the unit/integration test suite.
test:
    go test ./...

# Run tests with the race detector (slower — this is what CI actually gates on).
test-race:
    go test -race ./...

# Launch the TUI against real, attached hardware.
run:
    go run ./cmd/openbitdo

# Launch the TUI against a mock device, no real hardware needed.
run-mock:
    go run ./cmd/openbitdo --mock

# Lint with the same tool and config CI uses.
lint:
    golangci-lint run ./...

# Regenerate the PID/command registry from spec/*.csv (run after editing them).
generate:
    go generate ./...

# List any files gofmt would reformat, without changing them.
fmt-check:
    gofmt -l .

# Reformat every Go file in place.
fmt:
    gofmt -w .

# Run everything CI runs, so you catch a failure before pushing instead of after.
check: generate build fmt-check lint test-race
    ./scripts/cleanroom_guard.sh
    ./scripts/check_docs_consistency.sh
