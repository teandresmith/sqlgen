package gen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	pgparser "github.com/teandresmith/sqlgen/parser/postgres"
)

// parsedPostgresType returns the type the PostgreSQL parser writes for a
// column declared as sqlType.
func parsedPostgresType(t *testing.T, sqlType string) string {
	t.Helper()
	p := pgparser.New("")
	ddl := fmt.Sprintf("CREATE TABLE t (c %s);", sqlType)
	if err := p.Parse("schema.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse(%q) error: %v", ddl, err)
	}
	return p.Schema().Tables[0].Columns[0].Type
}

// stripTypeModifiers drops a written type's modifiers: char(1) → char.
func stripTypeModifiers(sqlType string) string {
	open := strings.IndexByte(sqlType, '(')
	if open < 0 {
		return sqlType
	}
	end := strings.IndexByte(sqlType[open:], ')')
	if end < 0 {
		return sqlType
	}
	return sqlType[:open] + sqlType[open+end+1:]
}

// TestPostgresTypeKey_MatchesParser keeps config's PostgreSQL synonym table in
// step with the parser it mirrors (config cannot import the parser). Every
// synonym config names must be one the parser rewrites, to the spelling
// config tells the user to key it by — or, where the parser adds a default
// modifier (character is char(1)), to that spelling with the modifier, which
// the modifier-stripped override lookup still reaches. The spelling config
// recommends must be one the parser keeps, and config must accept it.
func TestPostgresTypeKey_MatchesParser(t *testing.T) {
	check := func(t *testing.T, synonym string) {
		t.Helper()
		want, ok := config.PostgresTypeKey(synonym)
		if !ok {
			t.Fatalf("config.PostgresTypeKey(%q) = _, false, want a synonym", synonym)
		}
		got := parsedPostgresType(t, synonym)
		if got != want && stripTypeModifiers(got) != want {
			t.Errorf("parser writes %q as %q; config.PostgresTypeKey says %q", synonym, got, want)
		}
		if again := parsedPostgresType(t, want); again != want && stripTypeModifiers(again) != want {
			t.Errorf("parser writes the recommended key %q as %q", want, again)
		}
		if _, ok := config.PostgresTypeKey(want); ok {
			t.Errorf("config.PostgresTypeKey(%q) = _, true; the recommended key must be accepted", want)
		}
	}

	t.Run("every bare synonym", func(t *testing.T) {
		for _, synonym := range config.PostgresTypeSynonyms() {
			check(t, synonym)
		}
	})

	tests := []struct {
		name    string
		synonym string
	}{
		{name: "array", synonym: "int4[]"},
		{name: "length", synonym: "character varying(10)"},
		{name: "length and array", synonym: "character varying(10)[]"},
		{name: "fixed length", synonym: "character(5)"},
		{name: "precision and scale", synonym: "decimal(10,2)"},
		{name: "precision inside a multi-word name", synonym: "timestamp(3) with time zone"},
		{name: "time precision inside a multi-word name", synonym: "time(2) with time zone"},
		{name: "bit varying length", synonym: "bit varying(4)"},
		{name: "float single precision", synonym: "float(24)"},
		{name: "float double precision", synonym: "float(25)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, tt.synonym)
		})
	}
}
