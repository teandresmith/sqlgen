package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// `json[]` and `jsonb[]` columns must not appear in the generated
// filter struct because their element type (`types.JSON` /
// `map[string]any`) violates the `T comparable` constraint required by
// `comparator.Slice[T]`. The model side marks them Filterable=false and the
// model + GraphQL templates skip them.

func TestResolveComparator_jsonbArrayIsNotFilterable(t *testing.T) {
	cases := []struct {
		name string
		col  gen.ColumnContext
	}{
		{
			name: "jsonb[] resolves to []types.JSON (Postgres default)",
			col: gen.ColumnContext{
				Name:     "tags",
				GoType:   "[]types.JSON",
				SQLType:  "jsonb",
				IsSlice:  true,
				Nullable: false,
			},
		},
		{
			name: "json[] resolves to []types.JSON",
			col: gen.ColumnContext{
				Name:     "payloads",
				GoType:   "[]types.JSON",
				SQLType:  "json",
				IsSlice:  true,
				Nullable: false,
			},
		},
		{
			name: "bare []map[string]any (no override)",
			col: gen.ColumnContext{
				Name:    "raw_blobs",
				GoType:  "[]map[string]any",
				SQLType: "jsonb",
				IsSlice: true,
			},
		},
		{
			name: "nullable jsonb[]",
			col: gen.ColumnContext{
				Name:     "tags",
				GoType:   "[]types.JSON",
				SQLType:  "jsonb",
				IsSlice:  true,
				Nullable: true,
			},
		},
	}

	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	resolve := fm["comparatorType"].(func(gen.ColumnContext) string)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolve(tc.col)
			// `resolveGenericComparator` returns "" for non-comparable slice
			// elements; the outer fallback hits resolveSimpleComparator which
			// routes unknown Go types to the String comparator. The point of
			// this assertion is to forbid the broken Slice form regardless of
			// what fallback the compiler picks — `comparator.Slice[types.JSON]`
			// would not compile because types.JSON underlies json.RawMessage
			// = []byte (a slice, not comparable).
			forbidden := []string{
				"comparator.Slice[types.JSON]",
				"comparator.Slice[map[string]any]",
				"comparator.NullableSlice[types.JSON]",
				"comparator.NullableSlice[map[string]any]",
			}
			for _, f := range forbidden {
				if strings.Contains(got, f) {
					t.Fatalf("resolveComparatorType emitted forbidden form %q for json-array column: %q", f, got)
				}
			}
		})
	}
}

func TestResolveComparator_regularSliceStillFilterable(t *testing.T) {
	// Sanity check: non-JSON slice columns continue to emit a Slice
	// comparator. Only json[]/jsonb[] should be dropped.
	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	resolve := fm["comparatorType"].(func(gen.ColumnContext) string)

	cases := []struct {
		name string
		col  gen.ColumnContext
		want string
	}{
		{
			name: "text[]",
			col:  gen.ColumnContext{GoType: "[]string", SQLType: "text", IsSlice: true},
			want: "*comparator.Slice[string]",
		},
		{
			name: "int4[]",
			col:  gen.ColumnContext{GoType: "[]int32", SQLType: "integer", IsSlice: true},
			want: "*comparator.Slice[int32]",
		},
		{
			name: "nullable uuid[]",
			col:  gen.ColumnContext{GoType: "[]uuid.UUID", SQLType: "uuid", IsSlice: true, Nullable: true},
			want: "*comparator.NullableSlice[uuid.UUID]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolve(tc.col); got != tc.want {
				t.Errorf("comparatorType = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildContext_jsonbArrayMarkedNonFilterable(t *testing.T) {
	// End-to-end through BuildTableContexts: a Postgres jsonb[] column must
	// land in TableContext.FilterFields with Filterable=false.
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "documents",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "title", Type: "text"},
					{Name: "tags", Type: "jsonb[]"},
				},
			},
		},
	}

	input := testInput(schema)
	input.Config.Input.Dialect = config.DialectPostgres

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("want 1 table context, got %d", len(contexts))
	}
	tc := contexts[0]

	var tagsField *gen.FilterFieldContext
	for i, ff := range tc.FilterFields {
		if ff.ColumnName == "tags" {
			tagsField = &tc.FilterFields[i]
			break
		}
	}
	if tagsField == nil {
		t.Fatal("FilterFields missing entry for tags")
	}
	if tagsField.Filterable {
		t.Errorf("tags FilterFieldContext.Filterable = true, want false (jsonb[] cannot satisfy comparator T)")
	}
	if tagsField.ComparatorType != "" {
		t.Errorf("tags FilterFieldContext.ComparatorType = %q, want empty (non-filterable)", tagsField.ComparatorType)
	}

	// Companion columns remain filterable.
	for _, ff := range tc.FilterFields {
		if ff.ColumnName == "tags" {
			continue
		}
		if !ff.Filterable {
			t.Errorf("column %q marked non-filterable, want filterable", ff.ColumnName)
		}
		if ff.ComparatorType == "" {
			t.Errorf("column %q has empty ComparatorType, want non-empty", ff.ColumnName)
		}
	}
}

func TestFilterTemplate_skipsNonFilterableField(t *testing.T) {
	// Render `_filter.tmpl` directly with a non-filterable entry; the
	// generated struct must omit the field entirely (no field, no
	// ToConditions nil-check) so the output compiles.
	ctx := gen.TableContext{
		StructName:        "Document",
		TableName:         "documents",
		TableNameConstant: "TableDocuments",
		Schema:            "public",
		Package:           "db",
		Dialect:           "postgres",
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "ID", ComparatorType: "*comparator.ID", ColumnName: "id", Filterable: true},
			{FieldName: "Tags", ComparatorType: "", ColumnName: "tags", Filterable: false},
			{FieldName: "Title", ComparatorType: "*comparator.String", ColumnName: "title", Filterable: true},
		},
	}
	output := executeFilterTemplate(t, ctx)

	wantContains := []string{
		"type DocumentFilter struct {",
		"ID *comparator.ID",
		"Title *comparator.String",
		"if f.ID != nil {",
		"if f.Title != nil {",
	}
	for _, want := range wantContains {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	forbidden := []string{
		"Tags ",                    // no struct field
		`json:"tags"`,              // no struct tag
		"f.Tags",                   // no ToConditions reference
		`comparator.Slice`,         // no Slice comparator emitted at all
		`comparator.NullableSlice`, // no NullableSlice comparator
	}
	for _, bad := range forbidden {
		if strings.Contains(output, bad) {
			t.Errorf("output contains forbidden text %q\n\nfull output:\n%s", bad, output)
		}
	}

	// The full rendered fragment, wrapped, must format cleanly.
	wrapped := wrapFilterForCompile(ctx, output)
	if _, err := gen.FormatOnly([]byte(wrapped), "v0.0.0-test", "filter_documents_gen.go"); err != nil {
		t.Fatalf("FormatOnly: %v\n\nrendered:\n%s", err, wrapped)
	}
}
