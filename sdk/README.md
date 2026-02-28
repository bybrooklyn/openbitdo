# OpenBitdo SDK

`bitdo_proto` and `bitdoctl` provide the clean-room protocol core and CLI.

## Build
```bash
cargo build --workspace
```

## Test
```bash
cargo test --workspace --all-targets
```

## Guard
```bash
./scripts/cleanroom_guard.sh
```

## Hardware smoke report
```bash
./scripts/run_hardware_smoke.sh
```

## CLI examples
```bash
cargo run -p bitdoctl -- --mock list
cargo run -p bitdoctl -- --mock --json --pid 24585 identify
cargo run -p bitdoctl -- --mock --json --pid 24585 diag probe
```
