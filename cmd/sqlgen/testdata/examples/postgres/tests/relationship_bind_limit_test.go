package tests

// A relationship load whose parent set exceeds PostgreSQL's bind-parameter
// limit (65535) still returns every child set. With pgx the O2M fetch binds
// every parent key as one array parameter (= ANY($1)), so it takes one
// statement. The M2M junction and target steps bind one parameter per key, so
// they split the parent keys across statements of at most half the limit,
// leaving the rest for the consumer filter: three statements each below.

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
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	sqlgen "github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
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

	log := &statementLog{inner: dbpgx.New(testPool)}
	client := models.New(log)
	categories, err := client.Categories().GetMany(ctx, &models.GetCategoriesInput{
		Filter: &models.CategoryFilter{Name: &comparator.String{StartsWith: new(bindLimitCatPrefix)}},
		Limit:  new(0),
	}, func(o *models.CallOptions[models.CategoryFieldOptions]) {
		o.FieldOptions = &models.CategoryFieldOptions{
			Name: true,
			Products: &models.ProductRelationshipOptions{
				FieldOptions: &models.ProductFieldOptions{Name: true},
				Filter:       &models.ProductFilter{IsActive: &comparator.Bool{Eq: new(true)}},
				Sorts:        []sqlgen.Sort{{Column: "name", Direction: sqlgen.Desc}},
			},
			Users: &models.PublicUserRelationshipOptions{
				FieldOptions: &models.PublicUserFieldOptions{Name: true},
				Filter:       &models.PublicUserFilter{Name: &comparator.String{StartsWith: new("bind-limit-user-")}},
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
			gotProducts = append(gotProducts, p.Name)
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
	for table, want := range map[string]int{
		`"public"."products"`:        1,
		`"public"."user_categories"`: chunks,
		`"public"."users"`:           chunks,
	} {
		if got := log.from(table); got != want {
			t.Errorf("statements reading %s = %d, want %d", table, got, want)
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
		fmt.Sprintf(`INSERT INTO users (name, email)
			SELECT format('bind-limit-target-%%s', lpad(i::text, 5, '0')), format('bind-limit-target-%%s@example.com', i)
			FROM generate_series(1, %d) AS i`, targets),
		`INSERT INTO user_categories (user_id, category_id)
			SELECT u.id, c.id FROM users u CROSS JOIN categories c
			WHERE u.name LIKE 'bind-limit-target-%' AND c.name LIKE 'bind-limit-shared-_'`,
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM user_categories WHERE user_id IN (SELECT id FROM users WHERE name LIKE 'bind-limit-target-%')`,
			`DELETE FROM users WHERE name LIKE 'bind-limit-target-%'`,
			`DELETE FROM categories WHERE name LIKE 'bind-limit-shared-_'`,
		} {
			if _, err := testPool.Exec(ctx, q); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
	})
	for _, q := range stmts {
		if _, err := testPool.Exec(ctx, q); err != nil {
			t.Fatalf("seeding: %v\n%s", err, q)
		}
	}

	categories, err := newClient().Categories().GetMany(ctx, &models.GetCategoriesInput{
		Filter: &models.CategoryFilter{Name: &comparator.String{Like: new("bind-limit-shared-_")}},
		Limit:  new(0),
	}, func(o *models.CallOptions[models.CategoryFieldOptions]) {
		o.FieldOptions = &models.CategoryFieldOptions{
			Name: true,
			Users: &models.PublicUserRelationshipOptions{
				FieldOptions: &models.PublicUserFieldOptions{Name: true},
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

// bindLimitWantProducts is the active product names of category n, name
// descending. Every 1000th category, and the first and last, holds two
// active products and one inactive.
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

func seedBindLimit(ctx context.Context, t *testing.T) {
	t.Helper()
	ord := "substr(c.name, " + strconv.Itoa(len(bindLimitCatPrefix)+1) + ")::int"
	stmts := []string{
		fmt.Sprintf(`INSERT INTO categories (name) SELECT '%s' || i FROM generate_series(1, %d) AS i`, bindLimitCatPrefix, bindLimitParents),
		`INSERT INTO users (name, email) VALUES
			('bind-limit-user-0', 'bind-limit-0@example.com'),
			('bind-limit-user-1', 'bind-limit-1@example.com'),
			('bind-limit-user-2', 'bind-limit-2@example.com')`,
		`INSERT INTO user_categories (user_id, category_id)
			SELECT u.id, c.id FROM categories c JOIN users u
			ON u.name = 'bind-limit-user-' || (` + ord + ` % 3)
			OR (` + ord + ` % 5 = 0 AND u.name = 'bind-limit-user-' || ((` + ord + ` + 1) % 3))
			WHERE c.name LIKE '` + bindLimitCatPrefix + `%'`,
		fmt.Sprintf(`INSERT INTO products (category_id, name, price, is_active)
			SELECT c.id, 'bl-' || %[1]s || '-' || s.suffix, 1.00, s.active
			FROM categories c CROSS JOIN (VALUES ('a', true), ('b', true), ('c', false)) AS s(suffix, active)
			WHERE c.name LIKE '%[2]s%%' AND (%[1]s %% 1000 = 0 OR %[1]s IN (1, %[3]d))`, ord, bindLimitCatPrefix, bindLimitParents),
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM products WHERE name LIKE 'bl-%'`,
			`DELETE FROM user_categories WHERE user_id IN (SELECT id FROM users WHERE name LIKE 'bind-limit-user-%')`,
			`DELETE FROM users WHERE name LIKE 'bind-limit-user-%'`,
			`DELETE FROM categories WHERE name LIKE '` + bindLimitCatPrefix + `%'`,
		} {
			if _, err := testPool.Exec(ctx, q); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
	})
	for _, q := range stmts {
		if _, err := testPool.Exec(ctx, q); err != nil {
			t.Fatalf("seeding: %v\n%s", err, q)
		}
	}
}
