package mcp

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teandresmith/sqlgen/manifest"
)

// registerSQLTools installs the SQL-inspection tool: show_sql (MCP.md §4.1).
func registerSQLTools(srv *mcp.Server, d *toolDeps) {
	addTool(srv, "sqlgen_show_sql",
		"Show the SQL a generated method emits for the active dialect (override with the dialect argument).",
		showSQL, d)
}

// ShowSQLInput is the input for sqlgen_show_sql.
type ShowSQLInput struct {
	// Entity is the entity name that defines the method.
	Entity string `json:"entity" jsonschema:"the entity name that defines the method"`
	// Method is the method name to inspect.
	Method string `json:"method" jsonschema:"the method name, e.g. GetMany or UpdateWhere"`
	// Dialect optionally overrides the manifest's active dialect.
	Dialect string `json:"dialect,omitempty" jsonschema:"override the dialect: postgres, mysql, or sqlite"`
}

// SQLParam is one bound parameter of a generated SQL statement.
type SQLParam struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ShowSQLOutput is the sqlgen_show_sql result.
type ShowSQLOutput struct {
	SQL     string     `json:"sql"`
	Params  []SQLParam `json:"params"`
	Dialect string     `json:"dialect"`
}

// showSQL returns the canonical SQL a method emits (MCP.md §4.1). It
// distinguishes -32003 (unknown entity), -32006 (unknown method, with fuzzy
// suggestions), and -32005 (the method exists but carries no SQL body for the
// resolved dialect — e.g. a manifest generated before the sql_bodies addendum).
func showSQL(d *toolDeps, in ShowSQLInput) (ShowSQLOutput, error) {
	e, ok := d.store.EntityByName(in.Entity)
	if !ok {
		return ShowSQLOutput{}, notFoundError(CodeEntityNotFound,
			fmt.Sprintf("entity not found: %s", in.Entity), in.Entity, d.store.EntityNames())
	}
	var method *manifest.Method
	for _, m := range allMethods(e) {
		if m.Name == in.Method {
			mm := m
			method = &mm
			break
		}
	}
	if method == nil {
		return ShowSQLOutput{}, notFoundError(CodeMethodNotFound,
			fmt.Sprintf("method not found on %s: %s", in.Entity, in.Method), in.Method, methodNames(e))
	}

	dialect := manifest.Dialect(strings.TrimSpace(in.Dialect))
	if dialect == "" {
		dialect = d.store.Manifest().Dialect
	}

	if len(method.SQLBodies) == 0 {
		return ShowSQLOutput{}, newToolError(CodeMethodSQLUnavailable,
			fmt.Sprintf("no SQL body recorded for %s.%s — the method issues no statement of its own (Paginate and Connection compose Count and GetMany), or the manifest predates the sql_bodies addendum", in.Entity, in.Method))
	}
	sql, ok := method.SQLBodies[dialect]
	if !ok {
		return ShowSQLOutput{}, newToolError(CodeMethodSQLUnavailable,
			fmt.Sprintf("no SQL body for dialect %q on %s.%s", dialect, in.Entity, in.Method))
	}

	params := make([]SQLParam, len(method.Params))
	for i, p := range method.Params {
		params[i] = SQLParam{Name: p.Name, Type: p.Type}
	}
	return ShowSQLOutput{SQL: sql, Params: params, Dialect: string(dialect)}, nil
}
