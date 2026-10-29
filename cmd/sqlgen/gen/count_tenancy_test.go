package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestGenerate_TenantedCountAppendsTenantPredicate generates a tenancy
// project holding a tenanted table and a shared one, on every dialect. The
// tenanted table's Count resolves the tenant and appends the same tenant
// predicate its GetMany does to the conditions it hands sql.BuildCount; the
// shared table's Count appends none. Run against a recording querier, the
// tenanted Count binds the resolved tenant and the shared one never calls the
// resolver.
func TestGenerate_TenantedCountAppendsTenantPredicate(t *testing.T) {
	const (
		tenantPredicate = `conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("workspace_id")).Eq(resolvedTenant))`
		resolve         = `c.resolveTenant(ctx, options.Tenant)`
		build           = `sql.BuildCount(c.dialect, c.table, conds)`
		tenancyYAML     = "tenancy:\n  enabled: true\n  column: workspace_id\n  required: true\n"
	)
	tests := []struct {
		name    string
		dialect config.Dialect
		ddl     string
	}{
		{
			name:    "postgres",
			dialect: config.DialectPostgres,
			ddl: `CREATE TABLE invoices (id bigserial PRIMARY KEY, workspace_id bigint NOT NULL, total bigint NOT NULL);
CREATE TABLE currencies (id bigserial PRIMARY KEY, code text NOT NULL);`,
		},
		{
			name:    "mysql",
			dialect: config.DialectMySQL,
			ddl: "CREATE TABLE invoices (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, workspace_id BIGINT NOT NULL, total BIGINT NOT NULL);\n" +
				"CREATE TABLE currencies (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, code VARCHAR(3) NOT NULL);",
		},
		{
			name:    "sqlite",
			dialect: config.DialectSQLite,
			ddl: `CREATE TABLE invoices (id INTEGER PRIMARY KEY AUTOINCREMENT, workspace_id INTEGER NOT NULL, total INTEGER NOT NULL);
CREATE TABLE currencies (id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT NOT NULL);`,
		},
	}

	t.Run("rendered methods", func(t *testing.T) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				models := readGenFile(t, gnwGenerate(t, tt.dialect, tt.ddl, tenancyYAML), "models_gen.go")

				getMany := extractTopLevelFunc(t, models, "func (c *invoiceClient) GetMany(")
				if !strings.Contains(getMany, tenantPredicate) {
					t.Fatalf("control: invoices GetMany has no tenant predicate %q\nfull method:\n%s", tenantPredicate, getMany)
				}

				count := extractTopLevelFunc(t, models, "func (c *invoiceClient) Count(")
				r, p, b := strings.Index(count, resolve), strings.Index(count, tenantPredicate), strings.Index(count, build)
				switch {
				case r < 0:
					t.Errorf("invoices Count never resolves the tenant (%q)\nfull method:\n%s", resolve, count)
				case p < 0:
					t.Errorf("invoices Count does not append the tenant predicate %q\nfull method:\n%s", tenantPredicate, count)
				case b < 0:
					t.Errorf("invoices Count does not build its statement from conds (%q)\nfull method:\n%s", build, count)
				case r > p || p > b:
					t.Errorf("invoices Count appends the tenant predicate outside resolve -> append -> BuildCount (offsets %d, %d, %d)\nfull method:\n%s", r, p, b, count)
				}

				shared := extractTopLevelFunc(t, models, "func (c *currencyClient) Count(")
				for _, s := range []string{resolve, `"workspace_id"`} {
					if strings.Contains(shared, s) {
						t.Errorf("shared currencies Count contains %q; an untenanted table's Count takes no tenant predicate\nfull method:\n%s", s, shared)
					}
				}
			})
		}
	})

	t.Run("generated counts", func(t *testing.T) {
		pkgs := make([]gnwProbePackage, 0, len(tests))
		for _, tt := range tests {
			pkgs = append(pkgs, gnwProbePackage{dir: tt.name, dialect: tt.dialect, ddl: tt.ddl, extra: tenancyYAML})
		}
		gnwRunGeneratedProbe(t, pkgs, gnwCountTenancyProbe)
	})
}

// gnwCountTenancyProbe runs inside a generated package. The tenanted invoices
// Count binds the resolved tenant into its one statement; the shared
// currencies Count never calls the resolver and binds nothing tenant-shaped.
const gnwCountTenancyProbe = `package db

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

const probeTenant = int64(4242)

func probeCount(t *testing.T, count func(c *Client) error) (probeStatement, int) {
	t.Helper()
	q := &probeQuerier{}
	calls := 0
	c := New(q, WithTenantResolver(func(context.Context) (int64, error) {
		calls++
		return probeTenant, nil
	}))
	if err := count(c); !errors.Is(err, errProbe) {
		t.Fatalf("Count error = %v, want the recorded statement's error", err)
	}
	stmts := q.statements()
	if len(stmts) != 1 {
		t.Fatalf("Count issued %d statements, want 1: %v", len(stmts), stmts)
	}
	return stmts[0], calls
}

func TestProbe_TenantedCountBindsResolvedTenant(t *testing.T) {
	st, calls := probeCount(t, func(c *Client) error {
		_, err := c.Invoices().Count(t.Context(), nil)
		return err
	})
	if calls == 0 {
		t.Errorf("invoices Count never called the tenant resolver")
	}
	if !strings.Contains(st.sql, "workspace_id") {
		t.Errorf("invoices Count statement has no tenant predicate:\n%s", st.sql)
	}
	if !slices.Contains(st.args, any(probeTenant)) {
		t.Errorf("invoices Count binds %v, want the resolved tenant %d among them\nSQL: %s", st.args, probeTenant, st.sql)
	}
}

func TestProbe_SharedCountBindsNoTenant(t *testing.T) {
	st, calls := probeCount(t, func(c *Client) error {
		_, err := c.Currencies().Count(t.Context(), nil)
		return err
	})
	if calls != 0 {
		t.Errorf("currencies Count called the tenant resolver %d times, want 0", calls)
	}
	if strings.Contains(st.sql, "workspace_id") || slices.Contains(st.args, any(probeTenant)) {
		t.Errorf("currencies Count carries a tenant predicate: %s %v", st.sql, st.args)
	}
}
`
