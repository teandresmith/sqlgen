package tests

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"
	"uuid"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/shopspring/decimal"

	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

// The GraphQL projection of nested mutations (PRD §26.5.1, §26.5.5, §9.9).
// Every test here goes through the booted gqlgen server rather than through the
// Go client: what is under test is the projection — the schema gqlgen parsed,
// the translators that carry each member to the Go surface, and the error
// mapper — so a Go-client call would skip exactly the layer this projection
// adds. The Go surface's own semantics are pinned by the three
// nested_mutations_*_test.go files beside this one; the database is read back
// here only to prove a member reached it.

// nestedGQLOccurredAt is the DateTime every nested event in this file carries.
const nestedGQLOccurredAt = "2026-09-23T12:00:00Z"

type nestedGQLID struct {
	ID string `json:"id"`
}

// nestedGQLIntID is a row keyed on an `Int` — `categories` or `user_badges`,
// whose id the GraphQL surface serializes as a number rather than a string.
type nestedGQLIntID struct {
	ID int64 `json:"id"`
}

type nestedGQLUser struct {
	ID         string           `json:"id"`
	Email      string           `json:"email"`
	Name       string           `json:"name"`
	Events     []nestedGQLID    `json:"events"`
	Orders     []nestedGQLID    `json:"orders"`
	Categories []nestedGQLIntID `json:"categories"`
}

// nestedGQLDocument is a document selected through an `assets` edge, with the
// discriminator the edge set on it.
type nestedGQLDocument struct {
	Name       string `json:"name"`
	EntityType string `json:"entityType"`
}

func nestedGQLIDs(xs []nestedGQLID) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.ID)
	}
	slices.Sort(out)
	return out
}

// extensionsPath reads `extensions.path` off a GraphQL error as the list of
// strings PRD §26.5.5 specifies, and reports whether the key is present at all.
func extensionsPath(e gqlError) ([]string, bool) {
	raw, ok := e.Extensions["path"]
	if !ok {
		return nil, false
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, true
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, _ := it.(string)
		out = append(out, s)
	}
	return out, true
}

// TestGraphQLNested_CreateReachesEveryShape runs createUserWithRelated over
// `users`' three list shapes in one mutation — O2M on a nullable FK (`create` +
// `connect`), O2M on a NOT NULL FK (`create` alone) and M2M on an Int-keyed
// target (`create` + `connect`, the `Int` → int64 conversion) — and selects all
// three edges on the result. The fourth edge, the has-one `Badge`, is
// TestGraphQLNested_HasOneIntKeyRelinks's. The selection is served by the walker and the Go
// method's terminal read, so the response is the post-write state.
func TestGraphQLNested_CreateReachesEveryShape(t *testing.T) {
	truncateAll(t)

	orphan := seedOrphanEvent(t, "adopted-over-graphql")
	existingCat := seedCategoryRow(t, "gql-existing")

	var out struct {
		CreateUserWithRelated nestedGQLUser `json:"createUserWithRelated"`
	}
	gqlExecData(t, `
		mutation ($at: Time!, $occ: DateTime!, $orphan: UUID!, $cat: Int!) {
			createUserWithRelated(input: {
				user: { email: "nested-create@example.com", name: "Nested", isActive: true, createdAt: $at }
				events: { create: [{ action: "signup", occurredAt: $occ }], connect: [$orphan] }
				orders: { create: [{ total: "10.00", createdAt: $at }, { total: "20.00", createdAt: $at }] }
				categories: { create: [{ name: "gql-new", createdAt: $at }], connect: [$cat] }
			}) {
				id email
				events { id }
				orders { id }
				categories { id }
			}
		}
	`, map[string]any{
		"at": fixedTimestamp, "occ": nestedGQLOccurredAt,
		"orphan": orphan.String(), "cat": existingCat,
	}, &out)

	got := out.CreateUserWithRelated
	if got.Email != "nested-create@example.com" {
		t.Errorf("email = %q, want the flat half's value", got.Email)
	}
	if len(got.Events) != 2 || !slices.Contains(nestedGQLIDs(got.Events), orphan.String()) {
		t.Errorf("events = %v, want the created signup plus the connected orphan %s", nestedGQLIDs(got.Events), orphan)
	}
	if len(got.Orders) != 2 {
		t.Errorf("orders = %v, want the two created on the NOT NULL edge", nestedGQLIDs(got.Orders))
	}
	if len(got.Categories) != 2 {
		t.Errorf("categories = %+v, want the created one plus category %d", got.Categories, existingCat)
	}

	// The response could in principle be the walker echoing its own input;
	// the rows are what matters.
	owner := uuid.MustParse(got.ID)
	if ids := eventIDs(t, owner); !ids[orphan] || len(ids) != 2 {
		t.Errorf("events.user_id in the database = %v, want 2 rows including %s", ids, orphan)
	}
}

// TestGraphQLNested_HasOneAndUUIDKeyedM2M covers a has-one `create`, whose verb
// takes one value rather than a list (`primaryDocument: { create: {...} }`),
// and an M2M edge into a UUID-keyed target, which `users` does not carry, whose
// ids thread through with no conversion. The has-one relink verbs are
// TestGraphQLNested_HasOneIntKeyRelinks's. The three `assets` discriminator
// edges also prove discriminator ownership on the wire: the nested child inputs
// have no `entityType` member, and the rows still come back with the edge's
// value.
func TestGraphQLNested_HasOneAndUUIDKeyedM2M(t *testing.T) {
	truncateAll(t)

	unrelated := seedDocumentRow(t, "pre-existing", uuid.New(), models.DocumentEntityTypeEnumSpv)

	var out struct {
		CreateAssetWithRelated struct {
			ID              string              `json:"id"`
			PrimaryDocument *nestedGQLDocument  `json:"primaryDocument"`
			Attachments     []nestedGQLDocument `json:"attachments"`
			Documents       []nestedGQLID       `json:"documents"`
		} `json:"createAssetWithRelated"`
	}
	gqlExecData(t, `
		mutation ($at: Time!, $doc: UUID!, $entity: UUID!) {
			createAssetWithRelated(input: {
				asset: { name: "nested-asset", declaredCategories: [], createdAt: $at }
				primaryDocument: { create: { name: "cover", createdAt: $at } }
				attachments: { create: [{ name: "scan", createdAt: $at }] }
				documents: {
					create: [{ entityID: $entity, entityType: SPV, name: "linked", createdAt: $at }]
					connect: [$doc]
				}
			}) {
				id
				primaryDocument { name entityType }
				attachments { name entityType }
				documents { id }
			}
		}
	`, map[string]any{"at": fixedTimestamp, "doc": unrelated.String(), "entity": uuid.New().String()}, &out)

	got := out.CreateAssetWithRelated
	if got.PrimaryDocument == nil || got.PrimaryDocument.Name != "cover" || got.PrimaryDocument.EntityType != "ASSETPRIMARY" {
		t.Errorf("primaryDocument = %+v, want cover carrying the edge's ASSETPRIMARY discriminator", got.PrimaryDocument)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].EntityType != "ASSETATTACHMENT" {
		t.Errorf("attachments = %+v, want one row carrying ASSETATTACHMENT", got.Attachments)
	}
	if len(got.Documents) != 2 || !slices.Contains(nestedGQLIDs(got.Documents), unrelated.String()) {
		t.Errorf("documents = %v, want the created one plus the connected %s", nestedGQLIDs(got.Documents), unrelated)
	}
}

// TestGraphQLNested_UpdateOnlyTouchesNestedRows omits the flat half entirely —
// the member is nullable on the update wrapper precisely so a nested-only call
// need not send `user: {}` — and runs the two verbs the create side cannot
// express: `clear`, replacing an O2M set wholesale together with a `connect`,
// and an M2M `disconnect`.
func TestGraphQLNested_UpdateOnlyTouchesNestedRows(t *testing.T) {
	truncateAll(t)

	user := seedUserRow(t, "nested-update@example.com")
	stale := seedParentedEvent(t, user, "stale")
	adopt := seedOrphanEvent(t, "adopt")
	keep := seedCategoryRow(t, "keep")
	drop := seedCategoryRow(t, "drop")
	if _, err := testClient.Users().UpdateWithRelated(nestedCtx(), user, &models.UpdateUserWithRelatedInput{
		Categories: &models.UserCategoriesUpdateNested{Connect: []int64{keep, drop}},
	}); err != nil {
		t.Fatalf("seeding categories: %v", err)
	}

	var out struct {
		UpdateUserWithRelated nestedGQLUser `json:"updateUserWithRelated"`
	}
	gqlExecData(t, `
		mutation ($id: UUID!, $adopt: UUID!, $drop: Int!) {
			updateUserWithRelated(id: $id, input: {
				events: { clear: true, connect: [$adopt] }
				categories: { disconnect: [$drop] }
			}) {
				id name
				events { id }
				categories { id }
			}
		}
	`, map[string]any{"id": user.String(), "adopt": adopt.String(), "drop": drop}, &out)

	got := out.UpdateUserWithRelated
	if got.Name != "nested-update@example.com" {
		t.Errorf("name = %q, want it untouched by an update with no flat half", got.Name)
	}
	if ids := nestedGQLIDs(got.Events); !slices.Equal(ids, []string{adopt.String()}) {
		t.Errorf("events = %v, want exactly [%s] — clear + connect is the set", ids, adopt)
	}
	if ids := eventIDs(t, user); ids[stale] {
		t.Errorf("event %s is still parented after `clear`", stale)
	}
	if len(got.Categories) != 1 || got.Categories[0].ID != keep {
		t.Errorf("categories = %+v, want only %d after disconnecting %d", got.Categories, keep, drop)
	}
}

// TestGraphQLNested_UpsertTakesAConflictTarget pins the conflict-target
// argument on the wire: the argument defaults to PK, so a fresh row takes the
// insert branch, and naming EMAIL routes a second call with the same email onto
// the existing row — the branch whose update-side block the `create` then
// extends.
func TestGraphQLNested_UpsertTakesAConflictTarget(t *testing.T) {
	truncateAll(t)

	const upsert = `
		mutation ($at: Time!, $occ: DateTime!, $name: String!, $target: UserConflictTarget!) {
			upsertUserWithRelated(input: {
				user: { email: "nested-upsert@example.com", name: $name, isActive: true, createdAt: $at }
				events: { create: [{ action: $name, occurredAt: $occ }] }
			}, conflictTarget: $target) {
				id name
				events { id }
			}
		}`
	var first, second struct {
		UpsertUserWithRelated nestedGQLUser `json:"upsertUserWithRelated"`
	}
	gqlExecData(t, upsert, map[string]any{"at": fixedTimestamp, "occ": nestedGQLOccurredAt, "name": "first", "target": "EMAIL"}, &first)
	gqlExecData(t, upsert, map[string]any{"at": fixedTimestamp, "occ": nestedGQLOccurredAt, "name": "second", "target": "EMAIL"}, &second)

	if first.UpsertUserWithRelated.ID != second.UpsertUserWithRelated.ID {
		t.Fatalf("second upsert on EMAIL landed on %s, want the first call's row %s", second.UpsertUserWithRelated.ID, first.UpsertUserWithRelated.ID)
	}
	if got := second.UpsertUserWithRelated; got.Name != "second" || len(got.Events) != 2 {
		t.Errorf("after the conflict branch: name = %q, events = %d; want \"second\" and both calls' events", got.Name, len(got.Events))
	}

	// The default: omitting conflictTarget is PK, and a caller who supplies
	// no id always takes the insert branch — here, into the unique email.
	resp := gqlExec(t, `
		mutation ($at: Time!) {
			upsertUserWithRelated(input: {
				user: { email: "nested-upsert@example.com", name: "third", isActive: true, createdAt: $at }
				orders: { create: [{ total: "1.00", createdAt: $at }] }
			}) { id }
		}
	`, map[string]any{"at": fixedTimestamp}, nil)
	if len(resp.Errors) != 1 || extensionsCode(resp.Errors[0]) != "CONFLICT" {
		t.Fatalf("PK-default upsert of an existing email: errors = %+v, want one CONFLICT (the insert branch hit users.email)", resp.Errors)
	}
	// The flat half failed, not a nested block, so no edge is named.
	if path, ok := extensionsPath(resp.Errors[0]); ok {
		t.Errorf("extensions.path = %v on a parent-write failure, want it absent", path)
	}
}

// TestGraphQLNested_ErrorCodesAndPath pins PRD §26.5.5 per sentinel through the
// live mapper: ErrAlreadyRelated → CONFLICT, ErrNestedVerbConflict →
// INVALID_INPUT, and a connect visibility miss → NOT_FOUND, never
// BAD_REFERENCE. Each also carries `extensions.path: ["<Edge>"]` — the Go
// field name, PascalCase, read off *NestedMutationError.Edge (§9.9.8).
func TestGraphQLNested_ErrorCodesAndPath(t *testing.T) {
	truncateAll(t)

	other := seedUserRow(t, "nested-errors-other@example.com")
	user := seedUserRow(t, "nested-errors@example.com")
	theirs := seedParentedEvent(t, other, "theirs")
	orphan := seedOrphanEvent(t, "orphan")

	const update = `
		mutation ($id: UUID!, $events: UserEventsUpdateNested, $categories: UserCategoriesUpdateNested) {
			updateUserWithRelated(id: $id, input: { events: $events, categories: $categories }) { id }
		}`
	tests := []struct {
		name     string
		vars     map[string]any
		wantCode string
		wantPath []string
	}{
		{
			name:     "a target parented to another row is CONFLICT",
			vars:     map[string]any{"events": map[string]any{"connect": []string{theirs.String()}}},
			wantCode: "CONFLICT",
			wantPath: []string{"Events"},
		},
		{
			name: "one target under two verbs is INVALID_INPUT",
			vars: map[string]any{"events": map[string]any{
				"connect": []string{orphan.String()}, "disconnect": []string{orphan.String()},
			}},
			wantCode: "INVALID_INPUT",
			wantPath: []string{"Events"},
		},
		{
			name:     "an O2M target that is not visible is NOT_FOUND",
			vars:     map[string]any{"events": map[string]any{"connect": []string{uuid.New().String()}}},
			wantCode: "NOT_FOUND",
			wantPath: []string{"Events"},
		},
		{
			name:     "an M2M target that is not visible is NOT_FOUND",
			vars:     map[string]any{"categories": map[string]any{"connect": []int{987654}}},
			wantCode: "NOT_FOUND",
			wantPath: []string{"Categories"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := map[string]any{"id": user.String()}
			for k, v := range tt.vars {
				vars[k] = v
			}
			resp := gqlExec(t, update, vars, nil)
			if len(resp.Errors) != 1 {
				t.Fatalf("errors = %+v, want exactly one", resp.Errors)
			}
			if got := extensionsCode(resp.Errors[0]); got != tt.wantCode {
				t.Errorf("extensions.code = %q, want %q (message %q)", got, tt.wantCode, resp.Errors[0].Message)
			}
			if got, _ := extensionsPath(resp.Errors[0]); !slices.Equal(got, tt.wantPath) {
				t.Errorf("extensions.path = %v, want %v", got, tt.wantPath)
			}
		})
	}

	// Every case above refused before or inside the method's transaction, so
	// none of them moved a row.
	if ids := eventIDs(t, user); len(ids) != 0 {
		t.Errorf("user %s owns events %v after four refused calls, want none", user, ids)
	}
}

// TestGraphQLNested_IncrementsShareTheTransaction pins the ruling on the flat
// half's `_inc` / `_dec` operators: they are applied, and they are
// applied inside the same transaction as the nested writes. The failing case
// is what proves the second half — an increment that overflows the INTEGER
// column fails after UpdateWithRelated has already created a child, and the
// child must be gone afterwards along with the increment.
func TestGraphQLNested_IncrementsShareTheTransaction(t *testing.T) {
	truncateAll(t)

	url := newHeaderTenantedHandler(t)
	headers := map[string]string{"X-Workspace-ID": nestedTenant.String()}
	client, ctx := nestedTenantedClient(t)

	note, err := client.WorkspaceNotes().Create(ctx, &models.CreateWorkspaceNoteInput{
		Body:        "parent",
		PinnedOrder: omittable.Set(int32(5)),
	})
	if err != nil {
		t.Fatalf("seeding note: %v", err)
	}

	const update = `
		mutation ($id: UUID!, $ws: UUID!, $at: Time!, $labels: JSON!, $inc: Int, $body: String!) {
			updateWorkspaceNoteWithRelated(id: $id, input: {
				workspaceNote: { pinnedOrder_inc: $inc }
				children: { create: [{ workspaceID: $ws, body: $body, pinnedOrder: 0, kind: PUBLISHED, labels: $labels, createdAt: $at }] }
			}) {
				pinnedOrder
				children { body }
			}
		}`
	vars := func(inc int, body string) map[string]any {
		return map[string]any{
			"id": note.ID.String(), "ws": nestedTenant.String(), "at": fixedTimestamp,
			"labels": map[string]any{}, "inc": inc, "body": body,
		}
	}

	t.Run("the increment and the nested create both land", func(t *testing.T) {
		resp := postGQLAt(t, url, update, vars(2, "kept"), headers)
		if len(resp.Errors) != 0 {
			t.Fatalf("errors = %+v", resp.Errors)
		}
		reloaded, err := client.WorkspaceNotes().Get(ctx, note.ID)
		if err != nil {
			t.Fatalf("reading note: %v", err)
		}
		if reloaded.PinnedOrder != 7 {
			t.Errorf("pinned_order = %d, want 5 + 2", reloaded.PinnedOrder)
		}
	})

	t.Run("an increment that fails rolls the nested create back", func(t *testing.T) {
		const maxInt32 = 1<<31 - 1
		resp := postGQLAt(t, url, update, vars(maxInt32, "rolled-back"), headers)
		if len(resp.Errors) != 1 {
			t.Fatalf("errors = %+v, want the overflow", resp.Errors)
		}
		notes, err := client.WorkspaceNotes().GetMany(ctx, &models.GetWorkspaceNotesInput{})
		if err != nil {
			t.Fatalf("reading notes: %v", err)
		}
		for _, n := range notes {
			if n.Body == "rolled-back" {
				t.Errorf("child %s survived an increment failure in the same mutation — the nested write committed on its own", n.ID)
			}
			if n.ID == note.ID && n.PinnedOrder != 7 {
				t.Errorf("pinned_order = %d after a failed call, want 7 unchanged", n.PinnedOrder)
			}
		}
	})

	t.Run("setting and incrementing one column is INVALID_INPUT", func(t *testing.T) {
		resp := postGQLAt(t, url, `
			mutation ($id: UUID!) {
				updateWorkspaceNoteWithRelated(id: $id, input: {
					workspaceNote: { pinnedOrder: 1, pinnedOrder_inc: 1 }
				}) { id }
			}`, map[string]any{"id": note.ID.String()}, headers)
		if len(resp.Errors) != 1 || extensionsCode(resp.Errors[0]) != "INVALID_INPUT" {
			t.Errorf("errors = %+v, want one INVALID_INPUT (PRD §26.5.4)", resp.Errors)
		}
	})
}

// TestGraphQLNested_IncrementReadBackBypassesTheCache pins the read-back the
// increment transaction ends with. It is the Go method's terminal re-read
// moved into the resolver — the increments have to precede it — so it runs as
// that read does, with hooks skipped (PRD §9.9.6): the writes before it defer
// their cache invalidation to commit, and a read-through inside the
// transaction would answer the mutation with the row's pre-mutation state
// from the cache. The selection is scalar-only on purpose: any relationship
// selection bypasses the cache on its own (§27.6), which is how the
// increment test above misses this.
func TestGraphQLNested_IncrementReadBackBypassesTheCache(t *testing.T) {
	truncateAll(t)

	backend, err := cachememory.New(cachememory.Options{MaxSize: 1_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	c, err := models.NewCache(backend)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	client := models.New(dbpgx.New(testPool), models.WithCache(c))
	es := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}})
	srv := httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(handler.NewDefaultServer(es)))
	t.Cleanup(srv.Close)

	ctx := nestedCtx()
	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "cached",
		Price:      decimal.NewFromInt(1),
		Stock:      omittable.Set(int32(10)),
		CategoryID: seedCategoryRow(t, "cached-category"),
	})
	if err != nil {
		t.Fatalf("seeding product: %v", err)
	}
	// A full-entity Get populates the cache with stock = 10.
	if _, err := client.Products().Get(ctx, product.ID); err != nil {
		t.Fatalf("warming the cache: %v", err)
	}

	var out struct {
		UpdateProductWithRelated struct {
			Stock int `json:"stock"`
		} `json:"updateProductWithRelated"`
	}
	resp := postGQLAt(t, srv.URL, `
		mutation ($id: UUID!) {
			updateProductWithRelated(id: $id, input: { product: { stock_inc: 5 } }) { stock }
		}`, map[string]any{"id": product.ID.String()}, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %+v", resp.Errors)
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := out.UpdateProductWithRelated.Stock; got != 15 {
		t.Errorf("mutation returned stock = %d, want 15 — the read-back was served the cached pre-mutation row", got)
	}
}

// TestGraphQLNested_VerbsAreTheSchemaNotARuntimeCheck asks the running schema
// what each block declares. A NOT NULL edge's verbs are a type fact — it has
// no `connect`, `disconnect` or `clear` member to send — and the create-side
// block has no unlink verb. A `filter:` edge is absent from the
// wrapper altogether, which is the accept-and-drop failure the separate input
// types exist to prevent.
func TestGraphQLNested_VerbsAreTheSchemaNotARuntimeCheck(t *testing.T) {
	fields := func(t *testing.T, typeName string) []string {
		t.Helper()
		var out struct {
			Type *struct {
				InputFields []struct {
					Name string `json:"name"`
				} `json:"inputFields"`
			} `json:"__type"`
		}
		gqlExecData(t, `query ($name: String!) { __type(name: $name) { inputFields { name } } }`,
			map[string]any{"name": typeName}, &out)
		if out.Type == nil {
			return nil
		}
		names := make([]string, 0, len(out.Type.InputFields))
		for _, f := range out.Type.InputFields {
			names = append(names, f.Name)
		}
		return names
	}

	tests := []struct {
		typeName string
		want     []string
	}{
		{"UserOrdersUpdateNested", []string{"create"}},
		{"UserEventsCreateNested", []string{"create", "connect"}},
		{"UserEventsUpdateNested", []string{"clear", "create", "connect", "disconnect"}},
		{"UserCategoriesUpdateNested", []string{"clear", "create", "connect", "disconnect"}},
		{"CreateAssetWithRelatedInput", []string{"asset", "attachments", "documents", "invoices", "primaryDocument"}},
	}
	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			if got := fields(t, tt.typeName); !slices.Equal(got, tt.want) {
				t.Errorf("%s input fields = %v, want %v", tt.typeName, got, tt.want)
			}
		})
	}
}

// badgeOwner reads user_badges.user_id straight from the table, so an assertion
// on it does not rest on the mutation's own read-back.
func badgeOwner(t *testing.T, id int64) *uuid.UUID {
	t.Helper()
	b, err := testClient.UserBadges().Get(nestedCtx(), id)
	if err != nil {
		t.Fatalf("reading badge %d: %v", id, err)
	}
	return b.UserID
}

// TestGraphQLNested_HasOneIntKeyRelinks runs the relink verbs of the one
// has-one edge over a nullable FK into an Int-keyed target, `users.Badge`. Each
// verb takes a single `Int`, which the translator converts to the model's int64
// through a local — an arm no other edge renders. It renders at three call
// sites, `connect` on both blocks and `disconnect` on the update block, and
// each mutation below runs one of them. The row is read back after every step:
// a translator that drops the id still compiles, and the call is then a no-op
// the response alone would not reveal.
func TestGraphQLNested_HasOneIntKeyRelinks(t *testing.T) {
	truncateAll(t)

	badge, err := testClient.UserBadges().Create(nestedCtx(), &models.CreateUserBadgeInput{Label: "early-adopter"})
	if err != nil {
		t.Fatalf("seeding badge: %v", err)
	}

	var created struct {
		CreateUserWithRelated struct {
			ID    string          `json:"id"`
			Badge *nestedGQLIntID `json:"badge"`
		} `json:"createUserWithRelated"`
	}
	gqlExecData(t, `
		mutation ($at: Time!, $badge: Int!) {
			createUserWithRelated(input: {
				user: { email: "nested-badge@example.com", name: "Badge", isActive: true, createdAt: $at }
				badge: { connect: $badge }
			}) {
				id
				badge { id }
			}
		}
	`, map[string]any{"at": fixedTimestamp, "badge": badge.ID}, &created)

	got := created.CreateUserWithRelated
	if got.Badge == nil || got.Badge.ID != badge.ID {
		t.Fatalf("createUserWithRelated badge = %+v, want the connected badge %d", got.Badge, badge.ID)
	}
	owner := uuid.MustParse(got.ID)
	if o := badgeOwner(t, badge.ID); o == nil || *o != owner {
		t.Errorf("user_badges.user_id = %v after a create-side connect, want %s", o, owner)
	}

	relink := func(verb string) *nestedGQLIntID {
		t.Helper()
		var out struct {
			UpdateUserWithRelated struct {
				Badge *nestedGQLIntID `json:"badge"`
			} `json:"updateUserWithRelated"`
		}
		gqlExecData(t, `
			mutation ($id: UUID!, $badge: UserBadgeUpdateNested) {
				updateUserWithRelated(id: $id, input: { badge: $badge }) {
					badge { id }
				}
			}
		`, map[string]any{"id": got.ID, "badge": map[string]any{verb: badge.ID}}, &out)
		return out.UpdateUserWithRelated.Badge
	}

	if b := relink("disconnect"); b != nil {
		t.Errorf("updateUserWithRelated badge = %+v after disconnect, want null", b)
	}
	if o := badgeOwner(t, badge.ID); o != nil {
		t.Errorf("user_badges.user_id = %s after disconnect, want NULL", o)
	}

	if b := relink("connect"); b == nil || b.ID != badge.ID {
		t.Errorf("updateUserWithRelated badge = %+v after an update-side connect, want badge %d", b, badge.ID)
	}
	if o := badgeOwner(t, badge.ID); o == nil || *o != owner {
		t.Errorf("user_badges.user_id = %v after an update-side connect, want %s", o, owner)
	}
}

// reloadAssetEdges reads an asset's primary document and its M2M `documents`
// back through the Go client, so an assertion on them does not rest on the
// mutation's own read-back.
func reloadAssetEdges(t *testing.T, id uuid.UUID) *models.Asset {
	t.Helper()
	a, err := testClient.Assets().Get(nestedCtx(), id, func(o *models.CallOptions[models.AssetFieldOptions]) {
		o.FieldOptions = &models.AssetFieldOptions{
			ID:              true,
			PrimaryDocument: &models.DocumentFieldOptions{ID: true, Name: true, EntityType: true},
			Documents:       &models.DocumentRelationshipOptions{FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true}},
		}
	})
	if err != nil {
		t.Fatalf("reading asset %s: %v", id, err)
	}
	return a
}

// TestGraphQLNested_UpdateAndUpsertReachTheRemainingShapes covers the family ×
// shape cells the tests above leave out: a has-one `create` on the update-side
// block, and the upsert wrapper carrying a has-one and an M2M block. Upsert
// reuses the update-side block (§9.9.5), so the two mutations share each edge's
// block translator; what the upsert adds is its own wrapper translator, which
// no other test runs with these members. `assets` has only a PK conflict target
// and its GraphQL create input has no `id`, so the upsert here takes the insert
// branch; the conflict branch is
// TestGraphQLNested_UpsertTakesAConflictTarget's.
func TestGraphQLNested_UpdateAndUpsertReachTheRemainingShapes(t *testing.T) {
	truncateAll(t)

	// The asset starts with no primary document: a has-one `create` on an asset
	// that already has one is refused with ErrAlreadyRelated.
	t.Run("update: has-one create", func(t *testing.T) {
		asset, err := testClient.Assets().Create(nestedCtx(), &models.CreateAssetInput{Name: "no-cover-yet"})
		if err != nil {
			t.Fatalf("seeding asset: %v", err)
		}

		var out struct {
			UpdateAssetWithRelated struct {
				PrimaryDocument *nestedGQLDocument `json:"primaryDocument"`
			} `json:"updateAssetWithRelated"`
		}
		gqlExecData(t, `
			mutation ($id: UUID!, $at: Time!) {
				updateAssetWithRelated(id: $id, input: {
					primaryDocument: { create: { name: "late-cover", createdAt: $at } }
				}) {
					primaryDocument { name entityType }
				}
			}
		`, map[string]any{"id": asset.ID.String(), "at": fixedTimestamp}, &out)

		if got := out.UpdateAssetWithRelated.PrimaryDocument; got == nil || got.Name != "late-cover" || got.EntityType != "ASSETPRIMARY" {
			t.Errorf("updateAssetWithRelated primaryDocument = %+v, want late-cover carrying ASSETPRIMARY", got)
		}
		row := reloadAssetEdges(t, asset.ID).PrimaryDocument
		if row == nil || row.Name != "late-cover" || row.EntityType != models.DocumentEntityTypeEnumAssetprimary {
			t.Errorf("primary document in the database = %+v, want late-cover with entity_type asset.primary", row)
		}
	})

	t.Run("upsert: has-one and M2M", func(t *testing.T) {
		existing := seedDocumentRow(t, "upsert-existing", uuid.New(), models.DocumentEntityTypeEnumSpv)

		var out struct {
			UpsertAssetWithRelated struct {
				ID              string             `json:"id"`
				PrimaryDocument *nestedGQLDocument `json:"primaryDocument"`
				Documents       []nestedGQLID      `json:"documents"`
			} `json:"upsertAssetWithRelated"`
		}
		gqlExecData(t, `
			mutation ($at: Time!, $doc: UUID!, $entity: UUID!) {
				upsertAssetWithRelated(input: {
					asset: { name: "upserted-asset", declaredCategories: [], createdAt: $at }
					primaryDocument: { create: { name: "upsert-cover", createdAt: $at } }
					documents: {
						create: [{ entityID: $entity, entityType: SPV, name: "upsert-linked", createdAt: $at }]
						connect: [$doc]
					}
				}) {
					id
					primaryDocument { name entityType }
					documents { id }
				}
			}
		`, map[string]any{"at": fixedTimestamp, "doc": existing.String(), "entity": uuid.New().String()}, &out)

		got := out.UpsertAssetWithRelated
		if got.PrimaryDocument == nil || got.PrimaryDocument.Name != "upsert-cover" || got.PrimaryDocument.EntityType != "ASSETPRIMARY" {
			t.Errorf("upsertAssetWithRelated primaryDocument = %+v, want upsert-cover carrying ASSETPRIMARY", got.PrimaryDocument)
		}
		if len(got.Documents) != 2 || !slices.Contains(nestedGQLIDs(got.Documents), existing.String()) {
			t.Errorf("upsertAssetWithRelated documents = %v, want the created one plus the connected %s", nestedGQLIDs(got.Documents), existing)
		}

		row := reloadAssetEdges(t, uuid.MustParse(got.ID))
		if row.PrimaryDocument == nil || row.PrimaryDocument.Name != "upsert-cover" || row.PrimaryDocument.EntityType != models.DocumentEntityTypeEnumAssetprimary {
			t.Errorf("primary document in the database = %+v, want upsert-cover with entity_type asset.primary", row.PrimaryDocument)
		}
		linked := make([]string, 0, len(row.Documents))
		for _, d := range row.Documents {
			linked = append(linked, d.Name)
		}
		slices.Sort(linked)
		if !slices.Equal(linked, []string{"upsert-existing", "upsert-linked"}) {
			t.Errorf("linked documents in the database = %v, want [upsert-existing upsert-linked]", linked)
		}
	})
}
