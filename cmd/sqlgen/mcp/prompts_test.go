package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// specByName returns the registered prompt spec with the given name.
func specByName(t *testing.T, name string) promptSpec {
	t.Helper()
	for _, s := range promptSpecs() {
		if s.prompt.Name == name {
			return s
		}
	}
	t.Fatalf("no prompt spec named %q", name)
	return promptSpec{}
}

// messageText returns the concatenated text of a prompt result's messages,
// asserting each is a user-role TextContent.
func messageText(t *testing.T, res *mcp.GetPromptResult) string {
	t.Helper()
	var b strings.Builder
	for _, m := range res.Messages {
		if m.Role != "user" {
			t.Errorf("message role = %q, want user", m.Role)
		}
		tc, ok := m.Content.(*mcp.TextContent)
		if !ok {
			t.Fatalf("message content = %T, want *TextContent", m.Content)
		}
		b.WriteString(tc.Text)
	}
	return b.String()
}

func TestExpandWriteQuerySubstitutesArguments(t *testing.T) {
	res, err := expandPrompt(specByName(t, promptWriteQuery), map[string]string{"entity": "User", "goal": "find active admins"})
	if err != nil {
		t.Fatalf("expandPrompt: %v", err)
	}
	text := messageText(t, res)
	for _, want := range []string{"User", "find active admins", "sqlgen_get_entity", "sqlgen_get_conventions"} {
		if !strings.Contains(text, want) {
			t.Errorf("expansion missing %q:\n%s", want, text)
		}
	}
}

func TestExpandAddRelationshipSubstitutesArguments(t *testing.T) {
	res, err := expandPrompt(specByName(t, promptAddRelationship), map[string]string{"entity": "User", "relationship": "roles"})
	if err != nil {
		t.Fatalf("expandPrompt: %v", err)
	}
	text := messageText(t, res)
	for _, want := range []string{"User", "roles", "sqlgen_describe_relationship"} {
		if !strings.Contains(text, want) {
			t.Errorf("expansion missing %q:\n%s", want, text)
		}
	}
}

func TestExpandDebugNotFoundBranchesOnMethod(t *testing.T) {
	withMethod, err := expandPrompt(specByName(t, promptDebugNotFound), map[string]string{"entity": "User", "method": "Get"})
	if err != nil {
		t.Fatalf("expandPrompt (with method): %v", err)
	}
	if got := messageText(t, withMethod); !strings.Contains(got, "User.Get") {
		t.Errorf("with method: want User.Get in:\n%s", got)
	}

	withoutMethod, err := expandPrompt(specByName(t, promptDebugNotFound), map[string]string{"entity": "User"})
	if err != nil {
		t.Fatalf("expandPrompt (no method): %v", err)
	}
	got := messageText(t, withoutMethod)
	if strings.Contains(got, "User.") {
		t.Errorf("no method: must not render a dotted method form:\n%s", got)
	}
	if !strings.Contains(got, "a method on User") {
		t.Errorf("no method: want the any-method phrasing:\n%s", got)
	}
}

func TestExpandMissingRequiredArgIsInvalidParams(t *testing.T) {
	cases := []struct {
		name string
		spec string
		args map[string]string
	}{
		{"write-query missing goal", promptWriteQuery, map[string]string{"entity": "User"}},
		{"write-query blank goal", promptWriteQuery, map[string]string{"entity": "User", "goal": "  "}},
		{"add-relationship missing relationship", promptAddRelationship, map[string]string{"entity": "User"}},
		{"debug-not-found missing entity", promptDebugNotFound, map[string]string{"method": "Get"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := expandPrompt(specByName(t, tc.spec), tc.args)
			var je *jsonrpc.Error
			if !errors.As(err, &je) {
				t.Fatalf("error = %T (%v), want *jsonrpc.Error", err, err)
			}
			if je.Code != jsonrpc.CodeInvalidParams {
				t.Errorf("code = %d, want %d (invalid params)", je.Code, jsonrpc.CodeInvalidParams)
			}
		})
	}
}

// --- SDK dispatch round-trips ---

func TestDispatchListsThreePrompts(t *testing.T) {
	cs := connectWithSurface(t.Context(), t, newFixtureStore())
	res, err := cs.ListPrompts(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	got := make(map[string]bool)
	for _, p := range res.Prompts {
		got[p.Name] = true
	}
	for _, name := range []string{promptWriteQuery, promptAddRelationship, promptDebugNotFound} {
		if !got[name] {
			t.Errorf("prompt %q not listed", name)
		}
	}
	if len(res.Prompts) != 3 {
		t.Errorf("listed %d prompts, want 3", len(res.Prompts))
	}
}

func TestDispatchGetPromptExpands(t *testing.T) {
	cs := connectWithSurface(t.Context(), t, newFixtureStore())
	res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{
		Name:      promptWriteQuery,
		Arguments: map[string]string{"entity": "User", "goal": "list recent posts"},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(res.Messages))
	}
	if got := messageText(t, res); !strings.Contains(got, "list recent posts") {
		t.Errorf("expansion missing the goal:\n%s", got)
	}
}

func TestDispatchGetPromptUnknownIsError(t *testing.T) {
	cs := connectWithSurface(t.Context(), t, newFixtureStore())
	_, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "sqlgen-nope"})
	if err == nil {
		t.Fatal("expected an error for an unknown prompt")
	}
	// The session must remain usable after an unknown-prompt error.
	if _, err := cs.ListPrompts(t.Context(), nil); err != nil {
		t.Errorf("session unusable after an unknown-prompt error: %v", err)
	}
}

func TestDispatchGetPromptMissingArgIsError(t *testing.T) {
	cs := connectWithSurface(t.Context(), t, newFixtureStore())
	_, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{
		Name:      promptWriteQuery,
		Arguments: map[string]string{"entity": "User"},
	})
	if err == nil {
		t.Fatal("expected an error for a missing required argument")
	}
}
