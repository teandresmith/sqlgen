package parser_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
)

func TestParseViewAnnotations_Basic(t *testing.T) {
	content := `-- @pk: id
-- @type avg_rating: float64
-- @nullable: discount

CREATE VIEW product_summary AS
SELECT ...`

	annotations, err := parser.ParseViewAnnotations(content)
	if err != nil {
		t.Fatalf("ParseViewAnnotations() error: %v", err)
	}

	if diff := cmp.Diff([]string{"id"}, annotations.PKColumns); diff != "" {
		t.Errorf("PKColumns mismatch (-want +got):\n%s", diff)
	}

	ta, ok := annotations.TypeOverrides["avg_rating"]
	if !ok {
		t.Fatal("missing type override for 'avg_rating'")
	}
	if ta.GoType != "float64" {
		t.Errorf("avg_rating GoType = %q, want %q", ta.GoType, "float64")
	}

	if diff := cmp.Diff([]string{"discount"}, annotations.NullableColumns); diff != "" {
		t.Errorf("NullableColumns mismatch (-want +got):\n%s", diff)
	}
}

func TestParseViewAnnotations_TypeWithImport(t *testing.T) {
	content := `-- @type ext_id: uuid.UUID github.com/google/uuid

CREATE VIEW ...`

	annotations, err := parser.ParseViewAnnotations(content)
	if err != nil {
		t.Fatalf("ParseViewAnnotations() error: %v", err)
	}

	ta := annotations.TypeOverrides["ext_id"]
	if ta.GoType != "uuid.UUID" {
		t.Errorf("GoType = %q, want %q", ta.GoType, "uuid.UUID")
	}
	if ta.ImportPath != "github.com/google/uuid" {
		t.Errorf("ImportPath = %q, want %q", ta.ImportPath, "github.com/google/uuid")
	}
}

func TestParseViewAnnotations_ExternalTypeRequiresImport(t *testing.T) {
	content := `-- @type id: uuid.UUID

CREATE VIEW ...`

	_, err := parser.ParseViewAnnotations(content)
	if err == nil {
		t.Fatal("expected error for external type without import, got nil")
	}
}

func TestParseViewAnnotations_DuplicatePK(t *testing.T) {
	content := `-- @pk: id
-- @pk: name

CREATE VIEW ...`

	_, err := parser.ParseViewAnnotations(content)
	if err == nil {
		t.Fatal("expected error for duplicate @pk, got nil")
	}
}

func TestParseViewAnnotations_DuplicateType(t *testing.T) {
	content := `-- @type id: int64
-- @type id: string

CREATE VIEW ...`

	_, err := parser.ParseViewAnnotations(content)
	if err == nil {
		t.Fatal("expected error for duplicate @type, got nil")
	}
}

func TestBuildView_TypeResolution(t *testing.T) {
	tables := []parser.Table{
		{
			Name: "products",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid"},
				{Name: "name", Type: "text"},
				{Name: "discount", Type: "numeric(10,2)"},
			},
		},
		{
			Name: "reviews",
			Columns: []parser.Column{
				{Name: "id", Type: "bigint"},
				{Name: "rating", Type: "integer"},
			},
		},
	}

	columns := []parser.ViewSelectColumn{
		{Alias: "id", SourceTable: "p", SourceColumn: "id"},
		{Alias: "name", SourceTable: "p", SourceColumn: "name"},
		{Alias: "discount", SourceTable: "p", SourceColumn: "discount"},
		{Alias: "avg_rating", Aggregate: "AVG", SourceTable: "r", SourceColumn: "rating"},
		{Alias: "review_count", Aggregate: "COUNT", SourceTable: "r", SourceColumn: "id"},
	}
	aliases := map[string]string{"p": "products", "r": "reviews"}

	annotations := &parser.ViewAnnotations{
		PKColumns:       []string{"id"},
		TypeOverrides:   map[string]parser.ViewTypeAnnotation{"avg_rating": {GoType: "float64"}},
		NullableColumns: []string{"discount"},
	}

	parsed := parser.ViewSQL{
		Name: "product_summary", Columns: columns, TableAliases: aliases,
	}
	view, err := parser.BuildView(parsed, "CREATE VIEW ...", annotations, tables)
	if err != nil {
		t.Fatalf("BuildView() error: %v", err)
	}

	if view.Name != "product_summary" {
		t.Errorf("Name = %q, want %q", view.Name, "product_summary")
	}

	want := []struct {
		name       string
		typ        string
		nullable   bool
		primaryKey bool
	}{
		{"id", "uuid", false, true},
		{"name", "text", false, false},
		{"discount", "numeric(10,2)", true, false},
		{"avg_rating", "float64", false, false}, // @type override, not nullable
		{"review_count", "int64", false, false}, // COUNT aggregate
	}

	if len(view.Columns) != len(want) {
		t.Fatalf("columns = %d, want %d", len(view.Columns), len(want))
	}
	for i, w := range want {
		got := view.Columns[i]
		if got.Name != w.name {
			t.Errorf("column[%d].Name = %q, want %q", i, got.Name, w.name)
		}
		if got.Type != w.typ {
			t.Errorf("column[%d].Type = %q, want %q", i, got.Type, w.typ)
		}
		if got.Nullable != w.nullable {
			t.Errorf("column[%d].Nullable = %v, want %v", i, got.Nullable, w.nullable)
		}
		if got.PrimaryKey != w.primaryKey {
			t.Errorf("column[%d].PrimaryKey = %v, want %v", i, got.PrimaryKey, w.primaryKey)
		}
	}
}

func TestBuildView_AggregateInference(t *testing.T) {
	tests := []struct {
		name     string
		col      parser.ViewSelectColumn
		baseCol  *parser.Column // if non-nil, added to a table for schema matching
		wantType string
		wantNull bool
	}{
		{"COUNT", parser.ViewSelectColumn{Alias: "cnt", Aggregate: "COUNT"}, nil, "int64", false},
		{"AVG", parser.ViewSelectColumn{Alias: "avg_val", Aggregate: "AVG"}, nil, "*float64", true},
		{"SUM with base", parser.ViewSelectColumn{Alias: "total", Aggregate: "SUM", SourceTable: "o", SourceColumn: "amount"}, &parser.Column{Name: "amount", Type: "numeric(10,2)"}, "numeric(10,2)", true},
		{"MIN with base", parser.ViewSelectColumn{Alias: "min_price", Aggregate: "MIN", SourceTable: "o", SourceColumn: "price"}, &parser.Column{Name: "price", Type: "numeric(10,2)"}, "numeric(10,2)", true},
		{"MAX with base", parser.ViewSelectColumn{Alias: "max_price", Aggregate: "MAX", SourceTable: "o", SourceColumn: "price"}, &parser.Column{Name: "price", Type: "numeric(10,2)"}, "numeric(10,2)", true},
		{"STRING_AGG", parser.ViewSelectColumn{Alias: "names", Aggregate: "STRING_AGG"}, nil, "*string", true},
		{"GROUP_CONCAT", parser.ViewSelectColumn{Alias: "names", Aggregate: "GROUP_CONCAT"}, nil, "*string", true},
		// ARRAY_AGG is nullable on both arms for the reason every other
		// empty-set aggregate is: no input rows — or a FILTER that empties the
		// group — yields SQL NULL, not an empty array (PRD §16.3, "nullable (a
		// nil slice conveys SQL NULL)"). Unlike AVG/MIN/MAX the flag does not
		// pointer-wrap the Go type; a slice already carries NULL as nil. What
		// it drives is the nullable comparator (NullableSlice) and the
		// manifest.
		{"ARRAY_AGG", parser.ViewSelectColumn{Alias: "tags", Aggregate: "ARRAY_AGG"}, nil, "[]any", true},
		{"ARRAY_AGG with base", parser.ViewSelectColumn{Alias: "tags", Aggregate: "ARRAY_AGG", SourceTable: "o", SourceColumn: "tag"}, &parser.Column{Name: "tag", Type: "text"}, "text[]", true},
		{"BOOL_AND", parser.ViewSelectColumn{Alias: "all_active", Aggregate: "BOOL_AND"}, nil, "*bool", true},
		{"EVERY", parser.ViewSelectColumn{Alias: "all_verified", Aggregate: "EVERY"}, nil, "*bool", true},
		{"BOOL_OR", parser.ViewSelectColumn{Alias: "any_premium", Aggregate: "BOOL_OR"}, nil, "*bool", true},
		{"JSON_AGG", parser.ViewSelectColumn{Alias: "all_data", Aggregate: "JSON_AGG"}, nil, "types.JSON", false},
		{"JSONB_AGG", parser.ViewSelectColumn{Alias: "all_meta", Aggregate: "JSONB_AGG"}, nil, "types.JSON", false},
		{"JSON_ARRAYAGG", parser.ViewSelectColumn{Alias: "all_info", Aggregate: "JSON_ARRAYAGG"}, nil, "types.JSON", false},
		{"JSON_OBJECT_AGG", parser.ViewSelectColumn{Alias: "kv", Aggregate: "JSON_OBJECT_AGG"}, nil, "types.JSON", false},
		{"JSONB_OBJECT_AGG", parser.ViewSelectColumn{Alias: "kv", Aggregate: "JSONB_OBJECT_AGG"}, nil, "types.JSON", false},
		{"JSON_OBJECTAGG", parser.ViewSelectColumn{Alias: "kv", Aggregate: "JSON_OBJECTAGG"}, nil, "types.JSON", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tables []parser.Table
			aliases := map[string]string{"o": "orders"}
			if tt.baseCol != nil {
				tables = append(tables, parser.Table{
					Name:    "orders",
					Columns: []parser.Column{*tt.baseCol},
				})
			}

			annotations := &parser.ViewAnnotations{TypeOverrides: map[string]parser.ViewTypeAnnotation{}}
			parsed := parser.ViewSQL{
				Name: "test", Columns: []parser.ViewSelectColumn{tt.col}, TableAliases: aliases,
			}
			view, err := parser.BuildView(parsed, "", annotations, tables)
			if err != nil {
				t.Fatalf("BuildView() error: %v", err)
			}

			col := view.Columns[0]
			if col.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", col.Type, tt.wantType)
			}
			if col.Nullable != tt.wantNull {
				t.Errorf("Nullable = %v, want %v", col.Nullable, tt.wantNull)
			}
		})
	}
}

// TestBuildView_ArrayAggRouting pins which of the two type paths an ARRAY_AGG
// column takes. A resolvable element type must leave GoTypeLiteral empty and
// record the aggregate, so codegen resolves the array SQL type through the
// SQL-to-Go resolver — the only path that marks the result a slice and so the
// only one that earns the driver's array scan handling. Freezing it as a Go
// literal instead compiles and then fails to scan at runtime.
func TestBuildView_ArrayAggRouting(t *testing.T) {
	tests := []struct {
		name              string
		col               parser.ViewSelectColumn
		baseCol           *parser.Column
		wantType          string
		wantGoTypeLiteral string
		wantAggregate     string
	}{
		{
			name:              "resolvable element type goes through the resolver",
			col:               parser.ViewSelectColumn{Alias: "tags", Aggregate: "ARRAY_AGG", SourceTable: "o", SourceColumn: "tag"},
			baseCol:           &parser.Column{Name: "tag", Type: "text"},
			wantType:          "text[]",
			wantGoTypeLiteral: "",
			wantAggregate:     "ARRAY_AGG",
		},
		{
			name:              "unresolvable element type stays a frozen literal",
			col:               parser.ViewSelectColumn{Alias: "tags", Aggregate: "ARRAY_AGG"},
			baseCol:           nil,
			wantType:          "[]any",
			wantGoTypeLiteral: "[]any",
			wantAggregate:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tables []parser.Table
			if tt.baseCol != nil {
				tables = append(tables, parser.Table{
					Name:    "orders",
					Columns: []parser.Column{*tt.baseCol},
				})
			}

			annotations := &parser.ViewAnnotations{TypeOverrides: map[string]parser.ViewTypeAnnotation{}}
			parsed := parser.ViewSQL{
				Name:         "test",
				Columns:      []parser.ViewSelectColumn{tt.col},
				TableAliases: map[string]string{"o": "orders"},
			}

			view, err := parser.BuildView(parsed, "", annotations, tables)
			if err != nil {
				t.Fatalf("BuildView() error: %v", err)
			}

			col := view.Columns[0]
			if col.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", col.Type, tt.wantType)
			}
			if col.GoTypeLiteral != tt.wantGoTypeLiteral {
				t.Errorf("GoTypeLiteral = %q, want %q", col.GoTypeLiteral, tt.wantGoTypeLiteral)
			}
			if col.Aggregate != tt.wantAggregate {
				t.Errorf("Aggregate = %q, want %q", col.Aggregate, tt.wantAggregate)
			}
		})
	}
}

func TestBuildView_FallbackToAny(t *testing.T) {
	col := parser.ViewSelectColumn{Alias: "computed"}
	annotations := &parser.ViewAnnotations{TypeOverrides: map[string]parser.ViewTypeAnnotation{}}
	parsed := parser.ViewSQL{Name: "test", Columns: []parser.ViewSelectColumn{col}}

	view, err := parser.BuildView(parsed, "", annotations, nil)
	if err != nil {
		t.Fatalf("BuildView() error: %v", err)
	}
	if view.Columns[0].Type != "any" {
		t.Errorf("Type = %q, want %q", view.Columns[0].Type, "any")
	}
}

func TestBuildView_UnknownColumnInAnnotation(t *testing.T) {
	col := parser.ViewSelectColumn{Alias: "id", SourceColumn: "id"}
	annotations := &parser.ViewAnnotations{
		PKColumns:     []string{"nonexistent"},
		TypeOverrides: map[string]parser.ViewTypeAnnotation{},
	}
	parsed := parser.ViewSQL{Name: "test", Columns: []parser.ViewSelectColumn{col}}

	_, err := parser.BuildView(parsed, "", annotations, nil)
	if err == nil {
		t.Fatal("expected error for @pk referencing unknown column, got nil")
	}
}

func TestMergeViewAnnotations_AnnotationWins(t *testing.T) {
	introspected := []parser.View{
		{
			Name:   "product_summary",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "integer", Nullable: true},
				{Name: "total", Type: "numeric", Nullable: true},
			},
		},
	}

	annotated := []parser.View{
		{
			Name:   "product_summary",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid"},
				{Name: "total", Type: "decimal.Decimal"},
			},
		},
	}

	annotationMap := map[string]*parser.ViewAnnotations{
		"public.product_summary": {
			PKColumns: []string{"id"},
			TypeOverrides: map[string]parser.ViewTypeAnnotation{
				"total": {GoType: "decimal.Decimal", ImportPath: "github.com/shopspring/decimal"},
			},
			NullableColumns: []string{"total"},
		},
	}

	result := parser.MergeViewAnnotations(introspected, annotated, annotationMap)

	if len(result) != 1 {
		t.Fatalf("merged views = %d, want 1", len(result))
	}

	view := result[0]
	if !view.Columns[0].PrimaryKey {
		t.Error("id PrimaryKey = false, want true")
	}
	if view.Columns[0].Nullable {
		t.Error("id Nullable = true, want false (PK)")
	}
	if view.Columns[1].Type != "decimal.Decimal" {
		t.Errorf("total Type = %q, want %q", view.Columns[1].Type, "decimal.Decimal")
	}
	if !view.Columns[1].Nullable {
		t.Error("total Nullable = false, want true")
	}
}

func TestMergeViewAnnotations_AnnotationOnlyView(t *testing.T) {
	annotated := []parser.View{
		{Name: "my_view", Columns: []parser.Column{{Name: "x", Type: "text"}}},
	}

	result := parser.MergeViewAnnotations(nil, annotated, nil)
	if len(result) != 1 {
		t.Fatalf("merged views = %d, want 1", len(result))
	}
	if result[0].Name != "my_view" {
		t.Errorf("Name = %q, want %q", result[0].Name, "my_view")
	}
}

func TestBuildView_Materialized(t *testing.T) {
	parsed := parser.ViewSQL{
		Name:   "order_totals",
		Schema: "reporting",
		Columns: []parser.ViewSelectColumn{
			{Alias: "customer_id", SourceTable: "o", SourceColumn: "customer_id"},
			{Alias: "total", SourceTable: "o", SourceColumn: "amount", Aggregate: "SUM"},
		},
		TableAliases: map[string]string{"o": "orders"},
		Materialized: true,
	}
	tables := []parser.Table{
		{
			Name: "orders",
			Columns: []parser.Column{
				{Name: "customer_id", Type: "bigint"},
				{Name: "amount", Type: "numeric"},
			},
		},
	}

	tests := []struct {
		name             string
		annotations      *parser.ViewAnnotations
		wantPKCols       []string
		wantConcurrently bool
	}{
		{
			name: "@pk present marks PK and enables concurrent refresh",
			annotations: &parser.ViewAnnotations{
				PKColumns:     []string{"customer_id"},
				TypeOverrides: map[string]parser.ViewTypeAnnotation{},
			},
			wantPKCols:       []string{"customer_id"},
			wantConcurrently: true,
		},
		{
			name: "@pk absent means no PK and no concurrent refresh",
			annotations: &parser.ViewAnnotations{
				TypeOverrides: map[string]parser.ViewTypeAnnotation{},
			},
			wantConcurrently: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view, err := parser.BuildView(parsed, "raw sql", tt.annotations, tables)
			if err != nil {
				t.Fatalf("BuildView() error: %v", err)
			}

			if !view.Materialized {
				t.Error("Materialized = false, want true")
			}
			if view.ConcurrentlyRefreshable != tt.wantConcurrently {
				t.Errorf("ConcurrentlyRefreshable = %v, want %v", view.ConcurrentlyRefreshable, tt.wantConcurrently)
			}

			var pkCols []string
			for _, col := range view.Columns {
				if col.PrimaryKey {
					pkCols = append(pkCols, col.Name)
					if col.Nullable {
						t.Errorf("PK column %s is nullable, want NOT NULL", col.Name)
					}
				}
			}
			if diff := cmp.Diff(tt.wantPKCols, pkCols); diff != "" {
				t.Errorf("PK columns mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildView_RegularViewNeverConcurrentlyRefreshable(t *testing.T) {
	parsed := parser.ViewSQL{
		Name: "plain",
		Columns: []parser.ViewSelectColumn{
			{Alias: "id", SourceColumn: "id"},
		},
		TableAliases: map[string]string{},
	}
	annotations := &parser.ViewAnnotations{
		PKColumns:     []string{"id"},
		TypeOverrides: map[string]parser.ViewTypeAnnotation{},
	}

	view, err := parser.BuildView(parsed, "raw sql", annotations, nil)
	if err != nil {
		t.Fatalf("BuildView() error: %v", err)
	}
	if view.Materialized {
		t.Error("Materialized = true, want false")
	}
	if view.ConcurrentlyRefreshable {
		t.Error("ConcurrentlyRefreshable = true for a regular view with @pk, want false (invariant: !Materialized => !ConcurrentlyRefreshable)")
	}
}

func TestMergeViewAnnotations_MatviewPKOverride(t *testing.T) {
	// Introspected matview with a discovered PK on customer_id; the annotation
	// overlay's @pk on email must REPLACE the discovered PK, not union with it.
	introspected := []parser.View{
		{
			Name:         "order_totals",
			Schema:       "public",
			Materialized: true,
			// No qualifying unique index discovered on email — the user's @pk
			// assertion flips ConcurrentlyRefreshable.
			ConcurrentlyRefreshable: false,
			Columns: []parser.Column{
				{Name: "customer_id", Type: "bigint", PrimaryKey: true},
				{Name: "email", Type: "text", Nullable: true},
			},
		},
	}
	annotated := []parser.View{
		{
			Name:         "order_totals",
			Schema:       "public",
			Materialized: true,
			Columns: []parser.Column{
				{Name: "customer_id", Type: "bigint"},
				{Name: "email", Type: "text"},
			},
		},
	}
	annotationMap := map[string]*parser.ViewAnnotations{
		"public.order_totals": {
			PKColumns:     []string{"email"},
			TypeOverrides: map[string]parser.ViewTypeAnnotation{},
		},
	}

	result := parser.MergeViewAnnotations(introspected, annotated, annotationMap)
	if len(result) != 1 {
		t.Fatalf("merged views = %d, want 1", len(result))
	}

	view := result[0]
	if !view.Materialized {
		t.Error("Materialized = false, want true (must survive the merge)")
	}
	if !view.ConcurrentlyRefreshable {
		t.Error("ConcurrentlyRefreshable = false, want true (@pk asserts a unique key)")
	}
	if view.Columns[0].PrimaryKey {
		t.Error("customer_id PrimaryKey = true, want false (@pk overrides the discovered PK)")
	}
	if !view.Columns[1].PrimaryKey {
		t.Error("email PrimaryKey = false, want true (@pk column)")
	}
	if view.Columns[1].Nullable {
		t.Error("email Nullable = true, want false (PK)")
	}
}

func TestMergeViewAnnotations_MatviewNoPKOverlayKeepsDiscovered(t *testing.T) {
	// An overlay without @pk (e.g. only @type/@nullable) must not disturb the
	// discovered PK or the discovered concurrent-refresh capability.
	introspected := []parser.View{
		{
			Name:                    "order_totals",
			Schema:                  "public",
			Materialized:            true,
			ConcurrentlyRefreshable: true,
			Columns: []parser.Column{
				{Name: "customer_id", Type: "bigint", PrimaryKey: true},
				{Name: "total", Type: "numeric", Nullable: true},
			},
		},
	}
	annotationMap := map[string]*parser.ViewAnnotations{
		"public.order_totals": {
			TypeOverrides: map[string]parser.ViewTypeAnnotation{
				"total": {GoType: "decimal.Decimal", ImportPath: "github.com/shopspring/decimal"},
			},
		},
	}
	annotated := []parser.View{
		{Name: "order_totals", Schema: "public", Materialized: true},
	}

	result := parser.MergeViewAnnotations(introspected, annotated, annotationMap)
	if len(result) != 1 {
		t.Fatalf("merged views = %d, want 1", len(result))
	}

	view := result[0]
	if !view.Columns[0].PrimaryKey {
		t.Error("customer_id PrimaryKey = false, want true (discovered PK preserved)")
	}
	if !view.ConcurrentlyRefreshable {
		t.Error("ConcurrentlyRefreshable = false, want true (discovered capability preserved)")
	}
	if view.Columns[1].Type != "decimal.Decimal" {
		t.Errorf("total Type = %q, want %q", view.Columns[1].Type, "decimal.Decimal")
	}
}
