package mcp

import "fmt"

// ErrorCode is an error code in the sqlgen-reserved JSON-RPC server range
// (-32000..-32099) plus the standard -32603 internal code. The loader uses
// -32001/-32002 to distinguish a missing manifest from an invalid one at
// startup; the tool-dispatch layer stamps -32003..-32006 into the structured
// error envelope of a tool result (MCP.md §5.3, see toolError).
type ErrorCode int

const (
	// CodeInternal is the standard JSON-RPC 2.0 internal-error code, applied
	// as-is per MCP.md §5.3 (e.g. a manifest that exists but cannot be read).
	CodeInternal ErrorCode = -32603
	// CodeManifestNotFound signals no manifest at the given / discovered path
	// (MCP.md §5.3, -32001).
	CodeManifestNotFound ErrorCode = -32001
	// CodeManifestInvalid signals a manifest that fails JSON Schema validation
	// (MCP.md §5.3, -32002).
	CodeManifestInvalid ErrorCode = -32002
	// CodeEntityNotFound signals get_entity / describe_relationship /
	// find_join_path / get_example / show_sql on an unknown entity name. The
	// error's data carries fuzzy suggestions (MCP.md §5.3, -32003).
	CodeEntityNotFound ErrorCode = -32003
	// CodeRelationshipNotFound signals a relationship name not defined on the
	// named entity. Carries fuzzy suggestions (MCP.md §5.3, -32004).
	CodeRelationshipNotFound ErrorCode = -32004
	// CodeMethodSQLUnavailable signals show_sql on a manifest that predates the
	// methods[].sql_bodies addendum, or that carries no SQL for the resolved
	// dialect (MCP.md §5.3, -32005).
	CodeMethodSQLUnavailable ErrorCode = -32005
	// CodeMethodNotFound signals show_sql on a method name not defined for the
	// named entity. Carries fuzzy suggestions (MCP.md §5.3, -32006).
	CodeMethodNotFound ErrorCode = -32006
)

// Error is a sqlgen MCP error carrying a JSON-RPC error code alongside a
// human-readable message and the underlying cause.
type Error struct {
	Code    ErrorCode
	Message string
	Err     error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Err }

// toolError is a per-tool business error (unknown entity, method, etc.). The
// dispatch layer renders it as an MCP tool result with IsError set, carrying a
// structured {code, message, suggestions} envelope (MCP.md §5.3).
//
// Why a tool result rather than a protocol-level JSON-RPC error: the go-sdk's
// MCP client collapses a coded JSON-RPC error returned from a tool handler into
// an opaque transport error, dropping the code and the suggestions the agent
// needs to self-correct. Surfacing the error as an IsError result — the model
// the SDK documents for tool-execution errors — keeps the code and suggestions
// visible to the LLM, which is exactly what §5.3's typo-tolerant contract is
// for. The transport/startup codes -32001/-32002 remain genuine errors (see
// *Error), surfaced at load, never as a tool result.
type toolError struct {
	Code    ErrorCode
	Message string
	// Suggestions is non-nil (possibly empty) for the not-found codes
	// (-32003/-32004/-32006), so the envelope always carries a suggestions
	// array for those; it is nil for codes that never suggest (-32005/-32603).
	Suggestions *[]string
}

// Error implements the error interface.
func (e *toolError) Error() string { return e.Message }

// newToolError builds a tool error without suggestions (e.g. -32005 SQL
// unavailable, -32603 internal).
func newToolError(code ErrorCode, message string) *toolError {
	return &toolError{Code: code, Message: message}
}

// notFoundError builds a not-found tool error carrying fuzzy suggestions drawn
// from candidates (MCP.md §5.3). The suggestions array is always present, empty
// when no candidate is within the Levenshtein threshold.
func notFoundError(code ErrorCode, message, missing string, candidates []string) *toolError {
	s := suggest(missing, candidates)
	return &toolError{Code: code, Message: message, Suggestions: &s}
}
