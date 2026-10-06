package postgres

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/teandresmith/sqlgen/parser"
)

// applyRule parses expr, applies one rewrite rule to every node and deparses
// the result.
func applyRule(t *testing.T, expr string, rule func(*pg_query.Node)) string {
	t.Helper()
	node, err := parseExpr(expr)
	if err != nil {
		t.Fatalf("parseExpr(%q) error: %v", expr, err)
	}
	walkExpr(node, rule)
	return deparseExpr(node)
}

func TestStripLiteralCast(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{name: "empty string to text", expr: "''::text", want: "''"},
		{name: "string to varchar", expr: "'x'::character varying", want: "'x'"},
		{name: "enum label", expr: "'active'::status_enum", want: "'active'"},
		{name: "schema-qualified enum label", expr: "'active'::app.status_enum", want: "'active'"},
		{name: "array literal", expr: "'{}'::text[]", want: "'{}'"},
		{name: "doubled quote kept", expr: "'it''s'::text", want: "'it''s'"},
		{name: "integer to numeric", expr: "price * (2)::numeric", want: "price * 2"},
		{name: "quoted negative integer", expr: "'-1'::integer", want: "-1"},
		{name: "quoted negative numeric", expr: "price > '-1.5'::numeric", want: "price > -1.5"},
		{name: "integer past 32 bits", expr: "'3000000000'::bigint", want: "3000000000"},
		{name: "number past 32 bits in arithmetic", expr: "('2147483648'::bigint - 1)", want: "2147483648 - 1"},
		{name: "quoted non-number to numeric stays quoted", expr: "'NaN'::numeric", want: "'NaN'"},
		{name: "function argument", expr: "lower('X'::text)", want: "lower('X')"},
		{name: "array elements", expr: "ARRAY['a'::text, 'b'::text]", want: "ARRAY['a', 'b']"},
		{name: "inside CASE", expr: "CASE WHEN (s = 'a'::text) THEN 'b'::text ELSE NULL::text END", want: "CASE WHEN s = 'a' THEN 'b' ELSE NULL END"},
		{name: "regclass argument", expr: "nextval('app.myseq'::regclass)", want: "nextval('app.myseq')"},
		{name: "cast of a column kept", expr: "(qty)::text", want: "qty::text"},
		{name: "cast of a function kept", expr: "(now())::date", want: "now()::date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyRule(t, tt.expr, stripLiteralCast); got != tt.want {
				t.Errorf("stripLiteralCast(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestStripImplicitColumnCast(t *testing.T) {
	implicit := map[string][]string{
		"score": {"integer"},
		"code":  {"text"},
		"value": {"varchar", "text"},
		"tags":  {"text[]"},
	}
	tests := []struct {
		name string
		expr string
		want string
	}{
		{name: "domain column to its base type", expr: "((score)::integer < 1000)", want: "score < 1000"},
		{name: "varchar column to text", expr: "((code)::text <> 'x')", want: "code <> 'x'"},
		{name: "VALUE to text", expr: "(length((VALUE)::text) < 5)", want: "length(value) < 5"},
		{name: "nested casts unwind", expr: "(((VALUE)::character varying)::text <> 'x')", want: "value <> 'x'"},
		{name: "array column", expr: "cardinality((tags)::text[])", want: "cardinality(tags)"},
		{name: "cast to another type kept", expr: "((score)::text <> 'x')", want: "score::text <> 'x'"},
		{name: "column without implicit casts kept", expr: "((qty)::text <> 'x')", want: "qty::text <> 'x'"},
		{name: "qualified column", expr: "((c.code)::text <> 'x')", want: "c.code <> 'x'"},
		{name: "cast of a constant kept", expr: "('5'::text <> code)", want: "'5'::text <> code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyRule(t, tt.expr, func(n *pg_query.Node) { stripImplicitColumnCast(n, implicit) })
			if got != tt.want {
				t.Errorf("stripImplicitColumnCast(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

// TestCatalogExpr pairs the text PostgreSQL 16's catalog printed for an
// expression with the expression as the DDL wrote it, and expects CatalogExpr
// to give the parser's text for the DDL.
func TestCatalogExpr(t *testing.T) {
	implicit := map[string][]string{"score": {"integer"}, "code": {"text"}, "value": {"varchar", "text"}}
	tests := []struct {
		name    string
		catalog string
		ddl     string
	}{
		{name: "enum default", catalog: "'active'::app.status_enum", ddl: "'active'"},
		{name: "empty text default", catalog: "''::text", ddl: "''"},
		{name: "negative default", catalog: "'-1'::integer", ddl: "-1"},
		{name: "function default", catalog: "gen_random_uuid()", ddl: "gen_random_uuid()"},
		{name: "keyword default", catalog: "CURRENT_TIMESTAMP", ddl: "CURRENT_TIMESTAMP"},
		{name: "parenthesized default", catalog: "(1 + 2)", ddl: "(1 + 2)"},
		{name: "AT TIME ZONE", catalog: "(now() AT TIME ZONE 'utc'::text)", ddl: "now() AT TIME ZONE 'utc'"},
		{name: "array default", catalog: "ARRAY['a'::text, 'b'::text]", ddl: "ARRAY['a','b']"},
		{name: "domain CHECK on a table column", catalog: "((score)::integer < 1000)", ddl: "score < 1000"},
		{name: "CHECK on varchar", catalog: "((code)::text <> ''::text)", ddl: "code <> ''"},
		{name: "numeric arithmetic", catalog: "((price * (2)::numeric) < (100)::numeric)", ddl: "price * 2 < 100"},
		{name: "VALUE keyword case", catalog: "(VALUE > 0)", ddl: "VALUE > 0"},
		{name: "domain CHECK on varchar base", catalog: "((VALUE)::text ~ '^[a-z]+$'::text)", ddl: "VALUE ~ '^[a-z]+$'"},
		{name: "pretty-printed CASE", catalog: "CASE\n    WHEN (qty > 5) THEN true\n    ELSE false\nEND", ddl: "CASE WHEN qty > 5 THEN true ELSE false END"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CatalogExpr(tt.catalog, implicit)
			if err != nil {
				t.Fatalf("CatalogExpr(%q) error: %v", tt.catalog, err)
			}
			want, err := NormalizeExpr(tt.ddl)
			if err != nil {
				t.Fatalf("NormalizeExpr(%q) error: %v", tt.ddl, err)
			}
			if got != want {
				t.Errorf("CatalogExpr(%q) = %q, want the parser's %q", tt.catalog, got, want)
			}
		})
	}
	if _, err := CatalogExpr("qty *", nil); err == nil {
		t.Error(`CatalogExpr("qty *") error = nil, want a parse error`)
	}
}

func TestTableImplicitCasts(t *testing.T) {
	domains := []parser.DomainType{
		{Schema: "app", Name: "posint", BaseType: "integer"},
		{Schema: "app", Name: "code", BaseType: "varchar(20)"},
		{Schema: "app", Name: "small_code", BaseType: "code"},
		{Schema: "other", Name: "tag", BaseType: "text"},
	}
	table := &parser.Table{Schema: "app", Name: "t", Columns: []parser.Column{
		{Name: "id", Type: "integer"},
		{Name: "score", Type: "posint"},
		{Name: "v", Type: "varchar(10)"},
		{Name: "c", Type: "char(3)"},
		{Name: "vs", Type: "varchar(10)[]"},
		{Name: "code", Type: "code"},
		{Name: "small", Type: "small_code"},
		{Name: "tag", Type: "other.tag"},
		{Name: "unknown", Type: "posint_other"},
	}}
	want := map[string][]string{
		"score": {"integer"},
		"v":     {"text"},
		"c":     {"text"},
		"vs":    {"text[]"},
		"code":  {"varchar", "text"},
		"small": {"code", "varchar", "text"},
		"tag":   {"text"},
	}
	if diff := cmp.Diff(want, TableImplicitCasts(table, domains)); diff != "" {
		t.Errorf("TableImplicitCasts() mismatch (-want +got):\n%s", diff)
	}

	gotValue := DomainImplicitCasts(&domains[2], domains)
	if diff := cmp.Diff(map[string][]string{"value": {"varchar", "text"}}, gotValue); diff != "" {
		t.Errorf("DomainImplicitCasts(small_code) mismatch (-want +got):\n%s", diff)
	}
}

func TestIsDecimalNumber(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"0", true},
		{"-1", true},
		{"2.5", true},
		{"-1.5", true},
		{".5", true},
		{"1.", true},
		{"1e10", true},
		{"1.5E-3", true},
		{"", false},
		{"-", false},
		{".", false},
		{"1e", false},
		{"NaN", false},
		{"Infinity", false},
		{"0x1F", false},
		{"1 2", false},
		{"+1", false},
	}
	for _, tt := range tests {
		if got := isDecimalNumber(tt.s); got != tt.want {
			t.Errorf("isDecimalNumber(%q) = %t, want %t", tt.s, got, tt.want)
		}
	}
}

func TestStripTypmods(t *testing.T) {
	tests := []struct{ in, want string }{
		{"numeric(12,2)", "numeric"},
		{"varchar(10)[]", "varchar[]"},
		{"text", "text"},
		{"text[]", "text[]"},
	}
	for _, tt := range tests {
		if got := stripTypmods(tt.in); got != tt.want {
			t.Errorf("stripTypmods(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
