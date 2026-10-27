package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

// limResponse is the slice of a GraphQL response these tests read.
type limResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

// limServe serves the one-shot graph.NewServer bootstrap, which applies the
// regenerated sqlgenresolver limits (PRD §26.6).
func limServe(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(graph.NewServer(testClient))
	t.Cleanup(srv.Close)
	return srv.URL
}

// limPost posts query and decodes the response.
func limPost(t *testing.T, url, query string) limResponse {
	t.Helper()
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		t.Fatalf("encoding request: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	var out limResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding response %s: %v", raw, err)
	}
	return out
}

// limDepthQuery selects `levels` relationship fields deep by alternating the
// User.categories / Category.users M2M edges under the users connection. The
// root field and the edges / node wrappers add no depth.
func limDepthQuery(levels int) string {
	var b strings.Builder
	b.WriteString("{ users(first: 1) { edges { node { id ")
	for i := range levels {
		if i%2 == 0 {
			b.WriteString("categories { id ")
		} else {
			b.WriteString("users { id ")
		}
	}
	b.WriteString(strings.Repeat("} ", levels))
	b.WriteString("} } } }")
	return b.String()
}

// limWideQuery selects n aliased scalar fields under the users connection, so
// its gqlgen complexity is n plus the three wrapping fields.
func limWideQuery(n int) string {
	var b strings.Builder
	b.WriteString("{ users(first: 1) { edges { node { ")
	for i := range n {
		fmt.Fprintf(&b, "f%d: id ", i)
	}
	b.WriteString("} } } }")
	return b.String()
}

func TestQueryLimits(t *testing.T) {
	url := limServe(t)

	rejected := []struct {
		name     string
		query    string
		wantCode string
	}{
		{
			name:     "one relationship level past max_depth is rejected",
			query:    limDepthQuery(sqlgenresolver.MaxQueryDepth + 1),
			wantCode: "DEPTH_LIMIT_EXCEEDED",
		},
		{
			name:     "complexity past max_complexity is rejected",
			query:    limWideQuery(sqlgenresolver.MaxQueryComplexity),
			wantCode: "COMPLEXITY_LIMIT_EXCEEDED",
		},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			resp := limPost(t, url, tt.query)
			if len(resp.Errors) == 0 {
				t.Fatalf("errors = none, want a %s rejection; data = %s", tt.wantCode, resp.Data)
			}
			if got := resp.Errors[0].Extensions["code"]; got != tt.wantCode {
				t.Errorf("errors[0].extensions.code = %v, want %s (message %q)", got, tt.wantCode, resp.Errors[0].Message)
			}
			if d := strings.TrimSpace(string(resp.Data)); d != "" && d != "null" {
				t.Errorf("data = %s, want none: a rejected operation must not execute", d)
			}
		})
	}

	t.Run("a query at max_depth and within max_complexity passes", func(t *testing.T) {
		resp := limPost(t, url, limDepthQuery(sqlgenresolver.MaxQueryDepth))
		if len(resp.Errors) != 0 {
			t.Fatalf("errors = %+v, want none", resp.Errors)
		}
		if d := strings.TrimSpace(string(resp.Data)); d == "" || d == "null" {
			t.Errorf("data = %q, want the users connection", d)
		}
	})
}
