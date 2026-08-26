package protocol

import "testing"

// Ported from sdk/tests/error_codes.rs.
func TestErrorMapsToStableCodes(t *testing.T) {
	err := errInvalidInput("bad")
	if err.Code() != CodeInvalidInput {
		t.Fatalf("expected CodeInvalidInput, got %s", err.Code())
	}
	if ErrTimeout.Code() != CodeTimeout {
		t.Fatalf("expected CodeTimeout, got %s", ErrTimeout.Code())
	}
}
