package gen_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// Increment eligibility excludes identifying columns (PRD §26.5.4).
//
// A primary key is the row's own identity; a foreign key is another row's
// identity. `category_id + 1` lands on whichever row happens to sit at the
// next value — never the intended operation, and on a tenanted schema it
// walks the reference into another tenant, since Increment constrains which
// rows it matches but not which column it targets.
//
// Only integer FKs were ever affected: a uuid or text FK is not an
// incrementable Go type, which is why the gap stayed invisible.
func TestIncrementColumns_ExcludeForeignKeys(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{
		{Name: "categories", Columns: []parser.Column{
			{Name: "id", Type: "bigserial", PrimaryKey: true},
			{Name: "name", Type: "text"},
		}},
		{Name: "products", Columns: []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
			{Name: "name", Type: "text"},
			// Genuine numeric columns — must keep increment support.
			{Name: "stock", Type: "integer"},
			{Name: "price", Type: "numeric"},
			// Integer FK — incrementable Go type, but an identity.
			{
				Name: "category_id", Type: "bigint",
				FKReference: &parser.FKReference{Table: "categories", Column: "id"},
			},
			// Nullable integer FK — same rule.
			{
				Name: "backup_category_id", Type: "bigint", Nullable: true,
				FKReference: &parser.FKReference{Table: "categories", Column: "id"},
			},
		}},
	}}

	in := apiTestInput(t, schema)
	tables, err := gen.BuildTableContextsFromSchema(schema, in.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}

	var products gen.TableContext
	for _, tc := range tables {
		if tc.TableName == "products" {
			products = tc
		}
	}

	got := map[string]bool{}
	for _, c := range products.IncrementColumns {
		got[c.Name] = true
	}
	for _, want := range []string{"stock", "price"} {
		if !got[want] {
			t.Errorf("IncrementColumns missing %q — genuine numeric columns must stay incrementable", want)
		}
	}
	for _, forbidden := range []string{"category_id", "backup_category_id", "id"} {
		if got[forbidden] {
			t.Errorf("IncrementColumns contains identifying column %q", forbidden)
		}
	}

	// The API surface follows: no `_inc` / `_dec` operators, and no entry in
	// the resolver-side UpdateOps list that dispatches Increment.
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	for _, tc := range apiCtx.Tables {
		if tc.SQLTable != "products" {
			continue
		}
		out := renderAPITableSchema(t, tc)
		updateBlock := inputBlock(t, out, "UpdateProductInput")
		mustNotContain(
			t, updateBlock,
			"categoryID_inc", "categoryID_dec",
			"backupCategoryID_inc", "backupCategoryID_dec",
		)
		// The FK itself is still settable — this narrows the operator, not
		// the ability to repoint the reference.
		mustContainAll(
			t, updateBlock,
			"categoryID: Int",
			"stock_inc: Int", "stock_dec: Int",
		)
		for _, op := range tc.UpdateOps {
			if op.SQLName == "category_id" || op.SQLName == "backup_category_id" {
				t.Errorf("UpdateOps still dispatches Increment for FK column %q", op.SQLName)
			}
		}
	}
}
