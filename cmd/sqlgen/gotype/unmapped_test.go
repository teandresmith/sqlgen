package gotype_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// TestResolveReportsUnmappedSQLType pins that a SQL type no resolution step
// maps is reported through GoType.UnmappedSQLType while still resolving to
// string, and that a mapped type reports nothing.
func TestResolveReportsUnmappedSQLType(t *testing.T) {
	cases := []struct {
		name         string
		dialect      config.Dialect
		sqlType      string
		nullable     bool
		global       map[string]config.TypeOverride
		scoped       map[string]config.TypeOverride
		typeMap      map[string]string
		wantName     string
		wantUnmapped string
		// wantComposite is the composite struct the unmapped type names.
		wantComposite string
	}{
		{name: "unknown type falls back and is reported", dialect: config.DialectPostgres, sqlType: "citext", wantName: "string", wantUnmapped: "citext"},
		{name: "unknown nullable type falls back and is reported", dialect: config.DialectPostgres, sqlType: "citext", nullable: true, wantName: "*string", wantUnmapped: "citext"},
		{name: "modifiers are stripped from the reported type", dialect: config.DialectPostgres, sqlType: "Geometry(Point, 4326)", wantName: "string", wantUnmapped: "geometry"},
		{name: "unknown array element is reported", dialect: config.DialectPostgres, sqlType: "citext[]", wantName: "[]string", wantUnmapped: "citext"},
		{name: "domain over an unknown type reports the base", dialect: config.DialectPostgres, sqlType: "ci_email", wantName: "string", wantUnmapped: "citext"},
		{name: "unknown mysql type is reported", dialect: config.DialectMySQL, sqlType: "geometry", wantName: "string", wantUnmapped: "geometry"},
		{name: "unknown sqlite type is reported", dialect: config.DialectSQLite, sqlType: "money", wantName: "string", wantUnmapped: "money"},
		{name: "mapped type is not reported", dialect: config.DialectPostgres, sqlType: "text", wantName: "string"},
		{name: "mapped type with modifiers is not reported", dialect: config.DialectPostgres, sqlType: "varchar(255)", wantName: "string"},
		{name: "mysql bit is not reported", dialect: config.DialectMySQL, sqlType: "bit(8)", wantName: "[]byte"},
		{name: "enum is not reported", dialect: config.DialectPostgres, sqlType: "mood", wantName: "Mood"},
		{name: "domain over a mapped type is not reported", dialect: config.DialectPostgres, sqlType: "email", wantName: "string"},
		{name: "global override is not reported", dialect: config.DialectPostgres, sqlType: "citext", global: map[string]config.TypeOverride{"citext": {Type: "string"}}, wantName: "string"},
		{name: "table override is not reported", dialect: config.DialectPostgres, sqlType: "citext", scoped: map[string]config.TypeOverride{"citext": {Type: "string"}}, wantName: "string"},
		{name: "type_map literal is not reported", dialect: config.DialectPostgres, sqlType: "citext", typeMap: map[string]string{"c": "string"}, wantName: "string"},
		{name: "composite falls back to string and names its struct", dialect: config.DialectPostgres, sqlType: "postal_address", wantName: "string", wantUnmapped: "postal_address", wantComposite: "PostalAddress"},
		{name: "nullable composite falls back to *string", dialect: config.DialectPostgres, sqlType: "postal_address", nullable: true, wantName: "*string", wantUnmapped: "postal_address", wantComposite: "PostalAddress"},
		{name: "composite array element names its struct", dialect: config.DialectPostgres, sqlType: "postal_address[]", wantName: "[]string", wantUnmapped: "postal_address", wantComposite: "PostalAddress"},
		{name: "domain over a composite names its struct", dialect: config.DialectPostgres, sqlType: "home_address", wantName: "string", wantUnmapped: "postal_address", wantComposite: "PostalAddress"},
		{name: "composite override is not reported", dialect: config.DialectPostgres, sqlType: "postal_address", global: map[string]config.TypeOverride{"postal_address": {Type: "string"}}, wantName: "string"},
		{name: "composite type_map literal is not reported", dialect: config.DialectPostgres, sqlType: "postal_address", typeMap: map[string]string{"c": "PostalAddress"}, wantName: "PostalAddress"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gotype.NewResolver(tc.dialect, true, tc.global)
			r.RegisterEnum("", "mood", "Mood")
			r.RegisterDomain("", "email", "text")
			r.RegisterDomain("", "ci_email", "citext")
			r.RegisterComposite("", "postal_address", "PostalAddress")
			r.RegisterDomain("", "home_address", "postal_address")
			got := r.Resolve(tc.sqlType, tc.nullable, "c", tc.typeMap, tc.scoped)
			if got.Name != tc.wantName {
				t.Errorf("Resolve(%q).Name = %q, want %q", tc.sqlType, got.Name, tc.wantName)
			}
			if got.UnmappedSQLType != tc.wantUnmapped {
				t.Errorf("Resolve(%q).UnmappedSQLType = %q, want %q", tc.sqlType, got.UnmappedSQLType, tc.wantUnmapped)
			}
			if got.UnmappedComposite != tc.wantComposite {
				t.Errorf("Resolve(%q).UnmappedComposite = %q, want %q", tc.sqlType, got.UnmappedComposite, tc.wantComposite)
			}
		})
	}
}

// TestCompositeSharesTheTypeNamespace pins that a composite type takes part in
// the owner-schema-first lookup of PRD §7.1: a bare name a column's own schema
// declares as a composite is that composite, never another schema's enum of
// the same name.
func TestCompositeSharesTheTypeNamespace(t *testing.T) {
	cases := []struct {
		name          string
		owner         string
		sqlType       string
		wantName      string
		wantComposite string
	}{
		{name: "bare name in the composite's schema is the composite", owner: "public", sqlType: "kind", wantName: "*string", wantComposite: "Kind"},
		{name: "qualified composite from another schema", owner: "app", sqlType: "public.kind", wantName: "*string", wantComposite: "Kind"},
		{name: "bare name in the enum's schema is the enum", owner: "app", sqlType: "kind", wantName: "*AppKind"},
		{name: "qualified enum from another schema", owner: "public", sqlType: "app.kind", wantName: "*AppKind"},
		{name: "bare shared name in a third schema is neither", owner: "billing", sqlType: "kind", wantName: "*string"},
	}
	// Registration order does not matter: the second schema to register a
	// name drops the bare key from every type map.
	orders := []struct {
		name     string
		register func(r *gotype.Resolver)
	}{
		{name: "enum first", register: func(r *gotype.Resolver) {
			r.RegisterEnum("app", "kind", "AppKind")
			r.RegisterComposite("public", "kind", "Kind")
		}},
		{name: "composite first", register: func(r *gotype.Resolver) {
			r.RegisterComposite("public", "kind", "Kind")
			r.RegisterEnum("app", "kind", "AppKind")
		}},
	}
	for _, order := range orders {
		for _, tc := range cases {
			t.Run(order.name+"/"+tc.name, func(t *testing.T) {
				r := gotype.NewResolver(config.DialectPostgres, true, nil)
				order.register(r)
				got := r.ForSchema(tc.owner).Resolve(tc.sqlType, true, "k", nil, nil)
				if got.Name != tc.wantName {
					t.Errorf("Resolve(%q).Name = %q, want %q", tc.sqlType, got.Name, tc.wantName)
				}
				if got.UnmappedComposite != tc.wantComposite {
					t.Errorf("Resolve(%q).UnmappedComposite = %q, want %q", tc.sqlType, got.UnmappedComposite, tc.wantComposite)
				}
			})
		}
	}
}
