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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gotype.NewResolver(tc.dialect, true, tc.global)
			r.RegisterEnum("", "mood", "Mood")
			r.RegisterDomain("", "email", "text")
			r.RegisterDomain("", "ci_email", "citext")
			got := r.Resolve(tc.sqlType, tc.nullable, "c", tc.typeMap, tc.scoped)
			if got.Name != tc.wantName {
				t.Errorf("Resolve(%q).Name = %q, want %q", tc.sqlType, got.Name, tc.wantName)
			}
			if got.UnmappedSQLType != tc.wantUnmapped {
				t.Errorf("Resolve(%q).UnmappedSQLType = %q, want %q", tc.sqlType, got.UnmappedSQLType, tc.wantUnmapped)
			}
		})
	}
}
