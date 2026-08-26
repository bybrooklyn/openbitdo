package protocol

// CommandID identifies a declared protocol command. Values are generated
// (see registry_generated.go) directly from the command_id column of
// docs/spec/command_matrix.csv, so the Go identifier vocabulary is always the
// spec's vocabulary — no separate name-mapping layer to drift.
type CommandID string
