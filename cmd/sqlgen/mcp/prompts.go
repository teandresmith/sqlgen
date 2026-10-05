package mcp

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Prompt names (MCP.md §4.3). Kebab-case with a sqlgen- prefix (MCP.md §4.4).
const (
	promptWriteQuery      = "sqlgen-write-query"
	promptAddRelationship = "sqlgen-add-relationship-usage"
	promptDebugNotFound   = "sqlgen-debug-not-found"
)

// promptSpec pairs a prompt's registered metadata with the text/template that
// expands its single user-turn message. Expansion is stateless: no manifest data
// is pre-fetched, so no store dependency and no per-invocation error surface —
// the message instructs the LLM to call the relevant sqlgen tools itself, with
// the caller's arguments substituted (MCP.md §4.3).
type promptSpec struct {
	prompt *mcp.Prompt
	body   *template.Template
}

// registerPrompts installs the three §4.3 templates on the SDK prompt registry.
// Prompts never change at runtime, so they are registered once at startup and
// never re-registered — no notifications/prompts/list_changed is ever emitted
// (MCP.md §4.3, §5.2). Clients without prompt support ignore the capability
// cleanly, leaving tools and resources unaffected (MCP.md §4.3).
func (s *Server) registerPrompts() {
	for _, spec := range promptSpecs() {
		s.mcp.AddPrompt(spec.prompt, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return expandPrompt(spec, req.Params.Arguments)
		})
	}
}

// promptSpecs builds the three prompt specs. Parsing the templates here (rather
// than at package init) keeps template-parse failures — a programming error, not
// a runtime input — attributable to registration.
func promptSpecs() []promptSpec {
	return []promptSpec{
		{
			prompt: &mcp.Prompt{
				Name:        promptWriteQuery,
				Description: "Write a type-safe sqlgen query against an entity to accomplish a goal.",
				Arguments: []*mcp.PromptArgument{
					{Name: "entity", Description: "the entity to query, e.g. User", Required: true},
					{Name: "goal", Description: "what the query should accomplish", Required: true},
				},
			},
			body: mustParseTemplate(promptWriteQuery, writeQueryBody),
		},
		{
			prompt: &mcp.Prompt{
				Name:        promptAddRelationship,
				Description: "Correctly preload, join, or write across a generated relationship.",
				Arguments: []*mcp.PromptArgument{
					{Name: "entity", Description: "the entity that declares the relationship", Required: true},
					{Name: "relationship", Description: "the relationship name to use", Required: true},
				},
			},
			body: mustParseTemplate(promptAddRelationship, addRelationshipBody),
		},
		{
			prompt: &mcp.Prompt{
				Name:        promptDebugNotFound,
				Description: "Diagnose why a NotFound (or analogous) error comes back from a method.",
				Arguments: []*mcp.PromptArgument{
					{Name: "entity", Description: "the entity the failing method belongs to", Required: true},
					{Name: "method", Description: "the failing method (optional; omit to consider any method on the entity)", Required: false},
				},
			},
			body: mustParseTemplate(promptDebugNotFound, debugNotFoundBody),
		},
	}
}

// expandPrompt validates the required arguments, expands the template, and
// returns the single-message prompt result (MCP.md §4.3). A missing required
// argument yields -32602 invalid-params.
func expandPrompt(spec promptSpec, args map[string]string) (*mcp.GetPromptResult, error) {
	for _, a := range spec.prompt.Arguments {
		if a.Required && strings.TrimSpace(args[a.Name]) == "" {
			return nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInvalidParams,
				Message: fmt.Sprintf("prompt %q requires argument %q", spec.prompt.Name, a.Name),
			}
		}
	}
	var b strings.Builder
	if err := spec.body.Execute(&b, args); err != nil {
		return nil, &jsonrpc.Error{Code: int64(CodeInternal), Message: fmt.Sprintf("expand prompt %q: %v", spec.prompt.Name, err)}
	}
	return &mcp.GetPromptResult{
		Description: spec.prompt.Description,
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: b.String()}},
		},
	}, nil
}

// mustParseTemplate parses a prompt body template, panicking on a parse error
// (the templates are compile-time constants, so a failure is a programming bug).
func mustParseTemplate(name, body string) *template.Template {
	return template.Must(template.New(name).Option("missingkey=zero").Parse(body))
}

// The prompt bodies. Each expands into a single user turn instructing the LLM to
// pull the relevant manifest surface via sqlgen tools before answering, with the
// caller's arguments substituted (MCP.md §4.3). text/template dot-fields index
// the argument map (e.g. {{.entity}}).
const writeQueryBody = `Help me write a type-safe sqlgen query.

First gather the real surface for this entity:
  - call sqlgen_get_entity({"name": "{{.entity}}", "compact": false}) for its columns, methods, relationships, filter, and sort types
  - call sqlgen_get_conventions({}) for the package-wide CallOptions, pagination, comparator, and error-sentinel conventions

Then, using only the real method names and types from that manifest data, write a sqlgen query against {{.entity}} that accomplishes this goal:

    {{.goal}}

Produce a Go snippet referencing the actual generated method names. Do not invent methods — if no single method fits, compose the query from the entity's filter + sort types and the client's query accessor.`

const addRelationshipBody = `Help me use a generated relationship correctly.

Gather the relationship surface:
  - call sqlgen_describe_relationship({"entity": "{{.entity}}", "relationship": "{{.relationship}}"}) for its kind, target entity, and join/junction shape
  - call sqlgen_get_example({"entity": "{{.entity}}"}) for a worked snippet
  - call sqlgen_get_conventions({}) for the pagination and filter caveats that apply when traversing relationships

Then walk me through correctly preloading, joining, or writing across the {{.relationship}} relationship on {{.entity}}, calling out any pagination or filter caveats.`

const debugNotFoundBody = `Help me diagnose a NotFound (or analogous) error.

Gather context:
  - call sqlgen_get_entity({"name": "{{.entity}}"}) for its PK shape, features, and methods
  - call sqlgen_get_conventions({}) and read its error-sentinels and soft-delete sections

Then diagnose why {{if .method}}{{.entity}}.{{.method}}{{else}}a method on {{.entity}}{{end}} might return a not-found error. Check, in order:
  1. Tenancy filtering — is a tenant scope excluding the row?
  2. Soft-delete state — is the row soft-deleted and excluded from finds by default?
  3. Primary-key shape — is the lookup key (single vs composite) being built correctly?`
