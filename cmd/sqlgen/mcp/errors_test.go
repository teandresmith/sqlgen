package mcp

import "testing"

// TestErrorCode_String pins the reserved name every code renders as (MCP.md
// §5.3, PRD §31.4) — the name the CLI prints beside the number on a startup
// failure — and the numeric fallback for a code outside the reserved set.
func TestErrorCode_String(t *testing.T) {
	tests := []struct {
		code ErrorCode
		want string
	}{
		{CodeInternal, "INTERNAL_ERROR"},
		{CodeManifestNotFound, "MANIFEST_NOT_FOUND"},
		{CodeManifestInvalid, "MANIFEST_INVALID"},
		{CodeEntityNotFound, "ENTITY_NOT_FOUND"},
		{CodeRelationshipNotFound, "RELATIONSHIP_NOT_FOUND"},
		{CodeMethodSQLUnavailable, "METHOD_SQL_UNAVAILABLE"},
		{CodeMethodNotFound, "METHOD_NOT_FOUND"},
		{ErrorCode(-32099), "-32099"},
	}
	for _, tt := range tests {
		if got := tt.code.String(); got != tt.want {
			t.Errorf("ErrorCode(%d).String() = %q, want %q", int(tt.code), got, tt.want)
		}
	}
}
