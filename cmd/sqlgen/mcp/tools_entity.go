package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teandresmith/sqlgen/manifest"
)

// registerEntityTools installs the discovery/lookup tools that read a single
// entity or scan across entities: list_entities, get_entity, find_method
// (MCP.md §4.1).
func registerEntityTools(srv *mcp.Server, d *toolDeps) {
	addTool(srv, "sqlgen_list_entities",
		"List the entities (tables and views) in the generated package, optionally filtered by kind.",
		listEntities, d)
	addTool(srv, "sqlgen_get_entity",
		"Return the full manifest record for one entity (columns, methods, relationships, filter, sort, example). Pass compact=true to drop examples and verbose metadata.",
		getEntity, d)
	addTool(srv, "sqlgen_find_method",
		"Find which entity defines a method by name. Pass fuzzy=true for a case-insensitive substring match.",
		findMethod, d)
}

// ListEntitiesInput is the input for sqlgen_list_entities.
type ListEntitiesInput struct {
	// Kind optionally restricts the listing to "table" or "view".
	Kind string `json:"kind,omitempty" jsonschema:"filter to a single entity kind: table or view"`
}

// EntitySummary is one row of sqlgen_list_entities output.
type EntitySummary struct {
	Name            string `json:"name"`
	Table           string `json:"table"`
	PKKind          string `json:"pk_kind"`
	FeaturesSummary string `json:"features_summary"`
}

// ListEntitiesOutput wraps the entity summaries.
type ListEntitiesOutput struct {
	Entities []EntitySummary `json:"entities"`
}

// listEntities answers "what's available in this package?" (MCP.md §4.1). The
// result is sorted by entity name (the store already returns entities sorted).
func listEntities(d *toolDeps, in ListEntitiesInput) (ListEntitiesOutput, error) {
	kind := strings.TrimSpace(in.Kind)
	out := ListEntitiesOutput{Entities: []EntitySummary{}}
	for _, e := range d.store.Entities() {
		if kind != "" && string(e.Kind) != kind {
			continue
		}
		out.Entities = append(out.Entities, EntitySummary{
			Name:            e.Name,
			Table:           e.Table,
			PKKind:          e.PK.Kind,
			FeaturesSummary: featuresSummary(e),
		})
	}
	return out, nil
}

// featuresSummary renders a stable, comma-joined list of the entity's active
// feature flags (e.g. "cache, soft_delete, tenancy"), or "" when none are on.
// Deterministic: the feature names are emitted in a fixed order (MCP.md §6.5).
func featuresSummary(e *manifest.Entity) string {
	var active []string
	if e.Features.SoftDelete != nil {
		active = append(active, "soft_delete")
	}
	if e.Features.Cache != nil {
		active = append(active, "cache")
	}
	if e.Features.Events != nil {
		active = append(active, "events")
	}
	if e.Features.Tenancy != nil {
		active = append(active, "tenancy")
	}
	if len(e.Features.AuditColumns) > 0 {
		active = append(active, "audit_columns")
	}
	sort.Strings(active)
	return strings.Join(active, ", ")
}

// GetEntityInput is the input for sqlgen_get_entity.
type GetEntityInput struct {
	// Name is the entity name (not the SQL table name).
	Name string `json:"name" jsonschema:"the entity name, e.g. User"`
	// Compact drops example snippets and verbose metadata (MCP.md §4.1 / §10 #10).
	Compact bool `json:"compact,omitempty" jsonschema:"drop examples and verbose metadata to save context budget"`
}

// getEntity returns the full manifest record for one entity (MCP.md §4.1). An
// unknown name yields -32003 with fuzzy suggestions.
func getEntity(d *toolDeps, in GetEntityInput) (*manifest.Entity, error) {
	e, ok := d.store.EntityByName(in.Name)
	if !ok {
		return nil, notFoundError(CodeEntityNotFound,
			fmt.Sprintf("entity not found: %s", in.Name), in.Name, d.store.EntityNames())
	}
	if in.Compact {
		return compactEntity(e), nil
	}
	return e, nil
}

// compactEntity returns a copy of e with the fields MCP.md §4.1 lists as
// compact-droppable removed: example snippets, long-form descriptions (entity
// and column comments), declared indexes, and the non-lookup per-column
// metadata (check, default_kind). Required identity — name, type, PK, FK,
// method signatures, join shape, filter sub-categories — is retained. The copy
// is deep enough that the store's shared record is never mutated.
func compactEntity(e *manifest.Entity) *manifest.Entity {
	c := *e
	c.Comment = ""
	c.Examples = nil
	c.Indexes = nil
	c.Columns = make([]manifest.Column, len(e.Columns))
	for i, col := range e.Columns {
		col.Comment = ""
		col.Check = ""
		col.DefaultKind = ""
		c.Columns[i] = col
	}
	return &c
}

// FindMethodInput is the input for sqlgen_find_method.
type FindMethodInput struct {
	// Query is the method name to search for.
	Query string `json:"query" jsonschema:"the method name to search for, e.g. GetMany or UpdateWhere"`
	// Fuzzy switches from case-insensitive exact match to substring match.
	Fuzzy bool `json:"fuzzy,omitempty" jsonschema:"match method names by case-insensitive substring instead of exact name"`
}

// MethodHit is one match from sqlgen_find_method.
type MethodHit struct {
	Entity    string `json:"entity"`
	Method    string `json:"method"`
	Signature string `json:"signature"`
}

// FindMethodOutput wraps the method hits.
type FindMethodOutput struct {
	Matches []MethodHit `json:"matches"`
}

// findMethod answers "which entity has method X?" (MCP.md §4.1). By default the
// match is case-insensitive exact; fuzzy=true switches to a case-insensitive
// substring match. Results are sorted by (entity, method) for determinism.
func findMethod(d *toolDeps, in FindMethodInput) (FindMethodOutput, error) {
	q := strings.ToLower(strings.TrimSpace(in.Query))
	out := FindMethodOutput{Matches: []MethodHit{}}
	for _, e := range d.store.Entities() {
		for _, m := range allMethods(e) {
			name := strings.ToLower(m.Name)
			matched := name == q
			if in.Fuzzy {
				matched = strings.Contains(name, q)
			}
			if matched {
				out.Matches = append(out.Matches, MethodHit{
					Entity:    e.Name,
					Method:    m.Name,
					Signature: methodSignature(m),
				})
			}
		}
	}
	// Sort on (entity, method, signature): the signature tiebreak keeps the
	// order defined if the same name appears on both the query and mutation
	// side of one entity (MCP.md §6.5 determinism).
	sort.Slice(out.Matches, func(i, j int) bool {
		switch {
		case out.Matches[i].Entity != out.Matches[j].Entity:
			return out.Matches[i].Entity < out.Matches[j].Entity
		case out.Matches[i].Method != out.Matches[j].Method:
			return out.Matches[i].Method < out.Matches[j].Method
		default:
			return out.Matches[i].Signature < out.Matches[j].Signature
		}
	})
	return out, nil
}
