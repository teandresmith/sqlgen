package tests

// Omittable null-vs-unset and zero-change Update short-circuit (MySQL).
//
// Mirror of postgres/tests/omittable_test.go adapted for MySQL syntax:
//   - MySQL identifiers are quoted with backticks (`name`), not double quotes.
//   - Placeholders are `?` (positional, not numbered).
//
// Coverage parity with postgres:
//   - Zero-field Update issues no UPDATE statement (§9.5 short-circuit) — the
//     follow-up SELECT (Get) is the only DB round-trip.
//   - Null-vs-unset on articles.body: omittable.Omit() omits the column from
//     SET; omittable.Set[*string](nil) writes SQL NULL via `body` = ?.
//   - Create with omittable.Omit() on a DEFAULT-bearing column (products.in_stock
//     NOT NULL DEFAULT TRUE): the generated INSERT excludes `in_stock`, the DB
//     applies its default, and the read-back row has InStock = true.
//   - JSON round-trip semantics (PRD §10.4) — identical contract to postgres.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// --- zero-field Update short-circuit ---

func TestUpdate_ZeroFields_NoUpdateSQL_MySQL(t *testing.T) {
	ctx := context.Background()

	cap := newCapturingQuerier(dbstdlib.New(testDB))
	client := models.New(cap)

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ZeroUpdate-14.12"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	seed, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID,
		Title:      "ZeroUpdateProduct",
		Price:      10.00,
		SKU:        "ZERO-UPDATE-001",
		Attributes: mustJSON(map[string]any{"k": "v"}),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, seed.ID) })

	cap.reset()

	got, err := client.Products().Update(ctx, seed.ID, &models.UpdateProductInput{})
	if err != nil {
		t.Fatalf("Update(empty): %v", err)
	}
	if got == nil || got.ID != seed.ID {
		t.Fatalf("Update(empty) = %+v, want product with ID %d", got, seed.ID)
	}
	if got.Title != seed.Title || got.Price != seed.Price || got.InStock != seed.InStock {
		t.Errorf("Update(empty) returned modified entity: got=%+v want=%+v", got, seed)
	}

	for _, sqlStr := range cap.snapshot() {
		if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqlStr)), "UPDATE ") {
			t.Errorf("Update(empty) issued an UPDATE statement: %q", sqlStr)
		}
	}
}

// --- null-vs-unset on nullable column ---

func TestUpdate_NullableColumn_OmitVsSetNil_MySQL(t *testing.T) {
	ctx := context.Background()

	cap := newCapturingQuerier(dbstdlib.New(testDB))
	client := models.New(cap)

	initial := "initial body"
	seed, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "Null-vs-unset",
		Author: "omittable-14.12",
		Body:   omittable.Set(&initial),
	})
	if err != nil {
		t.Fatalf("Create article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, seed.ID) })

	// 1. Update with Body Omit(): no `body` in UPDATE SET, original preserved.
	cap.reset()
	updated1, err := client.Articles().Update(ctx, seed.ID, &models.UpdateArticleInput{
		Title: omittable.Set("Null-vs-unset v2"),
	})
	if err != nil {
		t.Fatalf("Update(Body=Omit): %v", err)
	}
	if got := lastUpdateSQL(t, cap); strings.Contains(got, "`body`") {
		t.Errorf("Body=Omit included `body` in UPDATE SET: %q", got)
	}
	if updated1.Body == nil || *updated1.Body != initial {
		t.Errorf("Body=Omit: body=%v, want %q (preserved)", updated1.Body, initial)
	}

	// 2. Update with Body Set(nil): `body` present in SET, value becomes NULL.
	cap.reset()
	updated2, err := client.Articles().Update(ctx, seed.ID, &models.UpdateArticleInput{
		Body: omittable.Set[*string](nil),
	})
	if err != nil {
		t.Fatalf("Update(Body=Set(nil)): %v", err)
	}
	if got := lastUpdateSQL(t, cap); !strings.Contains(got, "`body`") {
		t.Errorf("Body=Set(nil) missing `body` in UPDATE SET: %q", got)
	}
	if updated2.Body != nil {
		t.Errorf("Body=Set(nil): body=%v, want nil (SQL NULL)", *updated2.Body)
	}

	// 3. Update with Body Set(&"replaced"): `body` present in SET, value is "replaced".
	replaced := "replaced body"
	cap.reset()
	updated3, err := client.Articles().Update(ctx, seed.ID, &models.UpdateArticleInput{
		Body: omittable.Set(&replaced),
	})
	if err != nil {
		t.Fatalf("Update(Body=Set(&v)): %v", err)
	}
	if got := lastUpdateSQL(t, cap); !strings.Contains(got, "`body`") {
		t.Errorf("Body=Set(&v) missing `body` in UPDATE SET: %q", got)
	}
	if updated3.Body == nil || *updated3.Body != replaced {
		t.Errorf("Body=Set(&v): body=%v, want %q", updated3.Body, replaced)
	}

	// 4. Update with Body Omit() again: `body` stays NOT in SET; existing "replaced" preserved.
	cap.reset()
	updated4, err := client.Articles().Update(ctx, seed.ID, &models.UpdateArticleInput{
		Author: omittable.Set("omittable-14.12-final"),
	})
	if err != nil {
		t.Fatalf("Update(Body=Omit, after replaced): %v", err)
	}
	if got := lastUpdateSQL(t, cap); strings.Contains(got, "`body`") {
		t.Errorf("Body=Omit (post-replace) included `body` in UPDATE SET: %q", got)
	}
	if updated4.Body == nil || *updated4.Body != replaced {
		t.Errorf("Body=Omit (post-replace): body=%v, want %q (preserved)", updated4.Body, replaced)
	}
}

// --- Create — Omit on DEFAULT-bearing column honors DB default ---

func TestCreate_OmitDefaultColumn_DBDefaultHonored_MySQL(t *testing.T) {
	ctx := context.Background()

	cap := newCapturingQuerier(dbstdlib.New(testDB))
	client := models.New(cap)

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "OmitDefault-14.12"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	// InStock is omitted; weight_kg/created_at also omitted.
	cap.reset()
	created, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID,
		Title:      "OmitDefaultProduct",
		Price:      9.99,
		SKU:        "OMIT-DEFAULT-001",
		Attributes: mustJSON(map[string]any{"k": "v"}),
	})
	if err != nil {
		t.Fatalf("Create(InStock=Omit): %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, created.ID) })

	insertSQL := lastInsertSQL(t, cap)
	if strings.Contains(insertSQL, "`in_stock`") {
		t.Errorf("Create(InStock=Omit) included `in_stock` in INSERT columns: %q", insertSQL)
	}

	if !created.InStock {
		t.Errorf("Create(InStock=Omit): InStock=%v, want true (DB default)", created.InStock)
	}

	// Negative control: explicitly Set(false), assert column appears in INSERT and DB
	// honors the explicit value rather than the default.
	cap.reset()
	explicit, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID,
		Title:      "ExplicitOutOfStock",
		Price:      1.00,
		SKU:        "OMIT-DEFAULT-002",
		Attributes: mustJSON(map[string]any{"k": "v"}),
		InStock:    omittable.Set(false),
	})
	if err != nil {
		t.Fatalf("Create(InStock=Set(false)): %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, explicit.ID) })

	insertSQL2 := lastInsertSQL(t, cap)
	if !strings.Contains(insertSQL2, "`in_stock`") {
		t.Errorf("Create(InStock=Set(false)) missing `in_stock` in INSERT columns: %q", insertSQL2)
	}
	if explicit.InStock {
		t.Errorf("Create(InStock=Set(false)): InStock=%v, want false (explicit)", explicit.InStock)
	}
}

// --- JSON round-trip (PRD §10.4) ---

func TestOmittable_JSONRoundTrip_MySQL(t *testing.T) {
	t.Run("Omit_marshals_absent", func(t *testing.T) {
		input := models.UpdateArticleInput{}
		got, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("Marshal(empty): %v", err)
		}
		if string(got) != `{}` {
			t.Errorf("Marshal(all Omit) = %s, want {}", got)
		}
	})

	t.Run("Set_marshals_value", func(t *testing.T) {
		input := models.UpdateArticleInput{
			Title:  omittable.Set("My Title"),
			Author: omittable.Set("Author A"),
		}
		got, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("Marshal(Set): %v", err)
		}
		s := string(got)
		if !strings.Contains(s, `"title":"My Title"`) {
			t.Errorf("Marshal(Set) missing title: %s", s)
		}
		if !strings.Contains(s, `"author":"Author A"`) {
			t.Errorf("Marshal(Set) missing author: %s", s)
		}
		for _, key := range []string{`"body"`, `"created_at"`, `"deleted_at"`} {
			if strings.Contains(s, key) {
				t.Errorf("Marshal(Set) included unset key %s: %s", key, s)
			}
		}
	})

	t.Run("SetNilPointer_marshals_null", func(t *testing.T) {
		input := models.UpdateArticleInput{
			Body: omittable.Set[*string](nil),
		}
		got, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("Marshal(Set(nil)): %v", err)
		}
		if !strings.Contains(string(got), `"body":null`) {
			t.Errorf("Marshal(Body=Set(nil)) missing \"body\":null: %s", got)
		}
	})

	t.Run("Unmarshal_absent_stays_unset", func(t *testing.T) {
		var got models.UpdateArticleInput
		if err := json.Unmarshal([]byte(`{}`), &got); err != nil {
			t.Fatalf("Unmarshal({}): %v", err)
		}
		if got.Title.IsSet() {
			t.Errorf("Title.IsSet()=true after Unmarshal({}), want false")
		}
		if got.Body.IsSet() {
			t.Errorf("Body.IsSet()=true after Unmarshal({}), want false")
		}
	})

	t.Run("Unmarshal_present_marks_set", func(t *testing.T) {
		var got models.UpdateArticleInput
		if err := json.Unmarshal([]byte(`{"title":"From JSON"}`), &got); err != nil {
			t.Fatalf("Unmarshal(title): %v", err)
		}
		if !got.Title.IsSet() {
			t.Fatalf("Title.IsSet()=false, want true")
		}
		if v := got.Title.MustGet(); v != "From JSON" {
			t.Errorf("Title.MustGet()=%q, want %q", v, "From JSON")
		}
		if got.Body.IsSet() {
			t.Errorf("Body.IsSet()=true, want false (absent in JSON)")
		}
	})

	t.Run("Unmarshal_null_pointer_sets_nil", func(t *testing.T) {
		var got models.UpdateArticleInput
		if err := json.Unmarshal([]byte(`{"body":null}`), &got); err != nil {
			t.Fatalf("Unmarshal(body=null): %v", err)
		}
		if !got.Body.IsSet() {
			t.Fatalf("Body.IsSet()=false after Unmarshal(null), want true")
		}
		if v := got.Body.MustGet(); v != nil {
			t.Errorf("Body.MustGet()=%v, want nil pointer", v)
		}
	})

	t.Run("Roundtrip_preserves_state", func(t *testing.T) {
		title := "Roundtrip"
		body := "body bytes"
		original := models.UpdateArticleInput{
			Title: omittable.Set(title),
			Body:  omittable.Set(&body),
		}
		raw, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded models.UpdateArticleInput
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !decoded.Title.IsSet() || decoded.Title.MustGet() != title {
			t.Errorf("Title roundtrip: got %v, want %q", decoded.Title, title)
		}
		if !decoded.Body.IsSet() || decoded.Body.MustGet() == nil || *decoded.Body.MustGet() != body {
			t.Errorf("Body roundtrip: got %v, want pointer to %q", decoded.Body, body)
		}
		if decoded.Author.IsSet() {
			t.Errorf("Author.IsSet()=true after roundtrip of unset field, want false")
		}
	})
}

// --- helpers ---

func lastUpdateSQL(t *testing.T, c *capturingQuerier) string {
	t.Helper()
	sqls := c.snapshot()
	for i := len(sqls) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqls[i])), "UPDATE ") {
			return sqls[i]
		}
	}
	t.Fatalf("no UPDATE captured; got %d sqls: %v", len(sqls), sqls)
	return ""
}

func lastInsertSQL(t *testing.T, c *capturingQuerier) string {
	t.Helper()
	sqls := c.snapshot()
	for i := len(sqls) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqls[i])), "INSERT ") {
			return sqls[i]
		}
	}
	t.Fatalf("no INSERT captured; got %d sqls: %v", len(sqls), sqls)
	return ""
}
