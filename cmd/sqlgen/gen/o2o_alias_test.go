package gen_test

import (
	"regexp"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// o2oAliasShape is the only shape an O2O JOIN alias may take: one lowercase
// letter, optionally followed by a counter from 2. The alias is written bare in
// SQL (PRD §13.2) and declared as a Go local in the scanner, and no SQL keyword
// in any supported dialect, no Go keyword and no predeclared identifier has
// this shape.
var o2oAliasShape = regexp.MustCompile(`^[a-z]([2-9]|[1-9][0-9]+)?$`)

// o2oAliasSchema holds the two shapes that used to produce keyword aliases.
// `accounts` has four has-one edges whose names share a leading keyword
// (`in`, `go`); their FK columns reference nothing, so the children have no
// edge back. `users` and `badges` form a has-one / belongs-to loop, which a
// chain walks down to the depth cap, growing a prefix of the edge name at each
// hop (`us`, `use`, `user`).
func o2oAliasSchema() *parser.Schema {
	child := func(name, fkCol string, ref *parser.FKReference) parser.Table {
		return parser.Table{
			Name: name,
			Columns: []parser.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: fkCol, Type: "bigint", Unique: true, FKReference: ref},
				{Name: "label", Type: "text"},
			},
		}
	}
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "accounts",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
			child("invoices", "account_id", nil),
			child("inventories", "account_id", nil),
			child("gophers", "account_id", nil),
			child("goals", "account_id", nil),
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "email", Type: "text"},
				},
			},
			child("badges", "user_id", &parser.FKReference{Table: "users", Column: "id"}),
		},
	}
}

// TestO2OJoinAliases_NeverAKeyword pins the O2O JOIN alias naming rule (PRD
// §13.2): a letter plus an optional counter, unique within the query. A
// prefix of the edge name made `in` (SQLite, MySQL and PostgreSQL), `use`
// (MySQL), `user` (PostgreSQL) and `go` (a Go keyword, failing generation).
func TestO2OJoinAliases_NeverAKeyword(t *testing.T) {
	in := testInput(o2oAliasSchema())
	parser.DetectRelationships(in.Schema)
	hasOne := func(name, table, fk string) config.TableRelationship {
		return config.TableRelationship{Name: name, Type: "one_to_one", Table: table, FK: fk}
	}
	in.Config.Tables["accounts"] = config.TableConfig{
		Relationships: []config.TableRelationship{
			hasOne("Invoice", "invoices", "account_id"),
			hasOne("Inventory", "inventories", "account_id"),
			hasOne("Gopher", "gophers", "account_id"),
			hasOne("Goal", "goals", "account_id"),
		},
	}
	in.Config.Tables["users"] = config.TableConfig{
		Relationships: []config.TableRelationship{hasOne("Badge", "badges", "user_id")},
	}

	tables, err := gen.BuildTableContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema = %v, want nil", err)
	}

	checked := 0
	for _, tc := range tables {
		if len(tc.O2OAllTargets) == 0 {
			continue
		}
		t.Run(tc.TableName, func(t *testing.T) {
			seen := map[string]bool{tc.VarName: true}
			for _, target := range tc.O2OAllTargets {
				if !o2oAliasShape.MatchString(target.Alias) {
					t.Errorf("%s: O2O alias %q for %s (%s) does not match %s", tc.TableName, target.Alias, target.FieldName, target.TargetTable, o2oAliasShape)
				}
				if seen[target.Alias] {
					t.Errorf("%s: O2O alias %q for %s is not unique (root alias %q)", tc.TableName, target.Alias, target.FieldName, tc.VarName)
				}
				seen[target.Alias] = true
			}
		})
		checked++
	}

	// The fixture must reach both shapes, or the loop above passes vacuously.
	for _, name := range []string{"accounts", "users", "badges"} {
		found := false
		for _, tc := range tables {
			if tc.TableName == name && len(tc.O2OAllTargets) > 0 {
				found = true
			}
		}
		if !found {
			t.Errorf("table %s has no O2O join targets; the fixture no longer exercises it (checked %d tables)", name, checked)
		}
	}
}
