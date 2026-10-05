package introspect

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestChooseMatviewPK(t *testing.T) {
	tests := []struct {
		name    string
		indexes []matviewUniqueIndex
		want    []string
	}{
		{
			name: "single index",
			indexes: []matviewUniqueIndex{
				{name: "mv_id_idx", columns: []string{"id"}},
			},
			want: []string{"id"},
		},
		{
			name: "fewest columns wins",
			indexes: []matviewUniqueIndex{
				{name: "a_two_col_idx", columns: []string{"tenant_id", "user_id"}},
				{name: "z_single_col_idx", columns: []string{"id"}},
			},
			want: []string{"id"},
		},
		{
			name: "column-count tie broken by index name ascending",
			indexes: []matviewUniqueIndex{
				{name: "mv_email_idx", columns: []string{"email"}},
				{name: "mv_account_idx", columns: []string{"account_id"}},
			},
			want: []string{"account_id"},
		},
		{
			name: "multi-column winner keeps column order",
			indexes: []matviewUniqueIndex{
				{name: "mv_three_idx", columns: []string{"a", "b", "c"}},
				{name: "mv_pair_idx", columns: []string{"tenant_id", "user_id"}},
			},
			want: []string{"tenant_id", "user_id"},
		},
		{
			name: "selection independent of input order",
			indexes: []matviewUniqueIndex{
				{name: "z_single_col_idx", columns: []string{"id"}},
				{name: "a_two_col_idx", columns: []string{"tenant_id", "user_id"}},
			},
			want: []string{"id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chooseMatviewPK(tt.indexes)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("chooseMatviewPK() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNormalizePostgresType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "character varying", in: "character varying(255)", want: "varchar(255)"},
		{name: "unbounded character varying", in: "character varying", want: "varchar"},
		{name: "character", in: "character(3)", want: "char(3)"},
		{name: "timestamp with precision", in: "timestamp(3) without time zone", want: "timestamp(3)"},
		{name: "timestamp with time zone", in: "timestamp with time zone", want: "timestamptz"},
		{name: "timestamp with precision and time zone", in: "timestamp(3) with time zone", want: "timestamptz(3)"},
		{name: "time with time zone", in: "time with time zone", want: "timetz"},
		{name: "time without time zone", in: "time without time zone", want: "time"},
		{name: "character varying array", in: "character varying(255)[]", want: "varchar(255)[]"},
		{name: "character array", in: "character(3)[]", want: "char(3)[]"},
		{name: "timestamp array", in: "timestamp(3) without time zone[]", want: "timestamp(3)[]"},
		{name: "timestamptz array", in: "timestamp with time zone[]", want: "timestamptz[]"},
		{name: "timetz array", in: "time with time zone[]", want: "timetz[]"},
		{name: "double precision", in: "double precision", want: "double precision"},
		// The file parser reads bit varying as pg_catalog.varbit.
		{name: "bit varying", in: "bit varying(8)", want: "varbit(8)"},
		{name: "bit varying array", in: "bit varying(8)[]", want: "varbit(8)[]"},
		{name: "schema-qualified name", in: "app.status_enum", want: "app.status_enum"},
		{name: "schema-qualified array", in: "app.status_enum[]", want: "app.status_enum[]"},
		{name: "passthrough", in: "numeric(12,2)", want: "numeric(12,2)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePostgresType(tt.in); got != tt.want {
				t.Errorf("normalizePostgresType(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParsePostgresArray(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "{}", want: nil},
		{name: "plain list", in: "{a,b,c}", want: []string{"a", "b", "c"}},
		{name: "quoted element with comma", in: `{"x,y",z}`, want: []string{"x,y", "z"}},
		{name: "quoted element with space", in: `{"a b",c}`, want: []string{"a b", "c"}},
		{name: "escaped quote", in: `{"q\"r"}`, want: []string{`q"r`}},
		{name: "escaped backslash", in: `{"b\\c"}`, want: []string{`b\c`}},
		{name: "mixed-case quoted identifier", in: `{"MixedCase",lower}`, want: []string{"MixedCase", "lower"}},
		{name: "empty string element first", in: `{"",a}`, want: []string{"", "a"}},
		{name: "empty string element last", in: `{a,""}`, want: []string{"a", ""}},
		{name: "only an empty string element", in: `{""}`, want: []string{""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePostgresArray(tt.in)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parsePostgresArray(%q) mismatch (-want +got):\n%s", tt.in, diff)
			}
		})
	}
}

func TestExtractCheckExpression(t *testing.T) {
	tests := []struct {
		name string
		def  string
		want string
	}{
		{name: "simple", def: "CHECK ((a > 0))", want: "(a > 0)"},
		{name: "compound AND", def: "CHECK (((a > 0) AND (b > 0)))", want: "((a > 0) AND (b > 0))"},
		{name: "compound OR", def: "CHECK (((a > 0) OR (b IS NULL)))", want: "((a > 0) OR (b IS NULL))"},
		{name: "NOT VALID suffix", def: "CHECK ((v > 0)) NOT VALID", want: "(v > 0)"},
		{name: "NO INHERIT suffix", def: "CHECK ((v > 0)) NO INHERIT", want: "(v > 0)"},
		{
			name: "parentheses inside a string literal",
			def:  "CHECK ((s <> ')('::text)) NO INHERIT",
			want: "(s <> ')('::text)",
		},
		{
			name: "parentheses inside a quoted identifier",
			def:  `CHECK (("a)b" > 0)) NOT VALID`,
			want: `("a)b" > 0)`,
		},
		{name: "column-less", def: "CHECK (true)", want: "true"},
		{name: "non-CHECK passthrough", def: "UNIQUE (a)", want: "UNIQUE (a)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractCheckExpression(tt.def); got != tt.want {
				t.Errorf("extractCheckExpression(%q) = %q, want %q", tt.def, got, tt.want)
			}
		})
	}
}

func TestPostgresGeneratedStorage(t *testing.T) {
	tests := []struct {
		attgenerated string
		want         string
	}{
		{attgenerated: "s", want: "STORED"},
		{attgenerated: "v", want: "VIRTUAL"},
	}

	for _, tt := range tests {
		t.Run(tt.attgenerated, func(t *testing.T) {
			if got := postgresGeneratedStorage(tt.attgenerated); got != tt.want {
				t.Errorf("postgresGeneratedStorage(%q) = %q, want %q", tt.attgenerated, got, tt.want)
			}
		})
	}
}
