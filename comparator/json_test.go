package comparator_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestJSON_Parse_Contains(t *testing.T) {
	jsonVal := any(`{"key":"value"}`)
	comp := comparator.JSON{Contains: &jsonVal}

	// The `::jsonb` cast is the operator's precondition, not decoration:
	// PostgreSQL defines `@>` for jsonb alone, so the uncast form raises
	// SQLSTATE 42883 on a `json` column.
	t.Run("postgres casts to jsonb for @>", func(t *testing.T) {
		got := comp.Parse("data", pgDialect)
		want := []sql.Condition{{Clause: "data::jsonb @> $", Value: `{"key":"value"}`, Column: "data"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("JSON.Parse() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("mysql uses JSON_CONTAINS", func(t *testing.T) {
		got := comp.Parse("data", mysqlDialect)
		want := []sql.Condition{{Clause: "JSON_CONTAINS(data, $)", Value: `{"key":"value"}`, Column: "data"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("JSON.Parse() mismatch (-want +got):\n%s", diff)
		}
	})
}

// HasKey's operand does not port between dialects and the comparator does not
// translate it: PostgreSQL's `?` matches a bare key name, MySQL's
// JSON_CONTAINS_PATH wants a `$.`-prefixed path (PRD §11.2). Each subtest
// therefore spells the operand its own dialect expects, which is what pins
// the divergence rather than papering over it.
func TestJSON_Parse_HasKey(t *testing.T) {
	t.Run("postgres casts to jsonb for ? and takes a bare key", func(t *testing.T) {
		comp := comparator.JSON{HasKey: new("name")}
		got := comp.Parse("data", pgDialect)
		want := []sql.Condition{{Clause: "data::jsonb ? $", Value: "name", Column: "data"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("JSON.Parse() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("mysql uses JSON_CONTAINS_PATH and takes a JSON path", func(t *testing.T) {
		comp := comparator.JSON{HasKey: new("$.name")}
		got := comp.Parse("data", mysqlDialect)
		want := []sql.Condition{{Clause: "JSON_CONTAINS_PATH(data, 'one', $)", Value: "$.name", Column: "data"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("JSON.Parse() mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestJSON_Parse_Custom(t *testing.T) {
	custom := sql.Condition{Clause: "data->>'type' = $", Value: "product", Column: "data"}
	comp := comparator.JSON{Custom: []sql.Condition{custom}}
	got := comp.Parse("data", pgDialect)
	want := []sql.Condition{custom}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("JSON.Parse() Custom mismatch (-want +got):\n%s", diff)
	}
}

func TestJSON_Parse_Empty(t *testing.T) {
	comp := comparator.JSON{}
	got := comp.Parse("data", pgDialect)
	if got != nil {
		t.Errorf("JSON.Parse() = %v, want nil", got)
	}
}

// TestJSON_Parse_SQLiteYieldsNoDocumentConditions pins the contract the
// generator's SQLite gate is built on. JSON.Parse has `postgres` and
// `mysql` arms only, so on any other dialect the two document operators
// contribute nothing — which is why a `types.JSON` column under
// `input.dialect: sqlite` gets no filter field on either surface rather than
// one that emits no SQL. Null is not a document operator and still tests the
// column, so it survives; a hand-written filter that reaches this silence is
// the one path left, and it is documented on the type.
//
// If a `sqlite` arm is ever added, this test is the first thing to change.
func TestJSON_Parse_SQLiteYieldsNoDocumentConditions(t *testing.T) {
	jsonVal := any(`{"key":"value"}`)

	t.Run("document operators drop", func(t *testing.T) {
		comp := comparator.JSON{Contains: &jsonVal, HasKey: new("key")}
		if got := comp.Parse("data", sqliteDialect); got != nil {
			t.Errorf("JSON.Parse(sqlite) = %v, want nil", got)
		}
	})

	t.Run("Custom still passes through", func(t *testing.T) {
		custom := sql.Condition{Clause: "json_type(data, $) IS NOT NULL", Value: "$.key", Column: "data"}
		comp := comparator.JSON{Contains: &jsonVal, Custom: []sql.Condition{custom}}
		want := []sql.Condition{custom}
		if diff := cmp.Diff(want, comp.Parse("data", sqliteDialect)); diff != "" {
			t.Errorf("JSON.Parse(sqlite) Custom mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Null still tests the column", func(t *testing.T) {
		comp := comparator.NullableJSON{
			JSON: comparator.JSON{Contains: &jsonVal, HasKey: new("key")},
			Null: new(true),
		}
		got := comp.Parse("data", sqliteDialect)
		want := []sql.Condition{{Clause: "data IS NULL", Column: "data"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("NullableJSON.Parse(sqlite) mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestNullableJSON_Parse(t *testing.T) {
	t.Run("Null true", func(t *testing.T) {
		comp := comparator.NullableJSON{Null: new(true)}
		got := comp.Parse("meta", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "meta IS NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "meta IS NULL")
		}
	})

	t.Run("Null false", func(t *testing.T) {
		comp := comparator.NullableJSON{Null: new(false)}
		got := comp.Parse("meta", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "meta IS NOT NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "meta IS NOT NULL")
		}
	})

	t.Run("inherits JSON operators", func(t *testing.T) {
		jsonVal := any(`{}`)
		comp := comparator.NullableJSON{
			JSON: comparator.JSON{Contains: &jsonVal},
			Null: new(true),
		}
		got := comp.Parse("meta", pgDialect)
		if len(got) != 2 {
			t.Fatalf("got %d conditions, want 2", len(got))
		}
		if got[0].Clause != "meta::jsonb @> $" {
			t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "meta::jsonb @> $")
		}
		if got[1].Clause != "meta IS NULL" {
			t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "meta IS NULL")
		}
	})
}

// TestJSON_Parse_SurvivesAliasQualification pins both dialects' clauses against
// the O2O-join read path, which alias-qualifies every filter condition before it
// reaches the builder.
//
// PostgreSQL's arms lead with the column, so qualification only has to leave the
// cast attached to the right operand: `::` binds tighter than the operator, so
// `t.settings::jsonb @> $` still reads as `(t.settings)::jsonb`.
//
// MySQL's arms are the reason sql.Condition carries a Column at all. They lead
// with a function name, so the historical blind prepend produced
// `t.JSON_CONTAINS(settings, $)` — which MySQL resolves as a stored routine in
// schema `t`, failing with "FUNCTION t.JSON_CONTAINS does not exist".
// Qualifying the named column instead puts the alias on the argument, where it
// belongs.
func TestJSON_Parse_SurvivesAliasQualification(t *testing.T) {
	jsonVal := any(`{"key":"value"}`)

	t.Run("postgres", func(t *testing.T) {
		comp := comparator.NullableJSON{
			JSON: comparator.JSON{Contains: &jsonVal, HasKey: new("key")},
			Null: new(true),
		}
		got := sql.PrefixConditions("t", comp.Parse("settings", pgDialect))
		want := []sql.Condition{
			{Clause: `t.settings::jsonb @> $`, Value: `{"key":"value"}`, Column: "t.settings"},
			{Clause: `t.settings::jsonb ? $`, Value: "key", Column: "t.settings"},
			{Clause: `t.settings IS NULL`, Column: "t.settings"},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("PrefixConditions(NullableJSON.Parse()) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("mysql qualifies the argument, not the function", func(t *testing.T) {
		comp := comparator.NullableJSON{
			JSON: comparator.JSON{Contains: &jsonVal, HasKey: new("$.key")},
			Null: new(true),
		}
		got := sql.PrefixConditions("t", comp.Parse("settings", mysqlDialect))
		want := []sql.Condition{
			{Clause: `JSON_CONTAINS(t.settings, $)`, Value: `{"key":"value"}`, Column: "t.settings"},
			{Clause: `JSON_CONTAINS_PATH(t.settings, 'one', $)`, Value: "$.key", Column: "t.settings"},
			{Clause: `t.settings IS NULL`, Column: "t.settings"},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("PrefixConditions(NullableJSON.Parse()) mismatch (-want +got):\n%s", diff)
		}
	})
}
