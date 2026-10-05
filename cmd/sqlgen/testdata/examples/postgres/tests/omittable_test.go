package tests

// Omittable null-vs-unset and zero-change Update short-circuit (PostgreSQL).
//
// Coverage:
//   - Zero-field Update: an UpdateInput with every field unset issues no UPDATE
//     statement (the §9.5 "Empty Update" short-circuit). Verified by wrapping
//     database.Querier with capturingQuerier and asserting no captured SQL
//     starts with "UPDATE". The method still loads and returns the current
//     entity unchanged via a follow-up Get/SELECT — that single SELECT is the
//     only DB round-trip in the captured stream.
//   - Null-vs-unset on a nullable column (articles.body, TEXT, no DEFAULT):
//     omittable.Omit() omits the column from the SET clause entirely (existing
//     value preserved); omittable.Set[*string](nil) writes SQL NULL via the
//     "body" = $N placeholder. Asserted both at the SQL string level (column
//     present/absent in captured UPDATE) and at the read-back level (post-Update
//     row state).
//   - Create with omittable.Omit() on a DEFAULT-bearing column (products.is_active
//     NOT NULL DEFAULT true): the generated INSERT excludes "is_active" from
//     the column list, the DB applies its default, and the read-back row has
//     is_active = true. The negative control sets IsActive explicitly and
//     asserts both column inclusion and the read-back value.
//   - JSON round-trip: omittable.Omit marshals to absent (via omitzero); Set(v)
//     marshals to v; Set[*T](nil) marshals to null; absent JSON unmarshals to
//     unset; "null" unmarshals to Set[*T](nil) for pointer T (PRD §10.4).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// --- zero-field Update short-circuit ---

func TestUpdate_ZeroFields_NoUpdateSQL_Postgres(t *testing.T) {
	ctx := context.Background()

	cap := newCapturingQuerier(dbpgx.New(testPool))
	client := models.New(cap)

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ZeroUpdate-14.12"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	seed, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "ZeroUpdateProduct",
		Price:      10.00,
		CategoryID: cat.ID,
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
		t.Fatalf("Update(empty) = %+v, want product with ID %s", got, seed.ID)
	}
	if got.Name != seed.Name || got.Price != seed.Price || got.IsActive != seed.IsActive {
		t.Errorf("Update(empty) returned modified entity: got=%+v want=%+v", got, seed)
	}

	for _, sqlStr := range cap.snapshot() {
		if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqlStr)), "UPDATE ") {
			t.Errorf("Update(empty) issued an UPDATE statement: %q", sqlStr)
		}
	}
}

// --- null-vs-unset on nullable column ---

func TestUpdate_NullableColumn_OmitVsSetNil_Postgres(t *testing.T) {
	ctx := context.Background()

	cap := newCapturingQuerier(dbpgx.New(testPool))
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

	// 1. Update with Body Omit(): no "body" in UPDATE SET, original value preserved.
	cap.reset()
	updated1, err := client.Articles().Update(ctx, seed.ID, &models.UpdateArticleInput{
		Title: omittable.Set("Null-vs-unset v2"),
	})
	if err != nil {
		t.Fatalf("Update(Body=Omit): %v", err)
	}
	if got := updateSQL(t, cap); strings.Contains(got, `"body"`) {
		t.Errorf("Body=Omit included \"body\" in UPDATE SET: %q", got)
	}
	if updated1.Body == nil || *updated1.Body != initial {
		t.Errorf("Body=Omit: body=%v, want %q (preserved)", updated1.Body, initial)
	}

	// 2. Update with Body Set(nil): "body" present in UPDATE SET, value becomes NULL.
	cap.reset()
	updated2, err := client.Articles().Update(ctx, seed.ID, &models.UpdateArticleInput{
		Body: omittable.Set[*string](nil),
	})
	if err != nil {
		t.Fatalf("Update(Body=Set(nil)): %v", err)
	}
	if got := updateSQL(t, cap); !strings.Contains(got, `"body"`) {
		t.Errorf("Body=Set(nil) missing \"body\" in UPDATE SET: %q", got)
	}
	if updated2.Body != nil {
		t.Errorf("Body=Set(nil): body=%v, want nil (SQL NULL)", *updated2.Body)
	}

	// 3. Update with Body Set(&"replaced"): "body" present in UPDATE SET, value is "replaced".
	replaced := "replaced body"
	cap.reset()
	updated3, err := client.Articles().Update(ctx, seed.ID, &models.UpdateArticleInput{
		Body: omittable.Set(&replaced),
	})
	if err != nil {
		t.Fatalf("Update(Body=Set(&v)): %v", err)
	}
	if got := updateSQL(t, cap); !strings.Contains(got, `"body"`) {
		t.Errorf("Body=Set(&v) missing \"body\" in UPDATE SET: %q", got)
	}
	if updated3.Body == nil || *updated3.Body != replaced {
		t.Errorf("Body=Set(&v): body=%v, want %q", updated3.Body, replaced)
	}

	// 4. Update with Body Omit() again: "body" stays NOT in SET; existing "replaced" preserved.
	cap.reset()
	updated4, err := client.Articles().Update(ctx, seed.ID, &models.UpdateArticleInput{
		Author: omittable.Set("omittable-14.12-final"),
	})
	if err != nil {
		t.Fatalf("Update(Body=Omit, after replaced): %v", err)
	}
	if got := updateSQL(t, cap); strings.Contains(got, `"body"`) {
		t.Errorf("Body=Omit (post-replace) included \"body\" in UPDATE SET: %q", got)
	}
	if updated4.Body == nil || *updated4.Body != replaced {
		t.Errorf("Body=Omit (post-replace): body=%v, want %q (preserved)", updated4.Body, replaced)
	}
}

// --- Create — Omit on DEFAULT-bearing column honors DB default ---

func TestCreate_OmitDefaultColumn_DBDefaultHonored_Postgres(t *testing.T) {
	ctx := context.Background()

	cap := newCapturingQuerier(dbpgx.New(testPool))
	client := models.New(cap)

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "OmitDefault-14.12"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	// IsActive is omitted; price/quantity/is_active/tags/metadata/created_at/updated_at all omitted.
	cap.reset()
	created, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "OmitDefaultProduct",
		Price:      9.99,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create(IsActive=Omit): %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, created.ID) })

	insertSQL := lastInsert(t, cap)
	if strings.Contains(insertSQL, `"is_active"`) {
		t.Errorf("Create(IsActive=Omit) included \"is_active\" in INSERT columns: %q", insertSQL)
	}
	if strings.Contains(insertSQL, `"quantity"`) {
		t.Errorf("Create(Quantity=Omit) included \"quantity\" in INSERT columns: %q", insertSQL)
	}

	// DB DEFAULT honored.
	if !created.IsActive {
		t.Errorf("Create(IsActive=Omit): IsActive=%v, want true (DB default)", created.IsActive)
	}
	if created.Quantity != 1 {
		t.Errorf("Create(Quantity=Omit): Quantity=%d, want 1 (DB default)", created.Quantity)
	}

	// Negative control — explicitly Set(false), assert column appears in INSERT and DB
	// honors the explicit value rather than the default.
	cap.reset()
	explicit, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "ExplicitInactiveProduct",
		Price:      1.00,
		CategoryID: cat.ID,
		IsActive:   omittable.Set(false),
	})
	if err != nil {
		t.Fatalf("Create(IsActive=Set(false)): %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, explicit.ID) })

	insertSQL2 := lastInsert(t, cap)
	if !strings.Contains(insertSQL2, `"is_active"`) {
		t.Errorf("Create(IsActive=Set(false)) missing \"is_active\" in INSERT columns: %q", insertSQL2)
	}
	if explicit.IsActive {
		t.Errorf("Create(IsActive=Set(false)): IsActive=%v, want false (explicit)", explicit.IsActive)
	}
}

// --- JSON round-trip (PRD §10.4) ---

func TestOmittable_JSONRoundTrip_Postgres(t *testing.T) {
	t.Run("Omit_marshals_absent", func(t *testing.T) {
		input := models.UpdateArticleInput{}
		got, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("Marshal(empty): %v", err)
		}
		// Every field is unset; omitzero must drop them all.
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
		// Unset fields must remain absent.
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
		// Other fields stay unset.
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
			t.Errorf("Body.MustGet()=%v, want nil pointer (Set[*string](nil))", v)
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
			t.Errorf("Title roundtrip: got %v, want %q (set)", decoded.Title, title)
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

// updateSQL returns the last captured UPDATE statement; fails the test if none captured.
func updateSQL(t *testing.T, c *capturingQuerier) string {
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

// lastInsert returns the last captured INSERT statement; fails the test if none captured.
func lastInsert(t *testing.T, c *capturingQuerier) string {
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
