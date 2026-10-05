package sql_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/sql"
)

// Dialect shortcuts for tests.
var (
	pg    = sql.NewPostgresDialect()
	pgStd = sql.NewPostgresStdlibDialect()
	my    = sql.NewMySQLDialect()
	lite  = sql.NewSQLiteDialect()
	pgTbl = sql.Table{Schema: "public", Name: "users"}
	myTbl = sql.Table{Name: "users"}
	ltTbl = sql.Table{Name: "users"}
)

// --- BuildSelect ---

func TestBuildSelect(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.SelectOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres basic with columns, conditions, order, limit, offset",
			dialect: pg,
			table:   pgTbl,
			opts: sql.SelectOptions{
				Columns:    []string{"name", "id", "email"},
				Conditions: []sql.Condition{{Clause: `"status" = $`, Value: "active"}},
				OrderBy:    []sql.Sort{{Column: "created_at", Direction: sql.Desc}},
				Limit:      new(10),
				Offset:     new(20),
			},
			wantSQL:  `SELECT "email", "id", "name" FROM "public"."users" WHERE "status" = $1 ORDER BY "created_at" DESC LIMIT 10 OFFSET 20`,
			wantArgs: []any{"active"},
		},
		{
			name:    "mysql basic",
			dialect: my,
			table:   myTbl,
			opts: sql.SelectOptions{
				Columns:    []string{"name", "id"},
				Conditions: []sql.Condition{{Clause: "`status` = $", Value: "active"}},
				OrderBy:    []sql.Sort{{Column: "id", Direction: sql.Asc}},
				Limit:      new(5),
			},
			wantSQL:  "SELECT `id`, `name` FROM `users` WHERE `status` = ? ORDER BY `id` ASC LIMIT 5",
			wantArgs: []any{"active"},
		},
		{
			name:    "sqlite basic",
			dialect: lite,
			table:   ltTbl,
			opts: sql.SelectOptions{
				Columns:    []string{"name", "id"},
				Conditions: []sql.Condition{{Clause: `"age" > $`, Value: 18}},
			},
			wantSQL:  `SELECT "id", "name" FROM "users" WHERE "age" > ?`,
			wantArgs: []any{18},
		},
		{
			name:    "empty conditions no WHERE",
			dialect: pg,
			table:   pgTbl,
			opts:    sql.SelectOptions{Columns: []string{"id"}},
			wantSQL: `SELECT "id" FROM "public"."users"`,
		},
		{
			name:    "no columns SELECT star",
			dialect: pg,
			table:   pgTbl,
			opts:    sql.SelectOptions{},
			wantSQL: `SELECT * FROM "public"."users"`,
		},
		{
			name:    "schema qualified table",
			dialect: pg,
			table:   sql.Table{Schema: "billing", Name: "invoices"},
			opts:    sql.SelectOptions{Columns: []string{"id"}},
			wantSQL: `SELECT "id" FROM "billing"."invoices"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelect() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildSelectJoin ---

func TestBuildSelectJoin(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		joins    []sql.JoinClause
		opts     sql.SelectOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres single O2O join",
			dialect: pg,
			table:   sql.Table{Schema: "public", Name: "products"},
			joins: []sql.JoinClause{
				{
					Table:   sql.Table{Schema: "public", Name: "companies"},
					Alias:   "c",
					On:      `c."id" = p."company_id"`,
					Columns: []string{"name", "id"},
				},
			},
			opts: sql.SelectOptions{
				Alias:   "p",
				Columns: []string{"name", "id"},
			},
			wantSQL: `SELECT p."id" AS "p.id", p."name" AS "p.name", c."id" AS "c.id", c."name" AS "c.name" FROM "public"."products" p LEFT JOIN "public"."companies" c ON c."id" = p."company_id"`,
		},
		{
			name:    "mysql single O2O join",
			dialect: my,
			table:   sql.Table{Name: "products"},
			joins: []sql.JoinClause{
				{
					Table:   sql.Table{Name: "companies"},
					Alias:   "c",
					On:      "c.`id` = p.`company_id`",
					Columns: []string{"name", "id"},
				},
			},
			opts: sql.SelectOptions{
				Alias:   "p",
				Columns: []string{"name", "id"},
			},
			wantSQL: "SELECT p.`id` AS `p.id`, p.`name` AS `p.name`, c.`id` AS `c.id`, c.`name` AS `c.name` FROM `products` p LEFT JOIN `companies` c ON c.`id` = p.`company_id`",
		},
		{
			name:    "chained O2O joins",
			dialect: pg,
			table:   sql.Table{Schema: "public", Name: "products"},
			joins: []sql.JoinClause{
				{
					Table:   sql.Table{Schema: "public", Name: "companies"},
					Alias:   "c",
					On:      `c."id" = p."company_id"`,
					Columns: []string{"id", "name"},
				},
				{
					Table:   sql.Table{Schema: "public", Name: "countries"},
					Alias:   "co",
					On:      `co."id" = c."country_id"`,
					Columns: []string{"id", "name"},
				},
			},
			opts: sql.SelectOptions{
				Alias:   "p",
				Columns: []string{"id", "name"},
			},
			wantSQL: `SELECT p."id" AS "p.id", p."name" AS "p.name", c."id" AS "c.id", c."name" AS "c.name", co."id" AS "co.id", co."name" AS "co.name" FROM "public"."products" p LEFT JOIN "public"."companies" c ON c."id" = p."company_id" LEFT JOIN "public"."countries" co ON co."id" = c."country_id"`,
		},
		{
			name:    "join with soft delete timestamp",
			dialect: pg,
			table:   sql.Table{Schema: "public", Name: "products"},
			joins: []sql.JoinClause{
				{
					Table:      sql.Table{Schema: "public", Name: "companies"},
					Alias:      "c",
					On:         `c."id" = p."company_id"`,
					Columns:    []string{"id", "name"},
					SoftDelete: &sql.SoftDeleteOptions{Column: "deleted_at", Type: sql.SoftDeleteTimestamp},
				},
			},
			opts: sql.SelectOptions{
				Alias:      "p",
				Columns:    []string{"id"},
				Conditions: []sql.Condition{{Clause: `p."price" > $`, Value: 100}},
			},
			wantSQL:  `SELECT p."id" AS "p.id", c."id" AS "c.id", c."name" AS "c.name" FROM "public"."products" p LEFT JOIN "public"."companies" c ON c."id" = p."company_id" AND c."deleted_at" IS NULL WHERE p."price" > $1`,
			wantArgs: []any{100},
		},
		{
			name:    "join with soft delete bool",
			dialect: my,
			table:   sql.Table{Name: "products"},
			joins: []sql.JoinClause{
				{
					Table:      sql.Table{Name: "companies"},
					Alias:      "c",
					On:         "c.`id` = p.`company_id`",
					Columns:    []string{"id"},
					SoftDelete: &sql.SoftDeleteOptions{Column: "is_deleted", Type: sql.SoftDeleteBool},
				},
			},
			opts: sql.SelectOptions{
				Alias:   "p",
				Columns: []string{"id"},
			},
			wantSQL: "SELECT p.`id` AS `p.id`, c.`id` AS `c.id` FROM `products` p LEFT JOIN `companies` c ON c.`id` = p.`company_id` AND c.`is_deleted` = FALSE",
		},
		{
			name:    "join with soft delete integer",
			dialect: lite,
			table:   sql.Table{Name: "products"},
			joins: []sql.JoinClause{
				{
					Table:      sql.Table{Name: "companies"},
					Alias:      "c",
					On:         `c."id" = p."company_id"`,
					Columns:    []string{"id"},
					SoftDelete: &sql.SoftDeleteOptions{Column: "deleted", Type: sql.SoftDeleteInteger},
				},
			},
			opts: sql.SelectOptions{
				Alias:   "p",
				Columns: []string{"id"},
			},
			wantSQL: `SELECT p."id" AS "p.id", c."id" AS "c.id" FROM "products" p LEFT JOIN "companies" c ON c."id" = p."company_id" AND c."deleted" = 0`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelectJoin(tt.dialect, tt.table, tt.joins, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelectJoin() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelectJoin() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildInsert ---

func TestBuildInsert(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.InsertOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres single row",
			dialect: pg,
			table:   pgTbl,
			opts: sql.InsertOptions{
				Columns: []string{"name", "email", "id"},
				Values:  []any{"alice", "alice@test.com", 1},
			},
			wantSQL:  `INSERT INTO "public"."users" ("email", "id", "name") VALUES ($1, $2, $3)`,
			wantArgs: []any{"alice@test.com", 1, "alice"},
		},
		{
			name:    "mysql single row",
			dialect: my,
			table:   myTbl,
			opts: sql.InsertOptions{
				Columns: []string{"name", "id"},
				Values:  []any{"bob", 2},
			},
			wantSQL:  "INSERT INTO `users` (`id`, `name`) VALUES (?, ?)",
			wantArgs: []any{2, "bob"},
		},
		{
			name:    "sqlite single row",
			dialect: lite,
			table:   ltTbl,
			opts: sql.InsertOptions{
				Columns: []string{"name", "id"},
				Values:  []any{"charlie", 3},
			},
			wantSQL:  `INSERT INTO "users" ("id", "name") VALUES (?, ?)`,
			wantArgs: []any{3, "charlie"},
		},
		{
			name:    "postgres with RETURNING",
			dialect: pg,
			table:   pgTbl,
			opts: sql.InsertOptions{
				Columns:          []string{"name"},
				Values:           []any{"alice"},
				ReturningColumns: []string{"id", "created_at"},
			},
			wantSQL:  `INSERT INTO "public"."users" ("name") VALUES ($1) RETURNING "id", "created_at"`,
			wantArgs: []any{"alice"},
		},
		{
			name:    "mysql with RETURNING ignored",
			dialect: my,
			table:   myTbl,
			opts: sql.InsertOptions{
				Columns:          []string{"name"},
				Values:           []any{"bob"},
				ReturningColumns: []string{"id"},
			},
			wantSQL:  "INSERT INTO `users` (`name`) VALUES (?)",
			wantArgs: []any{"bob"},
		},
		{
			name:    "postgres with upsert",
			dialect: pg,
			table:   pgTbl,
			opts: sql.InsertOptions{
				Columns:             []string{"email", "name", "id"},
				Values:              []any{"a@b.com", "alice", 1},
				UpsertConflictKeys:  []string{"email"},
				UpsertUpdateColumns: []string{"name"},
			},
			wantSQL:  `INSERT INTO "public"."users" ("email", "id", "name") VALUES ($1, $2, $3) ON CONFLICT ("email") DO UPDATE SET "name" = excluded."name"`,
			wantArgs: []any{"a@b.com", 1, "alice"},
		},
		{
			name:    "mysql with upsert",
			dialect: my,
			table:   myTbl,
			opts: sql.InsertOptions{
				Columns:             []string{"email", "name"},
				Values:              []any{"a@b.com", "alice"},
				UpsertConflictKeys:  []string{"email"},
				UpsertUpdateColumns: []string{"name"},
			},
			wantSQL:  "INSERT INTO `users` (`email`, `name`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)",
			wantArgs: []any{"a@b.com", "alice"},
		},
		// A Create that supplies no column. PostgreSQL and SQLite
		// reject `() VALUES ()`; MySQL rejects DEFAULT VALUES.
		{
			name:     "postgres no column",
			dialect:  pg,
			table:    pgTbl,
			opts:     sql.InsertOptions{ReturningColumns: []string{"id"}},
			wantSQL:  `INSERT INTO "public"."users" DEFAULT VALUES RETURNING "id"`,
			wantArgs: []any{},
		},
		{
			name:     "mysql no column",
			dialect:  my,
			table:    myTbl,
			opts:     sql.InsertOptions{ReturningColumns: []string{"id"}},
			wantSQL:  "INSERT INTO `users` () VALUES ()",
			wantArgs: []any{},
		},
		{
			name:     "sqlite no column",
			dialect:  lite,
			table:    ltTbl,
			opts:     sql.InsertOptions{ReturningColumns: []string{"id"}},
			wantSQL:  `INSERT INTO "users" DEFAULT VALUES RETURNING "id"`,
			wantArgs: []any{},
		},
		// With no column supplied the conflict clause is omitted: SQLite
		// accepts none after DEFAULT VALUES.
		{
			name:    "postgres no column with upsert",
			dialect: pg,
			table:   pgTbl,
			opts: sql.InsertOptions{
				UpsertConflictKeys:    []string{"id"},
				UpsertResolvePKColumn: "id",
				ReturningColumns:      []string{"id"},
			},
			wantSQL:  `INSERT INTO "public"."users" DEFAULT VALUES RETURNING "id"`,
			wantArgs: []any{},
		},
		{
			name:    "mysql no column with upsert",
			dialect: my,
			table:   myTbl,
			opts: sql.InsertOptions{
				UpsertConflictKeys:    []string{"id"},
				UpsertResolvePKColumn: "id",
			},
			wantSQL:  "INSERT INTO `users` () VALUES ()",
			wantArgs: []any{},
		},
		{
			name:    "sqlite no column with upsert",
			dialect: lite,
			table:   ltTbl,
			opts: sql.InsertOptions{
				UpsertConflictKeys:    []string{"id"},
				UpsertResolvePKColumn: "id",
				ReturningColumns:      []string{"id"},
			},
			wantSQL:  `INSERT INTO "users" DEFAULT VALUES RETURNING "id"`,
			wantArgs: []any{},
		},
		// A conflict target naming a column the statement does not
		// write cannot match, so the clause is omitted. MySQL would otherwise
		// update whichever row a different unique index collided with.
		{
			name:    "postgres upsert target not supplied",
			dialect: pg,
			table:   pgTbl,
			opts: sql.InsertOptions{
				Columns:               []string{"email", "name"},
				Values:                []any{"a@b.com", "alice"},
				UpsertConflictKeys:    []string{"id"},
				UpsertUpdateColumns:   []string{"email", "name"},
				UpsertResolvePKColumn: "id",
				ReturningColumns:      []string{"id"},
			},
			wantSQL:  `INSERT INTO "public"."users" ("email", "name") VALUES ($1, $2) RETURNING "id"`,
			wantArgs: []any{"a@b.com", "alice"},
		},
		{
			name:    "mysql upsert target not supplied",
			dialect: my,
			table:   myTbl,
			opts: sql.InsertOptions{
				Columns:               []string{"email", "name"},
				Values:                []any{"a@b.com", "alice"},
				UpsertConflictKeys:    []string{"id"},
				UpsertUpdateColumns:   []string{"email", "name"},
				UpsertResolvePKColumn: "id",
			},
			wantSQL:  "INSERT INTO `users` (`email`, `name`) VALUES (?, ?)",
			wantArgs: []any{"a@b.com", "alice"},
		},
		{
			name:    "sqlite upsert composite target partly supplied",
			dialect: lite,
			table:   ltTbl,
			opts: sql.InsertOptions{
				Columns:             []string{"email", "name"},
				Values:              []any{"a@b.com", "alice"},
				UpsertConflictKeys:  []string{"email", "region"},
				UpsertUpdateColumns: []string{"name"},
			},
			wantSQL:  `INSERT INTO "users" ("email", "name") VALUES (?, ?)`,
			wantArgs: []any{"a@b.com", "alice"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildInsert(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildInsert() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildInsert() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildMultiInsert ---

func TestBuildMultiInsert(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.MultiInsertOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres multi-row",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns:   []string{"name", "id"},
				ValueRows: [][]any{{"alice", 1}, {"bob", 2}},
			},
			wantSQL:  `INSERT INTO "public"."users" ("id", "name") VALUES ($1, $2), ($3, $4)`,
			wantArgs: []any{1, "alice", 2, "bob"},
		},
		{
			name:    "mysql multi-row",
			dialect: my,
			table:   myTbl,
			opts: sql.MultiInsertOptions{
				Columns:   []string{"name", "id"},
				ValueRows: [][]any{{"alice", 1}, {"bob", 2}},
			},
			wantSQL:  "INSERT INTO `users` (`id`, `name`) VALUES (?, ?), (?, ?)",
			wantArgs: []any{1, "alice", 2, "bob"},
		},
		{
			name:    "sqlite multi-row",
			dialect: lite,
			table:   ltTbl,
			opts: sql.MultiInsertOptions{
				Columns:   []string{"name", "id"},
				ValueRows: [][]any{{"alice", 1}},
			},
			wantSQL:  `INSERT INTO "users" ("id", "name") VALUES (?, ?)`,
			wantArgs: []any{1, "alice"},
		},
		{
			name:    "postgres multi-row with RETURNING",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns:          []string{"name"},
				ValueRows:        [][]any{{"alice"}, {"bob"}},
				ReturningColumns: []string{"id"},
			},
			wantSQL:  `INSERT INTO "public"."users" ("name") VALUES ($1), ($2) RETURNING "id"`,
			wantArgs: []any{"alice", "bob"},
		},
		{
			name:    "zero-length batch",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns:   []string{"name"},
				ValueRows: nil,
			},
			wantSQL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildMultiInsert(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildMultiInsert() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildMultiInsert() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildMultiInsert upsert clause ---

func TestBuildMultiInsertUpsert(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.MultiInsertOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres conflict target with update columns",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns:             []string{"email", "name", "id"},
				ValueRows:           [][]any{{"a@b.com", "alice", 1}, {"c@d.com", "carol", 2}},
				UpsertConflictKeys:  []string{"email"},
				UpsertUpdateColumns: []string{"name"},
			},
			wantSQL:  `INSERT INTO "public"."users" ("email", "id", "name") VALUES ($1, $2, $3), ($4, $5, $6) ON CONFLICT ("email") DO UPDATE SET "name" = excluded."name"`,
			wantArgs: []any{"a@b.com", 1, "alice", "c@d.com", 2, "carol"},
		},
		{
			name:    "mysql conflict target with update columns",
			dialect: my,
			table:   myTbl,
			opts: sql.MultiInsertOptions{
				Columns:             []string{"email", "name"},
				ValueRows:           [][]any{{"a@b.com", "alice"}, {"c@d.com", "carol"}},
				UpsertConflictKeys:  []string{"email"},
				UpsertUpdateColumns: []string{"name"},
			},
			wantSQL:  "INSERT INTO `users` (`email`, `name`) VALUES (?, ?), (?, ?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)",
			wantArgs: []any{"a@b.com", "alice", "c@d.com", "carol"},
		},
		{
			name:    "sqlite conflict target with update columns",
			dialect: lite,
			table:   ltTbl,
			opts: sql.MultiInsertOptions{
				Columns:             []string{"email", "name"},
				ValueRows:           [][]any{{"a@b.com", "alice"}, {"c@d.com", "carol"}},
				UpsertConflictKeys:  []string{"email"},
				UpsertUpdateColumns: []string{"name"},
			},
			wantSQL:  `INSERT INTO "users" ("email", "name") VALUES (?, ?), (?, ?) ON CONFLICT ("email") DO UPDATE SET "name" = excluded."name"`,
			wantArgs: []any{"a@b.com", "alice", "c@d.com", "carol"},
		},
		{
			// Composite conflict target with nothing left to set — the pure
			// link table shape a batched junction upsert lands on.
			name:    "postgres composite target with no update columns",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns:            []string{"user_id", "role_id"},
				ValueRows:          [][]any{{1, 10}, {2, 20}},
				UpsertConflictKeys: []string{"user_id", "role_id"},
			},
			wantSQL:  `INSERT INTO "public"."users" ("role_id", "user_id") VALUES ($1, $2), ($3, $4) ON CONFLICT ("user_id", "role_id") DO NOTHING`,
			wantArgs: []any{10, 1, 20, 2},
		},
		{
			name:    "mysql composite target with no update columns",
			dialect: my,
			table:   myTbl,
			opts: sql.MultiInsertOptions{
				Columns:            []string{"user_id", "role_id"},
				ValueRows:          [][]any{{1, 10}, {2, 20}},
				UpsertConflictKeys: []string{"user_id", "role_id"},
			},
			wantSQL:  "INSERT INTO `users` (`role_id`, `user_id`) VALUES (?, ?), (?, ?) ON DUPLICATE KEY UPDATE `user_id` = `user_id`",
			wantArgs: []any{10, 1, 20, 2},
		},
		{
			name:    "sqlite composite target with no update columns",
			dialect: lite,
			table:   ltTbl,
			opts: sql.MultiInsertOptions{
				Columns:            []string{"user_id", "role_id"},
				ValueRows:          [][]any{{1, 10}, {2, 20}},
				UpsertConflictKeys: []string{"user_id", "role_id"},
			},
			wantSQL:  `INSERT INTO "users" ("role_id", "user_id") VALUES (?, ?), (?, ?) ON CONFLICT ("user_id", "role_id") DO NOTHING`,
			wantArgs: []any{10, 1, 20, 2},
		},
		{
			// ResolvePKColumn must lead the set list — MySQL evaluates
			// assignments left to right.
			name:    "mysql resolve PK column leads the set list",
			dialect: my,
			table:   myTbl,
			opts: sql.MultiInsertOptions{
				Columns:               []string{"email", "name"},
				ValueRows:             [][]any{{"a@b.com", "alice"}, {"c@d.com", "carol"}},
				UpsertConflictKeys:    []string{"email"},
				UpsertUpdateColumns:   []string{"name"},
				UpsertResolvePKColumn: "id",
			},
			wantSQL:  "INSERT INTO `users` (`email`, `name`) VALUES (?, ?), (?, ?) ON DUPLICATE KEY UPDATE `id` = LAST_INSERT_ID(`id`), `name` = VALUES(`name`)",
			wantArgs: []any{"a@b.com", "alice", "c@d.com", "carol"},
		},
		{
			// The clause precedes RETURNING, and ResolvePKColumn is ignored
			// on the RETURNING dialects.
			name:    "postgres upsert with RETURNING",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns:               []string{"email", "name"},
				ValueRows:             [][]any{{"a@b.com", "alice"}, {"c@d.com", "carol"}},
				ReturningColumns:      []string{"id"},
				UpsertConflictKeys:    []string{"email"},
				UpsertUpdateColumns:   []string{"name"},
				UpsertResolvePKColumn: "id",
			},
			wantSQL:  `INSERT INTO "public"."users" ("email", "name") VALUES ($1, $2), ($3, $4) ON CONFLICT ("email") DO UPDATE SET "name" = excluded."name" RETURNING "id"`,
			wantArgs: []any{"a@b.com", "alice", "c@d.com", "carol"},
		},
		{
			// A DEFAULT sentinel consumes no placeholder, so the clause must
			// not disturb the numbering the value rows established.
			name:    "postgres upsert after a DEFAULT sentinel",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns:             []string{"id", "name"},
				ValueRows:           [][]any{{sql.Default, "alice"}, {2, "bob"}},
				UpsertConflictKeys:  []string{"id"},
				UpsertUpdateColumns: []string{"name"},
			},
			wantSQL:  `INSERT INTO "public"."users" ("id", "name") VALUES (DEFAULT, $1), ($2, $3) ON CONFLICT ("id") DO UPDATE SET "name" = excluded."name"`,
			wantArgs: []any{"alice", 2, "bob"},
		},
		{
			// The empty-batch early return wins over the conflict clause.
			name:    "zero-length batch with a conflict target",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns:             []string{"name"},
				ValueRows:           nil,
				UpsertConflictKeys:  []string{"name"},
				UpsertUpdateColumns: []string{"name"},
			},
			wantSQL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildMultiInsert(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildMultiInsert() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildMultiInsert() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// conflictSuffix returns everything from a statement's conflict clause onward,
// or "" when it carries none. Both dialect spellings open with " ON ", and no
// identifier or value in these statements contains that token.
func conflictSuffix(stmt string) string {
	for _, marker := range []string{" ON CONFLICT ", " ON DUPLICATE KEY UPDATE "} {
		if i := strings.Index(stmt, marker); i >= 0 {
			return stmt[i+1:]
		}
	}
	return ""
}

// TestBuildMultiInsertUpsertMatchesSingleRow pins the acceptance criterion: for
// the same conflict target, the batched builder emits the byte-identical clause
// the single-row builder does on every dialect. The clause governs the whole
// statement, so it cannot vary with the row count.
func TestBuildMultiInsertUpsertMatchesSingleRow(t *testing.T) {
	dialects := []struct {
		name    string
		dialect sql.Dialect
		table   sql.Table
	}{
		{"postgres", pg, pgTbl},
		{"mysql", my, myTbl},
		{"sqlite", lite, ltTbl},
	}

	shapes := []struct {
		name            string
		columns         []string
		row             []any
		conflictKeys    []string
		updateColumns   []string
		resolvePKColumn string
	}{
		{
			name:          "single key with update columns",
			columns:       []string{"email", "name"},
			row:           []any{"a@b.com", "alice"},
			conflictKeys:  []string{"email"},
			updateColumns: []string{"name"},
		},
		{
			name:         "composite key with nothing to update",
			columns:      []string{"user_id", "role_id"},
			row:          []any{1, 10},
			conflictKeys: []string{"user_id", "role_id"},
		},
		{
			name:            "resolve PK column",
			columns:         []string{"email", "name"},
			row:             []any{"a@b.com", "alice"},
			conflictKeys:    []string{"email"},
			updateColumns:   []string{"name"},
			resolvePKColumn: "id",
		},
	}

	for _, d := range dialects {
		for _, s := range shapes {
			t.Run(d.name+"/"+s.name, func(t *testing.T) {
				single, _ := sql.BuildInsert(d.dialect, d.table, sql.InsertOptions{
					Columns:               s.columns,
					Values:                s.row,
					UpsertConflictKeys:    s.conflictKeys,
					UpsertUpdateColumns:   s.updateColumns,
					UpsertResolvePKColumn: s.resolvePKColumn,
				})
				multi, _ := sql.BuildMultiInsert(d.dialect, d.table, sql.MultiInsertOptions{
					Columns:               s.columns,
					ValueRows:             [][]any{s.row, s.row},
					UpsertConflictKeys:    s.conflictKeys,
					UpsertUpdateColumns:   s.updateColumns,
					UpsertResolvePKColumn: s.resolvePKColumn,
				})

				want := conflictSuffix(single)
				if want == "" {
					t.Fatalf("BuildInsert() emitted no conflict clause: %q", single)
				}
				if got := conflictSuffix(multi); got != want {
					t.Errorf("BuildMultiInsert() clause = %q, want %q (from %q)", got, want, single)
				}
			})
		}
	}
}

// TestBuildMultiInsertNoUpsert pins the back-compatible default: the clause is
// gated on ConflictKeys alone, exactly as BuildInsert gates it, so the fields
// nothing sets yet move no output.
func TestBuildMultiInsertNoUpsert(t *testing.T) {
	tests := []struct {
		name string
		opts sql.MultiInsertOptions
	}{
		{
			name: "no upsert fields set",
			opts: sql.MultiInsertOptions{
				Columns:   []string{"name"},
				ValueRows: [][]any{{"alice"}, {"bob"}},
			},
		},
		{
			name: "update columns without a conflict target",
			opts: sql.MultiInsertOptions{
				Columns:             []string{"name"},
				ValueRows:           [][]any{{"alice"}, {"bob"}},
				UpsertUpdateColumns: []string{"name"},
			},
		},
		{
			name: "resolve PK column without a conflict target",
			opts: sql.MultiInsertOptions{
				Columns:               []string{"name"},
				ValueRows:             [][]any{{"alice"}, {"bob"}},
				UpsertResolvePKColumn: "id",
			},
		},
	}

	for _, tt := range tests {
		for _, d := range []struct {
			name    string
			dialect sql.Dialect
			table   sql.Table
		}{
			{"postgres", pg, pgTbl},
			{"mysql", my, myTbl},
			{"sqlite", lite, ltTbl},
		} {
			t.Run(tt.name+"/"+d.name, func(t *testing.T) {
				got, _ := sql.BuildMultiInsert(d.dialect, d.table, tt.opts)
				if suffix := conflictSuffix(got); suffix != "" {
					t.Errorf("BuildMultiInsert() emitted a conflict clause %q in %q, want none", suffix, got)
				}
			})
		}
	}
}

// --- BuildMultiInsert with sql.Default ---

func TestBuildMultiInsertDefault(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.MultiInsertOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres default in some positions",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns: []string{"name", "id", "company_id"},
				ValueRows: [][]any{
					{"alice", 1, sql.Default},
					{"bob", 2, sql.Default},
				},
			},
			wantSQL:  `INSERT INTO "public"."users" ("company_id", "id", "name") VALUES (DEFAULT, $1, $2), (DEFAULT, $3, $4)`,
			wantArgs: []any{1, "alice", 2, "bob"},
		},
		{
			name:    "mysql default in some positions",
			dialect: my,
			table:   myTbl,
			opts: sql.MultiInsertOptions{
				Columns: []string{"name", "id", "company_id"},
				ValueRows: [][]any{
					{"alice", 1, sql.Default},
					{"bob", 2, sql.Default},
				},
			},
			wantSQL:  "INSERT INTO `users` (`company_id`, `id`, `name`) VALUES (DEFAULT, ?, ?), (DEFAULT, ?, ?)",
			wantArgs: []any{1, "alice", 2, "bob"},
		},
		{
			name:    "sqlite default in some positions",
			dialect: lite,
			table:   ltTbl,
			opts: sql.MultiInsertOptions{
				Columns: []string{"name", "id", "company_id"},
				ValueRows: [][]any{
					{"alice", 1, sql.Default},
				},
			},
			wantSQL:  `INSERT INTO "users" ("company_id", "id", "name") VALUES (DEFAULT, ?, ?)`,
			wantArgs: []any{1, "alice"},
		},
		{
			name:    "mixed rows some with default some with real values",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns: []string{"id", "name", "company_id", "deleted_at"},
				ValueRows: [][]any{
					{1, "alice", sql.Default, sql.Default},
					{2, "bob", 42, sql.Default},
					{sql.Default, "charlie", 99, sql.Default},
				},
			},
			wantSQL:  `INSERT INTO "public"."users" ("company_id", "deleted_at", "id", "name") VALUES (DEFAULT, DEFAULT, $1, $2), ($3, DEFAULT, $4, $5), ($6, DEFAULT, DEFAULT, $7)`,
			wantArgs: []any{1, "alice", 42, 2, "bob", 99, "charlie"},
		},
		{
			name:    "postgres with default and RETURNING",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns: []string{"name", "id"},
				ValueRows: [][]any{
					{"alice", sql.Default},
					{"bob", sql.Default},
				},
				ReturningColumns: []string{"id"},
			},
			wantSQL:  `INSERT INTO "public"."users" ("id", "name") VALUES (DEFAULT, $1), (DEFAULT, $2) RETURNING "id"`,
			wantArgs: []any{"alice", "bob"},
		},
		{
			name:    "no defaults behaves normally",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns:   []string{"name", "id"},
				ValueRows: [][]any{{"alice", 1}, {"bob", 2}},
			},
			wantSQL:  `INSERT INTO "public"."users" ("id", "name") VALUES ($1, $2), ($3, $4)`,
			wantArgs: []any{1, "alice", 2, "bob"},
		},
		{
			name:    "all defaults in a row",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns: []string{"id", "name"},
				ValueRows: [][]any{
					{sql.Default, sql.Default},
				},
			},
			wantSQL:  `INSERT INTO "public"."users" ("id", "name") VALUES (DEFAULT, DEFAULT)`,
			wantArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildMultiInsert(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildMultiInsert() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildMultiInsert() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildMultiInsert with sql.DefaultExpr ---

func TestBuildMultiInsertDefaultExpr(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.MultiInsertOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "sqlite DefaultExpr emits raw expression",
			dialect: lite,
			table:   ltTbl,
			opts: sql.MultiInsertOptions{
				Columns: []string{"name", "role", "bio"},
				ValueRows: [][]any{
					{"alice", sql.NewDefaultExpr("'viewer'"), sql.NewDefaultExpr("NULL")},
					{"bob", sql.NewDefaultExpr("'viewer'"), sql.NewDefaultExpr("NULL")},
				},
			},
			wantSQL:  `INSERT INTO "users" ("bio", "name", "role") VALUES (NULL, ?, 'viewer'), (NULL, ?, 'viewer')`,
			wantArgs: []any{"alice", "bob"},
		},
		{
			name:    "sqlite DefaultExpr with function call",
			dialect: lite,
			table:   ltTbl,
			opts: sql.MultiInsertOptions{
				Columns: []string{"name", "created_at"},
				ValueRows: [][]any{
					{"alice", sql.NewDefaultExpr("datetime('now')")},
				},
				ReturningColumns: []string{"id"},
			},
			wantSQL:  `INSERT INTO "users" ("created_at", "name") VALUES (datetime('now'), ?) RETURNING "id"`,
			wantArgs: []any{"alice"},
		},
		{
			name:    "mixed DefaultExpr and real values",
			dialect: lite,
			table:   ltTbl,
			opts: sql.MultiInsertOptions{
				Columns: []string{"name", "role", "bio"},
				ValueRows: [][]any{
					{"alice", "admin", sql.NewDefaultExpr("NULL")},
					{"bob", sql.NewDefaultExpr("'viewer'"), "hello"},
				},
			},
			wantSQL:  `INSERT INTO "users" ("bio", "name", "role") VALUES (NULL, ?, ?), (?, ?, 'viewer')`,
			wantArgs: []any{"alice", "admin", "hello", "bob"},
		},
		{
			name:    "postgres still uses Default not DefaultExpr",
			dialect: pg,
			table:   pgTbl,
			opts: sql.MultiInsertOptions{
				Columns: []string{"name", "role"},
				ValueRows: [][]any{
					{"alice", sql.Default},
				},
			},
			wantSQL:  `INSERT INTO "public"."users" ("name", "role") VALUES ($1, DEFAULT)`,
			wantArgs: []any{"alice"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildMultiInsert(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildMultiInsert() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildMultiInsert() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildUpdate ---

func TestBuildUpdate(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.UpdateOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres basic update",
			dialect: pg,
			table:   pgTbl,
			opts: sql.UpdateOptions{
				SetClauses: map[string]any{"name": "alice", "email": "a@b.com"},
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 1}},
			},
			wantSQL:  `UPDATE "public"."users" SET "email" = $1, "name" = $2 WHERE "id" = $3`,
			wantArgs: []any{"a@b.com", "alice", 1},
		},
		{
			name:    "mysql basic update",
			dialect: my,
			table:   myTbl,
			opts: sql.UpdateOptions{
				SetClauses: map[string]any{"name": "bob"},
				Conditions: []sql.Condition{{Clause: "`id` = $", Value: 2}},
			},
			wantSQL:  "UPDATE `users` SET `name` = ? WHERE `id` = ?",
			wantArgs: []any{"bob", 2},
		},
		{
			name:    "sqlite basic update",
			dialect: lite,
			table:   ltTbl,
			opts: sql.UpdateOptions{
				SetClauses: map[string]any{"name": "charlie"},
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 3}},
			},
			wantSQL:  `UPDATE "users" SET "name" = ? WHERE "id" = ?`,
			wantArgs: []any{"charlie", 3},
		},
		{
			name:    "postgres update with RETURNING",
			dialect: pg,
			table:   pgTbl,
			opts: sql.UpdateOptions{
				SetClauses:       map[string]any{"name": "alice"},
				Conditions:       []sql.Condition{{Clause: `"id" = $`, Value: 1}},
				ReturningColumns: []string{"id", "name", "updated_at"},
			},
			wantSQL:  `UPDATE "public"."users" SET "name" = $1 WHERE "id" = $2 RETURNING "id", "name", "updated_at"`,
			wantArgs: []any{"alice", 1},
		},
		{
			name:    "sorted column order in SET",
			dialect: pg,
			table:   pgTbl,
			opts: sql.UpdateOptions{
				SetClauses: map[string]any{"z_col": 3, "a_col": 1, "m_col": 2},
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 1}},
			},
			wantSQL:  `UPDATE "public"."users" SET "a_col" = $1, "m_col" = $2, "z_col" = $3 WHERE "id" = $4`,
			wantArgs: []any{1, 2, 3, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildUpdate(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildUpdate() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildUpdate() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildCount ---

func TestBuildCount(t *testing.T) {
	tests := []struct {
		name       string
		dialect    sql.Dialect
		table      sql.Table
		conditions []sql.Condition
		wantSQL    string
		wantArgs   []any
	}{
		{
			name:       "postgres with conditions",
			dialect:    pg,
			table:      pgTbl,
			conditions: []sql.Condition{{Clause: `"active" = $`, Value: true}},
			wantSQL:    `SELECT COUNT(*) FROM "public"."users" WHERE "active" = $1`,
			wantArgs:   []any{true},
		},
		{
			name:    "mysql with conditions",
			dialect: my,
			table:   myTbl,
			conditions: []sql.Condition{
				{Clause: "`role` = $", Value: "admin"},
			},
			wantSQL:  "SELECT COUNT(*) FROM `users` WHERE `role` = ?",
			wantArgs: []any{"admin"},
		},
		{
			name:    "empty conditions count all",
			dialect: pg,
			table:   pgTbl,
			wantSQL: `SELECT COUNT(*) FROM "public"."users"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildCount(tt.dialect, tt.table, tt.conditions)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildCount() sql = %q, want %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildCount() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildExists ---

func TestBuildExists(t *testing.T) {
	tests := []struct {
		name       string
		dialect    sql.Dialect
		table      sql.Table
		conditions []sql.Condition
		wantSQL    string
		wantArgs   []any
	}{
		{
			name:       "postgres",
			dialect:    pg,
			table:      pgTbl,
			conditions: []sql.Condition{{Clause: `"id" = $`, Value: 42}},
			wantSQL:    `SELECT EXISTS(SELECT 1 FROM "public"."users" WHERE "id" = $1)`,
			wantArgs:   []any{42},
		},
		{
			name:       "mysql",
			dialect:    my,
			table:      myTbl,
			conditions: []sql.Condition{{Clause: "`id` = $", Value: 42}},
			wantSQL:    "SELECT EXISTS(SELECT 1 FROM `users` WHERE `id` = ?)",
			wantArgs:   []any{42},
		},
		{
			name:       "sqlite",
			dialect:    lite,
			table:      ltTbl,
			conditions: []sql.Condition{{Clause: `"id" = $`, Value: 42}},
			wantSQL:    `SELECT EXISTS(SELECT 1 FROM "users" WHERE "id" = ?)`,
			wantArgs:   []any{42},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildExists(tt.dialect, tt.table, tt.conditions)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildExists() sql = %q, want %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildExists() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildIncrement ---

func TestBuildIncrement(t *testing.T) {
	tests := []struct {
		name       string
		dialect    sql.Dialect
		table      sql.Table
		column     string
		amount     any
		conditions []sql.Condition
		wantSQL    string
		wantArgs   []any
	}{
		{
			name:       "postgres",
			dialect:    pg,
			table:      pgTbl,
			column:     "view_count",
			amount:     1,
			conditions: []sql.Condition{{Clause: `"id" = $`, Value: 42}},
			wantSQL:    `UPDATE "public"."users" SET "view_count" = "view_count" + $1 WHERE "id" = $2`,
			wantArgs:   []any{1, 42},
		},
		{
			name:       "mysql",
			dialect:    my,
			table:      myTbl,
			column:     "view_count",
			amount:     5,
			conditions: []sql.Condition{{Clause: "`id` = $", Value: 42}},
			wantSQL:    "UPDATE `users` SET `view_count` = `view_count` + ? WHERE `id` = ?",
			wantArgs:   []any{5, 42},
		},
		{
			name:       "sqlite",
			dialect:    lite,
			table:      ltTbl,
			column:     "score",
			amount:     10,
			conditions: []sql.Condition{{Clause: `"id" = $`, Value: 1}},
			wantSQL:    `UPDATE "users" SET "score" = "score" + ? WHERE "id" = ?`,
			wantArgs:   []any{10, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildIncrement(tt.dialect, tt.table, tt.column, tt.amount, tt.conditions)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildIncrement() sql = %q, want %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildIncrement() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildIncrementReturning(t *testing.T) {
	tests := []struct {
		name             string
		dialect          sql.Dialect
		table            sql.Table
		column           string
		amount           any
		conditions       []sql.Condition
		returningColumns []string
		wantSQL          string
		wantArgs         []any
	}{
		{
			name:             "postgres appends returning clause",
			dialect:          pg,
			table:            pgTbl,
			column:           "view_count",
			amount:           1,
			conditions:       []sql.Condition{{Clause: `"id" = $`, Value: 42}},
			returningColumns: []string{"workspace_id"},
			wantSQL:          `UPDATE "public"."users" SET "view_count" = "view_count" + $1 WHERE "id" = $2 RETURNING "workspace_id"`,
			wantArgs:         []any{1, 42},
		},
		{
			name:             "sqlite appends returning clause",
			dialect:          lite,
			table:            ltTbl,
			column:           "score",
			amount:           10,
			conditions:       []sql.Condition{{Clause: `"id" = $`, Value: 1}},
			returningColumns: []string{"workspace_id"},
			wantSQL:          `UPDATE "users" SET "score" = "score" + ? WHERE "id" = ? RETURNING "workspace_id"`,
			wantArgs:         []any{10, 1},
		},
		{
			name:             "mysql omits returning clause",
			dialect:          my,
			table:            myTbl,
			column:           "view_count",
			amount:           5,
			conditions:       []sql.Condition{{Clause: "`id` = $", Value: 42}},
			returningColumns: []string{"workspace_id"},
			wantSQL:          "UPDATE `users` SET `view_count` = `view_count` + ? WHERE `id` = ?",
			wantArgs:         []any{5, 42},
		},
		{
			name:       "empty returning columns matches BuildIncrement",
			dialect:    pg,
			table:      pgTbl,
			column:     "view_count",
			amount:     1,
			conditions: []sql.Condition{{Clause: `"id" = $`, Value: 42}},
			wantSQL:    `UPDATE "public"."users" SET "view_count" = "view_count" + $1 WHERE "id" = $2`,
			wantArgs:   []any{1, 42},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildIncrementReturning(tt.dialect, tt.table, tt.column, tt.amount, tt.conditions, tt.returningColumns)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildIncrementReturning() sql = %q, want %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildIncrementReturning() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildSoftDelete ---

func TestBuildSoftDelete(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.SoftDeleteOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "timestamp",
			dialect: pg,
			table:   pgTbl,
			opts: sql.SoftDeleteOptions{
				Column:     "deleted_at",
				Type:       sql.SoftDeleteTimestamp,
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 1}},
			},
			wantSQL:  `UPDATE "public"."users" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" = $1`,
			wantArgs: []any{1},
		},
		{
			name:    "bool",
			dialect: my,
			table:   myTbl,
			opts: sql.SoftDeleteOptions{
				Column:     "is_deleted",
				Type:       sql.SoftDeleteBool,
				Conditions: []sql.Condition{{Clause: "`id` = $", Value: 2}},
			},
			wantSQL:  "UPDATE `users` SET `is_deleted` = TRUE WHERE `id` = ?",
			wantArgs: []any{2},
		},
		{
			name:    "integer",
			dialect: lite,
			table:   ltTbl,
			opts: sql.SoftDeleteOptions{
				Column:     "deleted",
				Type:       sql.SoftDeleteInteger,
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 3}},
			},
			wantSQL:  `UPDATE "users" SET "deleted" = 1 WHERE "id" = ?`,
			wantArgs: []any{3},
		},
		{
			name:    "timestamp with returning",
			dialect: pg,
			table:   pgTbl,
			opts: sql.SoftDeleteOptions{
				Column:           "deleted_at",
				Type:             sql.SoftDeleteTimestamp,
				Conditions:       []sql.Condition{{Clause: `"name" = $`, Value: "alice"}},
				ReturningColumns: []string{"id"},
			},
			wantSQL:  `UPDATE "public"."users" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "name" = $1 RETURNING "id"`,
			wantArgs: []any{"alice"},
		},
		{
			name:    "timestamp with returning multiple columns",
			dialect: pg,
			table:   pgTbl,
			opts: sql.SoftDeleteOptions{
				Column:           "deleted_at",
				Type:             sql.SoftDeleteTimestamp,
				Conditions:       []sql.Condition{{Clause: `"active" = $`, Value: true}},
				ReturningColumns: []string{"id", "name"},
			},
			wantSQL:  `UPDATE "public"."users" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "active" = $1 RETURNING "id", "name"`,
			wantArgs: []any{true},
		},
		{
			name:    "returning ignored on mysql",
			dialect: my,
			table:   myTbl,
			opts: sql.SoftDeleteOptions{
				Column:           "deleted_at",
				Type:             sql.SoftDeleteTimestamp,
				Conditions:       []sql.Condition{{Clause: "`id` = $", Value: 1}},
				ReturningColumns: []string{"id"},
			},
			wantSQL:  "UPDATE `users` SET `deleted_at` = CURRENT_TIMESTAMP WHERE `id` = ?",
			wantArgs: []any{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSoftDelete(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSoftDelete() sql = %q, want %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSoftDelete() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- BuildHardDelete ---

func TestBuildHardDelete(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.HardDeleteOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres",
			dialect: pg,
			table:   pgTbl,
			opts: sql.HardDeleteOptions{
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 1}},
			},
			wantSQL:  `DELETE FROM "public"."users" WHERE "id" = $1`,
			wantArgs: []any{1},
		},
		{
			name:    "mysql",
			dialect: my,
			table:   myTbl,
			opts: sql.HardDeleteOptions{
				Conditions: []sql.Condition{{Clause: "`id` = $", Value: 2}},
			},
			wantSQL:  "DELETE FROM `users` WHERE `id` = ?",
			wantArgs: []any{2},
		},
		{
			name:    "sqlite",
			dialect: lite,
			table:   ltTbl,
			opts: sql.HardDeleteOptions{
				Conditions: []sql.Condition{{Clause: `"id" = $`, Value: 3}},
			},
			wantSQL:  `DELETE FROM "users" WHERE "id" = ?`,
			wantArgs: []any{3},
		},
		{
			name:    "postgres with returning",
			dialect: pg,
			table:   pgTbl,
			opts: sql.HardDeleteOptions{
				Conditions:       []sql.Condition{{Clause: `"id" = $`, Value: 1}},
				ReturningColumns: []string{"id"},
			},
			wantSQL:  `DELETE FROM "public"."users" WHERE "id" = $1 RETURNING "id"`,
			wantArgs: []any{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildHardDelete(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildHardDelete() sql = %q, want %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildHardDelete() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- WHERE clause expansion ---

func TestWhereAndComposition(t *testing.T) {
	cond := sql.And(
		sql.Condition{Clause: `"a" = $`, Value: 1},
		sql.Condition{Clause: `"b" = $`, Value: 2},
	)
	gotSQL, gotArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
		Conditions: []sql.Condition{cond},
	})
	wantSQL := `SELECT * FROM "public"."users" WHERE ("a" = $1 AND "b" = $2)`
	if gotSQL != wantSQL {
		t.Errorf("And composition sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{1, 2}, gotArgs); diff != "" {
		t.Errorf("And composition args mismatch (-want +got):\n%s", diff)
	}
}

func TestWhereOrComposition(t *testing.T) {
	cond := sql.Or(
		sql.Condition{Clause: `"role" = $`, Value: "admin"},
		sql.Condition{Clause: `"role" = $`, Value: "mod"},
	)
	gotSQL, gotArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
		Conditions: []sql.Condition{cond},
	})
	wantSQL := `SELECT * FROM "public"."users" WHERE ("role" = $1 OR "role" = $2)`
	if gotSQL != wantSQL {
		t.Errorf("Or composition sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{"admin", "mod"}, gotArgs); diff != "" {
		t.Errorf("Or composition args mismatch (-want +got):\n%s", diff)
	}
}

func TestWhereNestedAndOr(t *testing.T) {
	cond := sql.And(
		sql.Condition{Clause: `"active" = $`, Value: true},
		sql.Or(
			sql.Condition{Clause: `"role" = $`, Value: "admin"},
			sql.And(
				sql.Condition{Clause: `"role" = $`, Value: "user"},
				sql.Condition{Clause: `"verified" = $`, Value: true},
			),
		),
	)
	gotSQL, gotArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
		Conditions: []sql.Condition{cond},
	})
	wantSQL := `SELECT * FROM "public"."users" WHERE ("active" = $1 AND ("role" = $2 OR ("role" = $3 AND "verified" = $4)))`
	if gotSQL != wantSQL {
		t.Errorf("Nested And/Or sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{true, "admin", "user", true}, gotArgs); diff != "" {
		t.Errorf("Nested And/Or args mismatch (-want +got):\n%s", diff)
	}
}

func TestWhereBetween(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     "postgres",
			dialect:  pg,
			wantSQL:  `SELECT * FROM "public"."users" WHERE "price" BETWEEN $1 AND $2`,
			wantArgs: []any{10, 100},
		},
		{
			name:     "mysql",
			dialect:  my,
			wantSQL:  "SELECT * FROM `users` WHERE `price` BETWEEN ? AND ?",
			wantArgs: []any{10, 100},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tbl := pgTbl
			col := `"price"`
			if tt.dialect.Name() == "mysql" {
				tbl = myTbl
				col = "`price`"
			}
			cond := sql.Condition{
				Clause: col + " BETWEEN $ AND $",
				Value:  sql.Range{Start: 10, End: 100},
			}
			gotSQL, gotArgs := sql.BuildSelect(tt.dialect, tbl, sql.SelectOptions{
				Conditions: []sql.Condition{cond},
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("Between sql = %q, want %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("Between args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWhereINExpansion(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		cond     sql.Condition
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres pgx uses ANY",
			dialect: pg,
			table:   pgTbl,
			// pgx IN conditions come from comparator with = ANY($) clause
			cond:     sql.Condition{Clause: `"status" = ANY($)`, Value: []string{"active", "pending"}},
			wantSQL:  `SELECT * FROM "public"."users" WHERE "status" = ANY($1)`,
			wantArgs: []any{[]string{"active", "pending"}},
		},
		{
			name:     "postgres stdlib expanded IN",
			dialect:  pgStd,
			table:    pgTbl,
			cond:     sql.Condition{Clause: `"status" IN $`, Value: []any{"active", "pending"}},
			wantSQL:  `SELECT * FROM "public"."users" WHERE "status" IN ($1, $2)`,
			wantArgs: []any{"active", "pending"},
		},
		{
			name:     "mysql expanded IN",
			dialect:  my,
			table:    myTbl,
			cond:     sql.Condition{Clause: "`status` IN $", Value: []any{"active", "pending"}},
			wantSQL:  "SELECT * FROM `users` WHERE `status` IN (?, ?)",
			wantArgs: []any{"active", "pending"},
		},
		{
			name:     "sqlite expanded IN",
			dialect:  lite,
			table:    ltTbl,
			cond:     sql.Condition{Clause: `"status" IN $`, Value: []any{"active", "pending"}},
			wantSQL:  `SELECT * FROM "users" WHERE "status" IN (?, ?)`,
			wantArgs: []any{"active", "pending"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.dialect, tt.table, sql.SelectOptions{
				Conditions: []sql.Condition{tt.cond},
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("IN sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("IN args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWhereIsNullIsNotNull(t *testing.T) {
	conds := []sql.Condition{
		{Clause: `"deleted_at" IS NULL`},
		{Clause: `"email" IS NOT NULL`},
		{Clause: `"name" = $`, Value: "alice"},
	}
	gotSQL, gotArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
		Conditions: conds,
	})
	wantSQL := `SELECT * FROM "public"."users" WHERE "deleted_at" IS NULL AND "email" IS NOT NULL AND "name" = $1`
	if gotSQL != wantSQL {
		t.Errorf("IS NULL sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	// IS NULL / IS NOT NULL consume no placeholder
	if diff := cmp.Diff([]any{"alice"}, gotArgs); diff != "" {
		t.Errorf("IS NULL args mismatch (-want +got):\n%s", diff)
	}
}

func TestWhereEmptyINList(t *testing.T) {
	tests := []struct {
		name    string
		cond    sql.Condition
		wantSQL string
	}{
		{
			name:    "empty IN produces always false",
			cond:    sql.Condition{Clause: `"id" IN $`, Value: []any{}},
			wantSQL: `SELECT * FROM "public"."users" WHERE 1 = 0`,
		},
		{
			name:    "empty NOT IN produces always true",
			cond:    sql.Condition{Clause: `"id" NOT IN $`, Value: []any{}},
			wantSQL: `SELECT * FROM "public"."users" WHERE 1 = 1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
				Conditions: []sql.Condition{tt.cond},
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("empty IN sql = %q, want %q", gotSQL, tt.wantSQL)
			}
			if len(gotArgs) != 0 {
				t.Errorf("empty IN args = %v, want empty", gotArgs)
			}
		})
	}
}

func TestWhereSubquery(t *testing.T) {
	cond := sql.Condition{
		Clause: `"company_id" IN $`,
		Value: sql.Subquery{
			SQL:  `SELECT "id" FROM "public"."companies" WHERE "country" = $`,
			Args: []any{"US"},
		},
	}
	gotSQL, gotArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
		Conditions: []sql.Condition{cond},
	})
	wantSQL := `SELECT * FROM "public"."users" WHERE "company_id" IN (SELECT "id" FROM "public"."companies" WHERE "country" = $1)`
	if gotSQL != wantSQL {
		t.Errorf("Subquery sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{"US"}, gotArgs); diff != "" {
		t.Errorf("Subquery args mismatch (-want +got):\n%s", diff)
	}
}

// TestWhereInSubquery covers the documented subquery form (PRD 11.5,
// guidelines/SQL.md §13): In / Nin given a single Subquery render a nested
// SELECT rather than binding the Subquery as a parameter, and the subquery's
// "$" tokens are numbered into the enclosing statement's sequence, here
// between two other arg-bearing conditions.
func TestWhereInSubquery(t *testing.T) {
	sub := sql.Subquery{
		SQL:  `SELECT "id" FROM "companies" WHERE "country" = $ AND "size" > $`,
		Args: []any{"US", 10},
	}
	tests := []struct {
		name    string
		d       sql.Dialect
		tbl     sql.Table
		cond    sql.Condition
		wantSQL string
	}{
		{
			"postgres in", pg, pgTbl, sql.Where("company_id").In(sub),
			`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT "id" FROM "companies" WHERE "country" = $2 AND "size" > $3) AND age > $4`,
		},
		{
			"postgres not in", pg, pgTbl, sql.Where("company_id").Nin(sub),
			`SELECT * FROM "public"."users" WHERE active = $1 AND company_id NOT IN (SELECT "id" FROM "companies" WHERE "country" = $2 AND "size" > $3) AND age > $4`,
		},
		{
			"mysql in", my, myTbl, sql.Where("company_id").In(sub),
			"SELECT * FROM `users` WHERE active = ? AND company_id IN (SELECT \"id\" FROM \"companies\" WHERE \"country\" = ? AND \"size\" > ?) AND age > ?",
		},
		{
			"mysql not in", my, myTbl, sql.Where("company_id").Nin(sub),
			"SELECT * FROM `users` WHERE active = ? AND company_id NOT IN (SELECT \"id\" FROM \"companies\" WHERE \"country\" = ? AND \"size\" > ?) AND age > ?",
		},
		{
			"sqlite in", lite, ltTbl, sql.Where("company_id").In(sub),
			`SELECT * FROM "users" WHERE active = ? AND company_id IN (SELECT "id" FROM "companies" WHERE "country" = ? AND "size" > ?) AND age > ?`,
		},
		{
			"sqlite not in", lite, ltTbl, sql.Where("company_id").Nin(sub),
			`SELECT * FROM "users" WHERE active = ? AND company_id NOT IN (SELECT "id" FROM "companies" WHERE "country" = ? AND "size" > ?) AND age > ?`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.d, tt.tbl, sql.SelectOptions{
				Conditions: []sql.Condition{sql.Where("active").Eq(true), tt.cond, sql.Where("age").Gt(3)},
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff([]any{true, "US", 10, 3}, gotArgs); diff != "" {
				t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSubquery_NumberedPlaceholders covers the second token form PRD 11.5
// allows: "$N" is the subquery's own Args[N-1]. PostgreSQL renders
// the absolute position in the enclosing statement, so a repeated "$N" reuses
// one position and the args stay as written; a "?" dialect emits one "?" per
// token and orders, and duplicates, the args to match. Before the fix "$1"
// rendered as "$21" on PostgreSQL and as "?1" on SQLite.
func TestSubquery_NumberedPlaceholders(t *testing.T) {
	type want struct {
		sql  string
		args []any
	}
	tests := []struct {
		name string
		cond func(sql.Subquery) sql.Condition
		sub  sql.Subquery
		// The statement is active = $, the condition, then age > $, so the
		// subquery sits at a non-first position and the next condition
		// shows where numbering resumes.
		pg, my, lite want
	}{
		{
			name: "in order",
			cond: func(s sql.Subquery) sql.Condition { return sql.Where("company_id").In(s) },
			sub:  sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "country" = $1 AND "size" > $2`, Args: []any{"US", 10}},
			pg: want{
				`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT "id" FROM "companies" WHERE "country" = $2 AND "size" > $3) AND age > $4`,
				[]any{true, "US", 10, 3},
			},
			my: want{
				"SELECT * FROM `users` WHERE active = ? AND company_id IN (SELECT \"id\" FROM \"companies\" WHERE \"country\" = ? AND \"size\" > ?) AND age > ?",
				[]any{true, "US", 10, 3},
			},
			lite: want{
				`SELECT * FROM "users" WHERE active = ? AND company_id IN (SELECT "id" FROM "companies" WHERE "country" = ? AND "size" > ?) AND age > ?`,
				[]any{true, "US", 10, 3},
			},
		},
		{
			name: "out of order",
			cond: func(s sql.Subquery) sql.Condition { return sql.Where("company_id").Nin(s) },
			sub:  sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "size" > $2 AND "country" = $1`, Args: []any{"US", 10}},
			pg: want{
				`SELECT * FROM "public"."users" WHERE active = $1 AND company_id NOT IN (SELECT "id" FROM "companies" WHERE "size" > $3 AND "country" = $2) AND age > $4`,
				[]any{true, "US", 10, 3},
			},
			my: want{
				"SELECT * FROM `users` WHERE active = ? AND company_id NOT IN (SELECT \"id\" FROM \"companies\" WHERE \"size\" > ? AND \"country\" = ?) AND age > ?",
				[]any{true, 10, "US", 3},
			},
			lite: want{
				`SELECT * FROM "users" WHERE active = ? AND company_id NOT IN (SELECT "id" FROM "companies" WHERE "size" > ? AND "country" = ?) AND age > ?`,
				[]any{true, 10, "US", 3},
			},
		},
		{
			name: "reused",
			cond: func(s sql.Subquery) sql.Condition { return sql.Where("company_id").In(s) },
			sub:  sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "country" = $1 OR "region" = $1`, Args: []any{"US"}},
			pg: want{
				`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT "id" FROM "companies" WHERE "country" = $2 OR "region" = $2) AND age > $3`,
				[]any{true, "US", 3},
			},
			my: want{
				"SELECT * FROM `users` WHERE active = ? AND company_id IN (SELECT \"id\" FROM \"companies\" WHERE \"country\" = ? OR \"region\" = ?) AND age > ?",
				[]any{true, "US", "US", 3},
			},
			lite: want{
				`SELECT * FROM "users" WHERE active = ? AND company_id IN (SELECT "id" FROM "companies" WHERE "country" = ? OR "region" = ?) AND age > ?`,
				[]any{true, "US", "US", 3},
			},
		},
		{
			name: "reused and out of order inside exists",
			cond: func(s sql.Subquery) sql.Condition { return sql.Exists("id", s) },
			sub: sql.Subquery{
				SQL:  `SELECT 1 FROM "orders" o WHERE o."user_id" = ` + sql.CorrelationToken + ` AND o."total" > $2 AND (o."status" = $1 OR o."prev_status" = $1)`,
				Args: []any{"paid", 100},
			},
			pg: want{
				`SELECT * FROM "public"."users" WHERE active = $1 AND EXISTS (SELECT 1 FROM "orders" o WHERE o."user_id" = "public"."users"."id" AND o."total" > $3 AND (o."status" = $2 OR o."prev_status" = $2)) AND age > $4`,
				[]any{true, "paid", 100, 3},
			},
			my: want{
				"SELECT * FROM `users` WHERE active = ? AND EXISTS (SELECT 1 FROM \"orders\" o WHERE o.\"user_id\" = `users`.`id` AND o.\"total\" > ? AND (o.\"status\" = ? OR o.\"prev_status\" = ?)) AND age > ?",
				[]any{true, 100, "paid", "paid", 3},
			},
			lite: want{
				`SELECT * FROM "users" WHERE active = ? AND EXISTS (SELECT 1 FROM "orders" o WHERE o."user_id" = "users"."id" AND o."total" > ? AND (o."status" = ? OR o."prev_status" = ?)) AND age > ?`,
				[]any{true, 100, "paid", "paid", 3},
			},
		},
		{
			name: "numbered token beside a literal dollar",
			cond: func(s sql.Subquery) sql.Condition { return sql.Where("company_id").In(s) },
			sub:  sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "label" <> '$1 off' AND "country" = $1`, Args: []any{"US"}},
			pg: want{
				`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT "id" FROM "companies" WHERE "label" <> '$1 off' AND "country" = $2) AND age > $3`,
				[]any{true, "US", 3},
			},
			my: want{
				"SELECT * FROM `users` WHERE active = ? AND company_id IN (SELECT \"id\" FROM \"companies\" WHERE \"label\" <> '$1 off' AND \"country\" = ?) AND age > ?",
				[]any{true, "US", 3},
			},
			lite: want{
				`SELECT * FROM "users" WHERE active = ? AND company_id IN (SELECT "id" FROM "companies" WHERE "label" <> '$1 off' AND "country" = ?) AND age > ?`,
				[]any{true, "US", 3},
			},
		},
	}
	for _, tt := range tests {
		dialects := []struct {
			name string
			d    sql.Dialect
			tbl  sql.Table
			want want
		}{
			{"postgres", pg, pgTbl, tt.pg},
			{"postgres stdlib", pgStd, pgTbl, tt.pg},
			{"mysql", my, myTbl, tt.my},
			{"sqlite", lite, ltTbl, tt.lite},
		}
		for _, dd := range dialects {
			t.Run(tt.name+"/"+dd.name, func(t *testing.T) {
				gotSQL, gotArgs := sql.BuildSelect(dd.d, dd.tbl, sql.SelectOptions{
					Conditions: []sql.Condition{sql.Where("active").Eq(true), tt.cond(tt.sub), sql.Where("age").Gt(3)},
				})
				if gotSQL != dd.want.sql {
					t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, dd.want.sql)
				}
				if diff := cmp.Diff(dd.want.args, gotArgs); diff != "" {
					t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}
}

// TestSubquery_UnreadablePlaceholdersPassThrough pins how a subquery outside
// both PRD 11.5 token forms renders: nothing is checked or
// repaired, and the database rejects or runs what the caller wrote. The bare
// form numbers the first len(Args) tokens, leaves any further "$" as written
// and returns every arg. A subquery with any "$N" token is in the numbered
// form, which copies a token it cannot read as one of the subquery's args (a
// bare "$", "$0", a "$N" past len(Args)) through as written; PostgreSQL keeps
// the args as written, and a "?" dialect lists only the args its tokens
// reference, in token order.
func TestSubquery_UnreadablePlaceholdersPassThrough(t *testing.T) {
	const sel = `SELECT "id" FROM "companies" WHERE `
	tests := []struct {
		name string
		cond func(sql.Subquery) sql.Condition
		sub  sql.Subquery
		// pgSQL is the whole PostgreSQL statement; qWhere is the "?"
		// dialects' WHERE clause, the same for MySQL and SQLite.
		pgSQL  string
		pgArgs []any
		qWhere string
		qArgs  []any
	}{
		{
			name:   "bare form, more tokens than args",
			sub:    sql.Subquery{SQL: sel + `"country" = $ AND "size" > $`, Args: []any{"US"}},
			pgSQL:  `SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (` + sel + `"country" = $2 AND "size" > $) AND age > $3`,
			pgArgs: []any{true, "US", 3},
			qWhere: `active = ? AND company_id IN (` + sel + `"country" = ? AND "size" > $) AND age > ?`,
			qArgs:  []any{true, "US", 3},
		},
		{
			name:   "bare form, fewer tokens than args",
			sub:    sql.Subquery{SQL: sel + `"country" = $`, Args: []any{"US", 10}},
			pgSQL:  `SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (` + sel + `"country" = $2) AND age > $3`,
			pgArgs: []any{true, "US", 10, 3},
			qWhere: `active = ? AND company_id IN (` + sel + `"country" = ?) AND age > ?`,
			qArgs:  []any{true, "US", 10, 3},
		},
		{
			name:   "bare form, args but no token",
			sub:    sql.Subquery{SQL: sel + `"country" = 'US'`, Args: []any{"US"}},
			pgSQL:  `SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (` + sel + `"country" = 'US') AND age > $2`,
			pgArgs: []any{true, "US", 3},
			qWhere: `active = ? AND company_id IN (` + sel + `"country" = 'US') AND age > ?`,
			qArgs:  []any{true, "US", 3},
		},
		{
			name:   "bare and numbered tokens mixed",
			sub:    sql.Subquery{SQL: sel + `"country" = $ AND "size" > $2`, Args: []any{"US", 10}},
			pgSQL:  `SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (` + sel + `"country" = $ AND "size" > $3) AND age > $4`,
			pgArgs: []any{true, "US", 10, 3},
			qWhere: `active = ? AND company_id IN (` + sel + `"country" = $ AND "size" > ?) AND age > ?`,
			qArgs:  []any{true, 10, 3},
		},
		{
			name:   "numbered token zero",
			sub:    sql.Subquery{SQL: sel + `"country" = $0`, Args: []any{"US"}},
			pgSQL:  `SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (` + sel + `"country" = $0) AND age > $3`,
			pgArgs: []any{true, "US", 3},
			qWhere: `active = ? AND company_id IN (` + sel + `"country" = $0) AND age > ?`,
			qArgs:  []any{true, 3},
		},
		{
			name:   "numbered token past the arg count",
			sub:    sql.Subquery{SQL: sel + `"country" = $1 AND "size" > $3`, Args: []any{"US", 10}},
			pgSQL:  `SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (` + sel + `"country" = $2 AND "size" > $3) AND age > $4`,
			pgArgs: []any{true, "US", 10, 3},
			qWhere: `active = ? AND company_id IN (` + sel + `"country" = ? AND "size" > $3) AND age > ?`,
			qArgs:  []any{true, "US", 3},
		},
		{
			name:   "numbered token that overflows an int",
			sub:    sql.Subquery{SQL: sel + `"country" = $99999999999999999999`, Args: []any{"US"}},
			pgSQL:  `SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (` + sel + `"country" = $99999999999999999999) AND age > $3`,
			pgArgs: []any{true, "US", 3},
			qWhere: `active = ? AND company_id IN (` + sel + `"country" = $99999999999999999999) AND age > ?`,
			qArgs:  []any{true, 3},
		},
		{
			name:   "numbered form with an unreferenced arg",
			sub:    sql.Subquery{SQL: sel + `"size" > $2`, Args: []any{"US", 10}},
			pgSQL:  `SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (` + sel + `"size" > $3) AND age > $4`,
			pgArgs: []any{true, "US", 10, 3},
			qWhere: `active = ? AND company_id IN (` + sel + `"size" > ?) AND age > ?`,
			qArgs:  []any{true, 10, 3},
		},
		{
			name:   "numbered form with an unreferenced arg inside exists",
			cond:   func(s sql.Subquery) sql.Condition { return sql.Exists("id", s) },
			sub:    sql.Subquery{SQL: `SELECT 1 FROM "orders" o WHERE o."user_id" = ` + sql.CorrelationToken + ` AND o."total" > $2`, Args: []any{"paid", 100}},
			pgSQL:  `SELECT * FROM "public"."users" WHERE active = $1 AND EXISTS (SELECT 1 FROM "orders" o WHERE o."user_id" = "public"."users"."id" AND o."total" > $3) AND age > $4`,
			pgArgs: []any{true, "paid", 100, 3},
			qWhere: `active = ? AND EXISTS (SELECT 1 FROM "orders" o WHERE o."user_id" = {{table}}.{{id}} AND o."total" > ?) AND age > ?`,
			qArgs:  []any{true, 100, 3},
		},
	}
	for _, tt := range tests {
		cond := tt.cond
		if cond == nil {
			cond = func(s sql.Subquery) sql.Condition { return sql.Where("company_id").In(s) }
		}
		dialects := []struct {
			name     string
			d        sql.Dialect
			tbl      sql.Table
			wantSQL  string
			wantArgs []any
		}{
			{"postgres", pg, pgTbl, tt.pgSQL, tt.pgArgs},
			{"postgres stdlib", pgStd, pgTbl, tt.pgSQL, tt.pgArgs},
			{"mysql", my, myTbl, strings.NewReplacer("{{table}}", "`users`", "{{id}}", "`id`").Replace("SELECT * FROM `users` WHERE " + tt.qWhere), tt.qArgs},
			{"sqlite", lite, ltTbl, strings.NewReplacer("{{table}}", `"users"`, "{{id}}", `"id"`).Replace(`SELECT * FROM "users" WHERE ` + tt.qWhere), tt.qArgs},
		}
		for _, dd := range dialects {
			t.Run(tt.name+"/"+dd.name, func(t *testing.T) {
				gotSQL, gotArgs := sql.BuildSelect(dd.d, dd.tbl, sql.SelectOptions{
					Conditions: []sql.Condition{sql.Where("active").Eq(true), cond(tt.sub), sql.Where("age").Gt(3)},
				})
				if gotSQL != dd.wantSQL {
					t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, dd.wantSQL)
				}
				if diff := cmp.Diff(dd.wantArgs, gotArgs); diff != "" {
					t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}
}

// TestSubquery_LiteralDollarIsNotAToken covers the "$" that belongs to the
// SQL's own text: inside a string literal (a JSON path, a price), a quoted
// identifier, a comment, or a PostgreSQL dollar-quoted string. It is copied
// through unchanged and does not take an arg, so the real tokens still get
// theirs. A scan that is not quote-aware numbers the first len(Args) "$" bytes,
// literal or not.
func TestSubquery_LiteralDollarIsNotAToken(t *testing.T) {
	tests := []struct {
		name    string
		d       sql.Dialect
		tbl     sql.Table
		sub     sql.Subquery
		wantSQL string
	}{
		{
			"postgres json path before a token", pg, pgTbl,
			sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "meta" @? '$.tier' AND "country" = $`, Args: []any{"US"}},
			`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT "id" FROM "companies" WHERE "meta" @? '$.tier' AND "country" = $2)`,
		},
		{
			"postgres dollar-quoted string and quoted identifier", pg, pgTbl,
			sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "name" = $$it's $1$$ AND "price$usd" > $tag$x$tag$ AND "size" > $`, Args: []any{10}},
			`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT "id" FROM "companies" WHERE "name" = $$it's $1$$ AND "price$usd" > $tag$x$tag$ AND "size" > $2)`,
		},
		{
			"postgres no args, literal only", pg, pgTbl,
			sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "label" <> '$1 off'`},
			`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT "id" FROM "companies" WHERE "label" <> '$1 off')`,
		},
		{
			"mysql json path, backquoted identifier and comment", my, myTbl,
			sql.Subquery{SQL: "SELECT `id` FROM `companies` WHERE `meta`->>'$.tier' = 'gold' AND `price$usd` > $ /* $ */", Args: []any{10}},
			"SELECT * FROM `users` WHERE active = ? AND company_id IN (SELECT `id` FROM `companies` WHERE `meta`->>'$.tier' = 'gold' AND `price$usd` > ? /* $ */)",
		},
		{
			"sqlite doubled quote inside a literal", lite, ltTbl,
			sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "note" = 'it''s $5' AND "size" > $ -- $` + "\n", Args: []any{10}},
			`SELECT * FROM "users" WHERE active = ? AND company_id IN (SELECT "id" FROM "companies" WHERE "note" = 'it''s $5' AND "size" > ? -- $` + "\n)",
		},
		{
			"mysql backslash escapes inside literals, not inside a backquoted identifier", my, myTbl,
			sql.Subquery{SQL: "SELECT `id` FROM `companies` WHERE `note` = 'it\\'s $5' AND `tag` = \"say \\\"$\\\"\" AND `path` = 'C:\\\\' AND `odd\\` = 1 AND `size` > $", Args: []any{10}},
			"SELECT * FROM `users` WHERE active = ? AND company_id IN (SELECT `id` FROM `companies` WHERE `note` = 'it\\'s $5' AND `tag` = \"say \\\"$\\\"\" AND `path` = 'C:\\\\' AND `odd\\` = 1 AND `size` > ?)",
		},
		{
			"postgres backslash is literal inside a literal", pg, pgTbl,
			sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "path" = 'C:\' AND "size" > $`, Args: []any{10}},
			`SELECT * FROM "public"."users" WHERE active = $1 AND company_id IN (SELECT "id" FROM "companies" WHERE "path" = 'C:\' AND "size" > $2)`,
		},
		{
			"sqlite backslash is literal inside a literal", lite, ltTbl,
			sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "path" = 'C:\' AND "size" > $`, Args: []any{10}},
			`SELECT * FROM "users" WHERE active = ? AND company_id IN (SELECT "id" FROM "companies" WHERE "path" = 'C:\' AND "size" > ?)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.d, tt.tbl, sql.SelectOptions{
				Conditions: []sql.Condition{sql.Where("active").Eq(true), sql.Where("company_id").In(tt.sub)},
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(append([]any{true}, tt.sub.Args...), gotArgs); diff != "" {
				t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestMultiPlaceholder_LiteralDollarIsNotAToken covers the other callers of the
// quote-aware scan: a [sql.Range] clause, a multi-arg [sql.Raw], and a scalar
// comparator clause. A "$" inside the caller's quoted column or string literal
// is the SQL's own text. Without the quote-aware scan, "price$usd" BETWEEN
// renders as "price$2usd" BETWEEN $3 AND $ and the JSON path takes the first
// arg, and the scalar "price$1" = $ renders as "price$21" = $ on PostgreSQL and
// "price?1" = $ on SQLite.
func TestMultiPlaceholder_LiteralDollarIsNotAToken(t *testing.T) {
	tests := []struct {
		name     string
		d        sql.Dialect
		tbl      sql.Table
		cond     sql.Condition
		wantSQL  string
		wantArgs []any
	}{
		{
			"postgres range on a quoted column with a dollar", pg, pgTbl,
			sql.Condition{Clause: `"price$usd" BETWEEN $ AND $`, Value: sql.Range{Start: 10, End: 100}},
			`SELECT * FROM "public"."users" WHERE active = $1 AND "price$usd" BETWEEN $2 AND $3`,
			[]any{true, 10, 100},
		},
		{
			"mysql range on a backquoted column with a dollar", my, myTbl,
			sql.Condition{Clause: "`price$usd` BETWEEN $ AND $", Value: sql.Range{Start: 10, End: 100}},
			"SELECT * FROM `users` WHERE active = ? AND `price$usd` BETWEEN ? AND ?",
			[]any{true, 10, 100},
		},
		{
			"sqlite raw with a json path before its tokens", lite, ltTbl,
			sql.Raw(`json_extract("meta", '$.tier') IN ($, $)`, "gold", "silver"),
			`SELECT * FROM "users" WHERE active = ? AND (json_extract("meta", '$.tier') IN (?, ?))`,
			[]any{true, "gold", "silver"},
		},
		{
			"postgres scalar comparator on a quoted column with a dollar", pg, pgTbl,
			sql.Where(`"price$1"`).Eq(5),
			`SELECT * FROM "public"."users" WHERE active = $1 AND "price$1" = $2`,
			[]any{true, 5},
		},
		{
			"sqlite scalar comparator on a quoted column with a dollar", lite, ltTbl,
			sql.Where(`"price$1"`).Eq(5),
			`SELECT * FROM "users" WHERE active = ? AND "price$1" = ?`,
			[]any{true, 5},
		},
		{
			"mysql raw with a backslash-escaped quote before its tokens", my, myTbl,
			sql.Raw("`note` <> 'it\\'s' AND `size` BETWEEN $ AND $", 1, 9),
			"SELECT * FROM `users` WHERE active = ? AND (`note` <> 'it\\'s' AND `size` BETWEEN ? AND ?)",
			[]any{true, 1, 9},
		},
		{
			"mysql scalar raw with a backslash-escaped quote before its token", my, myTbl,
			sql.Raw("`note` <> 'it\\'s' AND `size` > $", 9),
			"SELECT * FROM `users` WHERE active = ? AND (`note` <> 'it\\'s' AND `size` > ?)",
			[]any{true, 9},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.d, tt.tbl, sql.SelectOptions{
				Conditions: []sql.Condition{sql.Where("active").Eq(true), tt.cond},
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestRaw_NumberedPlaceholders pins a [sql.Raw] clause written with "$1"…"$k"
// on the scalar (one arg) and multi-arg paths. The clause is read
// by the same PRD 11.5 rule as a Subquery: "$N" is the clause's own args[N-1],
// PostgreSQL renders it at its absolute position, and a "?" dialect emits one
// "?" per token with the args in token order. Before the fix both paths
// replaced the "$" and kept the digits, so on SQLite "owner_id = $1" at
// position 2 rendered as "owner_id = ?1", a valid numbered parameter bound to
// the statement's first arg, and the query returned wrong rows with no error.
func TestRaw_NumberedPlaceholders(t *testing.T) {
	tests := []struct {
		name string
		cond sql.Condition
		// The statement is active = $, the condition, then age > $, so the
		// clause sits at a non-first position and the next condition shows
		// where numbering resumes. pgWhere and qWhere are the WHERE clause
		// on PostgreSQL and on the "?" dialects (MySQL and SQLite alike).
		pgWhere string
		pgArgs  []any
		qWhere  string
		qArgs   []any
	}{
		{
			name:    "scalar",
			cond:    sql.Raw(`"owner_id" = $1`, 10),
			pgWhere: `active = $1 AND ("owner_id" = $2) AND age > $3`,
			pgArgs:  []any{true, 10, 3},
			qWhere:  `active = ? AND ("owner_id" = ?) AND age > ?`,
			qArgs:   []any{true, 10, 3},
		},
		{
			name:    "scalar reused",
			cond:    sql.Raw(`"owner_id" = $1 OR "manager_id" = $1`, 10),
			pgWhere: `active = $1 AND ("owner_id" = $2 OR "manager_id" = $2) AND age > $3`,
			pgArgs:  []any{true, 10, 3},
			qWhere:  `active = ? AND ("owner_id" = ? OR "manager_id" = ?) AND age > ?`,
			qArgs:   []any{true, 10, 10, 3},
		},
		{
			name:    "scalar numbered token beside a literal dollar",
			cond:    sql.Raw(`"note" <> '$1 off' AND "owner_id" = $1`, 10),
			pgWhere: `active = $1 AND ("note" <> '$1 off' AND "owner_id" = $2) AND age > $3`,
			pgArgs:  []any{true, 10, 3},
			qWhere:  `active = ? AND ("note" <> '$1 off' AND "owner_id" = ?) AND age > ?`,
			qArgs:   []any{true, 10, 3},
		},
		{
			name:    "scalar bare token after a literal dollar",
			cond:    sql.Raw(`"note" <> '$x' AND "owner_id" = $`, 10),
			pgWhere: `active = $1 AND ("note" <> '$x' AND "owner_id" = $2) AND age > $3`,
			pgArgs:  []any{true, 10, 3},
			qWhere:  `active = ? AND ("note" <> '$x' AND "owner_id" = ?) AND age > ?`,
			qArgs:   []any{true, 10, 3},
		},
		{
			name:    "multi in order",
			cond:    sql.Raw(`"owner_id" = $1 AND "id" >= $2`, 10, 1),
			pgWhere: `active = $1 AND ("owner_id" = $2 AND "id" >= $3) AND age > $4`,
			pgArgs:  []any{true, 10, 1, 3},
			qWhere:  `active = ? AND ("owner_id" = ? AND "id" >= ?) AND age > ?`,
			qArgs:   []any{true, 10, 1, 3},
		},
		{
			name:    "multi out of order and reused",
			cond:    sql.Raw(`"id" >= $2 AND ("owner_id" = $1 OR "manager_id" = $1)`, 10, 1),
			pgWhere: `active = $1 AND ("id" >= $3 AND ("owner_id" = $2 OR "manager_id" = $2)) AND age > $4`,
			pgArgs:  []any{true, 10, 1, 3},
			qWhere:  `active = ? AND ("id" >= ? AND ("owner_id" = ? OR "manager_id" = ?)) AND age > ?`,
			qArgs:   []any{true, 1, 10, 10, 3},
		},
	}
	for _, tt := range tests {
		dialects := []struct {
			name string
			d    sql.Dialect
			tbl  sql.Table
			sql  string
			args []any
		}{
			{"postgres", pg, pgTbl, `SELECT * FROM "public"."users" WHERE ` + tt.pgWhere, tt.pgArgs},
			{"postgres stdlib", pgStd, pgTbl, `SELECT * FROM "public"."users" WHERE ` + tt.pgWhere, tt.pgArgs},
			{"mysql", my, myTbl, "SELECT * FROM `users` WHERE " + tt.qWhere, tt.qArgs},
			{"sqlite", lite, ltTbl, `SELECT * FROM "users" WHERE ` + tt.qWhere, tt.qArgs},
		}
		for _, dd := range dialects {
			t.Run(tt.name+"/"+dd.name, func(t *testing.T) {
				gotSQL, gotArgs := sql.BuildSelect(dd.d, dd.tbl, sql.SelectOptions{
					Conditions: []sql.Condition{sql.Where("active").Eq(true), tt.cond, sql.Where("age").Gt(3)},
				})
				if gotSQL != dd.sql {
					t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, dd.sql)
				}
				if diff := cmp.Diff(dd.args, gotArgs); diff != "" {
					t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}
}

// --- Composite PK ---

func TestBuildCompositePKConditions(t *testing.T) {
	conds := sql.BuildCompositePKConditions(pg, []string{"order_id", "product_id"}, []any{"o1", "p1"})
	gotSQL, gotArgs := sql.BuildSelect(pg, sql.Table{Schema: "public", Name: "order_items"}, sql.SelectOptions{
		Conditions: conds,
	})
	wantSQL := `SELECT * FROM "public"."order_items" WHERE "order_id" = $1 AND "product_id" = $2`
	if gotSQL != wantSQL {
		t.Errorf("Composite PK single row sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{"o1", "p1"}, gotArgs); diff != "" {
		t.Errorf("Composite PK single row args mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildCompositePKBatchCondition_TupleIN(t *testing.T) {
	// PostgreSQL and SQLite use tuple IN syntax
	tests := []struct {
		name    string
		dialect sql.Dialect
		table   sql.Table
		wantSQL string
	}{
		{
			name:    "postgres tuple IN",
			dialect: pg,
			table:   sql.Table{Schema: "public", Name: "order_items"},
			wantSQL: `SELECT * FROM "public"."order_items" WHERE ("order_id", "product_id") IN (($1, $2), ($3, $4))`,
		},
		{
			name:    "sqlite tuple IN",
			dialect: lite,
			table:   sql.Table{Name: "order_items"},
			wantSQL: `SELECT * FROM "order_items" WHERE ("order_id", "product_id") IN ((?, ?), (?, ?))`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := sql.BuildCompositePKBatchCondition(
				tt.dialect,
				[]string{"order_id", "product_id"},
				[][]any{{"o1", "p1"}, {"o2", "p2"}},
			)
			gotSQL, gotArgs := sql.BuildSelect(tt.dialect, tt.table, sql.SelectOptions{
				Conditions: []sql.Condition{cond},
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("Batch tuple IN sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff([]any{"o1", "p1", "o2", "p2"}, gotArgs); diff != "" {
				t.Errorf("Batch tuple IN args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildCompositePKBatchCondition_ExpandedOR(t *testing.T) {
	// MySQL uses expanded OR
	cond := sql.BuildCompositePKBatchCondition(
		my,
		[]string{"order_id", "product_id"},
		[][]any{{"o1", "p1"}, {"o2", "p2"}},
	)
	gotSQL, gotArgs := sql.BuildSelect(my, sql.Table{Name: "order_items"}, sql.SelectOptions{
		Conditions: []sql.Condition{cond},
	})
	wantSQL := "SELECT * FROM `order_items` WHERE ((`order_id` = ? AND `product_id` = ?) OR (`order_id` = ? AND `product_id` = ?))"
	if gotSQL != wantSQL {
		t.Errorf("Batch expanded OR sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{"o1", "p1", "o2", "p2"}, gotArgs); diff != "" {
		t.Errorf("Batch expanded OR args mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildCompositePKBatchCondition_Empty(t *testing.T) {
	cond := sql.BuildCompositePKBatchCondition(pg, []string{"a", "b"}, nil)
	gotSQL, _ := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
		Conditions: []sql.Condition{cond},
	})
	wantSQL := `SELECT * FROM "public"."users" WHERE 1 = 0`
	if gotSQL != wantSQL {
		t.Errorf("Empty batch sql = %q, want %q", gotSQL, wantSQL)
	}
}

// --- BuildColumnsFromFieldOptions ---

func TestBuildColumnsFromFieldOptions(t *testing.T) {
	m := map[string]string{
		"name":  "FieldName",
		"email": "FieldEmail",
		"id":    "FieldID",
	}
	got := sql.BuildColumnsFromFieldOptions(m)
	want := []string{"email", "id", "name"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("BuildColumnsFromFieldOptions() mismatch (-want +got):\n%s", diff)
	}
}

// --- Multiple conditions with correct placeholder numbering ---

func TestMultipleConditionsPlaceholderNumbering(t *testing.T) {
	conds := []sql.Condition{
		{Clause: `"name" = $`, Value: "alice"},
		{Clause: `"age" > $`, Value: 21},
		{Clause: `"active" = $`, Value: true},
	}
	gotSQL, gotArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
		Conditions: conds,
	})
	wantSQL := `SELECT * FROM "public"."users" WHERE "name" = $1 AND "age" > $2 AND "active" = $3`
	if gotSQL != wantSQL {
		t.Errorf("placeholder numbering sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{"alice", 21, true}, gotArgs); diff != "" {
		t.Errorf("placeholder numbering args mismatch (-want +got):\n%s", diff)
	}
}

// --- Tenancy composition (PRD §29.4) ---
//
// These tests lock in the convention documented on SelectOptions /
// UpdateOptions / HardDeleteOptions: tenancy adds rows to the existing
// Conditions slice. The generator composes user + soft-delete + tenancy
// conditions in that order; placeholder positions follow slice order with no
// hidden increments from JOIN `On` strings.

func TestTenancy_SelectAppendsTenantCondition(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		conds    []sql.Condition
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres user cond + tenant cond",
			dialect: pg,
			table:   pgTbl,
			conds: []sql.Condition{
				sql.Where(`"name"`).Eq("alice"),
				sql.Where(`"workspace_id"`).Eq("ws-abc"),
			},
			wantSQL:  `SELECT * FROM "public"."users" WHERE "name" = $1 AND "workspace_id" = $2`,
			wantArgs: []any{"alice", "ws-abc"},
		},
		{
			name:    "mysql user cond + tenant cond",
			dialect: my,
			table:   myTbl,
			conds: []sql.Condition{
				sql.Where("`name`").Eq("alice"),
				sql.Where("`workspace_id`").Eq("ws-abc"),
			},
			wantSQL:  "SELECT * FROM `users` WHERE `name` = ? AND `workspace_id` = ?",
			wantArgs: []any{"alice", "ws-abc"},
		},
		{
			name:    "sqlite user cond + tenant cond",
			dialect: lite,
			table:   ltTbl,
			conds: []sql.Condition{
				sql.Where(`"name"`).Eq("alice"),
				sql.Where(`"workspace_id"`).Eq("ws-abc"),
			},
			wantSQL:  `SELECT * FROM "users" WHERE "name" = ? AND "workspace_id" = ?`,
			wantArgs: []any{"alice", "ws-abc"},
		},
		{
			name:    "postgres tenant cond alone takes $1",
			dialect: pg,
			table:   pgTbl,
			conds: []sql.Condition{
				sql.Where(`"workspace_id"`).Eq("ws-abc"),
			},
			wantSQL:  `SELECT * FROM "public"."users" WHERE "workspace_id" = $1`,
			wantArgs: []any{"ws-abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.dialect, tt.table, sql.SelectOptions{
				Conditions: tt.conds,
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelect() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTenancy_UpdateAppendsTenantCondition(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.UpdateOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres pk + tenant in WHERE, SET excludes tenant",
			dialect: pg,
			table:   pgTbl,
			opts: sql.UpdateOptions{
				SetClauses: map[string]any{"name": "alice"},
				Conditions: []sql.Condition{
					sql.Where(`"id"`).Eq(1),
					sql.Where(`"workspace_id"`).Eq("ws-abc"),
				},
			},
			wantSQL:  `UPDATE "public"."users" SET "name" = $1 WHERE "id" = $2 AND "workspace_id" = $3`,
			wantArgs: []any{"alice", 1, "ws-abc"},
		},
		{
			name:    "mysql pk + tenant in WHERE",
			dialect: my,
			table:   myTbl,
			opts: sql.UpdateOptions{
				SetClauses: map[string]any{"name": "bob"},
				Conditions: []sql.Condition{
					sql.Where("`id`").Eq(2),
					sql.Where("`workspace_id`").Eq("ws-abc"),
				},
			},
			wantSQL:  "UPDATE `users` SET `name` = ? WHERE `id` = ? AND `workspace_id` = ?",
			wantArgs: []any{"bob", 2, "ws-abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildUpdate(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildUpdate() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildUpdate() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTenancy_DeleteAppendsTenantCondition(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		opts     sql.HardDeleteOptions
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres pk + tenant",
			dialect: pg,
			table:   pgTbl,
			opts: sql.HardDeleteOptions{
				Conditions: []sql.Condition{
					sql.Where(`"id"`).Eq(1),
					sql.Where(`"workspace_id"`).Eq("ws-abc"),
				},
			},
			wantSQL:  `DELETE FROM "public"."users" WHERE "id" = $1 AND "workspace_id" = $2`,
			wantArgs: []any{1, "ws-abc"},
		},
		{
			name:    "sqlite pk + tenant",
			dialect: lite,
			table:   ltTbl,
			opts: sql.HardDeleteOptions{
				Conditions: []sql.Condition{
					sql.Where(`"id"`).Eq(3),
					sql.Where(`"workspace_id"`).Eq("ws-abc"),
				},
			},
			wantSQL:  `DELETE FROM "users" WHERE "id" = ? AND "workspace_id" = ?`,
			wantArgs: []any{3, "ws-abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildHardDelete(tt.dialect, tt.table, tt.opts)
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildHardDelete() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildHardDelete() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTenancy_SelectJoinTenantInOuterWhere locks in the PRD §29.10 convention:
// when both parent and tenanted child participate in an o2o JOIN, the child's
// tenant filter lands in the outer WHERE (alongside the parent's), not inside
// the JOIN ON string. Placeholder numbering follows Conditions slice order
// with no contribution from the JOIN ON.
func TestTenancy_SelectJoinTenantInOuterWhere(t *testing.T) {
	products := sql.Table{Schema: "public", Name: "products"}
	join := sql.JoinClause{
		Table:   sql.Table{Schema: "public", Name: "companies"},
		Alias:   "c",
		On:      `c."id" = p."company_id"`,
		Columns: []string{"id", "name"},
	}
	conds := []sql.Condition{
		sql.Where(`p."workspace_id"`).Eq("ws-abc"),
		sql.Where(`c."workspace_id"`).Eq("ws-abc"),
	}

	gotSQL, gotArgs := sql.BuildSelectJoin(pg, products, []sql.JoinClause{join}, sql.SelectOptions{
		Alias:      "p",
		Columns:    []string{"id"},
		Conditions: conds,
	})

	wantSQL := `SELECT p."id" AS "p.id", c."id" AS "c.id", c."name" AS "c.name" FROM "public"."products" p LEFT JOIN "public"."companies" c ON c."id" = p."company_id" WHERE p."workspace_id" = $1 AND c."workspace_id" = $2`
	if gotSQL != wantSQL {
		t.Errorf("BuildSelectJoin() sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{"ws-abc", "ws-abc"}, gotArgs); diff != "" {
		t.Errorf("BuildSelectJoin() args mismatch (-want +got):\n%s", diff)
	}
}

// TestTenancy_RoundTripByteIdentical verifies the PRD §29.4.2 byte-identical
// redundant-match property at the builder layer: two SELECTs with the same
// condition shape but different tenant values produce byte-identical SQL;
// only args differ.
func TestTenancy_RoundTripByteIdentical(t *testing.T) {
	build := func(tenant any) (string, []any) {
		return sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
			Conditions: []sql.Condition{
				sql.Where(`"id"`).Eq(42),
				sql.Where(`"workspace_id"`).Eq(tenant),
			},
		})
	}

	sqlA, argsA := build("ws-abc")
	sqlB, argsB := build("ws-xyz")

	if sqlA != sqlB {
		t.Errorf("SQL differs across tenant values:\n  A: %q\n  B: %q", sqlA, sqlB)
	}
	if diff := cmp.Diff([]any{42, "ws-abc"}, argsA); diff != "" {
		t.Errorf("args A mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{42, "ws-xyz"}, argsB); diff != "" {
		t.Errorf("args B mismatch (-want +got):\n%s", diff)
	}
}

// TestTenancy_MixedSourcePlaceholderNumbering proves mixed-origin conditions
// (user + soft-delete + tenant) number sequentially in slice order. This is the
// invariant the generated tenancy code relies on when splicing the tenant
// condition in after user and soft-delete conditions.
func TestTenancy_MixedSourcePlaceholderNumbering(t *testing.T) {
	conds := []sql.Condition{
		sql.Where(`"name"`).Eq("alice"),          // user
		{Clause: `"deleted_at" IS NULL`},         // soft delete (no placeholder)
		sql.Where(`"workspace_id"`).Eq("ws-abc"), // tenant
	}
	gotSQL, gotArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{Conditions: conds})

	wantSQL := `SELECT * FROM "public"."users" WHERE "name" = $1 AND "deleted_at" IS NULL AND "workspace_id" = $2`
	if gotSQL != wantSQL {
		t.Errorf("mixed-source sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{"alice", "ws-abc"}, gotArgs); diff != "" {
		t.Errorf("mixed-source args mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildRefreshMaterializedView(t *testing.T) {
	tests := []struct {
		name         string
		dialect      sql.Dialect
		table        sql.Table
		concurrently bool
		want         string
	}{
		{
			name:    "postgres schema-qualified",
			dialect: sql.NewPostgresDialect(),
			table:   sql.Table{Schema: "reporting", Name: "order_totals"},
			want:    `REFRESH MATERIALIZED VIEW "reporting"."order_totals"`,
		},
		{
			name:    "postgres bare name",
			dialect: sql.NewPostgresDialect(),
			table:   sql.Table{Name: "order_totals"},
			want:    `REFRESH MATERIALIZED VIEW "order_totals"`,
		},
		{
			name:         "postgres schema-qualified concurrently",
			dialect:      sql.NewPostgresDialect(),
			table:        sql.Table{Schema: "reporting", Name: "order_totals"},
			concurrently: true,
			want:         `REFRESH MATERIALIZED VIEW CONCURRENTLY "reporting"."order_totals"`,
		},
		{
			name:         "postgres bare name concurrently",
			dialect:      sql.NewPostgresDialect(),
			table:        sql.Table{Name: "order_totals"},
			concurrently: true,
			want:         `REFRESH MATERIALIZED VIEW CONCURRENTLY "order_totals"`,
		},
		{
			// Materialized views are PostgreSQL-only; the builder still quotes
			// deterministically through FormatTable for any dialect.
			name:    "mysql quoting via FormatTable",
			dialect: sql.NewMySQLDialect(),
			table:   sql.Table{Name: "order_totals"},
			want:    "REFRESH MATERIALIZED VIEW `order_totals`",
		},
		{
			name:    "sqlite quoting via FormatTable",
			dialect: sql.NewSQLiteDialect(),
			table:   sql.Table{Name: "order_totals"},
			want:    `REFRESH MATERIALIZED VIEW "order_totals"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sql.BuildRefreshMaterializedView(tt.dialect, tt.table, tt.concurrently)
			if got != tt.want {
				t.Errorf("BuildRefreshMaterializedView() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Correlated EXISTS (PRD 11.5) ---

// existsOrders builds the correlated subquery a list-relationship filter
// compiles to, quoted for d. The parent-side reference is the token, never a
// baked-in qualifier: the builder resolves it against the enclosing statement.
func existsOrders(d sql.Dialect) sql.Condition {
	orders := d.FormatTable(sql.Table{Schema: "public", Name: "orders"})
	return sql.Exists("id", sql.Subquery{
		SQL: "SELECT 1 FROM " + orders + " tgt WHERE tgt." + d.QuoteIdentifier("user_id") +
			" = " + sql.CorrelationToken + " AND tgt." + d.QuoteIdentifier("status") +
			" = $ AND tgt." + d.QuoteIdentifier("total") + " > $",
		Args: []any{"paid", 100},
	})
}

// TestExists_QualifierResolvesPerReadPath proves the builder, not codegen,
// resolves the EXISTS qualifier: one emitted value renders the correlation
// against the quoted table on the plain read path and against the alias on the
// O2O-join path.
func TestExists_QualifierResolvesPerReadPath(t *testing.T) {
	tests := []struct {
		name      string
		dialect   sql.Dialect
		table     sql.Table
		join      sql.JoinClause
		wantPlain string
		wantJoin  string
	}{
		{
			name:    "postgres",
			dialect: pg,
			table:   pgTbl,
			join: sql.JoinClause{
				Table:   sql.Table{Schema: "public", Name: "profiles"},
				Alias:   "p",
				On:      `p."user_id" = t."id"`,
				Columns: []string{"bio"},
			},
			wantPlain: `SELECT * FROM "public"."users" WHERE EXISTS (SELECT 1 FROM "public"."orders" tgt WHERE tgt."user_id" = "public"."users"."id" AND tgt."status" = $1 AND tgt."total" > $2)`,
			wantJoin:  `SELECT t."id" AS "t.id", p."bio" AS "p.bio" FROM "public"."users" t LEFT JOIN "public"."profiles" p ON p."user_id" = t."id" WHERE EXISTS (SELECT 1 FROM "public"."orders" tgt WHERE tgt."user_id" = t."id" AND tgt."status" = $1 AND tgt."total" > $2)`,
		},
		{
			name:    "mysql",
			dialect: my,
			table:   myTbl,
			join: sql.JoinClause{
				Table:   sql.Table{Name: "profiles"},
				Alias:   "p",
				On:      "p.`user_id` = t.`id`",
				Columns: []string{"bio"},
			},
			wantPlain: "SELECT * FROM `users` WHERE EXISTS (SELECT 1 FROM `orders` tgt WHERE tgt.`user_id` = `users`.`id` AND tgt.`status` = ? AND tgt.`total` > ?)",
			wantJoin:  "SELECT t.`id` AS `t.id`, p.`bio` AS `p.bio` FROM `users` t LEFT JOIN `profiles` p ON p.`user_id` = t.`id` WHERE EXISTS (SELECT 1 FROM `orders` tgt WHERE tgt.`user_id` = t.`id` AND tgt.`status` = ? AND tgt.`total` > ?)",
		},
		{
			name:    "sqlite",
			dialect: lite,
			table:   ltTbl,
			join: sql.JoinClause{
				Table:   sql.Table{Name: "profiles"},
				Alias:   "p",
				On:      `p."user_id" = t."id"`,
				Columns: []string{"bio"},
			},
			wantPlain: `SELECT * FROM "users" WHERE EXISTS (SELECT 1 FROM "orders" tgt WHERE tgt."user_id" = "users"."id" AND tgt."status" = ? AND tgt."total" > ?)`,
			wantJoin:  `SELECT t."id" AS "t.id", p."bio" AS "p.bio" FROM "users" t LEFT JOIN "profiles" p ON p."user_id" = t."id" WHERE EXISTS (SELECT 1 FROM "orders" tgt WHERE tgt."user_id" = t."id" AND tgt."status" = ? AND tgt."total" > ?)`,
		},
	}

	wantArgs := []any{"paid", 100}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// One value, both read paths.
			cond := existsOrders(tt.dialect)

			plainSQL, plainArgs := sql.BuildSelect(tt.dialect, tt.table, sql.SelectOptions{
				Conditions: []sql.Condition{cond},
			})
			if plainSQL != tt.wantPlain {
				t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", plainSQL, tt.wantPlain)
			}
			if diff := cmp.Diff(wantArgs, plainArgs); diff != "" {
				t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
			}

			joinSQL, joinArgs := sql.BuildSelectJoin(tt.dialect, tt.table, []sql.JoinClause{tt.join}, sql.SelectOptions{
				Alias:      "t",
				Columns:    []string{"id"},
				Conditions: sql.PrefixConditions("t", []sql.Condition{cond}),
			})
			if joinSQL != tt.wantJoin {
				t.Errorf("BuildSelectJoin sql =\n  %q\nwant\n  %q", joinSQL, tt.wantJoin)
			}
			if diff := cmp.Diff(wantArgs, joinArgs); diff != "" {
				t.Errorf("BuildSelectJoin args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// mixedConds is a filter slice of every multi-arg condition kind, quoted for d,
// with the EXISTS in the middle: the shape that pins placeholder numbering as
// driven entirely by Conditions order.
func mixedConds(d sql.Dialect) []sql.Condition {
	return []sql.Condition{
		{Clause: d.QuoteIdentifier("status") + " = $", Value: "active", Column: d.QuoteIdentifier("status")},
		existsOrders(d),
		{Clause: d.QuoteIdentifier("age") + " BETWEEN $ AND $", Value: sql.Range{Start: 18, End: 65}, Column: d.QuoteIdentifier("age")},
		{Clause: d.QuoteIdentifier("role") + " IN $", Value: []any{"admin", "owner"}, Column: d.QuoteIdentifier("role")},
	}
}

// TestExists_PlaceholderNumberingInMixedConditions pins that an EXISTS carrying
// its own args consumes exactly its share of the enclosing statement's
// positional sequence, leaving the conditions after it correctly numbered.
func TestExists_PlaceholderNumberingInMixedConditions(t *testing.T) {
	tests := []struct {
		name    string
		dialect sql.Dialect
		table   sql.Table
		wantSQL string
	}{
		{
			name:    "postgres",
			dialect: pg,
			table:   pgTbl,
			wantSQL: `SELECT * FROM "public"."users" WHERE "status" = $1 AND EXISTS (SELECT 1 FROM "public"."orders" tgt WHERE tgt."user_id" = "public"."users"."id" AND tgt."status" = $2 AND tgt."total" > $3) AND "age" BETWEEN $4 AND $5 AND "role" IN ($6, $7)`,
		},
		{
			name:    "mysql",
			dialect: my,
			table:   myTbl,
			wantSQL: "SELECT * FROM `users` WHERE `status` = ? AND EXISTS (SELECT 1 FROM `orders` tgt WHERE tgt.`user_id` = `users`.`id` AND tgt.`status` = ? AND tgt.`total` > ?) AND `age` BETWEEN ? AND ? AND `role` IN (?, ?)",
		},
		{
			name:    "sqlite",
			dialect: lite,
			table:   ltTbl,
			wantSQL: `SELECT * FROM "users" WHERE "status" = ? AND EXISTS (SELECT 1 FROM "orders" tgt WHERE tgt."user_id" = "users"."id" AND tgt."status" = ? AND tgt."total" > ?) AND "age" BETWEEN ? AND ? AND "role" IN (?, ?)`,
		},
	}

	wantArgs := []any{"active", "paid", 100, 18, 65, "admin", "owner"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(tt.dialect, tt.table, sql.SelectOptions{
				Conditions: mixedConds(tt.dialect),
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestBuildSelectJoin_ExistsWithPrefixedLeaf covers the full O2O read path: a
// JOIN, a leaf condition that alias prefixing rewrites, and an EXISTS it must
// leave alone — with the args of both in Conditions order.
func TestBuildSelectJoin_ExistsWithPrefixedLeaf(t *testing.T) {
	tests := []struct {
		name    string
		dialect sql.Dialect
		table   sql.Table
		join    sql.JoinClause
		wantSQL string
	}{
		{
			name:    "postgres",
			dialect: pg,
			table:   pgTbl,
			join: sql.JoinClause{
				Table:   sql.Table{Schema: "public", Name: "profiles"},
				Alias:   "p",
				On:      `p."user_id" = t."id"`,
				Columns: []string{"bio"},
			},
			wantSQL: `SELECT t."id" AS "t.id", p."bio" AS "p.bio" FROM "public"."users" t LEFT JOIN "public"."profiles" p ON p."user_id" = t."id" WHERE t."status" = $1 AND EXISTS (SELECT 1 FROM "public"."orders" tgt WHERE tgt."user_id" = t."id" AND tgt."status" = $2 AND tgt."total" > $3) AND t."role" IN ($4, $5)`,
		},
		{
			name:    "mysql",
			dialect: my,
			table:   myTbl,
			join: sql.JoinClause{
				Table:   sql.Table{Name: "profiles"},
				Alias:   "p",
				On:      "p.`user_id` = t.`id`",
				Columns: []string{"bio"},
			},
			wantSQL: "SELECT t.`id` AS `t.id`, p.`bio` AS `p.bio` FROM `users` t LEFT JOIN `profiles` p ON p.`user_id` = t.`id` WHERE t.`status` = ? AND EXISTS (SELECT 1 FROM `orders` tgt WHERE tgt.`user_id` = t.`id` AND tgt.`status` = ? AND tgt.`total` > ?) AND t.`role` IN (?, ?)",
		},
	}

	wantArgs := []any{"active", "paid", 100, "admin", "owner"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.dialect
			conds := sql.PrefixConditions("t", []sql.Condition{
				{Clause: d.QuoteIdentifier("status") + " = $", Value: "active", Column: d.QuoteIdentifier("status")},
				existsOrders(d),
				{Clause: d.QuoteIdentifier("role") + " IN $", Value: []any{"admin", "owner"}, Column: d.QuoteIdentifier("role")},
			})
			gotSQL, gotArgs := sql.BuildSelectJoin(d, tt.table, []sql.JoinClause{tt.join}, sql.SelectOptions{
				Alias:      "t",
				Columns:    []string{"id"},
				Conditions: conds,
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelectJoin sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelectJoin args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestBuildSelectJoin_OrderByQualified pins that a JOIN's ORDER BY names the
// primary table's column through its alias, quoted, so a column the joined
// table also has is not ambiguous and a column that needs quoting still
// parses. The conditions have the composite shape Connection passes
// on this path: a composite cursor keyset, qualified by PrefixConditions.
func TestBuildSelectJoin_OrderByQualified(t *testing.T) {
	join := func(d sql.Dialect, schema string) sql.JoinClause {
		return sql.JoinClause{
			Table:   sql.Table{Schema: schema, Name: "profiles"},
			Alias:   "p",
			On:      "p." + d.QuoteIdentifier("user_id") + " = u." + d.QuoteIdentifier("id"),
			Columns: []string{"id"},
		}
	}
	// (name > $) OR (name = $ AND id > $): cursorKeyset's composite shape, with
	// unquoted, Column-less leaves (cursorKeyset itself quotes each key and sets
	// Column).
	keyset := sql.Or(
		sql.And(sql.Condition{Clause: "name > $", Value: "b"}),
		sql.And(sql.Condition{Clause: "name = $", Value: "b"}, sql.Condition{Clause: "id > $", Value: 7}),
	)
	sorts := []sql.Sort{
		{Column: "name", Direction: sql.Asc},
		{Column: "id", Direction: sql.Asc},
		{Column: "order", Direction: sql.Desc},
	}

	tests := []struct {
		name    string
		dialect sql.Dialect
		table   sql.Table
		schema  string
		wantSQL string
	}{
		{
			name:    "postgres",
			dialect: pg,
			table:   pgTbl,
			schema:  "public",
			wantSQL: `SELECT u."id" AS "u.id", p."id" AS "p.id" FROM "public"."users" u LEFT JOIN "public"."profiles" p ON p."user_id" = u."id" WHERE ((u.name > $1) OR (u.name = $2 AND u.id > $3)) ORDER BY u."name" ASC, u."id" ASC, u."order" DESC LIMIT 3`,
		},
		{
			name:    "mysql",
			dialect: my,
			table:   myTbl,
			wantSQL: "SELECT u.`id` AS `u.id`, p.`id` AS `p.id` FROM `users` u LEFT JOIN `profiles` p ON p.`user_id` = u.`id` WHERE ((u.name > ?) OR (u.name = ? AND u.id > ?)) ORDER BY u.`name` ASC, u.`id` ASC, u.`order` DESC LIMIT 3",
		},
		{
			name:    "sqlite",
			dialect: lite,
			table:   ltTbl,
			wantSQL: `SELECT u."id" AS "u.id", p."id" AS "p.id" FROM "users" u LEFT JOIN "profiles" p ON p."user_id" = u."id" WHERE ((u.name > ?) OR (u.name = ? AND u.id > ?)) ORDER BY u."name" ASC, u."id" ASC, u."order" DESC LIMIT 3`,
		},
	}

	wantArgs := []any{"b", "b", 7}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelectJoin(tt.dialect, tt.table, []sql.JoinClause{join(tt.dialect, tt.schema)}, sql.SelectOptions{
				Alias:      "u",
				Columns:    []string{"id"},
				Conditions: sql.PrefixConditions("u", []sql.Condition{keyset}),
				OrderBy:    sorts,
				Limit:      new(3),
			})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelectJoin() sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
				t.Errorf("BuildSelectJoin() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestExists_NestedInComposition pins that the qualifier reaches an EXISTS
// nested inside and/or members, and that prefixing still qualifies the leaves
// around it.
func TestExists_NestedInComposition(t *testing.T) {
	nested := func() sql.Condition {
		return sql.And(
			sql.Condition{Clause: `"status" = $`, Value: "active", Column: `"status"`},
			sql.Or(
				sql.Condition{Clause: `"role" = $`, Value: "admin", Column: `"role"`},
				existsOrders(pg),
			),
		)
	}
	wantArgs := []any{"active", "admin", "paid", 100}

	plainSQL, plainArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
		Conditions: []sql.Condition{nested()},
	})
	wantPlain := `SELECT * FROM "public"."users" WHERE ("status" = $1 AND ("role" = $2 OR EXISTS (SELECT 1 FROM "public"."orders" tgt WHERE tgt."user_id" = "public"."users"."id" AND tgt."status" = $3 AND tgt."total" > $4)))`
	if plainSQL != wantPlain {
		t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", plainSQL, wantPlain)
	}
	if diff := cmp.Diff(wantArgs, plainArgs); diff != "" {
		t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
	}

	joinSQL, joinArgs := sql.BuildSelectJoin(pg, pgTbl, []sql.JoinClause{{
		Table:   sql.Table{Schema: "public", Name: "profiles"},
		Alias:   "p",
		On:      `p."user_id" = t."id"`,
		Columns: []string{"bio"},
	}}, sql.SelectOptions{
		Alias:      "t",
		Columns:    []string{"id"},
		Conditions: sql.PrefixConditions("t", []sql.Condition{nested()}),
	})
	wantJoin := `SELECT t."id" AS "t.id", p."bio" AS "p.bio" FROM "public"."users" t LEFT JOIN "public"."profiles" p ON p."user_id" = t."id" WHERE (t."status" = $1 AND (t."role" = $2 OR EXISTS (SELECT 1 FROM "public"."orders" tgt WHERE tgt."user_id" = t."id" AND tgt."status" = $3 AND tgt."total" > $4)))`
	if joinSQL != wantJoin {
		t.Errorf("BuildSelectJoin sql =\n  %q\nwant\n  %q", joinSQL, wantJoin)
	}
	if diff := cmp.Diff(wantArgs, joinArgs); diff != "" {
		t.Errorf("BuildSelectJoin args mismatch (-want +got):\n%s", diff)
	}
}

// TestExists_ComposesWithSubqueryCondition pins the two subquery-bearing
// condition kinds side by side: a plain Subquery value numbers its own "$"
// tokens, so the EXISTS that follows it must pick up the positional sequence
// where the subquery's args left off.
func TestExists_ComposesWithSubqueryCondition(t *testing.T) {
	conds := []sql.Condition{
		{
			Clause: `"company_id" IN $`,
			Value: sql.Subquery{
				SQL:  `SELECT "id" FROM "public"."companies" WHERE "country" = $`,
				Args: []any{"US"},
			},
		},
		existsOrders(pg),
	}
	gotSQL, gotArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{Conditions: conds})
	wantSQL := `SELECT * FROM "public"."users" WHERE "company_id" IN (SELECT "id" FROM "public"."companies" WHERE "country" = $1) AND EXISTS (SELECT 1 FROM "public"."orders" tgt WHERE tgt."user_id" = "public"."users"."id" AND tgt."status" = $2 AND tgt."total" > $3)`
	if gotSQL != wantSQL {
		t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{"US", "paid", 100}, gotArgs); diff != "" {
		t.Errorf("BuildSelect args mismatch (-want +got):\n%s", diff)
	}
}

// --- SubqueryWhere (PRD 11.1 — the inner WHERE of a relationship filter) ---

// TestSubqueryWhere_RenumbersIntoEnclosingStatement pins the property that makes
// the fragment composable: it emits bare "$" tokens, which Exists renumbers into
// the enclosing statement's sequence. A fragment that rendered positions itself
// would number them twice and bind the wrong args on PostgreSQL.
func TestSubqueryWhere_RenumbersIntoEnclosingStatement(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		table    sql.Table
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "postgres",
			dialect: pg,
			table:   pgTbl,
			wantSQL: `SELECT * FROM "public"."users" WHERE "email" = $1 AND EXISTS (SELECT 1 FROM "public"."orders" tgt WHERE tgt."user_id" = "public"."users"."id" AND tgt."status" = $2 AND tgt."total" BETWEEN $3 AND $4)`,
		},
		{
			name:    "mysql",
			dialect: my,
			table:   myTbl,
			wantSQL: "SELECT * FROM `users` WHERE `email` = ? AND EXISTS (SELECT 1 FROM `orders` tgt WHERE tgt.`user_id` = `users`.`id` AND tgt.`status` = ? AND tgt.`total` BETWEEN ? AND ?)",
		},
		{
			name:    "sqlite",
			dialect: lite,
			table:   ltTbl,
			wantSQL: `SELECT * FROM "users" WHERE "email" = ? AND EXISTS (SELECT 1 FROM "orders" tgt WHERE tgt."user_id" = "users"."id" AND tgt."status" = ? AND tgt."total" BETWEEN ? AND ?)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.dialect
			inner := []sql.Condition{
				{Clause: d.QuoteIdentifier("status") + " = $", Column: d.QuoteIdentifier("status"), Value: "paid"},
				{Clause: d.QuoteIdentifier("total") + " BETWEEN $ AND $", Column: d.QuoteIdentifier("total"), Value: sql.Range{Start: 10, End: 20}},
			}
			frag, fragArgs := sql.SubqueryWhere(d, "tgt", inner)
			sub := "SELECT 1 FROM " + d.FormatTable(sql.Table{Schema: "public", Name: "orders"}) +
				" tgt WHERE tgt." + d.QuoteIdentifier("user_id") + " = " + sql.CorrelationToken + " AND " + frag

			gotSQL, gotArgs := sql.BuildSelect(d, tt.table, sql.SelectOptions{Conditions: []sql.Condition{
				{Clause: d.QuoteIdentifier("email") + " = $", Column: d.QuoteIdentifier("email"), Value: "a@b.c"},
				sql.Exists("id", sql.Subquery{SQL: sub, Args: fragArgs}),
			}})

			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff([]any{"a@b.c", "paid", 10, 20}, gotArgs); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSubqueryWhere_NumberedNestedSubquery covers a "$N" subquery inside a
// relationship-filter fragment. The fragment renders bare "$"
// tokens, so the nested subquery's tokens become bare "$" in token order with
// the args to match, and the enclosing Exists numbers them as usual.
func TestSubqueryWhere_NumberedNestedSubquery(t *testing.T) {
	tests := []struct {
		name    string
		d       sql.Dialect
		tbl     sql.Table
		wantSQL string
	}{
		{"postgres", pg, pgTbl, `SELECT * FROM "public"."users" WHERE email = $1 AND EXISTS (SELECT 1 FROM "orders" tgt WHERE tgt."user_id" = "public"."users"."id" AND tgt."company_id" IN (SELECT "id" FROM "companies" WHERE "size" > $2 AND "country" = $3))`},
		{"mysql", my, myTbl, "SELECT * FROM `users` WHERE email = ? AND EXISTS (SELECT 1 FROM \"orders\" tgt WHERE tgt.\"user_id\" = `users`.`id` AND tgt.\"company_id\" IN (SELECT \"id\" FROM \"companies\" WHERE \"size\" > ? AND \"country\" = ?))"},
		{"sqlite", lite, ltTbl, `SELECT * FROM "users" WHERE email = ? AND EXISTS (SELECT 1 FROM "orders" tgt WHERE tgt."user_id" = "users"."id" AND tgt."company_id" IN (SELECT "id" FROM "companies" WHERE "size" > ? AND "country" = ?))`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frag, fragArgs := sql.SubqueryWhere(tt.d, "tgt", []sql.Condition{
				sql.Where(`"company_id"`).In(sql.Subquery{SQL: `SELECT "id" FROM "companies" WHERE "size" > $2 AND "country" = $1`, Args: []any{"US", 10}}),
			})
			sub := `SELECT 1 FROM "orders" tgt WHERE tgt."user_id" = ` + sql.CorrelationToken + " AND " + frag
			gotSQL, gotArgs := sql.BuildSelect(tt.d, tt.tbl, sql.SelectOptions{Conditions: []sql.Condition{
				sql.Where("email").Eq("a@b.c"),
				sql.Exists("id", sql.Subquery{SQL: sub, Args: fragArgs}),
			}})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff([]any{"a@b.c", 10, "US"}, gotArgs); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestExists_MySQLFilterLiteralWithEscapedQuote is the generated relationship
// filter's shape on MySQL: the static `filter:` predicate, which the
// MySQL qualifier deparses with a backslash-escaped quote (a doubled quote
// becomes \'), followed by the "$" the target filter appends. Two hops deep the
// inner subquery is scanned through [sql.SubqueryWhere]'s bare dialect first.
// Before the fix both shapes failed on MySQL with Unknown column '$' (1054).
func TestExists_MySQLFilterLiteralWithEscapedQuote(t *testing.T) {
	docs := func(alias string) sql.Condition {
		return sql.Exists("id", sql.Subquery{
			SQL: "SELECT 1 FROM `documents` " + alias + " WHERE " + alias + ".`entity_id` = " + sql.CorrelationToken +
				" AND (" + alias + ".`name` != 'it\\'s') AND " + alias + ".`name` = $",
			Args: []any{"photo.jpg"},
		})
	}
	frag, fragArgs := sql.SubqueryWhere(my, "tgt", []sql.Condition{docs("tgt1")})
	tests := []struct {
		name    string
		cond    sql.Condition
		wantSQL string
	}{
		{
			"one hop", docs("tgt"),
			"SELECT * FROM `users` WHERE email = ? AND EXISTS (SELECT 1 FROM `documents` tgt WHERE tgt.`entity_id` = `users`.`id` AND (tgt.`name` != 'it\\'s') AND tgt.`name` = ?)",
		},
		{
			"two hops", sql.Exists("id", sql.Subquery{
				SQL:  "SELECT 1 FROM `assets` tgt WHERE tgt.`owner_id` = " + sql.CorrelationToken + " AND " + frag,
				Args: fragArgs,
			}),
			"SELECT * FROM `users` WHERE email = ? AND EXISTS (SELECT 1 FROM `assets` tgt WHERE tgt.`owner_id` = `users`.`id` AND EXISTS (SELECT 1 FROM `documents` tgt1 WHERE tgt1.`entity_id` = tgt.`id` AND (tgt1.`name` != 'it\\'s') AND tgt1.`name` = ?))",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs := sql.BuildSelect(my, myTbl, sql.SelectOptions{Conditions: []sql.Condition{
				sql.Where("email").Eq("a@b.c"), tt.cond,
			}})
			if gotSQL != tt.wantSQL {
				t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, tt.wantSQL)
			}
			if diff := cmp.Diff([]any{"a@b.c", "photo.jpg"}, gotArgs); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSubqueryWhere_QualifiesEveryColumn covers the reason the fragment cannot
// simply reuse the caller's conditions: an unqualified name inside a correlated
// subquery binds to the *enclosing* statement whenever the subquery's own table
// lacks the column, silently turning a filter on the related row into one on the
// parent row.
func TestSubqueryWhere_QualifiesEveryColumn(t *testing.T) {
	conds := []sql.Condition{
		{Clause: `"status" = $`, Column: `"status"`, Value: "paid"},
		sql.Or(
			sql.Condition{Clause: `"total" > $`, Column: `"total"`, Value: 10},
			sql.Condition{Clause: `"notes" IS NULL`, Column: `"notes"`},
		),
	}
	got, gotArgs := sql.SubqueryWhere(pg, "tgt", conds)
	want := `tgt."status" = $ AND (tgt."total" > $ OR tgt."notes" IS NULL)`
	if got != want {
		t.Errorf("SubqueryWhere() =\n  %q\nwant\n  %q", got, want)
	}
	if diff := cmp.Diff([]any{"paid", 10}, gotArgs); diff != "" {
		t.Errorf("args mismatch (-want +got):\n%s", diff)
	}
}

// TestSubqueryWhere_NestedExistsCorrelatesToItsOwnAlias is the two-hop proof. A
// relationship filter one level deeper must correlate to the subquery that
// encloses it, not to the outermost statement — which is why SubqueryWhere
// passes its alias down as the qualifier.
func TestSubqueryWhere_NestedExistsCorrelatesToItsOwnAlias(t *testing.T) {
	innerExists := sql.Exists("id", sql.Subquery{
		SQL: `SELECT 1 FROM "public"."order_items" tgt1 WHERE tgt1."order_id" = ` +
			sql.CorrelationToken + ` AND tgt1."sku" = $`,
		Args: []any{"abc"},
	})
	frag, fragArgs := sql.SubqueryWhere(pg, "tgt0", []sql.Condition{
		{Clause: `"status" = $`, Column: `"status"`, Value: "paid"},
		innerExists,
	})
	sub := `SELECT 1 FROM "public"."orders" tgt0 WHERE tgt0."user_id" = ` + sql.CorrelationToken + " AND " + frag

	gotSQL, gotArgs := sql.BuildSelect(pg, pgTbl, sql.SelectOptions{
		Conditions: []sql.Condition{sql.Exists("id", sql.Subquery{SQL: sub, Args: fragArgs})},
	})
	wantSQL := `SELECT * FROM "public"."users" WHERE EXISTS (SELECT 1 FROM "public"."orders" tgt0 WHERE tgt0."user_id" = "public"."users"."id" AND tgt0."status" = $1 AND EXISTS (SELECT 1 FROM "public"."order_items" tgt1 WHERE tgt1."order_id" = tgt0."id" AND tgt1."sku" = $2))`
	if gotSQL != wantSQL {
		t.Errorf("BuildSelect sql =\n  %q\nwant\n  %q", gotSQL, wantSQL)
	}
	if diff := cmp.Diff([]any{"paid", "abc"}, gotArgs); diff != "" {
		t.Errorf("args mismatch (-want +got):\n%s", diff)
	}
}

// TestSubqueryWhere_EmptyConditions covers the "nil filter contributes no SQL"
// contract at the fragment level: the caller appends nothing rather than an
// empty AND.
func TestSubqueryWhere_EmptyConditions(t *testing.T) {
	got, gotArgs := sql.SubqueryWhere(pg, "tgt", nil)
	if got != "" {
		t.Errorf("SubqueryWhere() = %q, want empty", got)
	}
	if gotArgs != nil {
		t.Errorf("SubqueryWhere() args = %v, want nil", gotArgs)
	}
}

// TestSubqueryWhere_MultiPlaceholderConditions checks the shapes whose
// placeholder count is not one — IN, NOT IN, and an empty IN — since the bare
// "$" rendering has to reach PlaceholderList as well as Placeholder.
func TestSubqueryWhere_MultiPlaceholderConditions(t *testing.T) {
	tests := []struct {
		name     string
		conds    []sql.Condition
		want     string
		wantArgs []any
	}{
		{
			name:     "in",
			conds:    []sql.Condition{{Clause: `"status" IN $`, Column: `"status"`, Value: []any{"paid", "shipped"}}},
			want:     `tgt."status" IN ($, $)`,
			wantArgs: []any{"paid", "shipped"},
		},
		{
			name:     "not in",
			conds:    []sql.Condition{{Clause: `"status" NOT IN $`, Column: `"status"`, Value: []any{"void"}}},
			want:     `tgt."status" NOT IN ($)`,
			wantArgs: []any{"void"},
		},
		{
			name:     "empty in is always false",
			conds:    []sql.Condition{{Clause: `"status" IN $`, Column: `"status"`, Value: []any{}}},
			want:     "1 = 0",
			wantArgs: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotArgs := sql.SubqueryWhere(pg, "tgt", tt.conds)
			if got != tt.want {
				t.Errorf("SubqueryWhere() = %q, want %q", got, tt.want)
			}
			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- Raw condition parenthesisation ---

// TestBuildSelect_RawConditionParenthesised pins the invariant a Raw condition
// depends on: it renders as one AND-able term. buildWhere joins conditions with
// AND and AND binds tighter than OR, so an unwrapped clause whose top-level
// operator is OR pulls its neighbours into the disjunction — here the FK
// correlation and the soft-delete default, both of which must survive.
//
// This is the shape a relationship `filter:` takes on the O2M/M2M loader path
// (PRD §13.7.1) and the shape a caller's own comparator `Custom` condition
// takes; a leak on either returns rows belonging to an entity the caller did
// not ask for.
func TestBuildSelect_RawConditionParenthesised(t *testing.T) {
	tests := []struct {
		name    string
		dialect sql.Dialect
		table   sql.Table
		want    string
	}{
		{
			name:    "postgres",
			dialect: pg,
			table:   pgTbl,
			want:    `SELECT "id" FROM "public"."users" WHERE "entity_id" = $1 AND "deleted_at" IS NULL AND (kind = 'x' OR name = $2)`,
		},
		{
			name:    "mysql",
			dialect: my,
			table:   myTbl,
			want:    "SELECT `id` FROM `users` WHERE `entity_id` = ? AND `deleted_at` IS NULL AND (kind = 'x' OR name = ?)",
		},
		{
			name:    "sqlite",
			dialect: lite,
			table:   ltTbl,
			want:    `SELECT "id" FROM "users" WHERE "entity_id" = ? AND "deleted_at" IS NULL AND (kind = 'x' OR name = ?)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.dialect
			conds := []sql.Condition{
				sql.Where(d.QuoteIdentifier("entity_id")).Eq(1),
				sql.Where(d.QuoteIdentifier("deleted_at")).IsNull(),
				sql.Raw("kind = 'x' OR name = $", "leak"),
			}
			got, gotArgs := sql.BuildSelect(d, tt.table, sql.SelectOptions{
				Columns:    []string{"id"},
				Conditions: conds,
			})
			if got != tt.want {
				t.Errorf("BuildSelect() =\n  %q\nwant\n  %q", got, tt.want)
			}
			if diff := cmp.Diff([]any{1, "leak"}, gotArgs); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestBuildSelectJoin_RawConditionSurvivesPrefixing covers the interaction that
// rules out wrapping inside [sql.Raw] itself. The O2O-join read path runs every
// condition through PrefixConditions first, and a Raw condition has no Column,
// so prefixCondition prepends the alias to the whole clause. Parenthesising at
// construction would make that `t.(kind = ...)` — a syntax error. The wrap
// happens at render time instead, so the prefix lands inside it.
func TestBuildSelectJoin_RawConditionSurvivesPrefixing(t *testing.T) {
	conds := sql.PrefixConditions("t", []sql.Condition{
		sql.Where(`"entity_id"`).Eq(1),
		sql.Raw("kind = 'x' OR name = $", "leak"),
	})
	got, gotArgs := sql.BuildSelectJoin(pg, pgTbl, []sql.JoinClause{{
		Table:   sql.Table{Schema: "public", Name: "profiles"},
		Alias:   "p",
		On:      `p."user_id" = t."id"`,
		Columns: []string{"bio"},
	}}, sql.SelectOptions{
		Alias:      "t",
		Columns:    []string{"id"},
		Conditions: conds,
	})

	want := `SELECT t."id" AS "t.id", p."bio" AS "p.bio" FROM "public"."users" t ` +
		`LEFT JOIN "public"."profiles" p ON p."user_id" = t."id" ` +
		`WHERE t."entity_id" = $1 AND (t.kind = 'x' OR name = $2)`
	if got != want {
		t.Errorf("BuildSelectJoin() =\n  %q\nwant\n  %q", got, want)
	}
	if strings.Contains(got, "t.(") {
		t.Errorf("alias prefixed onto the parenthesis instead of the clause: %q", got)
	}
	if diff := cmp.Diff([]any{1, "leak"}, gotArgs); diff != "" {
		t.Errorf("args mismatch (-want +got):\n%s", diff)
	}
}
