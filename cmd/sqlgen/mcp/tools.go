package mcp

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teandresmith/sqlgen/manifest"
)

// toolDeps is the read-only context every tool handler closes over. The store
// atomic-swaps its manifest on reload, so handlers always read the current good
// manifest by calling through it at invocation time (MCP.md §6.3).
type toolDeps struct {
	store        *Store
	version      string
	watchEnabled bool
}

// toolDeps snapshots the server state the tool handlers need.
func (s *Server) toolDeps() *toolDeps {
	return &toolDeps{store: s.store, version: s.version, watchEnabled: s.watchEnabled}
}

// registerTools installs all 11 read-only tools (MCP.md §4.1) on the underlying
// SDK server. It is called once at startup and again on every manifest reload;
// re-registering the same names replaces them in place and fires
// notifications/tools/list_changed to connected stdio clients (MCP.md §5.2).
func (s *Server) registerTools() {
	d := s.toolDeps()
	registerEntityTools(s.mcp, d)
	registerGraphTools(s.mcp, d)
	registerSQLTools(s.mcp, d)
	registerMetaTools(s.mcp, d)
	registerDiagTools(s.mcp, d)
}

// addTool is the thin adapter registering a pure (input -> output, error) tool
// function with the SDK. Keeping the tool logic in a plain function — rather
// than inline in the SDK closure — lets the unit tests exercise each tool
// directly without standing up an in-memory client/server session.
//
// The SDK output type is `any` so no output schema is inferred and the handler
// keeps full control of the result: a *toolError becomes an IsError result
// carrying the structured error envelope (MCP.md §5.3); a successful call
// returns the concrete output value, which the SDK marshals into both the
// structured content and a text block. Any non-toolError is left to the SDK,
// which folds it into an IsError result with its message text.
func addTool[In, Out any](srv *mcp.Server, name, description string, fn func(*toolDeps, In) (Out, error), d *toolDeps) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: description}, func(_ context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		out, err := fn(d, in)
		if err != nil {
			var te *toolError
			if errors.As(err, &te) {
				return toolErrorResult(te), nil, nil
			}
			return nil, nil, err
		}
		return nil, out, nil
	})
}

// errorEnvelope is the structured payload of an IsError tool result: the coded
// error of MCP.md §5.3. Suggestions is a pointer so the not-found codes always
// emit a suggestions array (possibly empty) while the other codes omit it.
type errorEnvelope struct {
	Code        int       `json:"code"`
	Message     string    `json:"message"`
	Suggestions *[]string `json:"suggestions,omitempty"`
}

// toolErrorResult renders a toolError as an MCP IsError result. The text content
// is a human/LLM-readable message (with any suggestions inlined); the structured
// content is the machine-parseable {code, message, suggestions} envelope.
func toolErrorResult(e *toolError) *mcp.CallToolResult {
	text := e.Message
	if e.Suggestions != nil && len(*e.Suggestions) > 0 {
		text += " (did you mean: " + strings.Join(*e.Suggestions, ", ") + "?)"
	}
	return &mcp.CallToolResult{
		IsError:           true,
		Content:           []mcp.Content{&mcp.TextContent{Text: text}},
		StructuredContent: errorEnvelope{Code: int(e.Code), Message: e.Message, Suggestions: e.Suggestions},
	}
}

// methodSignature renders a Go-style signature string for a manifest method,
// e.g. "Get(ctx context.Context, id uuid.UUID) (*User, error)". The leading ctx
// parameter is implicit in every generated method and is surfaced here so
// agents see the real call shape; the trailing variadic CallOptions argument is
// left off, as it is in manifest params.
//
// The return list is reconstructed from Returns, which names only the non-error
// result (PRD §30.4.2). Three shapes exist and all three must render: a value
// plus error, error alone, and an iterator that carries its own error.
func methodSignature(m manifest.Method) string {
	var b strings.Builder
	b.WriteString(m.Name)
	b.WriteByte('(')
	b.WriteString("ctx context.Context")
	for _, p := range m.Params {
		b.WriteString(", ")
		b.WriteString(p.Name)
		b.WriteByte(' ')
		b.WriteString(p.Type)
	}
	b.WriteByte(')')
	switch {
	case m.Returns == "":
		// Increment, Refresh and the hard deletes return error alone. Emitting
		// nothing here read as "returns nothing".
		b.WriteString(" error")
	case strings.HasPrefix(m.Returns, "iter.Seq2["):
		// Stream yields its errors through the iterator's second value; there
		// is no separate error return (PRD §9.4a).
		b.WriteString(" ")
		b.WriteString(m.Returns)
	default:
		b.WriteString(" (")
		b.WriteString(m.Returns)
		b.WriteString(", error)")
	}
	return b.String()
}

// allMethods returns an entity's query and mutation methods concatenated,
// query first, preserving each slice's manifest order (already deterministic).
func allMethods(e *manifest.Entity) []manifest.Method {
	out := make([]manifest.Method, 0, len(e.Methods.Query)+len(e.Methods.Mutation))
	out = append(out, e.Methods.Query...)
	out = append(out, e.Methods.Mutation...)
	return out
}

// methodNames returns every method name on an entity, sorted, for fuzzy
// suggestions on -32006 (MCP.md §5.3).
func methodNames(e *manifest.Entity) []string {
	ms := allMethods(e)
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Name
	}
	sort.Strings(names)
	return names
}

// relationshipNames returns every relationship name on an entity, sorted, for
// fuzzy suggestions on -32004 (MCP.md §5.3).
func relationshipNames(e *manifest.Entity) []string {
	names := make([]string, len(e.Relationships))
	for i, r := range e.Relationships {
		names[i] = r.Name
	}
	sort.Strings(names)
	return names
}
