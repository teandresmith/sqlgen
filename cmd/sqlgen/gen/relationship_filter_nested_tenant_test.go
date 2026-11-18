package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// gnwNestedTenantDDL holds two untenanted parents with relationship filters.
// regions reaches the tenanted orders table only through the untenanted
// stores table; countries reaches only the untenanted cities table.
var gnwNestedTenantDDL = map[config.Dialect]string{
	config.DialectPostgres: `
CREATE TABLE regions (id bigserial PRIMARY KEY, name text NOT NULL);
CREATE TABLE stores (id bigserial PRIMARY KEY, region_id bigint NOT NULL REFERENCES regions(id), name text NOT NULL);
CREATE TABLE orders (id bigserial PRIMARY KEY, store_id bigint NOT NULL REFERENCES stores(id), workspace_id bigint NOT NULL, total bigint NOT NULL);
CREATE TABLE countries (id bigserial PRIMARY KEY, name text NOT NULL);
CREATE TABLE cities (id bigserial PRIMARY KEY, country_id bigint NOT NULL REFERENCES countries(id), population bigint NOT NULL);`,
	config.DialectMySQL: `
CREATE TABLE regions (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, name VARCHAR(64) NOT NULL);
CREATE TABLE stores (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, region_id BIGINT NOT NULL, name VARCHAR(64) NOT NULL, FOREIGN KEY (region_id) REFERENCES regions(id));
CREATE TABLE orders (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, store_id BIGINT NOT NULL, workspace_id BIGINT NOT NULL, total BIGINT NOT NULL, FOREIGN KEY (store_id) REFERENCES stores(id));
CREATE TABLE countries (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, name VARCHAR(64) NOT NULL);
CREATE TABLE cities (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, country_id BIGINT NOT NULL, population BIGINT NOT NULL, FOREIGN KEY (country_id) REFERENCES countries(id));`,
	config.DialectSQLite: `
CREATE TABLE regions (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);
CREATE TABLE stores (id INTEGER PRIMARY KEY AUTOINCREMENT, region_id INTEGER NOT NULL REFERENCES regions(id), name TEXT NOT NULL);
CREATE TABLE orders (id INTEGER PRIMARY KEY AUTOINCREMENT, store_id INTEGER NOT NULL REFERENCES stores(id), workspace_id INTEGER NOT NULL, total INTEGER NOT NULL);
CREATE TABLE countries (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);
CREATE TABLE cities (id INTEGER PRIMARY KEY AUTOINCREMENT, country_id INTEGER NOT NULL REFERENCES countries(id), population INTEGER NOT NULL);`,
}

const gnwNestedTenantYAML = "tenancy:\n  enabled: true\n  column: workspace_id\n  required: true\n"

// gnwNestedTenantProbe runs inside a generated package. A regions read whose
// filter reaches orders through stores binds the resolved tenant into the
// orders subquery; a countries read through cities resolves no tenant at all.
const gnwNestedTenantProbe = `package db

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
)

const probeTenant = int64(4242)

func probeClient() (*Client, *probeQuerier, *atomic.Int64) {
	q := &probeQuerier{}
	var calls atomic.Int64
	c := New(q, WithTenantResolver(func(context.Context) (int64, error) {
		calls.Add(1)
		return probeTenant, nil
	}))
	return c, q, &calls
}

func probeOnly(t *testing.T, q *probeQuerier) probeStatement {
	t.Helper()
	stmts := q.statements()
	if len(stmts) != 1 {
		t.Fatalf("issued %d statements, want 1: %v", len(stmts), stmts)
	}
	return stmts[0]
}

func TestProbe_TenantReachesNestedMember(t *testing.T) {
	filter := func() *RegionFilter {
		return &RegionFilter{Stores: &StoreFilter{Orders: &OrderFilter{Total: &comparator.Number[int64]{Gt: new(int64(100))}}}}
	}
	reads := []struct {
		name string
		run  func(c *Client) error
	}{
		{name: "GetMany", run: func(c *Client) error {
			_, err := c.Regions().GetMany(t.Context(), &GetRegionsInput{Filter: filter()})
			return err
		}},
		{name: "Count", run: func(c *Client) error {
			_, err := c.Regions().Count(t.Context(), filter())
			return err
		}},
	}
	for _, r := range reads {
		t.Run(r.name, func(t *testing.T) {
			c, q, calls := probeClient()
			if err := r.run(c); !errors.Is(err, errProbe) {
				t.Fatalf("regions %s error = %v, want the recorded statement's error", r.name, err)
			}
			st := probeOnly(t, q)
			if !strings.Contains(st.sql, "workspace_id") {
				t.Errorf("regions %s statement has no orders tenant predicate:\n%s", r.name, st.sql)
			}
			if !slices.Contains(st.args, any(probeTenant)) {
				t.Errorf("regions %s binds %v, want the resolved tenant %d among them\nSQL: %s", r.name, st.args, probeTenant, st.sql)
			}
			if calls.Load() == 0 {
				t.Errorf("regions %s never called the tenant resolver", r.name)
			}
		})
	}
}

func TestProbe_NoTenantForUntenantedReach(t *testing.T) {
	filter := func() *CountryFilter {
		return &CountryFilter{Cities: &CityFilter{Population: &comparator.Number[int64]{Gt: new(int64(100))}}}
	}
	reads := []struct {
		name string
		run  func(c *Client) error
	}{
		{name: "GetMany", run: func(c *Client) error {
			_, err := c.Countries().GetMany(t.Context(), &GetCountriesInput{Filter: filter()})
			return err
		}},
		{name: "Count", run: func(c *Client) error {
			_, err := c.Countries().Count(t.Context(), filter())
			return err
		}},
	}
	for _, r := range reads {
		t.Run(r.name, func(t *testing.T) {
			c, q, calls := probeClient()
			if err := r.run(c); !errors.Is(err, errProbe) {
				t.Fatalf("countries %s error = %v, want the recorded statement's error", r.name, err)
			}
			st := probeOnly(t, q)
			// The cities EXISTS must be in the statement, or there is no
			// relationship filter for a tenant to be missing from.
			if !strings.Contains(st.sql, "cities") {
				t.Fatalf("countries %s statement has no cities subquery:\n%s", r.name, st.sql)
			}
			if got := calls.Load(); got != 0 {
				t.Errorf("countries %s called the tenant resolver %d times, want 0", r.name, got)
			}
			if slices.Contains(st.args, any(probeTenant)) || strings.Contains(st.sql, "workspace_id") {
				t.Errorf("countries %s carries a tenant predicate: %s %v", r.name, st.sql, st.args)
			}
		})
	}
}
`

// TestGenerate_RelationshipFilterTenantReachesNestedMember generates, on every
// dialect, a tenancy project where an untenanted parent's relationship
// filters reach a tenanted table only through an untenanted intermediate, and
// another untenanted parent's reach no tenanted table. The generated code is
// checked in two ways: the first parent's client resolves the tenant before
// compiling its filter and the second's does not; and, run against a
// recording querier, the first parent's GetMany and Count bind the resolved
// tenant into the nested orders subquery while the second's never call the
// resolver.
func TestGenerate_RelationshipFilterTenantReachesNestedMember(t *testing.T) {
	const (
		resolved = `ToConditions(c.dialect, filterOpts...)`
		plain    = `ToConditions(c.dialect)`
	)
	dialects := []struct {
		name    string
		dialect config.Dialect
	}{
		{name: "postgres", dialect: config.DialectPostgres},
		{name: "mysql", dialect: config.DialectMySQL},
		{name: "sqlite", dialect: config.DialectSQLite},
	}

	t.Run("rendered methods", func(t *testing.T) {
		for _, d := range dialects {
			t.Run(d.name, func(t *testing.T) {
				models := readGenFile(t, gnwGenerate(t, d.dialect, gnwNestedTenantDDL[d.dialect], gnwNestedTenantYAML), "models_gen.go")

				// The intermediate carries the tenanted orders filter; it is
				// what the parent's resolved tenant has to reach.
				if f := extractTopLevelFunc(t, models, "func storeOrdersFilterExists("); !strings.Contains(f, "scope.applyTenant") {
					t.Fatalf("control: stores' orders EXISTS helper applies no tenant\n%s", f)
				}
				for _, m := range []string{"GetMany", "Count"} {
					body := extractTopLevelFunc(t, models, "func (c *regionClient) "+m+"(")
					if !strings.Contains(body, "c.filterOptions(ctx, ") || !strings.Contains(body, resolved) {
						t.Errorf("regions %s does not compile its filter under the resolved tenant (%q)\nfull method:\n%s", m, resolved, body)
					}
					body = extractTopLevelFunc(t, models, "func (c *countryClient) "+m+"(")
					if strings.Contains(body, "filterOptions") || !strings.Contains(body, plain) {
						t.Errorf("countries %s resolves a tenant for a filter that reaches no tenanted table, want %q\nfull method:\n%s", m, plain, body)
					}
				}
				if strings.Contains(models, "func (c *countryClient) filterOptions(") {
					t.Errorf("countries client declares filterOptions; it reaches no tenanted table")
				}
			})
		}
	})

	t.Run("generated reads", func(t *testing.T) {
		pkgs := make([]gnwProbePackage, 0, len(dialects))
		for _, d := range dialects {
			pkgs = append(pkgs, gnwProbePackage{dir: d.name, dialect: d.dialect, ddl: gnwNestedTenantDDL[d.dialect], extra: gnwNestedTenantYAML})
		}
		gnwRunGeneratedProbe(t, pkgs, gnwNestedTenantProbe)
	})
}
