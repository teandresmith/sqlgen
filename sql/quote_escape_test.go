package sql_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/sql"
)

// TestDialect_ClausesEscapeThroughQuoteIdentifier pins that every identifier a
// dialect method writes goes through QuoteIdentifier, so an embedded quote
// character is doubled there too rather than closing the identifier early.
func TestDialect_ClausesEscapeThroughQuoteIdentifier(t *testing.T) {
	const col = "a\"b`c"
	tests := []struct {
		name string
		got  func() string
		want string
	}{
		{
			name: "postgres returning",
			got:  func() string { return sql.NewPostgresDialect().ReturningClause([]string{col}) },
			want: `RETURNING "a""b` + "`" + `c"`,
		},
		{
			name: "postgres upsert",
			got: func() string {
				return sql.NewPostgresDialect().UpsertClause(sql.UpsertClauseOptions{ConflictKeys: []string{col}, UpdateColumns: []string{col}})
			},
			want: `ON CONFLICT ("a""b` + "`" + `c") DO UPDATE SET "a""b` + "`" + `c" = excluded."a""b` + "`" + `c"`,
		},
		{
			name: "sqlite returning",
			got:  func() string { return sql.NewSQLiteDialect().ReturningClause([]string{col}) },
			want: `RETURNING "a""b` + "`" + `c"`,
		},
		{
			name: "sqlite upsert do nothing",
			got: func() string {
				return sql.NewSQLiteDialect().UpsertClause(sql.UpsertClauseOptions{ConflictKeys: []string{col}})
			},
			want: `ON CONFLICT ("a""b` + "`" + `c") DO NOTHING`,
		},
		{
			name: "mysql upsert",
			got: func() string {
				return sql.NewMySQLDialect().UpsertClause(sql.UpsertClauseOptions{UpdateColumns: []string{col}, ResolvePKColumn: col})
			},
			want: "ON DUPLICATE KEY UPDATE `a\"b``c` = LAST_INSERT_ID(`a\"b``c`), `a\"b``c` = VALUES(`a\"b``c`)",
		},
		{
			name: "mysql upsert no-op",
			got: func() string {
				return sql.NewMySQLDialect().UpsertClause(sql.UpsertClauseOptions{ConflictKeys: []string{col}})
			},
			want: "ON DUPLICATE KEY UPDATE `a\"b``c` = `a\"b``c`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.got(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
