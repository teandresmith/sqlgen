package mcp

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// asToolError extracts the *toolError a tool returns, failing the test if the
// error is nil or of another type.
func asToolError(t *testing.T, err error) *toolError {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var te *toolError
	if !errors.As(err, &te) {
		t.Fatalf("expected *toolError, got %T: %v", err, err)
	}
	return te
}

// assertCode asserts the error is a *toolError with the given code.
func assertCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	if te := asToolError(t, err); te.Code != code {
		t.Fatalf("code = %d, want %d (%s)", te.Code, code, te.Message)
	}
}

// assertNotFound asserts a not-found error with the given code and exact
// suggestions list (order-sensitive, per MCP.md §5.3).
func assertNotFound(t *testing.T, err error, code ErrorCode, wantSuggestions []string) {
	t.Helper()
	te := asToolError(t, err)
	if te.Code != code {
		t.Fatalf("code = %d, want %d (%s)", te.Code, code, te.Message)
	}
	if te.Suggestions == nil {
		t.Fatalf("expected a suggestions array on a not-found error, got nil")
	}
	if diff := cmp.Diff(wantSuggestions, *te.Suggestions); diff != "" {
		t.Errorf("suggestions mismatch (-want +got):\n%s", diff)
	}
}
