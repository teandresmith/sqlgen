package comparator_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

// TestParse_QuotedColumn pins the contract the generated ToConditions relies
// on: a comparator writes the column token it is given into Clause and Column
// verbatim, so a caller that passes Dialect.QuoteIdentifier(col) gets a
// quoted identifier on every read path — the plain WHERE, the O2O-join WHERE
// after PrefixConditions, and a relationship filter's EXISTS subquery after
// SubqueryWhere. Column carrying the same quoted token is what makes the
// alias land outside the quotes (u."2024_quota"), not inside them.
//
// The three columns are the three ways an unquoted name fails: a keyword
// (`order`), a mixed-case name PostgreSQL folds to lower case, and a
// digit-leading name that no dialect accepts bare.
func TestParse_QuotedColumn(t *testing.T) {
	one := int64(1)
	notNull := false
	conds := func(col string, d sql.Dialect) []sql.Condition {
		return comparator.NullableNumber[int64]{
			Number: comparator.Number[int64]{Eq: &one, In: []int64{1, 2}},
			Null:   &notNull,
		}.Parse(d.QuoteIdentifier(col), d)
	}

	tests := []struct {
		name       string
		dialect    sql.Dialect
		col        string
		wantPlain  string
		wantJoin   string
		wantExists string
	}{
		{
			name:       "postgres keyword",
			dialect:    pgDialect,
			col:        "order",
			wantPlain:  `SELECT "id" FROM "t" WHERE "order" = $1 AND "order" = ANY($2) AND "order" IS NOT NULL`,
			wantJoin:   `SELECT u."id" AS "u.id" FROM "t" u WHERE u."order" = $1 AND u."order" = ANY($2) AND u."order" IS NOT NULL`,
			wantExists: `tgt0."order" = $ AND tgt0."order" = ANY($) AND tgt0."order" IS NOT NULL`,
		},
		{
			name:       "postgres mixed case",
			dialect:    pgDialect,
			col:        "MixedCase",
			wantPlain:  `SELECT "id" FROM "t" WHERE "MixedCase" = $1 AND "MixedCase" = ANY($2) AND "MixedCase" IS NOT NULL`,
			wantJoin:   `SELECT u."id" AS "u.id" FROM "t" u WHERE u."MixedCase" = $1 AND u."MixedCase" = ANY($2) AND u."MixedCase" IS NOT NULL`,
			wantExists: `tgt0."MixedCase" = $ AND tgt0."MixedCase" = ANY($) AND tgt0."MixedCase" IS NOT NULL`,
		},
		{
			name:       "postgres digit leading",
			dialect:    pgDialect,
			col:        "2024_quota",
			wantPlain:  `SELECT "id" FROM "t" WHERE "2024_quota" = $1 AND "2024_quota" = ANY($2) AND "2024_quota" IS NOT NULL`,
			wantJoin:   `SELECT u."id" AS "u.id" FROM "t" u WHERE u."2024_quota" = $1 AND u."2024_quota" = ANY($2) AND u."2024_quota" IS NOT NULL`,
			wantExists: `tgt0."2024_quota" = $ AND tgt0."2024_quota" = ANY($) AND tgt0."2024_quota" IS NOT NULL`,
		},
		{
			name:       "mysql keyword",
			dialect:    mysqlDialect,
			col:        "order",
			wantPlain:  "SELECT `id` FROM `t` WHERE `order` = ? AND `order` IN (?, ?) AND `order` IS NOT NULL",
			wantJoin:   "SELECT u.`id` AS `u.id` FROM `t` u WHERE u.`order` = ? AND u.`order` IN (?, ?) AND u.`order` IS NOT NULL",
			wantExists: "tgt0.`order` = $ AND tgt0.`order` IN ($, $) AND tgt0.`order` IS NOT NULL",
		},
		{
			name:       "mysql mixed case",
			dialect:    mysqlDialect,
			col:        "MixedCase",
			wantPlain:  "SELECT `id` FROM `t` WHERE `MixedCase` = ? AND `MixedCase` IN (?, ?) AND `MixedCase` IS NOT NULL",
			wantJoin:   "SELECT u.`id` AS `u.id` FROM `t` u WHERE u.`MixedCase` = ? AND u.`MixedCase` IN (?, ?) AND u.`MixedCase` IS NOT NULL",
			wantExists: "tgt0.`MixedCase` = $ AND tgt0.`MixedCase` IN ($, $) AND tgt0.`MixedCase` IS NOT NULL",
		},
		{
			name:       "mysql digit leading",
			dialect:    mysqlDialect,
			col:        "2024_quota",
			wantPlain:  "SELECT `id` FROM `t` WHERE `2024_quota` = ? AND `2024_quota` IN (?, ?) AND `2024_quota` IS NOT NULL",
			wantJoin:   "SELECT u.`id` AS `u.id` FROM `t` u WHERE u.`2024_quota` = ? AND u.`2024_quota` IN (?, ?) AND u.`2024_quota` IS NOT NULL",
			wantExists: "tgt0.`2024_quota` = $ AND tgt0.`2024_quota` IN ($, $) AND tgt0.`2024_quota` IS NOT NULL",
		},
		{
			name:       "sqlite keyword",
			dialect:    sqliteDialect,
			col:        "order",
			wantPlain:  `SELECT "id" FROM "t" WHERE "order" = ? AND "order" IN (?, ?) AND "order" IS NOT NULL`,
			wantJoin:   `SELECT u."id" AS "u.id" FROM "t" u WHERE u."order" = ? AND u."order" IN (?, ?) AND u."order" IS NOT NULL`,
			wantExists: `tgt0."order" = $ AND tgt0."order" IN ($, $) AND tgt0."order" IS NOT NULL`,
		},
		{
			name:       "sqlite mixed case",
			dialect:    sqliteDialect,
			col:        "MixedCase",
			wantPlain:  `SELECT "id" FROM "t" WHERE "MixedCase" = ? AND "MixedCase" IN (?, ?) AND "MixedCase" IS NOT NULL`,
			wantJoin:   `SELECT u."id" AS "u.id" FROM "t" u WHERE u."MixedCase" = ? AND u."MixedCase" IN (?, ?) AND u."MixedCase" IS NOT NULL`,
			wantExists: `tgt0."MixedCase" = $ AND tgt0."MixedCase" IN ($, $) AND tgt0."MixedCase" IS NOT NULL`,
		},
		{
			name:       "sqlite digit leading",
			dialect:    sqliteDialect,
			col:        "2024_quota",
			wantPlain:  `SELECT "id" FROM "t" WHERE "2024_quota" = ? AND "2024_quota" IN (?, ?) AND "2024_quota" IS NOT NULL`,
			wantJoin:   `SELECT u."id" AS "u.id" FROM "t" u WHERE u."2024_quota" = ? AND u."2024_quota" IN (?, ?) AND u."2024_quota" IS NOT NULL`,
			wantExists: `tgt0."2024_quota" = $ AND tgt0."2024_quota" IN ($, $) AND tgt0."2024_quota" IS NOT NULL`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.dialect
			table := sql.Table{Name: "t"}

			got, _ := sql.BuildSelect(d, table, sql.SelectOptions{Columns: []string{"id"}, Conditions: conds(tt.col, d)})
			if got != tt.wantPlain {
				t.Errorf("BuildSelect(Parse(%q)) =\n  %s\nwant\n  %s", tt.col, got, tt.wantPlain)
			}

			got, _ = sql.BuildSelectJoin(d, table, nil, sql.SelectOptions{
				Alias:      "u",
				Columns:    []string{"id"},
				Conditions: sql.PrefixConditions("u", conds(tt.col, d)),
			})
			if got != tt.wantJoin {
				t.Errorf("BuildSelectJoin(PrefixConditions(u, Parse(%q))) =\n  %s\nwant\n  %s", tt.col, got, tt.wantJoin)
			}

			got, _ = sql.SubqueryWhere(d, "tgt0", conds(tt.col, d))
			if got != tt.wantExists {
				t.Errorf("SubqueryWhere(tgt0, Parse(%q)) =\n  %s\nwant\n  %s", tt.col, got, tt.wantExists)
			}
		})
	}
}

// TestJSON_Parse_QuotedColumnQualifiesInsideFunction pins the MySQL arms, where
// the column is not the clause's leading token: the quoted Column is what lets
// PrefixConditions qualify it inside JSON_CONTAINS rather than prepend the
// alias to the function name.
func TestJSON_Parse_QuotedColumnQualifiesInsideFunction(t *testing.T) {
	doc := any(`{"a":1}`)
	tests := []struct {
		name    string
		dialect sql.Dialect
		want    []string
	}{
		{
			name:    "postgres",
			dialect: pgDialect,
			want:    []string{`u."order"::jsonb @> $`, `u."order"::jsonb ? $`},
		},
		{
			name:    "mysql",
			dialect: mysqlDialect,
			want:    []string{"JSON_CONTAINS(u.`order`, $)", "JSON_CONTAINS_PATH(u.`order`, 'one', $)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sql.PrefixConditions("u", comparator.JSON{Contains: &doc, HasKey: new("a")}.Parse(tt.dialect.QuoteIdentifier("order"), tt.dialect))
			if len(got) != len(tt.want) {
				t.Fatalf("PrefixConditions(u, JSON.Parse(order)) returned %d conditions, want %d", len(got), len(tt.want))
			}
			for i, c := range got {
				if c.Clause != tt.want[i] {
					t.Errorf("condition %d Clause = %s, want %s", i, c.Clause, tt.want[i])
				}
			}
		})
	}
}
