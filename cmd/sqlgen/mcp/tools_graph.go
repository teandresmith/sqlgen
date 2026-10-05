package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teandresmith/sqlgen/manifest"
)

// registerGraphTools installs the relationship-graph tools: find_referencing,
// describe_relationship, find_join_path (MCP.md §4.1).
func registerGraphTools(srv *mcp.Server, d *toolDeps) {
	addTool(srv, "sqlgen_find_referencing",
		"Find the relationships whose foreign key references the given SQL table.",
		findReferencing, d)
	addTool(srv, "sqlgen_describe_relationship",
		"Describe how a named relationship on an entity resolves: kind, foreign key, junction table (m2m), filter, and target entity.",
		describeRelationship, d)
	addTool(srv, "sqlgen_find_join_path",
		"Find the shortest relationship paths joining one entity to another (BFS over the relationship graph).",
		findJoinPath, d)
}

// FindReferencingInput is the input for sqlgen_find_referencing.
type FindReferencingInput struct {
	// Table is the SQL table name whose inbound references are wanted.
	Table string `json:"table" jsonschema:"the SQL table name, e.g. users"`
}

// Reference is one inbound foreign-key reference to a table. All fields are
// strings so the value is comparable and usable as a dedup key.
type Reference struct {
	Entity           string `json:"entity"`
	RelationshipName string `json:"relationship_name"`
	Kind             string `json:"kind"`
	FKColumn         string `json:"fk_column"`
}

// FindReferencingOutput wraps the references.
type FindReferencingOutput struct {
	References []Reference `json:"references"`
}

// findReferencing answers "what references <table>?" (MCP.md §4.1). A
// relationship references the table when its foreign key constrains a column in
// another table to this one; for m2m relationships the junction references both
// endpoints. An unknown table is not an error — it simply yields no references.
// Results are deduplicated and sorted for determinism (MCP.md §6.5).
func findReferencing(d *toolDeps, in FindReferencingInput) (FindReferencingOutput, error) {
	table := strings.TrimSpace(in.Table)
	out := FindReferencingOutput{References: []Reference{}}
	seen := make(map[Reference]bool)
	add := func(r Reference) {
		if !seen[r] {
			seen[r] = true
			out.References = append(out.References, r)
		}
	}
	for _, e := range d.store.Entities() {
		targetTableOf := func(name string) string {
			if t, ok := d.store.EntityByName(name); ok {
				return t.Table
			}
			return ""
		}
		for _, r := range e.Relationships {
			switch {
			case r.FK != nil:
				// The FK column lives in r.FK.Table; the referenced table is the
				// other endpoint. The referencing entity is the one holding the
				// column.
				referenced, refEntity := e.Table, r.TargetEntity
				if fkOnEntity(r.FK, e) {
					referenced, refEntity = targetTableOf(r.TargetEntity), e.Name
				}
				if referenced == table {
					add(Reference{Entity: refEntity, RelationshipName: r.Name, Kind: string(r.Kind), FKColumn: r.FK.Column})
				}
			case r.Junction != nil:
				// The junction references this entity's table via LocalFK and the
				// target's table via TargetFK.
				if table == e.Table {
					add(Reference{Entity: r.TargetEntity, RelationshipName: r.Name, Kind: string(r.Kind), FKColumn: r.Junction.LocalFK})
				}
				if tt := targetTableOf(r.TargetEntity); tt != "" && table == tt {
					add(Reference{Entity: e.Name, RelationshipName: r.Name, Kind: string(r.Kind), FKColumn: r.Junction.TargetFK})
				}
			}
		}
	}
	sort.Slice(out.References, func(i, j int) bool { return lessReference(out.References[i], out.References[j]) })
	return out, nil
}

// fkOnEntity reports whether fk's column lives in e's own table. Same-named
// tables in two schemas (public.users / audit.users) differ only by schema, so
// the schema is compared whenever both sides carry one. A schema-less dialect
// carries none at all, so it falls back to the bare name.
func fkOnEntity(fk *manifest.FK, e *manifest.Entity) bool {
	if fk.Table != e.Table {
		return false
	}
	return fk.Schema == "" || e.Schema == "" || fk.Schema == e.Schema
}

// lessReference orders references on every field, so two references that
// differ only in Kind still have a defined order — findReferencing dedups on
// the full Reference struct, so the sort key must be just as wide to keep the
// output byte-stable under the unstable sort.Slice (MCP.md §6.5).
func lessReference(a, b Reference) bool {
	switch {
	case a.Entity != b.Entity:
		return a.Entity < b.Entity
	case a.RelationshipName != b.RelationshipName:
		return a.RelationshipName < b.RelationshipName
	case a.FKColumn != b.FKColumn:
		return a.FKColumn < b.FKColumn
	default:
		return a.Kind < b.Kind
	}
}

// DescribeRelationshipInput is the input for sqlgen_describe_relationship.
type DescribeRelationshipInput struct {
	// Entity is the entity name that owns the relationship.
	Entity string `json:"entity" jsonschema:"the entity name that owns the relationship"`
	// Relationship is the relationship name to describe.
	Relationship string `json:"relationship" jsonschema:"the relationship name, e.g. Tags"`
	// Compact drops the resolved target-entity summary (MCP.md §4.1 / §10 #10).
	Compact bool `json:"compact,omitempty" jsonschema:"drop the resolved target-entity summary to save context budget"`
}

// RelationshipDescription is the sqlgen_describe_relationship output: the join
// shape plus a summary of the resolved target entity (omitted when compact).
type RelationshipDescription struct {
	Entity       string             `json:"entity"`
	Relationship string             `json:"relationship"`
	Kind         string             `json:"kind"`
	TargetEntity string             `json:"target_entity"`
	FK           *manifest.FK       `json:"fk,omitempty"`
	Junction     *manifest.Junction `json:"junction,omitempty"`
	Filter       string             `json:"filter,omitempty"`
	Target       *EntitySummary     `json:"target,omitempty"`
}

// describeRelationship explains how one relationship resolves (MCP.md §4.1). An
// unknown entity yields -32003, an unknown relationship on a known entity yields
// -32004, each with fuzzy suggestions.
func describeRelationship(d *toolDeps, in DescribeRelationshipInput) (RelationshipDescription, error) {
	e, ok := d.store.EntityByName(in.Entity)
	if !ok {
		return RelationshipDescription{}, notFoundError(CodeEntityNotFound,
			fmt.Sprintf("entity not found: %s", in.Entity), in.Entity, d.store.EntityNames())
	}
	var rel *manifest.Relationship
	for i := range e.Relationships {
		if e.Relationships[i].Name == in.Relationship {
			rel = &e.Relationships[i]
			break
		}
	}
	if rel == nil {
		return RelationshipDescription{}, notFoundError(CodeRelationshipNotFound,
			fmt.Sprintf("relationship not found on %s: %s", in.Entity, in.Relationship), in.Relationship, relationshipNames(e))
	}
	desc := RelationshipDescription{
		Entity:       e.Name,
		Relationship: rel.Name,
		Kind:         string(rel.Kind),
		TargetEntity: rel.TargetEntity,
		FK:           rel.FK,
		Junction:     rel.Junction,
		Filter:       rel.Filter,
	}
	if !in.Compact {
		if t, ok := d.store.EntityByName(rel.TargetEntity); ok {
			desc.Target = &EntitySummary{
				Name:            t.Name,
				Table:           t.Table,
				PKKind:          t.PK.Kind,
				FeaturesSummary: featuresSummary(t),
			}
		}
	}
	return desc, nil
}

// FindJoinPathInput is the input for sqlgen_find_join_path.
type FindJoinPathInput struct {
	// From is the starting entity name.
	From string `json:"from" jsonschema:"the starting entity name"`
	// To is the destination entity name.
	To string `json:"to" jsonschema:"the destination entity name"`
	// MaxHops bounds the search depth. Defaults to 4, clamped to [1, 6].
	MaxHops *int `json:"max_hops,omitempty" jsonschema:"maximum relationship hops to search; default 4, clamped to [1,6]"`
}

// JoinHop is one step along a join path: the relationship traversed from the
// entity the path is currently at.
type JoinHop struct {
	Entity       string `json:"entity"`
	Relationship string `json:"relationship"`
}

// JoinPath is one route from the source to the destination entity.
type JoinPath struct {
	Hops int       `json:"hops"`
	Path []JoinHop `json:"path"`
}

// FindJoinPathOutput wraps the discovered paths.
type FindJoinPathOutput struct {
	Paths []JoinPath `json:"paths"`
}

const (
	defaultMaxHops = 4
	minMaxHops     = 1
	maxMaxHops     = 6
	maxJoinPaths   = 5
)

// findJoinPath enumerates the shortest cycle-free relationship paths from one
// entity to another (MCP.md §4.1 / §10 scope-expansion). max_hops defaults to 4
// and is clamped to [1, 6]; up to 5 paths are returned in increasing-hop order,
// ties broken lexicographically by serialized path. An unknown from/to entity
// yields -32003 with fuzzy suggestions; no path within max_hops yields an empty
// list.
func findJoinPath(d *toolDeps, in FindJoinPathInput) (FindJoinPathOutput, error) {
	names := d.store.EntityNames()
	from, ok := d.store.EntityByName(in.From)
	if !ok {
		return FindJoinPathOutput{}, notFoundError(CodeEntityNotFound,
			fmt.Sprintf("entity not found: %s", in.From), in.From, names)
	}
	if _, ok := d.store.EntityByName(in.To); !ok {
		return FindJoinPathOutput{}, notFoundError(CodeEntityNotFound,
			fmt.Sprintf("entity not found: %s", in.To), in.To, names)
	}

	maxHops := defaultMaxHops
	if in.MaxHops != nil {
		maxHops = min(max(*in.MaxHops, minMaxHops), maxMaxHops)
	}

	var found []JoinPath
	visited := map[string]bool{from.Name: true}
	var walk func(cur *manifest.Entity, path []JoinHop)
	walk = func(cur *manifest.Entity, path []JoinHop) {
		if len(path) >= maxHops {
			return
		}
		for _, r := range cur.Relationships {
			if visited[r.TargetEntity] {
				continue // cycle-free: never revisit an entity on this path
			}
			// Allocate a fresh path each branch so sibling iterations and the
			// recursion never alias the same backing array.
			next := make([]JoinHop, len(path)+1)
			copy(next, path)
			next[len(path)] = JoinHop{Entity: cur.Name, Relationship: r.Name}
			if r.TargetEntity == in.To {
				found = append(found, JoinPath{Hops: len(next), Path: next})
				continue // don't extend past the destination
			}
			target, ok := d.store.EntityByName(r.TargetEntity)
			if !ok {
				continue
			}
			visited[r.TargetEntity] = true
			walk(target, next)
			delete(visited, r.TargetEntity)
		}
	}
	walk(from, nil)

	sort.Slice(found, func(i, j int) bool {
		if found[i].Hops != found[j].Hops {
			return found[i].Hops < found[j].Hops
		}
		return serializePath(found[i].Path) < serializePath(found[j].Path)
	})
	if len(found) > maxJoinPaths {
		found = found[:maxJoinPaths]
	}
	if found == nil {
		found = []JoinPath{}
	}
	return FindJoinPathOutput{Paths: found}, nil
}

// serializePath renders a path as a stable string for lexicographic tie-breaking.
func serializePath(path []JoinHop) string {
	var b strings.Builder
	for _, h := range path {
		b.WriteString(h.Entity)
		b.WriteByte('.')
		b.WriteString(h.Relationship)
		b.WriteByte('>')
	}
	return b.String()
}
