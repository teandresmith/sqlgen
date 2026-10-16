package parser_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
)

func TestDerivePostgresConstraintName(t *testing.T) {
	// Expected names were measured on PostgreSQL 16.
	long := strings.Repeat("abcdefghij", 6) + "_ta" // 63 bytes, the longest table name
	longCol := "col" + strings.Repeat("abcdefghij", 5)
	accented := strings.Repeat("é", 29) // 58 bytes

	tests := []struct {
		name  string
		table string
		c     parser.Constraint
		taken []string
		want  string
	}{
		{
			name:  "primary key",
			table: "users",
			c:     parser.Constraint{Type: parser.PrimaryKey, Columns: []string{"id"}},
			want:  "users_pkey",
		},
		{
			name:  "unique over several columns",
			table: "pairs",
			c:     parser.Constraint{Type: parser.Unique, Columns: []string{"d", "c"}},
			want:  "pairs_d_c_key",
		},
		{
			name:  "foreign key",
			table: "posts",
			c:     parser.Constraint{Type: parser.ForeignKey, Columns: []string{"user_id"}},
			want:  "posts_user_id_fkey",
		},
		{
			name:  "check on one column",
			table: "t",
			c:     parser.Constraint{Type: parser.Check, Columns: []string{"a"}},
			want:  "t_a_check",
		},
		{
			name:  "check on several columns",
			table: "t",
			c:     parser.Constraint{Type: parser.Check, Columns: []string{"a", "b"}},
			want:  "t_check",
		},
		{
			name:  "check on no column",
			table: "t",
			c:     parser.Constraint{Type: parser.Check},
			want:  "t_check",
		},
		{
			name:  "index",
			table: "e",
			c:     parser.Constraint{Type: parser.Index, Columns: []string{"lower"}},
			want:  "e_lower_idx",
		},
		{
			name:  "a clash moves the label to 1",
			table: "z",
			c:     parser.Constraint{Type: parser.Unique, Columns: []string{"a"}},
			taken: []string{"z_a_key"},
			want:  "z_a_key1",
		},
		{
			name:  "a second clash moves it to 2",
			table: "t",
			c:     parser.Constraint{Type: parser.Check},
			taken: []string{"t_check", "t_check1"},
			want:  "t_check2",
		},
		{
			name:  "a long table name is cut to fit 63 bytes",
			table: long,
			c:     parser.Constraint{Type: parser.PrimaryKey, Columns: []string{"other"}},
			want:  "abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefgh_pkey",
		},
		{
			name:  "the longer of table and column is cut first",
			table: long,
			c:     parser.Constraint{Type: parser.Unique, Columns: []string{longCol}},
			want:  "abcdefghijabcdefghijabcdefghi_colabcdefghijabcdefghijabcdef_key",
		},
		{
			name:  "a cut keeps whole multibyte characters",
			table: accented,
			c:     parser.Constraint{Type: parser.Unique, Columns: []string{strings.Repeat("ç", 9)}},
			want:  strings.Repeat("é", 20) + "_" + strings.Repeat("ç", 9) + "_key",
		},
		{
			name:  "a cut that lands inside a character drops it",
			table: "x" + strings.Repeat("é", 40),
			c:     parser.Constraint{Type: parser.PrimaryKey},
			want:  "x" + strings.Repeat("é", 28) + "_pkey",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taken := func(name string) bool { return slices.Contains(tt.taken, name) }
			got := parser.DerivePostgresConstraintName(tt.table, tt.c, taken)
			if got != tt.want {
				t.Errorf("DerivePostgresConstraintName(%q, %v) = %q, want %q", tt.table, tt.c.Type, got, tt.want)
			}
			if len(got) > 63 {
				t.Errorf("DerivePostgresConstraintName(%q, %v) is %d bytes, want at most 63", tt.table, tt.c.Type, len(got))
			}
		})
	}
}

func TestPostgresIndexColumnNames(t *testing.T) {
	tests := []struct {
		cols []string
		want []string
	}{
		{cols: []string{"a", "b"}, want: []string{"a", "b"}},
		{cols: []string{"a", "a"}, want: []string{"a", "a1"}},
		{cols: []string{"expr", "expr", "expr"}, want: []string{"expr", "expr1", "expr2"}},
	}
	for _, tt := range tests {
		if diff := cmp.Diff(tt.want, parser.PostgresIndexColumnNames(tt.cols)); diff != "" {
			t.Errorf("PostgresIndexColumnNames(%q) mismatch (-want +got):\n%s", tt.cols, diff)
		}
	}
}

func TestDeriveMySQLConstraintName(t *testing.T) {
	// Expected names were measured on MySQL 8.0.
	tests := []struct {
		name     string
		c        parser.Constraint
		existing []string
		want     string
	}{
		{
			name: "primary key",
			c:    parser.Constraint{Type: parser.PrimaryKey, Columns: []string{"id"}},
			want: "PRIMARY",
		},
		{
			name: "first foreign key",
			c:    parser.Constraint{Type: parser.ForeignKey, Columns: []string{"a"}},
			want: "t_ibfk_1",
		},
		{
			name:     "next foreign key",
			c:        parser.Constraint{Type: parser.ForeignKey, Columns: []string{"b"}},
			existing: []string{"t_ibfk_1", "named_fk"},
			want:     "t_ibfk_2",
		},
		{
			name:     "foreign key after the highest number",
			c:        parser.Constraint{Type: parser.ForeignKey, Columns: []string{"a"}},
			existing: []string{"t_ibfk_1", "t_ibfk_9"},
			want:     "t_ibfk_10",
		},
		{
			name:     "check",
			c:        parser.Constraint{Type: parser.Check},
			existing: []string{"t_chk_1", "t_ibfk_4"},
			want:     "t_chk_2",
		},
		{
			name: "unique takes its first column's name",
			c:    parser.Constraint{Type: parser.Unique, Columns: []string{"email", "org"}},
			want: "email",
		},
		{
			name:     "unique with its column's name taken",
			c:        parser.Constraint{Type: parser.Unique, Columns: []string{"email"}},
			existing: []string{"PRIMARY", "email"},
			want:     "email_2",
		},
		{
			name:     "key names compare case-insensitively",
			c:        parser.Constraint{Type: parser.Index, Columns: []string{"A"}},
			existing: []string{"a"},
			want:     "A_2",
		},
		{
			name:     "index skips taken suffixes",
			c:        parser.Constraint{Type: parser.Index, Columns: []string{"a"}},
			existing: []string{"a", "a_2", "a_3"},
			want:     "a_4",
		},
		{
			name: "a column named primary",
			c:    parser.Constraint{Type: parser.Unique, Columns: []string{"primary"}},
			want: "primary_2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.DeriveMySQLConstraintName("t", tt.c, tt.existing)
			if got != tt.want {
				t.Errorf("DeriveMySQLConstraintName(%q, %v, %q) = %q, want %q", "t", tt.c.Type, tt.existing, got, tt.want)
			}
		})
	}
}

func TestRenameMySQLGeneratedName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "ren_ibfk_1", want: "ren2_ibfk_1"},
		{name: "ren_ibfk_9", want: "ren2_ibfk_9"},
		{name: "ren_chk_12", want: "ren2_chk_12"},
		{name: "named_fk", want: "named_fk"},
		{name: "ren_ibfk_", want: "ren_ibfk_"},
		{name: "ren_ibfk_x", want: "ren_ibfk_x"},
		{name: "renx_ibfk_1", want: "renx_ibfk_1"},
	}
	for _, tt := range tests {
		if got := parser.RenameMySQLGeneratedName(tt.name, "ren", "ren2"); got != tt.want {
			t.Errorf("RenameMySQLGeneratedName(%q, %q, %q) = %q, want %q", tt.name, "ren", "ren2", got, tt.want)
		}
	}
}
