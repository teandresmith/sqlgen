package gen

import (
	"errors"
	"fmt"
)

// ValidateAPIViewReadOnly asserts, at codegen time, that a view's API context
// can reach no mutation-emitting code path (PRD §16.4, §26.4 "Views on the
// GraphQL surface").
//
// It is a structural guard rather than a restatement of how buildAPIViewContext
// happens to fill the struct. Every mutation the schema, resolver and seed
// templates emit is gated on one of the fields below; the view builder leaves
// all of them at zero, and this lint is what turns "leaves them at zero today"
// into "cannot emit a mutation, checked". The failure it prevents is not a
// compile error — a view carrying, say, Operations.HardDelete would emit a
// `deleteProductSummary` mutation calling a client method that does not exist,
// and the surface would be wrong at exactly the layer PRD §16.4 says is
// read-only.
//
// Every violation is reported, not just the first, so a context built wrong in
// several places is diagnosed in one pass.
func ValidateAPIViewReadOnly(v APITableContext) error {
	if !v.IsView {
		return nil
	}

	var errs []error
	reject := func(cond bool, what string) {
		if cond {
			errs = append(errs, fmt.Errorf(
				"api: view %q reaches the %s mutation path — a view is read-only and emits no mutation, no Create/Update input, and no relationship loader (PRD §16.4 / §26.4)",
				v.SQLTable, what,
			))
		}
	}

	o := v.Operations
	reject(o.Create, "create")
	reject(o.CreateMany, "create-many")
	reject(o.Update, "update")
	reject(o.UpdateMany, "update-many")
	reject(o.UpdateWhere, "update-where")
	reject(o.Upsert, "upsert")
	reject(o.SoftDelete, "soft-delete")
	reject(o.HardDelete, "hard-delete")
	reject(o.Restore, "restore")
	reject(o.Increment, "increment")
	reject(v.HasCreateInput, "create-input")
	reject(v.HasUpdateInput, "update-input")
	reject(v.HasSoftDelete, "restore-eligibility")
	reject(v.HasConflictPK, "upsert-conflict-target")
	reject(v.IncrementEnumType != "", "increment-column-enum")
	reject(len(v.UpdateOps) > 0, "increment-operator")
	reject(len(v.CreateInputFields) > 0, "create-input-translator")
	reject(len(v.UpdateInputFields) > 0, "update-input-translator")

	for _, f := range v.Fields {
		if f.InCreateInput || f.InUpdateInput {
			errs = append(errs, fmt.Errorf(
				"api: view %q column %q claims membership in a mutation input — a view emits neither Create<V>Input nor Update<V>Input (PRD §16.4 / §26.4)",
				v.SQLTable, f.SQLName,
			))
		}
	}

	// Not a mutation path, but the same class of claim and checked here for the
	// same reason: §16.4 gives a view no relationships, and the §25.1
	// one-query-per-view-read guarantee is exactly the statement that its
	// field-selection walker has no relationship arm to descend into.
	if len(v.Relationships) > 0 {
		errs = append(errs, fmt.Errorf(
			"api: view %q carries %d relationship(s) — a view has none (PRD §16.4), and the one-query read guarantee (§25.1) depends on its walker having no relationship cases",
			v.SQLTable, len(v.Relationships),
		))
	}

	// mutationSurface is flatMutationSurface (what funcHasAnyMutation gates
	// on, IsView guard peeled off) plus the nested surface — whether any
	// `extend type Mutation` block, flat or nested, is emitted. Asserting it
	// last ties the field-level checks above to the one expression emission
	// turns on, so an operation added to it later is caught here even if
	// nobody extends the list above.
	//
	// It deliberately is NOT funcHasAnyMutation: that returns false for every
	// view by its own guard, so calling it would assert the guard against
	// itself and catch nothing.
	if mutationSurface(v) {
		errs = append(errs, fmt.Errorf(
			"api: view %q has a live mutation surface — without the IsView guard the schema template would emit an `extend type Mutation` block for a read-only entity (PRD §16.4 / §26.4)",
			v.SQLTable,
		))
	}

	return errors.Join(errs...)
}
