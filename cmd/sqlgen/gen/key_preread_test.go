package gen_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

var (
	preReadColumnsRe = regexp.MustCompile(`Columns:\s+\[\]string\{([^}]*)\}`)
	preReadScanRe    = regexp.MustCompile(`rows\.Scan\(([^)]*)\)`)
)

// A Where write on MySQL pre-reads the keys of the rows it will touch.
// sql.BuildSelect emits the pre-read's column list sorted, so the helper must
// scan its targets in that sorted order — whatever order the key columns are
// declared in and wherever the tenant column sorts among them — or a value
// lands in another column's field.
func TestKeyPreRead_mysqlScansInSortedSelectOrder(t *testing.T) {
	// reversedPK declares the composite key columns out of name order.
	reversedPK := func(tc gen.TableContext) gen.TableContext {
		tc.PKColumns = slices.Clone(tc.PKColumns)
		slices.Reverse(tc.PKColumns)
		return tc
	}
	// tenantSortsFirst renames the tenant column so it sorts before the key.
	tenantSortsFirst := func(tc gen.TableContext) gen.TableContext {
		tenancy := *tc.Tenancy
		tenancy.Column = "account_id"
		tc.Tenancy = &tenancy
		return tc
	}
	untenanted := func(tc gen.TableContext) gen.TableContext {
		tc.Tenancy = nil
		return tc
	}
	withoutUpdateWhere := func(tc gen.TableContext) gen.TableContext {
		tc.Operations.UpdateWhere = false
		return tc
	}
	withUpdateWhere := func(tc gen.TableContext) gen.TableContext {
		tc.Operations.UpdateWhere = true
		return tc
	}
	onMySQL := func(tc gen.TableContext) gen.TableContext {
		tc.Dialect = "mysql"
		tc.Driver = "stdlib"
		return tc
	}

	cases := []struct {
		name     string
		ctx      gen.TableContext
		template string
		helper   string
	}{
		{
			name:     "composite key delete helper",
			ctx:      reversedPK(onMySQL(testDeleteContext_compositePK())),
			template: "table/delete",
			helper:   "collectAffectedPKs",
		},
		{
			name:     "composite key update helper",
			ctx:      reversedPK(untenanted(compositeTenantOutsidePKContext("mysql", "stdlib"))),
			template: "table/update",
			helper:   "collectAffectedPKs",
		},
		{
			name:     "tenanted composite key delete helper",
			ctx:      tenantSortsFirst(reversedPK(withoutUpdateWhere(compositeTenantOutsidePKContext("mysql", "stdlib")))),
			template: "table/delete",
			helper:   "collectAffectedPKs",
		},
		{
			name:     "tenanted composite key update helper",
			ctx:      tenantSortsFirst(reversedPK(compositeTenantOutsidePKContext("mysql", "stdlib"))),
			template: "table/update",
			helper:   "collectAffectedPKs",
		},
		{
			name:     "tenanted single key delete helper",
			ctx:      tenantSortsFirst(onMySQL(tenantedProductContext())),
			template: "table/delete",
			helper:   "collectAffectedIDs",
		},
		{
			name:     "tenanted single key update helper",
			ctx:      tenantSortsFirst(withUpdateWhere(onMySQL(tenantedProductContext()))),
			template: "table/update",
			helper:   "collectAffectedIDs",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out := renderTenantCapture(t, tt.ctx, tt.template, sql.NewMySQLDialect())
			body := methodBody(t, out, tt.helper)

			colMatch := preReadColumnsRe.FindStringSubmatch(body)
			if colMatch == nil {
				t.Fatalf("no Columns list in %s:\n%s", tt.helper, body)
			}
			var selected []string
			for _, c := range strings.Split(colMatch[1], ",") {
				if c = strings.Trim(strings.TrimSpace(c), `"`); c != "" {
					selected = append(selected, c)
				}
			}
			sorted := slices.Sorted(slices.Values(selected))

			scanMatch := preReadScanRe.FindStringSubmatch(body)
			if scanMatch == nil {
				t.Fatalf("no rows.Scan in %s:\n%s", tt.helper, body)
			}
			columnOf := map[string]string{"&id": tt.ctx.PKColumns[0].Name}
			for _, col := range tt.ctx.PKColumns {
				columnOf["&pk."+col.FieldName] = col.Name
			}
			if tt.ctx.Tenancy != nil {
				columnOf["&rowTenant"] = tt.ctx.Tenancy.Column
			}
			var scanned []string
			for _, target := range strings.Split(scanMatch[1], ",") {
				target = strings.TrimSpace(target)
				col, ok := columnOf[target]
				if !ok {
					t.Fatalf("unknown scan target %q in %s:\n%s", target, tt.helper, body)
				}
				scanned = append(scanned, col)
			}

			if diff := cmp.Diff(sorted, scanned); diff != "" {
				t.Errorf("%s scans columns out of the sorted SELECT order (-sorted select +scan):\n%s\n%s", tt.helper, diff, body)
			}
		})
	}
}
