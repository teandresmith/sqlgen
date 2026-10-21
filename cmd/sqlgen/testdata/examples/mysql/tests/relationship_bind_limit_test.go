package tests

// Relationship loaders split the parent keys across several statements, so a
// load whose parent set exceeds MySQL's bind-parameter limit (65535) still
// returns every child set. Each step binds at most half the limit per
// statement, leaving the rest for the consumer filter, so the load below
// takes three statements per step.

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	sqlgen "github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

const (
	bindLimitParents   = 65600
	bindLimitChunk     = 32767
	bindLimitCatPrefix = "bind-limit-cat-"
)

// statementLog records every statement a client issues outside a transaction.
type statementLog struct {
	inner database.Querier
	mu    sync.Mutex
	sql   []string
}

func (s *statementLog) record(q string) {
	s.mu.Lock()
	s.sql = append(s.sql, q)
	s.mu.Unlock()
}

func (s *statementLog) Exec(ctx context.Context, q string, args ...any) (database.Result, error) {
	s.record(q)
	return s.inner.Exec(ctx, q, args...)
}

func (s *statementLog) Query(ctx context.Context, q string, args ...any) (database.Rows, error) {
	s.record(q)
	return s.inner.Query(ctx, q, args...)
}

func (s *statementLog) QueryRow(ctx context.Context, q string, args ...any) database.Row {
	s.record(q)
	return s.inner.QueryRow(ctx, q, args...)
}

func (s *statementLog) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return s.inner.Begin(ctx, name, opts...)
}

// from counts the statements whose outermost FROM names table.
func (s *statementLog) from(table string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, q := range s.sql {
		if _, rest, ok := strings.Cut(q, "FROM "); ok && strings.HasPrefix(rest, table) {
			n++
		}
	}
	return n
}

func TestRelationshipLoad_PastBindLimit(t *testing.T) {
	ctx := context.Background()
	seedBindLimit(ctx, t)

	log := &statementLog{inner: dbstdlib.New(testDB)}
	client := models.New(log)
	categories, err := client.Categories().GetMany(ctx, &models.GetCategoriesInput{
		Filter: &models.CategoryFilter{Name: &comparator.String{StartsWith: new(bindLimitCatPrefix)}},
		Limit:  new(0),
	}, func(o *models.CallOptions[models.CategoryFieldOptions]) {
		o.FieldOptions = &models.CategoryFieldOptions{
			Name: true,
			Products: &models.ProductRelationshipOptions{
				FieldOptions: &models.ProductFieldOptions{Title: true},
				Filter:       &models.ProductFilter{InStock: &comparator.Bool{Eq: new(true)}},
				Sorts:        []sqlgen.Sort{{Column: "title", Direction: sqlgen.Desc}},
			},
			Users: &models.UserRelationshipOptions{
				FieldOptions: &models.UserFieldOptions{Name: true},
				Filter:       &models.UserFilter{Name: &comparator.String{StartsWith: new("bind-limit-user-")}},
				Sorts:        []sqlgen.Sort{{Column: "name", Direction: sqlgen.Desc}},
			},
		}
	})
	if err != nil {
		t.Fatalf("GetMany categories with Products and Users: %v", err)
	}
	if len(categories) != bindLimitParents {
		t.Fatalf("GetMany returned %d categories, want %d", len(categories), bindLimitParents)
	}

	for _, c := range categories {
		n, err := strconv.Atoi(strings.TrimPrefix(c.Name, bindLimitCatPrefix))
		if err != nil {
			t.Fatalf("category name %q: %v", c.Name, err)
		}
		var gotProducts []string
		for _, p := range c.Products {
			gotProducts = append(gotProducts, p.Title)
		}
		if want := bindLimitWantProducts(n); !slices.Equal(gotProducts, want) {
			t.Fatalf("category %d products = %v, want %v", n, gotProducts, want)
		}
		var gotUsers []string
		for _, u := range c.Users {
			gotUsers = append(gotUsers, u.Name)
		}
		if want := bindLimitWantUsers(n); !slices.Equal(gotUsers, want) {
			t.Fatalf("category %d users = %v, want %v", n, gotUsers, want)
		}
	}

	chunks := (bindLimitParents + bindLimitChunk - 1) / bindLimitChunk
	for _, table := range []string{"`products`", "`user_categories`", "`users`"} {
		if got := log.from(table); got != chunks {
			t.Errorf("statements reading %s = %d, want %d", table, got, chunks)
		}
	}
}

// Two parents sharing more targets than one chunk holds: the junction holds
// twice that many rows, and every parent's targets must still arrive in the
// relationship's sort order, which no per-chunk statement can give if the
// targets are split across statements.
func TestRelationshipLoad_M2MTargetsPastBindLimit(t *testing.T) {
	ctx := context.Background()
	const targets = bindLimitChunk + 17
	stmts := []string{
		`INSERT INTO categories (name) VALUES ('bind-limit-shared-1'), ('bind-limit-shared-2')`,
		fmt.Sprintf(`INSERT INTO users (name, email, balance)
			WITH RECURSIVE seq (i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < %d)
			SELECT CONCAT('bind-limit-target-', LPAD(i, 5, '0')), CONCAT('bind-limit-target-', i, '@example.com'), 0 FROM seq`, targets),
		`INSERT INTO user_categories (user_id, category_id)
			SELECT u.id, c.id FROM users u CROSS JOIN categories c
			WHERE u.name LIKE 'bind-limit-target-%' AND c.name LIKE 'bind-limit-shared-_'`,
	}
	t.Cleanup(func() {
		execBindLimit(ctx, t, t.Errorf,
			`DELETE FROM user_categories WHERE user_id IN (SELECT id FROM users WHERE name LIKE 'bind-limit-target-%')`,
			`DELETE FROM users WHERE name LIKE 'bind-limit-target-%'`,
			`DELETE FROM categories WHERE name LIKE 'bind-limit-shared-_'`,
		)
	})
	execBindLimit(ctx, t, t.Fatalf, stmts...)

	categories, err := newClient().Categories().GetMany(ctx, &models.GetCategoriesInput{
		Filter: &models.CategoryFilter{Name: &comparator.String{Like: new("bind-limit-shared-_")}},
		Limit:  new(0),
	}, func(o *models.CallOptions[models.CategoryFieldOptions]) {
		o.FieldOptions = &models.CategoryFieldOptions{
			Name: true,
			Users: &models.UserRelationshipOptions{
				FieldOptions: &models.UserFieldOptions{Name: true},
				Sorts:        []sqlgen.Sort{{Column: "name", Direction: sqlgen.Desc}},
			},
		}
	})
	if err != nil {
		t.Fatalf("GetMany categories with Users: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("GetMany returned %d categories, want 2", len(categories))
	}
	want := make([]string, targets)
	for i := range want {
		want[i] = fmt.Sprintf("bind-limit-target-%05d", targets-i)
	}
	for _, c := range categories {
		got := make([]string, len(c.Users))
		for i, u := range c.Users {
			got[i] = u.Name
		}
		if !slices.Equal(got, want) {
			first := 0
			for first < min(len(got), len(want)) && got[first] == want[first] {
				first++
			}
			t.Errorf("category %s: %d users, want %d in name-descending order; first difference at %d", c.Name, len(got), len(want), first)
		}
	}
}

// bindLimitWantProducts is the in-stock product titles of category n, title
// descending. Every 1000th category, and the first and last, holds two
// in-stock products and one out of stock.
func bindLimitWantProducts(n int) []string {
	if n%1000 != 0 && n != 1 && n != bindLimitParents {
		return nil
	}
	return []string{fmt.Sprintf("bl-%d-b", n), fmt.Sprintf("bl-%d-a", n)}
}

// bindLimitWantUsers is the user names linked to category n, name descending.
// Category n links user n%3, and every fifth category links user (n+1)%3 too.
func bindLimitWantUsers(n int) []string {
	users := []string{fmt.Sprintf("bind-limit-user-%d", n%3)}
	if n%5 == 0 {
		users = append(users, fmt.Sprintf("bind-limit-user-%d", (n+1)%3))
	}
	slices.Sort(users)
	slices.Reverse(users)
	return users
}

// execBindLimit runs stmts on one session, whose recursive-CTE depth is raised
// past the 1000 default for the generated sequences.
func execBindLimit(ctx context.Context, t *testing.T, fail func(string, ...any), stmts ...string) {
	t.Helper()
	conn, err := testDB.Conn(ctx)
	if err != nil {
		fail("pinning a session: %v", err)
		return
	}
	defer conn.Close()
	for _, q := range append([]string{"SET SESSION cte_max_recursion_depth = 100000"}, stmts...) {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			fail("%v\n%s", err, q)
			return
		}
	}
}

func seedBindLimit(ctx context.Context, t *testing.T) {
	t.Helper()
	ord := "CAST(SUBSTRING(c.name, " + strconv.Itoa(len(bindLimitCatPrefix)+1) + ") AS UNSIGNED)"
	t.Cleanup(func() {
		execBindLimit(ctx, t, t.Errorf,
			`DELETE FROM products WHERE sku LIKE 'bl-%'`,
			`DELETE FROM user_categories WHERE user_id IN (SELECT id FROM users WHERE name LIKE 'bind-limit-user-%')`,
			`DELETE FROM users WHERE name LIKE 'bind-limit-user-%'`,
			`DELETE FROM categories WHERE name LIKE '`+bindLimitCatPrefix+`%'`,
		)
	})
	execBindLimit(ctx, t, t.Fatalf,
		fmt.Sprintf(`INSERT INTO categories (name)
			WITH RECURSIVE seq (i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < %d)
			SELECT CONCAT('%s', i) FROM seq`, bindLimitParents, bindLimitCatPrefix),
		`INSERT INTO users (name, email, balance) VALUES
			('bind-limit-user-0', 'bind-limit-0@example.com', 0),
			('bind-limit-user-1', 'bind-limit-1@example.com', 0),
			('bind-limit-user-2', 'bind-limit-2@example.com', 0)`,
		`INSERT INTO user_categories (user_id, category_id)
			SELECT u.id, c.id FROM categories c JOIN users u
			ON u.name = CONCAT('bind-limit-user-', `+ord+` % 3)
			OR (`+ord+` % 5 = 0 AND u.name = CONCAT('bind-limit-user-', (`+ord+` + 1) % 3))
			WHERE c.name LIKE '`+bindLimitCatPrefix+`%'`,
		fmt.Sprintf(`INSERT INTO products (category_id, title, price, in_stock, sku, attributes)
			SELECT c.id, CONCAT('bl-', %[1]s, '-', s.suffix), 1.00, s.stock, CONCAT('bl-', %[1]s, '-', s.suffix), JSON_OBJECT()
			FROM categories c CROSS JOIN (SELECT 'a' AS suffix, TRUE AS stock UNION ALL SELECT 'b', TRUE UNION ALL SELECT 'c', FALSE) s
			WHERE c.name LIKE '%[2]s%%' AND (%[1]s %% 1000 = 0 OR %[1]s IN (1, %[3]d))`, ord, bindLimitCatPrefix, bindLimitParents),
	)
}
