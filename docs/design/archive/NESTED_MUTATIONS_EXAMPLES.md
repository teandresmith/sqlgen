# SQLGen Nested Mutations — Worked Method Examples

> **Status: ARCHIVED (2026-10-04) — superseded by PRD §9.9** (synced 2026-10-02).
>
> **Do not read this file for current behavior.** This was the pre-implementation surface for
> Phase 27; its open questions (EQ1–EQ6) and "status today" tables describe the code before the
> phase landed. [PRD §9.9](../../PRD.md#99-nested-mutations) is normative and supersedes it on any
> conflict. Archived alongside its design doc, [`NESTED_MUTATIONS.md`](NESTED_MUTATIONS.md), because
> Phase 27 is closed and nothing in the code or the PRD references this document.
>
> **Original status note follows.**
>
> **Status: REFERENCE (2026-09-11).** Companion to `docs/design/archive/NESTED_MUTATIONS.md`. That document is
> the design; this one is the **surface**, written out method by method so a `/phase` breakdown
> has something concrete to implement against and a reviewer has something concrete to diff
> against.
>
> **Every Go example below was compiled**, not sketched. The full set was dropped into
> `cmd/sqlgen/testdata/examples/graphql/models/` as one file and built with `GOWORK=off go build`
> + `go vet`, clean, with five failing-first checks confirming the FK assignments are genuinely
> type-checked (§8.1). The probe was removed afterwards and is not checked in; §8.1 gives the
> recipe to reproduce it.
>
> **Every runtime claim was measured** against `postgres:16-alpine`, `mysql:8.0` and local
> SQLite (§6). Three of those measurements contradict or extend the design doc and are the
> reason `UpsertMany` is a larger ticket than **D5** currently scopes.
>
> **§7 covers events and cache invalidation**, which the design doc settles in one decision
> (**D16**) and never works through from a consumer's side. It states what fires and when, the
> four properties that are already correct by construction, three that are not, and six open
> questions (**EQ1–EQ6**). It also records why suppressing events and caching for these methods
> — the obvious response to their multi-table fan-out — was considered and rejected.
>
> **Phase: 27**, claimed 2026-09-16 (`docs/design/archive/NESTED_MUTATIONS.md` header). Nothing here is blessed
> — §8 of the design doc still gates code on a PRD sync, now numbered **27.0**.
>
> ---
>
> **⚠ Re-verified against HEAD on 2026-09-16, and this document is PARTLY STALE. Read
> `docs/design/archive/NESTED_MUTATIONS.md` §2.5 before trusting a Go listing here.** Three things moved under it:
>
> 1. **The Go listings no longer compile as written.** Phase 26 (`eb58ae2`) bound nullable UUID
>    columns to `*uuid.UUID`; the stdlib `uuid` package has no `NullUUID`. Every
>    `omittable.Set(uuid.NullUUID{UUID: parent.ID, Valid: true})` below is now
>    `omittable.Set(&parent.ID)`, and every `omittable.Set(uuid.NullUUID{})` is now
>    `omittable.Set[*uuid.UUID](nil)`. **The §3 executors were reassembled, repaired and re-built
>    clean at HEAD** — the design doc's §2.3 carries the current transcript. The listings below are
>    left in their original spelling rather than hand-patched, because a mechanical rewrite of ~600
>    lines of uncompiled Go would be a fresh source of error; the two substitutions above are the
>    whole diff.
> 2. **One failing-first check in §8.1 no longer fails.** Substituting the omitted form for the
>    NULL form on a nullable FK used to be a type error and now builds clean. That is the design
>    doc's new **F11**, and it is why `disconnect` and `clear` need a runtime pin.
> 3. **§2's edge count is now eighteen, not seventeen.** `3509180` added `user_credentials` to the
>    fixture, whose edge `UserCredential.Users` is a belongs-to with the PK doubling as the FK.
>    Ineligible under **NW-D5**, so no verb changes — but the classification table below is one row
>    short. The design doc's §4.1 now carries the full eighteen.
>
> Everything else held: all ten gaps in §8.2 are still open at HEAD, and §6's measurements and §7's
> analysis are untouched by the three commits.

## Contents

1. [The method inventory](#1-the-method-inventory)
2. [Every edge in the fixture, classified](#2-every-edge-in-the-fixture-classified)
   - [2.1 The verb vocabulary, checked against the field](#21-the-verb-vocabulary-checked-against-the-field)
3. [Go examples — the three `…WithRelated` families](#3-go-examples--the-three-withrelated-families)
4. [Go examples — the shapes the design doc never compiled](#4-go-examples--the-shapes-the-design-doc-never-compiled)
5. [GraphQL examples](#5-graphql-examples)
6. [Measured findings — `UpsertMany` is bigger than D5 says](#6-measured-findings--upsertmany-is-bigger-than-d5-says)
7. [Events and caching](#7-events-and-caching)
8. [Possible today vs. must be built](#8-possible-today-vs-must-be-built)
9. [Suggested amendments to the design doc](#9-suggested-amendments-to-the-design-doc)

---

## 1. The method inventory

Four generated methods, of which one is a prerequisite that stands on its own.

| # | Method | Receiver | Status today | Gate |
|---|---|---|---|---|
| **1** | `UpsertMany(ctx, inputs, target, opts…) ([]*T, error)` | every table client | **does not exist** — `grep -rn UpsertMany cmd/sqlgen` is empty | `operations.upsert_many` |
| **2** | `CreateWithRelated(ctx, input, opts…) (*T, error)` | eligible parents | does not exist | `operations.create_with_related` |
| **3** | `UpdateWithRelated(ctx, id\|pk, input, opts…) (*T, error)` | eligible parents | does not exist | `operations.update_with_related` |
| **4** | `UpsertWithRelated(ctx, input, target, opts…) (*T, error)` | eligible parents | does not exist | `operations.upsert_with_related` |

Method 4's signature is worth pinning now, because the design doc leaves it implicit. §5.3 says
"`UpsertWithRelated` is this function with `c.Update(ctx, id, …)` replaced by
`c.Upsert(ctx, &input.User, target, …)`" — which means the method takes a **conflict target
argument** and **no id argument**, exactly mirroring the flat `Upsert`:

```go
Upsert           (ctx context.Context, input *CreateUserInput,            target UserConflictTarget, opts ...func(*CallOptions[UserFieldOptions])) (*User, error)
UpsertWithRelated(ctx context.Context, input *UpsertUserWithRelatedInput, target UserConflictTarget, opts ...func(*CallOptions[UserFieldOptions])) (*User, error)
```

Alongside them, five generated type families per eligible parent (design doc §5.1) and two new
error sentinels plus one structured error in `database/` (**D15**).

---

## 2. Every edge in the fixture, classified

The design doc's §4.1 used to apply the eligibility matrix to six edges; **it now carries the full
set, which is the fix this section argued for (A1, A2, applied 2026-09-16).** The full
classification mattered because three of the edges the doc did not list are *more* verb-complete
than any edge it did list, and one of them is a shape the design never discussed at all.

> **The count below is one short.** It says seventeen; the fixture has **eighteen** since
> `3509180` (2026-09-15) added `user_credentials`. The eighteenth edge is `UserCredential.Users` —
> a belongs-to whose PK *is* the FK, ineligible under **NW-D5**, so no verb changes. The design
> doc's §4.1 table is the current one.

Derived mechanically from `models_gen.go` (relationship field comments) cross-referenced against
`schema.sql` FK nullability and `sqlgen.yml` relationship config.

| Parent | Edge | Shape | FK | Verbs emitted | Why |
|---|---|---|---|---|---|
| `User` | `Events` | O2M | `events.user_id` **NULL** | `create`, `connect`, `disconnect` | — |
| `User` | `Orders` | O2M | `orders.user_id` NOT NULL | `create` | **D8** |
| `User` | `Categories` | M2M | `user_categories` | `create`, `connect`, `disconnect` | — |
| `Category` | `Users` | M2M | `user_categories` | `create`, `connect`, `disconnect` | **not in the doc's table** — reverse of the above |
| `Category` | `Products` | O2M | `products.category_id` NOT NULL | `create` | **not in the doc's table** |
| `Order` | `OrderItems` | O2M | `order_items.order_id` NOT NULL | `create` | **not in the doc's table** |
| `Product` | `OrderItems` | O2M | `order_items.product_id` NOT NULL | `create` | — |
| `Asset` | `Documents` | M2M | `asset_document_links` | `create`, `connect`, `disconnect` | **not in the doc's table, and it contradicts it** — see below |
| `Document` | `Assets` | M2M | `asset_document_links` | `create`, `connect`, `disconnect` | **not in the doc's table** |
| `WorkspaceNote` | `Children` | O2M **self-referential** | `workspace_notes.parent_id` **NULL** | `create`, `connect`, `disconnect` | **not in the doc at all** — see below |
| `Profile` | `Users` | belongs-to | `profiles.user_id` on parent | none | **NW-D5** |
| `Asset` | `Attachments` | O2M | `documents.entity_id` | none | `filter:` — **E2** |
| `Asset` | `Invoices` | O2M | `documents.entity_id` | none | `filter:` — **E2** |
| `Asset` | `PhotoAttachments` | O2M | `documents.entity_id` | none | `filter:` uninvertible — **C4** |
| `Asset` | `PrimaryDocument` | O2O has-one | `documents.entity_id` | none | `filter:` — **E2** |
| `Asset` | `PrimaryActiveDocument` | O2O has-one | `documents.entity_id` | none | `filter:` uninvertible — **C4** |
| `Asset` | `LinkedAttachments` | M2M | `asset_document_links` | none | `filter:` uninvertible — **C4** |

Two rows change the picture.

**`Asset.Documents` / `Document.Assets` are fully eligible today.** The design doc's fixture
table says `assets` emits *"none until migrated to `discriminator:`"*. That is true of the six
config-declared, filtered edges — and false of the seventh. `asset_document_links` carries real
FKs to both `assets` and `documents` (`schema.sql:156-159`), so the parser auto-detects a plain
M2M in each direction with **no filter**, and both pass every eligibility rule unchanged. So the
`assets` half of the fixture already exercises M2M nesting on a **UUID-PK target**, which
`User.Categories` (an `int64` PK) does not. That is free coverage of the PK-type axis, and §4.2
compiles it.

**`WorkspaceNote.Children` is the most verb-complete edge in the fixture and the design never
mentions its shape.** It is simultaneously:

- **self-referential** — parent table *is* target table (`workspace_notes.parent_id REFERENCES workspace_notes(id)`),
- on a **nullable** FK, so all three verbs are eligible,
- on a **tenanted** table (`workspace_id`, §29.2.3 auto-detected),
- and its client already holds the self-reference it needs: `workspaceNoteClient.workspaceNoteClient *workspaceNoteClient` (`models_gen.go:29149`), wired at `client_gen.go:191`.

Self-reference brings one hazard nothing in the design closes: a `connect` naming **the parent's
own id** writes `parent_id = id`, a self-loop, and a `connect` naming an ancestor closes a cycle.
Neither is caught by **D7** (an unparented ancestor is a legitimate adopt) nor by **D13** (one
verb, one target). §4.3 shows the guard and §9 proposes it as a rule.

---

## 2.1 The verb vocabulary, checked against the field

`create` / `connect` / `disconnect` / `clear` was settled on 2026-09-11 (**D1**, **D17**) after
comparing the three tools that ship this surface. Recorded here because the design doc decided
the names without citing anyone.

| | associate existing | create + associate | unlink some | unlink all |
|---|---|---|---|---|
| **Prisma** | `connect`, `connectOrCreate` | `create` | **`disconnect`** | `set: []` |
| **ent** (entgql) | `addXIDs` | — **none** | `removeXIDs` | **`clearX`** |
| **Hasura** | — none | nested `{ data: … }` | — none | — |
| **sqlgen** | `connect` | `create` | `disconnect` | `clear` |

ent's row is from their generated schema, not docs prose: `UpdateUserInput` is literally
`addGroupIDs` / `removeGroupIDs` / `clearGroups`, and `CreateUserInput` is a bare `groupIDs: [ID!]`
with no verb at all.

Three conclusions, and two of them changed the design:

1. **`create` stays, and it is the differentiator.** Prisma has it, Hasura has it, **ent does
   not** — entgql associates by ID only; the `createChildren` in their tutorial is a custom
   extension the user writes. Dropping it would leave sqlgen at ent's level, which forfeits §1.2's
   whole value proposition.
2. **`remove` → `disconnect`** (**D1**). `connect` is Prisma's word and Prisma's inverse is
   `disconnect`; `remove` is ent's, and ent's pair is `add`/`remove` with no `connect` at all. So
   `connect` + `remove` matched neither tool, and `remove` is ambiguous between *unlink* and
   *destroy* — an ambiguity **D9** had to spend a decision cell disclaiming. `disconnect` needs no
   disclaimer.
3. **`clear` is added** (**D17**) — the one verb in the comparison sqlgen lacked.

Two decisions the design reached from first principles turn out to be independently confirmed:

- **Prisma: "disconnect is only available if the relation is optional."** That is **D8** exactly —
  FK nullability, not relationship type, decides the verb set. §1.4 calls that "the single fact
  that shapes §4".
- **Hasura processes nested inserts as object relationships → parent → array relationships**, and
  *forbids* supplying the FK alongside nested data. That is §1.3's shape-1-inverts-the-order
  finding plus the XOR check **NW-D5**'s deferral note proposes — so the belongs-to design this
  doc defers is the one Hasura already ships.

Sources: [Prisma relation queries](https://www.prisma.io/docs/orm/prisma-client/queries/relation-queries),
[Prisma Client reference](https://www.prisma.io/docs/orm/v7/reference/prisma-client-reference),
[ent mutation inputs](https://entgo.io/docs/tutorial-todo-gql-mutation-input/),
[ent generated schema](https://github.com/ent/contrib/blob/master/entgql/internal/todo/ent.graphql),
[Hasura nested insert](https://hasura.io/docs/2.0/mutations/postgres/insert/).

---

## 3. Go examples — the three `…WithRelated` families

All compiled. Stand-ins used, each of which is a finding: `probeUserCategoryClient` for the
un-wired junction client (**F8**), `probeCallbackMode` for the un-wired `callbackMode` (**F8**),
and a body-less `UpsertMany` (**F4**).

### 3.1 Input types — `User`, the three-shape parent

```go
type CreateUserWithRelatedInput struct {
	User       CreateUserInput             `json:"user"`
	Events     *UserEventsCreateNested     `json:"events,omitempty"`
	Orders     *UserOrdersCreateNested     `json:"orders,omitempty"`
	Categories *UserCategoriesCreateNested `json:"categories,omitempty"`
}

type UpdateUserWithRelatedInput struct {
	User       UpdateUserInput             `json:"user"`
	Events     *UserEventsUpdateNested     `json:"events,omitempty"`
	Orders     *UserOrdersUpdateNested     `json:"orders,omitempty"`
	Categories *UserCategoriesUpdateNested `json:"categories,omitempty"`
}

// D3 — the SAME update blocks, not upsert-specific ones.
type UpsertUserWithRelatedInput struct {
	User       CreateUserInput             `json:"user"`
	Events     *UserEventsUpdateNested     `json:"events,omitempty"`
	Orders     *UserOrdersUpdateNested     `json:"orders,omitempty"`
	Categories *UserCategoriesUpdateNested `json:"categories,omitempty"`
}

// D4 — the create block is a narrower TYPE. `disconnect` is not accepted-and-ignored;
// it is not expressible.
type UserEventsCreateNested struct {
	Create  []*CreateUserEventInput `json:"create,omitempty"`
	Connect []uuid.UUID             `json:"connect,omitempty"`
}

type UserEventsUpdateNested struct {
	Create  []*CreateUserEventInput `json:"create,omitempty"`
	Connect []uuid.UUID             `json:"connect,omitempty"`
	Disconnect []uuid.UUID            `json:"disconnect,omitempty"`
	Clear      bool                  `json:"clear,omitempty"`      // unlink ALL, runs FIRST (D17)
}

// D8 — orders.user_id is NOT NULL, so Connect and Remove are absent from BOTH
// blocks, and the two types are structurally identical. They stay distinct
// anyway: collapsing them would make the family non-uniform across edges.
type UserOrdersCreateNested struct {
	Create []*CreateUserOrderInput `json:"create,omitempty"`
}

type UserOrdersUpdateNested struct {
	Create []*CreateUserOrderInput `json:"create,omitempty"`
}

// M2M — no FK to elide on the target, so `create` reuses CreateCategoryInput verbatim.
type UserCategoriesUpdateNested struct {
	Create  []*CreateCategoryInput `json:"create,omitempty"`
	Connect []int64                `json:"connect,omitempty"`
	Disconnect []int64                `json:"disconnect,omitempty"`
	Clear      bool                  `json:"clear,omitempty"`      // unlink ALL, runs FIRST (D17)
}

// C2 — CreateEventInput minus the traversed FK. Field list elided.
type CreateUserEventInput struct {
	ID          omittable.Value[uuid.UUID]          `json:"id,omitzero"`
	Action      string                              `json:"action"`
	OccurredAt  types.DateTime                      `json:"occurred_at"`
	ProcessedAt omittable.Value[types.NullDateTime] `json:"processed_at,omitzero"`
	// …
}
```

### 3.2 The per-edge verb executor — O2M on a nullable FK

Factoring each edge into its own method is what makes **D3** literally true rather than
aspirational: `UpdateWithRelated` and `UpsertWithRelated` call the *same* function, so the update
block cannot drift between them. This is the shape the template should emit.

```go
// Edge: User.Events — O2M, events.user_id NULLABLE → all four verbs.
func (c *userClient) applyUserEventsUpdateNested(ctx context.Context, parent *User, nested *UserEventsUpdateNested, options CallOptions[UserFieldOptions]) error {
	if nested == nil {
		return nil
	}

	// D13 — validation before any statement runs. Clear subsumes Disconnect, so
	// the intersection is only a conflict when Clear is off: with Clear on,
	// Disconnect is ignored and cannot contradict anything (D17).
	if !nested.Clear && len(nested.Connect) > 0 && len(nested.Disconnect) > 0 {
		removing := make(map[uuid.UUID]bool, len(nested.Disconnect))
		for _, v := range nested.Disconnect {
			removing[v] = true
		}
		for _, v := range nested.Connect {
			if removing[v] {
				return nestedErr("events", "connect", v, ErrNestedVerbConflict)
			}
		}
	}
	// D13's non-vacuous half: a `create` carrying an explicit PK.
	if len(nested.Create) > 0 && len(nested.Disconnect) > 0 {
		removing := make(map[uuid.UUID]bool, len(nested.Disconnect))
		for _, v := range nested.Disconnect {
			removing[v] = true
		}
		for _, n := range nested.Create {
			if n == nil {
				continue
			}
			if id, ok := n.ID.Get(); ok && removing[id] {
				return nestedErr("events", "create", id, ErrNestedVerbConflict)
			}
		}
	}

	// clear — FIRST, and the ordering is load-bearing. After create or connect it
	// would wipe the rows those verbs had just linked. First, it makes
	// `clear + create` and `clear + connect` mean "replace the set with exactly
	// these" — what a `set` verb would do (rejected, §10) — except the caller
	// spells out both halves, so a partially-populated input cannot silently
	// mass-unlink. One UPDATE, parent-scoped, no id list, no read: structurally
	// incapable of touching another parent's rows, exactly as disconnect is (D6).
	if nested.Clear {
		pid := parent.ID.String()
		if _, err := c.eventClient.UpdateWhere(ctx,
			&EventFilter{UserID: &comparator.NullableID{ID: comparator.ID{Eq: &pid}}},
			&UpdateEventInput{UserID: omittable.Set(uuid.NullUUID{})}, // → NULL
			func(o *CallOptions[EventFieldOptions]) {
				nestedChildOptions(o, options)
				o.FieldOptions = &EventFieldOptions{}
			}); err != nil {
			return fmt.Errorf("events: clear: %w", err)
		}
	}

	if len(nested.Create) > 0 {
		inputs := make([]*CreateEventInput, 0, len(nested.Create))
		for _, n := range nested.Create {
			if n == nil {
				continue
			}
			inputs = append(inputs, &CreateEventInput{
				ID:     n.ID,
				UserID: omittable.Set(uuid.NullUUID{UUID: parent.ID, Valid: true}), // nullable FK
				Action: n.Action,
				// …mechanical field copies…
			})
		}
		if _, err := c.eventClient.CreateMany(ctx, inputs, func(o *CallOptions[EventFieldOptions]) {
			nestedChildOptions(o, options)
			o.FieldOptions = &EventFieldOptions{} // C6 — skip the re-fetch
		}); err != nil {
			return fmt.Errorf("events: create: %w", err)
		}
	}

	if len(nested.Connect) > 0 {
		ids := make([]string, 0, len(nested.Connect))
		for _, v := range nested.Connect {
			ids = append(ids, v.String())
		}
		// `Limit: new(0)` is load-bearing, not decoration: get.go.tmpl:229-235
		// reads *0 as "no LIMIT clause" AND skips the client's default
		// queryLimit. Passing nil instead would let queryLimit truncate the
		// visibility read on a large connect and report real, visible rows as
		// NOT_FOUND.
		visible, err := c.eventClient.GetMany(ctx, &GetEventsInput{
			Filter: &EventFilter{ID: &comparator.ID{In: ids}},
			Limit:  new(0),
		}, func(o *CallOptions[EventFieldOptions]) {
			nestedChildOptions(o, options)
			o.FieldOptions = &EventFieldOptions{ID: true, UserID: true}
			o.LockMode = sql.LockNone
		})
		if err != nil {
			return fmt.Errorf("events: connect: %w", err)
		}
		owner := make(map[uuid.UUID]uuid.NullUUID, len(visible))
		for _, row := range visible {
			owner[row.ID] = row.UserID
		}

		adopt := make([]string, 0, len(nested.Connect))
		for _, eid := range nested.Connect {
			cur, ok := owner[eid]
			switch {
			case !ok:
				// Absent, another tenant's, or soft-deleted — indistinguishable
				// to this caller, and all NOT_FOUND. Never CONFLICT (P13).
				return nestedErr("events", "connect", eid, ErrNotFound)
			case cur.Valid && cur.UUID == parent.ID:
				continue // already ours — the end state holds (D3/D7)
			case cur.Valid:
				return nestedErr("events", "connect", eid, ErrAlreadyRelated)
			}
			adopt = append(adopt, eid.String())
		}

		if len(adopt) > 0 {
			// The `fk IS NULL` guard makes the adoption atomic against a
			// concurrent connect that parented the row after the read above.
			adopted, err := c.eventClient.UpdateWhere(ctx,
				&EventFilter{
					ID:     &comparator.ID{In: adopt},
					UserID: &comparator.NullableID{Null: new(true)},
				},
				&UpdateEventInput{UserID: omittable.Set(uuid.NullUUID{UUID: parent.ID, Valid: true})},
				func(o *CallOptions[EventFieldOptions]) {
					nestedChildOptions(o, options)
					o.FieldOptions = &EventFieldOptions{ID: true}
				})
			if err != nil {
				return fmt.Errorf("events: connect: %w", err)
			}
			if len(adopted) != len(adopt) {
				return nestedErr("events", "connect", nil, ErrAlreadyRelated)
			}
		}
	}

	// Ignored when clear ran (D17): every id it names is already unlinked, so the
	// requested end state holds. Silent, not an error — the same reasoning as
	// D7's already-ours no-op and Q2's silent-miss convention.
	if !nested.Clear && len(nested.Disconnect) > 0 {
		ids := make([]string, 0, len(nested.Disconnect))
		for _, v := range nested.Disconnect {
			ids = append(ids, v.String())
		}
		pid := parent.ID.String()
		if _, err := c.eventClient.UpdateWhere(ctx,
			&EventFilter{
				ID:     &comparator.ID{In: ids},
				UserID: &comparator.NullableID{ID: comparator.ID{Eq: &pid}}, // the parent guard
			},
			&UpdateEventInput{UserID: omittable.Set(uuid.NullUUID{})}, // → NULL
			func(o *CallOptions[EventFieldOptions]) {
				nestedChildOptions(o, options)
				o.FieldOptions = &EventFieldOptions{}
			}); err != nil {
			return fmt.Errorf("events: disconnect: %w", err)
		}
	}
	return nil
}
```

### 3.3 The per-edge verb executor — O2M on a NOT NULL FK (`create` only)

```go
// Edge: User.Orders — D8. No connect (nothing is ever unparented), no remove
// (would violate the constraint). The verb set is computed from FKNullable,
// not from RelationshipType — Events and Orders are the same type.
func (c *userClient) applyUserOrdersUpdateNested(ctx context.Context, parent *User, nested *UserOrdersUpdateNested, options CallOptions[UserFieldOptions]) error {
	if nested == nil || len(nested.Create) == 0 {
		return nil
	}
	inputs := make([]*CreateOrderInput, 0, len(nested.Create))
	for _, n := range nested.Create {
		if n == nil {
			continue
		}
		inputs = append(inputs, &CreateOrderInput{
			ID:     n.ID,
			UserID: parent.ID, // NOT NULL FK, assigned bare — not omittable.Set
			Notes:  n.Notes,
		})
	}
	if _, err := c.orderClient.CreateMany(ctx, inputs, func(o *CallOptions[OrderFieldOptions]) {
		nestedChildOptions(o, options)
		o.FieldOptions = &OrderFieldOptions{}
	}); err != nil {
		return fmt.Errorf("orders: create: %w", err)
	}
	return nil
}
```

### 3.4 The per-edge verb executor — M2M

```go
// Edge: User.Categories — M2M via user_categories.
func (c *userClient) applyUserCategoriesUpdateNested(ctx context.Context, parent *User, nested *UserCategoriesUpdateNested, options CallOptions[UserFieldOptions]) error {
	if nested == nil {
		return nil
	}

	if !nested.Clear && len(nested.Connect) > 0 && len(nested.Disconnect) > 0 { /* D13 — as above */ }

	// clear — FIRST, as on the O2M edge. HardDeleteWhere filtered to this
	// parent's local FK: one statement, no id list, no read, and the filter IS
	// the parent scope. Hard delete is correct because user_categories carries
	// no soft-delete column; a junction that did would emit neither clear nor
	// disconnect (E11).
	if nested.Clear {
		pid := parent.ID.String()
		if err := c.userCategoryClient.HardDeleteWhere(ctx,
			&UserCategoryFilter{UserID: &comparator.ID{Eq: &pid}},
			func(o *CallOptions[UserCategoryFieldOptions]) {
				nestedChildOptions(o, options)
			}); err != nil {
			return fmt.Errorf("categories: clear: %w", err)
		}
	}

	targetIDs := make([]int64, 0, len(nested.Create)+len(nested.Connect))

	if len(nested.Create) > 0 {
		created, err := c.categoryClient.CreateMany(ctx, nested.Create, func(o *CallOptions[CategoryFieldOptions]) {
			nestedChildOptions(o, options)
			o.FieldOptions = &CategoryFieldOptions{ID: true} // PK-only — junction rows need the generated PKs
		})
		if err != nil {
			return fmt.Errorf("categories: create: %w", err)
		}
		for _, row := range created {
			targetIDs = append(targetIDs, row.ID)
		}
	}

	// NW-D13 — the junction FK proves existence, not visibility. Reading
	// through the target's OWN client applies its tenant filter, soft-delete
	// default and access rules.
	if len(nested.Connect) > 0 {
		visible, err := c.categoryClient.GetMany(ctx, &GetCategoriesInput{
			Filter: &CategoryFilter{ID: &comparator.Number[int64]{In: nested.Connect}},
			Limit:  new(0),
		}, func(o *CallOptions[CategoryFieldOptions]) {
			nestedChildOptions(o, options)
			o.FieldOptions = &CategoryFieldOptions{ID: true}
			o.LockMode = sql.LockNone // §13 of the design doc — measured, not stylistic
		})
		if err != nil {
			return fmt.Errorf("categories: connect: %w", err)
		}
		found := make(map[int64]bool, len(visible))
		for _, row := range visible {
			found[row.ID] = true
		}
		for _, tid := range nested.Connect {
			if !found[tid] {
				return nestedErr("categories", "connect", tid, ErrNotFound)
			}
		}
		targetIDs = append(targetIDs, nested.Connect...)
	}

	// UpsertMany, not CreateMany (F4/F6). The junction is a pure link table, so
	// this compiles to ON CONFLICT DO NOTHING (F7) on every dialect — which §6.1
	// measures as the one conflict shape that tolerates an in-statement duplicate
	// on PostgreSQL. The dedupe below is therefore belt-and-braces HERE and
	// mandatory in UpsertMany itself (§6.1).
	if len(targetIDs) > 0 {
		links := make([]*CreateUserCategoryInput, 0, len(targetIDs))
		seen := make(map[int64]bool, len(targetIDs))
		for _, tid := range targetIDs {
			if seen[tid] {
				continue
			}
			seen[tid] = true
			links = append(links, &CreateUserCategoryInput{UserID: parent.ID, CategoryID: tid})
		}
		if _, err := c.userCategoryClient.UpsertMany(ctx, links, UserCategoryConflictPK, func(o *CallOptions[UserCategoryFieldOptions]) {
			nestedChildOptions(o, options)
			o.FieldOptions = &UserCategoryFieldOptions{}
		}); err != nil {
			return fmt.Errorf("categories: link: %w", err)
		}
	}

	// remove — no visibility read (D6). The junction PK embeds the parent, so a
	// caller can only ever delete its own links, and a miss is a no-op.
	if !nested.Clear && len(nested.Disconnect) > 0 {
		pks := make([]UserCategoryPK, 0, len(nested.Disconnect))
		seen := make(map[int64]bool, len(nested.Disconnect))
		for _, tid := range nested.Disconnect {
			if seen[tid] {
				continue
			}
			seen[tid] = true
			pks = append(pks, UserCategoryPK{UserID: parent.ID, CategoryID: tid})
		}
		if err := c.userCategoryClient.HardDeleteMany(ctx, pks, func(o *CallOptions[UserCategoryFieldOptions]) {
			nestedChildOptions(o, options)
		}); err != nil {
			return fmt.Errorf("categories: unlink: %w", err)
		}
	}
	return nil
}
```

### 3.5 The three methods

Once the edges are factored out, the three methods differ in **exactly one statement** each.

```go
func (c *userClient) UpdateWithRelated(ctx context.Context, id uuid.UUID, input *UpdateUserWithRelatedInput, opts ...func(*CallOptions[UserFieldOptions])) (*User, error) {
	options := resolveCallOptions(opts)

	var user *User
	err := database.WithTransaction(ctx, c.querier, "update user with related", func(ctx context.Context) error {
		// ── the ONLY line that differs across the three families ──
		parent, err := c.Update(ctx, id, &input.User, func(o *CallOptions[UserFieldOptions]) {
			*o = options
			o.FieldOptions = userNestedParentFieldOptions(options.FieldOptions)
			o.LockMode = sql.LockNone
		})
		// ──────────────────────────────────────────────────────────
		if err != nil {
			return err
		}
		user = parent

		if err := c.applyUserEventsUpdateNested(ctx, parent, input.Events, options); err != nil {
			return err
		}
		if err := c.applyUserOrdersUpdateNested(ctx, parent, input.Orders, options); err != nil {
			return err
		}
		if err := c.applyUserCategoriesUpdateNested(ctx, parent, input.Categories, options); err != nil {
			return err
		}

		// Terminal read — only when the caller selected a relationship.
		if userSelectsRelationship(options.FieldOptions) {
			reloaded, err := c.Get(ctx, parent.ID, func(o *CallOptions[UserFieldOptions]) {
				*o = options
				o.SkipHooks = true
				o.LockMode = sql.LockNone
			})
			if err != nil {
				return err
			}
			user = reloaded
		}
		return nil
	}, database.TxOptions{CallbackMode: c.callbackMode})
	if err != nil {
		return nil, fmt.Errorf("update user with related: %w", err)
	}
	return user, nil
}
```

`UpsertWithRelated` substitutes:

```go
		parent, err := c.Upsert(ctx, &input.User, target, func(o *CallOptions[UserFieldOptions]) {
			*o = options
			o.FieldOptions = userNestedParentFieldOptions(options.FieldOptions)
			o.LockMode = sql.LockNone
		})
```

`CreateWithRelated` substitutes `c.Create(ctx, &input.User, …)` and widens the narrower create
blocks into the same executors — sound precisely because `Disconnect` is absent **by type** (**D4**),
so the widening cannot smuggle a verb in:

```go
		if n := input.Events; n != nil {
			if err := c.applyUserEventsUpdateNested(ctx, parent, &UserEventsUpdateNested{
				Create: n.Create, Connect: n.Connect,
			}, options); err != nil {
				return err
			}
		}
```

### 3.6 The two shared helpers

```go
// Generic in BOTH directions — the parent's field-options type varies per
// parent, so the design doc's single-type-parameter version compiles only for
// userClient.
func nestedChildOptions[FO, PFO any](o *CallOptions[FO], parent CallOptions[PFO]) {
	o.SkipCache, o.SkipEvents = parent.SkipCache, parent.SkipEvents
	o.SkipHooks, o.SkipTenancy = parent.SkipHooks, parent.SkipTenancy
	o.Tenant = parent.Tenant
}

// Relationship members are dropped — loading them on the parent write would run
// before the nested rows exist and return a stale set. The PK is forced on
// because every nested write keys on it. A nil selection passes through: the
// parent reads that as "every column".
func userNestedParentFieldOptions(fo *UserFieldOptions) *UserFieldOptions {
	if fo == nil {
		return nil
	}
	scalars := *fo
	scalars.Events, scalars.Orders, scalars.Categories = nil, nil, nil
	scalars.ID = true
	return &scalars
}
```

---

## 4. Go examples — the shapes the design doc never compiled

### 4.1 O2M where the child carries a second required FK

`order_items` has two NOT NULL FKs. Nesting under `products` elides `product_id` and
**`OrderID` survives** — nesting under products says nothing about which order the item belongs
to. It reads as a surprise the first time and it is correct.

```go
type CreateProductOrderItemInput struct {
	ID        omittable.Value[uuid.UUID] `json:"id,omitzero"`
	OrderID   uuid.UUID                  `json:"order_id"` // survives — a DIFFERENT FK
	Quantity  omittable.Value[int32]     `json:"quantity,omitzero"`
	UnitPrice decimal.Decimal            `json:"unit_price"`
}

// …inside CreateWithRelated:
inputs = append(inputs, &CreateOrderItemInput{
	ID:        child.ID,
	OrderID:   child.OrderID, // caller-supplied
	ProductID: parent.ID,     // traversed FK — NOT NULL, assigned bare
	Quantity:  child.Quantity,
	UnitPrice: child.UnitPrice,
})
```

### 4.2 M2M on a UUID-PK target — `Asset.Documents`

The edge the design doc's fixture table says does not exist. Same executor shape as
`User.Categories`, different PK type, and it exercises `uuid.UUID` map keys and
`comparator.ID{In: []string}` instead of `comparator.Number[int64]{In: []int64}`:

```go
if len(nested.Connect) > 0 {
	ids := make([]string, 0, len(nested.Connect))
	for _, v := range nested.Connect {
		ids = append(ids, v.String()) // UUID targets stringify for the comparator
	}
	visible, err := c.documentClient.GetMany(ctx, &GetDocumentsInput{
		Filter: &DocumentFilter{ID: &comparator.ID{In: ids}},
		Limit:  new(0),
	}, func(o *CallOptions[DocumentFieldOptions]) {
		nestedChildOptions(o, options)
		o.FieldOptions = &DocumentFieldOptions{ID: true}
		o.LockMode = sql.LockNone
	})
	// …
}

links = append(links, &CreateAssetDocumentLinkInput{AssetID: parent.ID, DocumentID: tid})
```

One wrinkle the fixture surfaces and no other edge does: an M2M `create` under `assets` reuses
`CreateDocumentInput` verbatim (no FK to elide), and that input **requires `EntityID` and
`EntityType`** — the polymorphic parent pointer, unrelated to the junction. So the caller must
supply them. That is correct — the junction and the polymorphic pointer are two independent
relationships into the same table — but it is the clearest example of why an M2M nested `create`
cannot be assumed to be "the target's create input minus nothing, therefore trivial".

### 4.3 Self-referential O2M on a nullable FK, on a tenanted table — `WorkspaceNote.Children`

```go
func (c *workspaceNoteClient) UpdateWithRelated(ctx context.Context, id uuid.UUID, input *UpdateWorkspaceNoteWithRelatedInput, opts ...func(*CallOptions[WorkspaceNoteFieldOptions])) (*WorkspaceNote, error) {
	// …parent Update, relationships stripped, as §3.5…

	if nested := input.Children; nested != nil {
		// Self-referential guard the design owes but does not state: a connect
		// naming the parent itself writes parent_id = id. D7 does not catch it
		// (the parent's own parent_id may legitimately be NULL, so it looks
		// like an adoptable orphan) and D13 does not catch it (one verb, one
		// target).
		for _, cid := range nested.Connect {
			if cid == parent.ID {
				return nestedErr("children", "connect", cid, ErrNestedVerbConflict)
			}
		}

		if len(nested.Create) > 0 {
			inputs := make([]*CreateWorkspaceNoteInput, 0, len(nested.Create))
			for _, n := range nested.Create {
				if n == nil {
					continue
				}
				inputs = append(inputs, &CreateWorkspaceNoteInput{
					ID:          n.ID,
					ParentID:    omittable.Set(uuid.NullUUID{UUID: parent.ID, Valid: true}),
					Body:        n.Body,
					PinnedOrder: n.PinnedOrder,
					// WorkspaceID deliberately UNSET — §29 auto-sets the tenant
					// from the ctx-cached resolver. Setting it here would
					// re-derive a fact the tenancy layer owns.
				})
			}
			// c.workspaceNoteClient is the SELF reference, already wired
			// (client_gen.go:191). No new client field is needed for this edge.
			if _, err := c.workspaceNoteClient.CreateMany(ctx, inputs, /* … */); err != nil {
				return fmt.Errorf("children: create: %w", err)
			}
		}
		// connect / disconnect: identical to §3.2 with ParentID in place of UserID.
	}
	// …
}
```

Three things this shape pins that no other fixture edge does:

1. **The tenant column is never assigned by the nested write.** `CreateWorkspaceNoteInput.WorkspaceID` is `omittable.Value[uuid.UUID]`, and leaving it unset is what routes it through §29's auto-set. Routing children through the target's own client (**NW-D10**) is what makes that free.
2. **The self-edge needs no new client wiring.** **F8** says every M2M edge needs its junction client injected; a self-referential O2M needs nothing, because the client already points at itself.
3. **Cycles are reachable and unguarded.** The self-connect guard above covers depth 0. `A.connect(B)` where `B` is already `A`'s ancestor closes a longer cycle, which no depth-1 check can see. §9 records it as an open question rather than pretending the guard above closes it.

### 4.4 Belongs-to emits nothing — `Profile.Users`

Not an omission, a decision (**NW-D5**). `profiles.user_id` lives on the **parent**, so the
insert order inverts: the target must be written first, then the parent. The link already has a
spelling that works today and costs nothing:

```go
// The whole feature, for a belongs-to edge:
_, err := client.Profiles().Create(ctx, &models.CreateProfileInput{UserID: existingUserID, /* … */})
```

---

## 5. GraphQL examples

Three added mutations per eligible parent; the existing five stay byte-identical.

```graphql
input CreateUserWithRelatedInput {
  user:       CreateUserInput!
  events:     UserEventsCreateNested
  orders:     UserOrdersCreateNested
  categories: UserCategoriesCreateNested
}

input UpdateUserWithRelatedInput {
  user:       UpdateUserInput!
  events:     UserEventsUpdateNested
  orders:     UserOrdersUpdateNested
  categories: UserCategoriesUpdateNested
}

input UpsertUserWithRelatedInput {
  user:       CreateUserInput!
  events:     UserEventsUpdateNested
  orders:     UserOrdersUpdateNested
  categories: UserCategoriesUpdateNested
}

"D4 — no `disconnect` member: there is nothing to remove from a row that does not exist yet."
input UserEventsCreateNested {
  create:  [CreateUserEventInput!]
  connect: [UUID!]
}

input UserEventsUpdateNested {
  create:  [CreateUserEventInput!]
  connect: [UUID!]
  disconnect: [UUID!]
  clear:      Boolean
}

"orders.user_id is NOT NULL — connect and remove are not expressible on this edge (D8)."
input UserOrdersUpdateNested {
  create: [CreateUserOrderInput!]
}

input UserCategoriesUpdateNested {
  create:  [CreateCategoryInput!]
  connect: [Int!]
  disconnect: [Int!]
  clear:      Boolean
}

extend type Mutation {
  createUser(input: CreateUserInput!): User!
  createUserWithRelated(input: CreateUserWithRelatedInput!): User!              # new
  updateUser(id: UUID!, input: UpdateUserInput!): User!
  updateUserWithRelated(id: UUID!, input: UpdateUserWithRelatedInput!): User!   # new
  upsertUser(input: CreateUserInput!): User!
  upsertUserWithRelated(                                                        # new
    input: UpsertUserWithRelatedInput!
    conflictTarget: UserConflictTarget = PK                                     # Q4
  ): User!
  # …unchanged…
}
```

Query:

```graphql
mutation {
  updateUserWithRelated(id: "3f1d…", input: {
    user:       { name: "Ada" }
    categories: { connect: [7, 9], create: [{ name: "New Topic" }], disconnect: [3] }
    events:     { disconnect: ["a7c2…"] }
    # `clear` + `connect` is replace-the-set, spelled out (D17). Because clear
    # runs first, orders ends up linked to exactly 4 and 5 — and `disconnect`
    # here would be ignored, since clear already covers it.
    # orders:   { clear: true, connect: ["…4", "…5"] }
  }) {
    id
    name
    categories { id name }
  }
}
```

The response selection is served by the existing §26.5.2 field-options walker and the single
terminal `Get`, so relationship hydration costs nothing new.

**On the `conflictTarget` argument (Q4).** The asymmetry the design doc records is real and
verified: `upsertUser` hard-codes the PK conflict target at
`expected/graph/sqlgenresolver/resolvers_gen.go:2442` —

```go
res, err := m.Client.Users().Upsert(ctx, input, models.UserConflictPK, func(o *models.CallOptions[models.UserFieldOptions]) {
```

so on a table with a db- or app-generated PK where the caller supplies no id, it always takes the
insert branch. Harmless on a flat upsert; on the nested one it would mean `disconnect` silently
matches nothing, which is why the nested mutation takes the enum and the flat one is left alone.

### 5.1 Go call site

```go
u, err := client.Users().UpdateWithRelated(ctx, uid, &models.UpdateUserWithRelatedInput{
	User: models.UpdateUserInput{Name: omittable.Set("Ada")},
	Categories: &models.UserCategoriesUpdateNested{
		Connect: []int64{7, 9},
		Create:  []*models.CreateCategoryInput{{Name: "New Topic"}},
		Disconnect:  []int64{3},
	},
	Events: &models.UserEventsUpdateNested{
		Disconnect: []uuid.UUID{oldEventID}, // sets events.user_id = NULL
	},
	// Replace-the-set, without a `set` verb: clear runs before connect, so the
	// end state is exactly the two ids named (D17).
	// Orders: &models.UserOrdersUpdateNested{Clear: true, Connect: []uuid.UUID{a, b}},
}, func(o *models.CallOptions[models.UserFieldOptions]) {
	o.FieldOptions = &models.UserFieldOptions{
		ID: true, Name: true,
		Categories: &models.CategoryRelationshipOptions{
			FieldOptions: &models.CategoryFieldOptions{ID: true, Name: true},
		},
	}
})
```

Note the relationship member is a `*CategoryRelationshipOptions`, not a bare
`*CategoryFieldOptions` — the design doc's §5.4 call site writes the latter and would not compile
(`UserFieldOptions.Categories` is `*CategoryRelationshipOptions`, `models_gen.go:28849`).

---

## 6. Measured findings — `UpsertMany` is bigger than D5 says

`UpsertMany` is the feature's one hard dependency and has never been probed. Measured
2026-09-11 on `postgres:16-alpine`, `mysql:8.0`, and SQLite 3. All three findings are new.

**These are Ticket D's scope, not an argument against it.** Removing `UpsertMany` was considered
on 2026-09-11 and reversed the same day — it is confirmed in scope (**D5**). The alternative was
built and compiled: one `CreateMany` for links to freshly created targets, whose PKs cannot
already carry a junction row, plus the junction's shipped single-row `Upsert` per connected
target. It works, and it costs one statement and one `PublishBatch` per connected target, which
is the thing `UpsertMany` exists to collapse. Each finding below is therefore a decision the
ticket must make, and three of the four are places the three dialects disagree — which is
precisely why they are worth knowing before the template is written rather than after.

### 6.1 M1 — PostgreSQL rejects an in-statement duplicate conflict key; MySQL and SQLite accept it

```sql
-- pure link table, conflict target covers every column → DO NOTHING
INSERT INTO link (a,b) VALUES (1,1),(1,1),(1,2) ON CONFLICT (a,b) DO NOTHING;
-- payload table, conflict target does not cover every column → DO UPDATE
INSERT INTO payload (a,b,role) VALUES (1,1,'x'),(1,1,'y') ON CONFLICT (a,b) DO UPDATE SET role = excluded.role;
```

| Statement | PostgreSQL 16 | MySQL 8.0 | SQLite 3 |
|---|---|---|---|
| `DO NOTHING`, duplicate in batch | **OK** — 2 rows inserted | **OK** | **OK** |
| `DO UPDATE`, duplicate in batch | **ERROR: `ON CONFLICT DO UPDATE command cannot affect row a second time`** | **OK** — last wins (`role='y'`) | **OK** — last wins (`role='y'`) |

**Consequence.** `UpsertMany` must **dedupe its inputs by conflict target** before building the
statement, or it is a PostgreSQL-only 500 on any table whose conflict target does not cover every
column. That is every table except a pure link table — i.e. every use of `UpsertMany` other than
the one the nested feature needs. Since **D5** justifies `UpsertMany` as *"independently useful"*,
this is squarely in its scope, and it is a one-dialect divergence, which is the class
`guidelines/SQL.md` exists to catch.

Dedupe raises a second question the ticket must answer: **which row wins.** MySQL and SQLite
answer *last*; a dedupe pass must therefore keep the last occurrence, not the first, or sqlgen
makes PostgreSQL disagree with the other two on a statement all three accept.

### 6.2 M2 — `DO NOTHING` + `RETURNING` returns only the rows actually inserted

```sql
INSERT INTO link (a,b) VALUES (5,5) ON CONFLICT (a,b) DO NOTHING;              -- (5,5) now exists
INSERT INTO link (a,b) VALUES (5,5),(6,6),(7,7) ON CONFLICT (a,b) DO NOTHING RETURNING a,b;
```

PostgreSQL and SQLite both return **two** rows — `(6,6)` and `(7,7)`. `(5,5)` is absent.
`DO UPDATE` returns all three.

**Consequence.** `UpsertMany` returning `[]*T` **cannot promise one entity per input** on the
`DO NOTHING` path, and the caller cannot tell which input was dropped from the result alone. This
is the **F5** class at batch scale: single-row `Upsert` needed `resolveUpsertConflictRow`
(FIX-196) for exactly this branch. The batched form needs either the same fallback lookup or an
explicitly documented contract. For the nested M2M `add` it is harmless — §3.4 discards the
result — so this is a `UpsertMany`-ticket decision, not a nested-mutations blocker.

### 6.3 M3 — MySQL `LAST_INSERT_ID()` after a mixed batch names the first *newly inserted* row

```sql
INSERT INTO gen (k,v) VALUES ('a','1') ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), v = VALUES(v);
-- → id 1
INSERT INTO gen (k,v) VALUES ('a','2'),('b','3'),('c','4') ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), v = VALUES(v);
-- LAST_INSERT_ID() = 2, ROW_COUNT() = 4
```

Final table: `a→1, b→2, c→3`. `LAST_INSERT_ID()` is **2** — the id of `'b'`, the first row that
was genuinely inserted — not `1`, the id of the batch's first row. `ROW_COUNT()` is 4 for 3 rows
(MySQL counts an update as 2).

**Consequence.** **C5**'s fabrication rule (`firstID + int64(i)`, `create.go.tmpl:523-530`) is
**unsound for `UpsertMany`** on a db-generated PK: applied here it yields `a→2, b→3, c→4`, all
wrong. Junctions are unaffected — every junction PK column is caller-known, which is **F7**'s
point — but a general-purpose `UpsertMany` on MySQL cannot resolve per-row PKs at all. The
ticket must either restrict `UpsertMany`'s MySQL return contract or gate the operation on a
caller-known PK.

### 6.4 N1 — `sql/` is not "None"

The design doc's blast radius (§11) says `sql/` changes **None**, on the grounds that
"`UpsertMany` reuses `BuildInsert` and that clause unchanged". `BuildInsert` builds a
**single-row** INSERT (`sql/builder.go:152-182`). The batched builder is `BuildMultiInsert`
(`:184`), and `MultiInsertOptions` (`:53-57`) carries **only** `Columns`, `ValueRows`,
`ReturningColumns` — no `UpsertConflictKeys`, no `UpsertUpdateColumns`, no
`UpsertResolvePKColumn`.

So one of two things is true, and the ticket must pick:

- `UpsertMany` is **one statement** (what **D12** and §25.1 require) → `MultiInsertOptions` grows the three upsert fields and `BuildMultiInsert` emits `d.UpsertClause(...)`, exactly as `BuildInsert` does at `:164-170`. Small — roughly ten lines — but it is a **runtime-module edit**, and `sql/` is a core package under the CLAUDE.md "stdlib only" rule (satisfied: no new imports).
- `UpsertMany` is **N statements** → no `sql/` change, and the §25.1 one-statement-per-table contract that motivated **F4/D5** in the first place is broken.

The first is correct. The blast-radius row should read `sql/`: **`MultiInsertOptions` + `BuildMultiInsert` grow the upsert clause**, and `hook/` is then not the only runtime module touched.

---

## 7. Events and caching

Neither system knows these methods exist. **D16** gives the three `…WithRelated` methods no
`hook.MutationOp` of their own, so events and cache invalidation are exactly the composition of
the inner calls — the same composition the design doc's §1.2 hand-written baseline already
produces. That is the design working as intended, and most of it falls out correctly. Three
things do not.

### 7.1 What fires, and when

Every inner call — the parent `Update` / `Create` / `Upsert`, `CreateMany` per `create` verb,
`UpdateWhere` per O2M `connect` / `disconnect`, `UpsertMany` and `HardDeleteMany` on a junction — runs
its own hook chain under its own `hook.TableName` and `hook.MutationOp`. Both the event hook
(`event_hooks_gen.go:152-156`) and the cache hook (`cache_gen.go:4785-4799`) register their work
through `tx.OnCommit`, so nothing is published or evicted until the **root** transaction commits —
not the savepoint the nested method opened.

One `UpdateWithRelated` carrying three populated edges therefore produces, after the root commit,
in registration (FIFO) order:

| # | Table | Op | Action published | Cache effect |
|---|---|---|---|---|
| 1 | `users` | `OpUpdate` | `update` | `InvalidateMany([uid])` |
| 2 | `events` | `OpCreateMany` | `create` × N | **none** — §7.5 |
| 3 | `events` | `OpUpdateWhere` | `update` × N | `InvalidateMany(child ids)` — §7.3, since FIX-208 |
| 4 | `categories` | `OpCreateMany` | `create` × N | **none** — §7.5 |
| 5 | `user_categories` | `OpUpsertMany` | **`upsert_many`** — a new wire action | **nothing** — §7.4 |
| 6 | `user_categories` | `OpHardDeleteMany` | `delete` × N | `InvalidateMany(pairs)` |

Rows 3, 4 and 5 are the three findings below.

**`clear` (D17) lands on the `*Where` ops, so it inherited §7.3 on both shapes** — O2M `clear` is
`OpUpdateWhere` and M2M `clear` is `OpHardDeleteWhere`, and both took the pattern-wipe arm on a
non-tenanted table, which made M2M `clear` *less* cache-precise than M2M `disconnect`. FIX-208
removed the asymmetry at its source: both verbs now evict exactly the rows they touched, from the
`AffectedPKs` the statement already captures, so neither needs the extra read-then-`HardDeleteMany`
round-trip that would have bought precision at the cost of **D12**.

### 7.2 What is already correct by construction

Worth stating, because each is a way this could plausibly have broken silently and does not.

| Property | Evidence |
|---|---|
| **C6 does not suppress observability.** `m.AffectedPKs` is assigned *before* the empty-`FieldOptions` early return on every write template, so passing a nothing-selected `FieldOptions` to skip a child's re-fetch saves the read and keeps both the events and the invalidation | `create.go.tmpl:135` vs `:140`; `:388` vs `:396`; `update.go.tmpl:827` vs `:833` |
| **A failed nested mutation emits nothing.** `ROLLBACK TO SAVEPOINT` discards the callbacks registered at that depth only. If the nested method fails inside a caller's outer `WithTx` and the caller recovers, no phantom event or invalidation survives for children that were written and rolled back | `database/transaction.go:110-133` (contract), `:364-366` (implementation) |
| **No cached entity can go stale through a relationship.** PRD §27.6 — one key = one entity = one table, and *any* read with a relationship flag set is a full cache bypass, read and write. A relationship-loaded parent is never cacheable, so a nested write cannot make one stale. The §4.3 terminal `Get` runs only when the caller selected a relationship, so it is always on the bypass path | PRD §27.6, §27.7 |
| **The skip flags propagate.** `nestedChildOptions` copies `SkipCache`, `SkipEvents`, `SkipHooks`, `SkipTenancy` and `Tenant`; `resolveCallOptions` already makes `SkipHooks` imply the first two | §3.6; `shared_types_gen.go:50-54` |

The third row is the one that makes the whole area tractable: the obvious fear — "a nested write
changes `user.categories`, and some cached `User` still carries the old set" — is impossible by
construction, because such a `User` was never cacheable in the first place.

### 7.3 E1 — an O2M `connect` or `disconnect` cleared the target table's entire cache namespace

> **Resolved by FIX-208 (2026-09-16).** **EQ1** settled on candidate (a): the `*Where` ops were
> merged into the key-based invalidation arm, so all four evict one key per matched row. The
> analysis below is kept as the decision record — it is the reasoning the FIX was filed on, and
> the measurements that settled it are recorded in the FIX entry.

Both verbs compile to `UpdateWhere`, which lands on the `hook.OpUpdateWhere` arm. For a
**non-tenanted** table that arm ignored the PKs it was given and pattern-invalidated:

```go
case hook.OpUpdateWhere, hook.OpSoftDeleteWhere, hook.OpHardDeleteWhere, hook.OpRestoreWhere:
	switch table {
	case TableWorkspaceNotes:    // tenanted — precise
		return c.invalidateAffectedTenanted(ctx, table, affectedTenants, affected)
	case TableWorkspaceSettings: // tenanted — precise
		return c.invalidateAffectedTenanted(ctx, table, affectedTenants, affected)
	}
	pattern := cache.BuildTablePattern(prefix, schema, string(table))
	return c.invalidatePattern(ctx, table, pattern)
```

`cache_gen.go:5041-5061`. Unlinking one event evicts **every** cached `Event`.

The asymmetry is the observation, not the pattern wipe on its own:

- **M2M `disconnect`** routes through `HardDeleteMany` → `OpHardDeleteMany` → precise `InvalidateMany(affected)` (`cache_gen.go:5028-5040`). Same user-visible verb, three keys instead of a namespace.
- **A tenanted O2M** is precise too, so `WorkspaceNote.Children` pays nothing.
- **A shared O2M** — `User.Events`, the design doc's flagship nullable-FK edge — pays the wipe on both `connect` and `disconnect`.

**The pattern wipe is deliberate, which is why this is a question and not a bug.**
`BuildTablePattern` omits both the `fingerprint:` and `pk:` segments on purpose, "so pattern
invalidations clear every fingerprint generation AND every PK in a single pass"
(`cache/key.go:56-62`). It is collecting orphans left behind by a prior schema fingerprint, which
a precise `InvalidateMany` cannot do because it builds keys at the *current* fingerprint. Those
orphans are unreachable rather than wrong — a read at the new fingerprint never hits an
old-fingerprint key — so the trade is precision against garbage-collection latency, not against
correctness.

What makes it worth revisiting is that **the project has already made that same trade the other
way once.** The tenanted arm went precise to avoid cross-tenant eviction (PRD §29.5, T7),
accepting exactly the orphan-GC cost the shared arm still refuses. And the input a precise shared
arm needs is already in hand: `m.AffectedPKs` is populated on *every* `UpdateWhere` path —
`$captureTenant` gates `AffectedTenants` only, never `AffectedPKs` (`update.go.tmpl:827`, `:872`).
See **EQ1**.

### 7.4 E2 — the missing `OpUpsertMany` arms, and why F9 understates the event half

**F9** is right that three switches have permissive defaults and that omitting the arms compiles
clean and misbehaves at runtime. Two of its specifics are wrong, and the event one is materially
worse than described.

F9 says the §32.3 redaction switch "leaves `inputVal` nil (`event_hooks_gen.go:117-135`)". Those
lines are the **`assets`** hook, which has no access-redacted column and therefore no redaction at
all. The table that actually exercises §32.3 is `users`, and its default arm reads:

```go
				default:
					inputVal = mc.Input
```

`event_hooks_gen.go:1271-1272`. `UpsertMany` will set `mc.Input` to the whole input slice, as
`CreateMany` does (`create.go.tmpl:219`). So an `UpsertMany` with no arm publishes, for **each**
of the N fanned-out events:

1. **the entire batch**, not the row that event is about — O(N²) on the wire, and every event
   carries an input that is not its own; and
2. that batch **un-redacted**. `redactUserCreateInput` clears `PasswordHash` and `NewPassword`
   (`event_hooks_gen.go:1308-1317`), and the `default` arm never calls it.

The redaction helper's own doc comment says nil passes through "rather than leaking the raw
input" — the code anticipates precisely this leak, and the default arm walks into it. That makes
the missing arm a **§32.3 violation**, not a cosmetic gap, and Ticket D should carry it with that
tag and a failing-first regression pin.

`mapOpToAction`'s default (`event_hooks_gen.go:44-46`) returns `event.Action("upsert_many")` — a
new wire action no subscriber filters on. **D16** already calls for mapping it to the existing
`event.Upsert`; this is just the evidence that the default is reached.

The cache half is the quieter one: no arm → the switch falls through to `return nil`
(`cache_gen.go:5063`) → no invalidation at all. Harmless on a pure link table, because sqlgen does
no negative caching, so a junction row that was absent was never cached as absent. Not harmless
the moment `UpsertMany` is used on a table with real columns — which is **D5**'s entire
justification for adding it as a general-purpose operation.

### 7.5 E3 — nested writes never populate the cache

`Create` and `CreateMany` normally *set* the cache with the entity the database returned (PRD
§27.7). A nested create cannot: it passes a non-nil, nothing-selected `FieldOptions` to take the
C6 skip-refetch branch, which leaves `result` nil, and the cache's populate path is gated on
`fieldOptsSet == false` regardless (`cache_gen.go:4819-4825`).

Both halves agree, so this is consistent rather than broken. But a row created through
`CreateWithRelated` starts cold where the same row created through `CreateMany` starts warm, and
that is a real, measurable difference between two paths this feature otherwise presents as
equivalent. It belongs in §27.7 rather than being discovered. See **EQ4**.

### 7.6 Open questions

None of these gate a `/phase` breakdown. All should be answered before Tickets D and F land.

**They are now listed alongside the design doc's own open items in `docs/design/archive/NESTED_MUTATIONS.md`
§14**, added 2026-09-11 — that section is the single place to look before the breakdown, and it
carries two more the design could not answer alone (**Q9**, the parameter ceiling on
`connect`/`disconnect`; **Q10**, what "declared order" means) plus four decisions it had simply
never made (**D18–D21**).

| # | Question | Why it is open |
|---|---|---|
| **EQ1** | ~~Should the non-tenanted `OpUpdateWhere` cache arm become precise, or should O2M `connect`/`disconnect` avoid `UpdateWhere` altogether?~~ **Answered: (a), by FIX-208 (2026-09-16).** | §7.3. Settled by measurement rather than argument: the pattern wipe is a full-keyspace `SCAN` on both shipped backends and loses to precise eviction at every row count (including a `*Where` touching the whole table), while the orphan GC it bought is ~190 B per entry that expires on TTL and was never a sweep anything could rely on. **(b)** was rejected as a cliff every existing `UpdateWhere` caller pays; **(c)** stands rejected on **D12**. Landed as its own FIX ahead of this phase, so the feature inherits the repair rather than the cost |
| **EQ2** | Is the FIFO ordering of events across edges a contract, or an accident? | §7.1. Callbacks fire in registration order, which is declared-edge order, so a subscriber *can* rely on `users.update` preceding `events.create`. Nothing promises it. Pin it or explicitly disclaim it — silence is the one option that lets a consumer depend on it by accident |
| **EQ3** | Does a nested mutation need a correlation marker on `hook.MutationContext`? | Restates **Q6** with weight it did not have when it was deferred. One call now fans out across up to four tables, and neither a subscriber nor an audit hook can tell the resulting events were one logical operation, or distinguish a nested `disconnect` from an unrelated bulk FK null-out. The §1.2 baseline has the identical property, so nothing regresses — but the baseline never produced a six-row burst from one function call |
| **EQ4** | Should nested creates populate the cache? | §7.5. Warming them costs the C6 read back, which is the saving C6 exists for, so the answer is probably "no, and say so in §27.7". It is still a deliberate asymmetry between two paths, and undocumented asymmetries are the ones that get filed as bugs |
| **EQ5** | What does `UpsertMany` put in each fanned-out event's `Input` on the `DO NOTHING` branch? | §6.2 measured that `RETURNING` yields only the rows actually **inserted**, so `AffectedPKs` is shorter than the input slice and the index-aligned fanout (`inputVal = batchInputs[i]`, `event_hooks_gen.go:1247-1251`) **misaligns** — event *i* would carry input *i* for a PK that is not input *i*'s. The arm therefore cannot be a copy of `OpCreateMany`'s. Either `UpsertMany` correlates PKs back to inputs, or the per-row `Input` is nil on that branch and the PK carries the event alone, as the `*Many` delete ops already do. **Resolved in 27.5 (verified 2026-09-17): this premise no longer holds.** `UpsertMany` sources `AffectedPKs` from the inputs, or from `resolveUpsertManyRows`, which returns exactly one key per deduped value row and errors otherwise — never from `RETURNING` (**A19**) — and the terminal republishes `m.Input = rowInputs`, the deduped slice, so it stays index-aligned with `AffectedPKs` (PRD §28.9, commit `ad575c5`). 27.6's arm therefore **is** an `OpCreateMany`-shaped index arm; see **FIX-219**. |
| **EQ6** | Should `…WithRelated` support a *partial* skip — events on, cache off, or the reverse? | Today `nestedChildOptions` copies both flags wholesale, which is right. The question is whether someone will want "emit the events but do not thrash the cache" once §7.3 is understood, and whether that is a `CallOptions` field or a config knob. Deferrable — and materially less likely to be asked for now that **EQ1** resolved as (a) and the cache thrash is gone |

**Not an open question: whether to suppress events and caching for these methods entirely.**
Considered and rejected. `SkipCache` on a mutation does not merely skip population — PRD §27.7
is explicit that it means "no cache set on miss, **no invalidation on mutation**", and the hook
returns before `dispatchMutation` (`cache_gen.go:4793-4795`). Suppressing would leave a cached
`Event` carrying a `user_id` a nested `disconnect` had already nulled, served to every *subsequent*
flat `Get` until TTL. That is not "this operation bypasses the cache", it is this operation
corrupting the cache for every other operation; population and invalidation are not symmetric,
and only the first is ever safe to skip. Event suppression is sound but silent: a subscriber
materialising a read model would diverge with no error and no gap marker. And the premise does
not hold — the §1.2 hand-written baseline touches the same tables and fires the same events, so
suppression would make two paths with identical database effects differ in observability, which
is the divergence this feature exists to remove. A caller who wants a silent nested write already
has `SkipEvents` / `SkipCache`, propagated per §7.2; that choice belongs to the consumer, not to
the generator.

---

## 8. Possible today vs. must be built

### 8.1 Possible today — verified by compilation

Everything in §3 and §4 type-checks against the real generated package. To reproduce, assemble
the §3 and §4 listings into one file in the `models` package — they are complete apart from the
elided mechanical field copies — and build it:

```bash
cp zz_probe_nested_gen.go cmd/sqlgen/testdata/examples/graphql/models/
cd cmd/sqlgen/testdata/examples/graphql && GOWORK=off go build ./models/ && GOWORK=off go vet ./models/
rm cmd/sqlgen/testdata/examples/graphql/models/zz_probe_nested_gen.go   # not checked in
```

Three stand-ins are required, and each is a finding rather than a convenience: a package-level
`*userCategoryClient` and `*assetDocumentLinkClient` (**G6**), a package-level `callbackMode`
(**G6**), and a body-less `UpsertMany` on each junction client (**G1**).

Failing-first checks, each reverted after: the probe is genuinely type-checking, not compiling
vacuously.

**Re-run at HEAD 2026-09-16.** The transcripts changed with Phase 26, and one check stopped
failing — which is the finding, not a footnote.

| Mutation to the probe | Compiler response at HEAD |
|---|---|
| nullable O2M FK assigned bare — `UserID: omittable.Set(parent.ID)` | `cannot use omittable.Set(parent.ID) (value of struct type omittable.Value[uuid.UUID]) as omittable.Value[*uuid.UUID] value in struct literal` |
| NOT NULL O2M FK given a string field — `UserID: parent.Name` | `cannot use parent.Name (variable of type string) as uuid.UUID value in struct literal` |
| junction FK given the parent UUID — `CategoryID: parent.ID` | `cannot use parent.ID (variable of array type uuid.UUID) as int64 value in struct literal` |
| self-referential nullable FK assigned bare — `ParentID: omittable.Set(parent.ID)` | same shape as row 1 — `omittable.Value[uuid.UUID]` vs `omittable.Value[*uuid.UUID]` |
| M2M junction reference FK given a string — `DocumentID: parent.Name` | `cannot use parent.Name (variable of type string) as uuid.UUID value` |
| **unlink spelled as omit** — `omittable.Set[*uuid.UUID](nil)` → `omittable.Value[*uuid.UUID]{}` | **none. Builds clean.** Under `uuid.NullUUID` this was a type error; under a pointer the compiler accepts either form wherever the other belongs. This is the design doc's **F11** — a silent no-op on `disconnect` and `clear`, needing a runtime pin |

Every composed operation the executors call already exists and is correct for this use:

| Operation | Signature (graphql fixture) | Used for |
|---|---|---|
| `CreateMany` | `(ctx, []*CreateEventInput, opts…) ([]*Event, error)` | `create` on O2M / M2M targets |
| `GetMany` + `Limit: new(0)` | `(ctx, *GetEventsInput, opts…) ([]*Event, error)` | `connect` visibility read, un-truncated |
| `UpdateWhere` | `(ctx, *EventFilter, *UpdateEventInput, opts…) ([]*Event, error)` | O2M `connect` adopt + `disconnect` unlink |
| `HardDeleteMany` | `(ctx, []UserCategoryPK, opts…) error` | M2M `disconnect` — one statement (**F3**) |
| `Upsert` | `(ctx, *CreateUserCategoryInput, UserCategoryConflictTarget, opts…) (*UserCategory, error)` | the idempotent shape `UpsertMany` must batch (**F7**) |
| `database.WithTransaction` | `(ctx, Querier, name, fn, opts…) error` | savepoint nesting under an ambient tx (§18.3) |

### 8.2 Must be built

Confirmed still-open against `HEAD` today, each with the evidence re-checked rather than carried
over from the design doc.

| # | Gap | Evidence re-verified | Size |
|---|---|---|---|
| **G1** | `UpsertMany` does not exist on any client, in any template, or in `Operations` | `grep -rn UpsertMany cmd/sqlgen` → empty; `config/config.go:399-418` | **Large** — and larger than **D5** scopes it: §6.1 dedupe, §6.2 return contract, §6.3 MySQL PK, §6.4 `sql/` |
| **G2** | `sql.MultiInsertOptions` carries no conflict clause | `sql/builder.go:53-57`, `:184-223` | Small (~10 lines), but it is a **runtime-module** edit the blast radius currently denies |
| **G3** | `hook.MutationOp` has no `OpUpsertMany`; three generated switches fail *silently* without it | `hook/hook.go:17-32` — 16 constants, none is `upsert_many` | Small, but each consumer switch has a permissive default, so omission compiles clean and misbehaves at runtime (**F9**) |
| **G4** | `RelationshipContext` has no `FKOnTarget` | `grep -rn FKOnTarget cmd/sqlgen` → empty; the fact is still re-derived inline by column scan in `buildO2OJoinDetails` | Small — **NW-D1**, zero golden movement |
| **G5** | Single-row `Update` has no skip-refetch escape | `update.go.tmpl:81` and `:203` return `c.Get(...)` unconditionally; `HasSelectedColumns` appears at `:630` and below, all inside `UpdateMany`/`UpdateWhere`/delete variants | 3 lines (**D10**); valuable on its own |
| **G6** | No entity client holds a junction client, and none holds `callbackMode` | `userClient` has `categoryClient`/`eventClient`/`orderClient` but no `userCategoryClient` (`models_gen.go:26205-26224`); `callbackMode` lives only on `Client` (`client_gen.go:21`) | Small, additive — **and it generalizes past F8**: `assetClient` (`:1480-1498`) and `categoryClient` (`:3995-4011`) have the same hole for `asset_document_links` / `user_categories` |
| **G7** | No `discriminator:` in config | `config.TableRelationship` has `Filter` only (`config/config.go:562-574`) | Medium — **NW-D6**; independently useful |
| **G8** | No `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `NestedMutationError` | `database/errors.go:9-47` — eight sentinels, none of these | Small — **D15** |
| **G9** | `resolved_names.go` cannot see the new names | It keys on each entity's **primary** name and its own doc comment scopes out cross-shape suffix collisions as "a `go build` error" (`gen/resolved_names.go:41-47`) | Medium — see below |
| **G10** | `ExpandPreset` enumerates every operation per preset, in **five** value-returning arms plus an erroring `default` (`all`/`""` is one arm covering two spellings) | `config/config.go:1322-1374` | Small but **error-prone**: a field left out of an arm is a nil, not a default |

**G9 is worth spelling out**, because "register the new names in `resolved_names.go` (**C8**)" is
not sufficient. The nested types are keyed on **(parent, edge)**, not on a primary name, so they
are not injective in any key the registry currently holds: a `user_events` table and the
`User.Events` edge both resolve to `CreateUserEventInput`, and the registry's own scope-boundary
comment says that class "is not discovered here and remains a `go build` error". The feature
therefore needs a **new key kind** in the registry, not just new entries in an existing one.
No fixture collides today — the six (parent, edge) pairs resolve to `CreateUserEventInput`,
`CreateUserOrderInput`, `CreateProductOrderItemInput`, `CreateOrderOrderItemInput`,
`CreateCategoryProductInput`, `CreateWorkspaceNoteChildInput`, and no table takes any of those
names — so the hazard is latent, not live.

### 8.3 Dependency order

`G2 → G1 → G3` is forced (the builder before the template before the op). `G4`, `G5`, `G7`, `G8`,
`G10` are independent and each ships value alone. `G6` and `G9` land with the first emitter.

```
G2 ─→ G1 ─→ G3 ─┐
G4 ──────────────┤
G5 ──────────────┼─→ CreateWithRelated (+G6, G9) ─→ Update/UpsertWithRelated ─→ GraphQL
G7 ──────────────┤
G8, G10 ─────────┘
```

This matches the design doc's A ∥ B ∥ C ∥ D → E → F → G with `sql/` inserted ahead of Ticket D.

---

## 9. Suggested amendments to the design doc

Each is a correction or an addition this document's evidence forces. None changes a decision;
they change scope, coverage, or a factual claim.

> **CLOSED 2026-09-16 — the table below is now a historical ledger, not a backlog.** All twenty
> are applied to `docs/design/archive/NESTED_MUTATIONS.md`: A4, A8, A9, A12, A13 and A20 were applied earlier;
> A5, A15 and A19 were folded into the scope of the `UpsertMany` ticket when **D5** was confirmed
> in scope; A1, A2, A3, A6, A7, A10, A11, A14, A16 and A17 landed in the 2026-09-16 amendment
> pass. **A18 remains owed as a standalone FIX, not as a doc edit** — it asks for the §32.3
> redaction switch's `default` arm to fail closed, which is code, and it is not yet logged in
> `docs/tracker/fixes.md`. Rows are kept as written so a citation against any of them still
> resolves.

| # | Target | Amendment |
|---|---|---|
| **A1** | §4.1 fixture table | Add the eleven missing edges (§2). In particular `Asset.Documents` and `Document.Assets` are **fully eligible M2M edges today** — the row saying `assets` emits nothing is wrong, and the mistake costs real coverage: they are the only M2M edges in any fixture with a UUID-PK target |
| **A2** | §4.1 / §4.2 | Add the **self-referential** shape (`WorkspaceNote.Children`). Nullable FK + tenanted + self-referential, so it is the most verb-complete edge in the fixture. Add an eligibility or runtime rule: a `connect` naming the parent's own id is refused |
| **A3** | §10 deferred | Record **cycles on a self-referential edge** as an open question. The self-connect guard closes depth 0; `A.connect(B)` where `B` is `A`'s ancestor closes a longer cycle, and no depth-1 check can see it |
| **A4** | §11 blast radius, `sql/` row | Change **None** → `MultiInsertOptions` + `BuildMultiInsert` grow the upsert clause (§6.4). `hook/` is then not the only runtime-module edit |
| **A5** | §9 Ticket D | Widen. `UpsertMany` must also: dedupe inputs by conflict target, last-wins (§6.1, PostgreSQL-only failure); decide and document the `[]*T` return contract on the `DO NOTHING` branch (§6.2); decide the MySQL db-generated-PK contract (§6.3). Add the three probes as failing-first regression pins |
| **A6** | §3.1 **C8** / §9 | `resolved_names.go` needs a **new key kind**, not new entries: the nested type names are keyed on (parent, edge) and fall in the registry's documented cross-shape blind spot (§8.2 G9) |
| **A7** | §2.2 **F8** | Generalize. It is not one client — every entity client with an M2M edge lacks its junction client (`assetClient`, `categoryClient` too) |
| **A8** | §5.3 | `nestedChildOptions` must be generic in **both** type parameters. As written (`parent CallOptions[UserFieldOptions]`) it compiles only for `userClient` |
| **A9** | §5.4 call site | `UserFieldOptions.Categories` is `*CategoryRelationshipOptions`, not `*CategoryFieldOptions`; the example as written does not compile |
| **A10** | §4.3 execution order | Pin `Limit: new(0)` on the visibility read as **normative**, with the reason: `get.go.tmpl:229-235` reads `*0` as "no LIMIT" *and* skips the client's default `queryLimit`. The obvious `Limit: nil` would let `queryLimit` truncate the read and report visible rows as `NOT_FOUND` |
| **A11** | §5.3 / the emitter | Emit **one executor method per edge**, called by all three families, rather than three inlined copies. **D3** ("upsert reuses the update block verbatim") is then enforced by the compiler instead of by review |
| **A12** | header | Phase 26 is now *Standard Library UUID as First-Class*. Re-number to 27 or later |
| **A13** | §2.2 **F9** | Correct both specifics and re-tag the severity. The cited lines (`event_hooks_gen.go:117-135`) are the **`assets`** hook, which has no access-redacted column; the table that exercises §32.3 is `users`, whose default arm is `inputVal = mc.Input` (`:1271-1272`). A missing `OpUpsertMany` arm therefore publishes the **whole un-redacted batch** on every fanned-out event — including `PasswordHash` and `NewPassword` — not a nil payload. That is a **§32.3 violation**, and Ticket D should carry it as one (§7.4) |
| **A14** | §11 blast radius, new row | ~~Record the cache cost of the two O2M verbs~~ — **discharged by FIX-208 (2026-09-16)**, which took **EQ1** as (a) and merged the `*Where` ops into the key-based invalidation arm. The §11 row now records the resolution rather than a cost: `connect` and `disconnect` evict one key per matched row, the same precision M2M `disconnect` always had |
| **A15** | §9 Ticket D | Add the event-fanout arm explicitly, and note that it **cannot** be a copy of `OpCreateMany`'s: §6.2 measured that `DO NOTHING` + `RETURNING` returns only the inserted rows, so `AffectedPKs` is shorter than the input slice and the index-aligned fanout misaligns (**EQ5**). **Resolved in 27.5 (verified 2026-09-17): this premise no longer holds.** `UpsertMany` sources `AffectedPKs` from the inputs, or from `resolveUpsertManyRows`, which returns exactly one key per deduped value row and errors otherwise — never from `RETURNING` (**A19**) — and the terminal republishes `m.Input = rowInputs`, the deduped slice, so it stays index-aligned with `AffectedPKs` (PRD §28.9, commit `ad575c5`). 27.6's arm therefore **is** an `OpCreateMany`-shaped index arm; see **FIX-219**. |
| **A16** | new §9.9 / PRD §27.7 + §28 | State the observability contract for the nested methods, which is currently nowhere: what fires and in what order (§7.1), that a rolled-back savepoint emits nothing (§7.2), that a relationship-loaded parent is never cacheable so nesting cannot make one stale (§7.2), and that nested creates do **not** warm the cache the way flat ones do (§7.5). Two new PRD deltas — a §27.7 row and a §28 note |
| **A17** | §10 rejected | Record **suppressing events and caching for nested methods** as considered and rejected, with the reason: `SkipCache` on a mutation suppresses *invalidation*, not just population (PRD §27.7), so it would corrupt the cache for unrelated flat reads rather than merely bypassing it for this one (§7.6) |
| **A20** | §14 of the design doc | **Applied 2026-09-11.** The readiness audit that produced §14 also settled four gaps as decisions — **D18** (a `discriminator:` edge owns its column on the write side: nested `create` sets it, the child input elides it, `connect` matches on it), **D19** (a parent with no eligible edge emits nothing), **D20** (a `…_with_related` operation without its base operation is a hard config error) and **D21** (a nil `FieldOptions` returns relationship members unpopulated) — and amended **D12**, whose flat "one statement per verb" is false for `create` because `CreateMany` batches at `batchSize` (default 200). Recorded here so this table stays the full ledger |
| **A18** | §2.2 **F9** → also a standalone FIX | Ticket D adds the three `OpUpsertMany` arms, so this feature is covered. The **class** is not: any future op added without a redaction arm leaks the raw input the same way (§7.4). Make the redaction switch's `default` **fail closed** — publish nil rather than `mc.Input` — so omission degrades to no payload instead of an un-redacted one. Independent of nested mutations, and strictly better than relying on every future op author remembering |
| **A19** | §3.1 **C7** / §9 Ticket D | Pin the invariant the generated single-row junction `Upsert` already holds and `UpsertMany` must not lose: **`AffectedPKs` comes from the inputs, never from `RETURNING`** (`models_gen.go:25521`). §6.2 measured that `DO NOTHING` + `RETURNING` returns only the inserted rows, so sourcing them from the result would silently shorten `AffectedPKs`, skip invalidation for the unchanged rows, and misalign the event fanout (**EQ5**). **A19 was implemented as written in 27.5**, which is exactly what retired EQ5's premise — see **FIX-219**. |

---

## References

- `docs/design/archive/NESTED_MUTATIONS.md` — the design this document works
- `docs/tracker/STATUS.md` — Phase 26 is stdlib UUID; this work is 27+
- Fixture: `cmd/sqlgen/testdata/examples/graphql/{schema.sql,sqlgen.yml,models/models_gen.go}`
- `sql/builder.go:38-57,152-223` — `InsertOptions` vs `MultiInsertOptions` (§6.4)
- `hook/hook.go:17-32` — the `MutationOp` list `OpUpsertMany` must join
- `gen/resolved_names.go:11-47` — the collision registry and its scope boundary (§8.2 G9)
- `database/transaction.go:110-133`, `:364-366` — `OnCommit` ordering and at-depth rollback discard (§7.2)
- `cache/key.go:56-71` — why `BuildTablePattern` omits `fingerprint:` and `pk:` (§7.3)
- `event_hooks_gen.go:44-46`, `:1247-1272`, `:1308-1317` — `mapOpToAction`, the per-row `Input` fanout, and the §32.3 redact helpers (§7.4)
- PRD §27.6 (cache invariant), §27.7 (behavior by operation), §29.5 (tenant-scoped invalidation)
