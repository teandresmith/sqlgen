package gen_test

import (
	"strings"
	"testing"
)

// renderAPIErrors renders the api/errors template body. The template takes
// no context (the file has no per-table fanout) so a nil data argument is
// passed verbatim.
func renderAPIErrors(t *testing.T) string {
	t.Helper()
	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/errors", nil); err != nil {
		t.Fatalf("rendering api/errors: %v", err)
	}
	return buf.String()
}

func TestMapErrorToGQL_PerSentinel(t *testing.T) {
	// PRD §26.5.5 — the sentinel → extensions.code mapping must cover every
	// row of the table. Each entry pins both the user-facing message and the
	// extensions.code value as they appear inside a gqlerror.Error literal
	// in the rendered template body.
	out := renderAPIErrors(t)

	cases := []struct {
		name    string
		message string
		code    string
	}{
		{"ErrNotFound", `"not found"`, "NOT_FOUND"},
		{"ErrUniqueViolation", `"duplicate"`, "CONFLICT"},
		{"ErrForeignKeyViolation", `"fk constraint"`, "BAD_REFERENCE"},
		{"ErrCheckViolation", `"check failed"`, "INVALID_INPUT"},
		{"ErrNotNullViolation", `"missing required"`, "INVALID_INPUT"},
		{"ErrAlreadyRelated", `"already related"`, "CONFLICT"},
		{"ErrNestedVerbConflict", `"conflicting nested verbs"`, "INVALID_INPUT"},
		{"ErrMissing", `"tenant required"`, "UNAUTHENTICATED"},
		{"ErrMismatch", `"tenant mismatch"`, "FORBIDDEN"},
		{"Fallthrough", "err.Error()", "INTERNAL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustContain(t, out, "Message:    "+c.message)
			mustContain(t, out, "\"code\": \""+c.code+"\"")
		})
	}

	// Spot-check the structural shape — every mapping is wrapped in a
	// `gqlerror.Error{...}` literal with the `Extensions: map[string]any`
	// shape defined by gqlgen.
	mustContain(t, out, "&gqlerror.Error{")
	mustContain(t, out, "Extensions: map[string]any{\"code\": \"NOT_FOUND\"}")
}

func TestMapErrorToGQL_WrappedErrorsCaught(t *testing.T) {
	// PRD §26.5.5 — mapErrorToGQL must absorb wrapping (`fmt.Errorf("ctx:
	// %w", err)`). The contract is enforced at the generated-source level
	// by using `errors.Is` / `errors.As` rather than `==` or type assertions.
	// Sentinel errors come through `errors.Is`; constraint violations come
	// through `errors.As(*database.ConstraintError)` (which Unwraps via
	// ErrConstraintViolation, so wrapping is preserved).
	out := renderAPIErrors(t)

	mustContain(t, out, "errors.Is(err, database.ErrNotFound)")
	mustContain(t, out, "errors.Is(err, database.ErrAlreadyRelated)")
	mustContain(t, out, "errors.Is(err, database.ErrNestedVerbConflict)")
	mustContain(t, out, "errors.Is(err, tenancy.ErrMissing)")
	mustContain(t, out, "errors.Is(err, tenancy.ErrMismatch)")
	mustContain(t, out, "errors.As(err, &ce)")
	mustContain(t, out, "var ce *database.ConstraintError")

	// The four ConstraintError.Type cases must dispatch through a type
	// switch so wrapped *ConstraintError values are translated correctly.
	cases := []string{
		"case database.ConstraintUnique:",
		"case database.ConstraintForeignKey:",
		"case database.ConstraintCheck:",
		"case database.ConstraintNotNull:",
	}
	for _, c := range cases {
		mustContain(t, out, c)
	}

	// No `==` checks — those would not absorb wrapping.
	if strings.Contains(out, "err == database.") {
		t.Errorf("mapErrorToGQL must not use `==` for sentinel matching (breaks errors.Is wrapping)\n%s", out)
	}
}

func TestMapErrorToGQL_FallthroughIsInternal(t *testing.T) {
	// PRD §26.5.5 — any error not matched by the sentinel table maps to
	// INTERNAL with the original error's message preserved. The fall-through
	// `gqlerror.Error{...}` literal must be the last `&gqlerror.Error{` in
	// the function (so a non-matching error reaches it) and must use
	// `err.Error()` for the message.
	out := renderAPIErrors(t)

	mustContain(t, out, "Message:    err.Error()")
	mustContain(t, out, "Extensions: map[string]any{\"code\": \"INTERNAL\"}")

	// nil-input short circuit so callers can blindly wrap returns.
	mustContain(t, out, "if err == nil {")
	mustContain(t, out, "return nil")
}

// TestMapErrorToGQL_NestedVisibilityMissStaysNotFound pins the one ordering
// property PRD §9.9.8 and §26.5.5 both call out as deliberate rather than
// incidental: a nested `connect` whose target is not visible surfaces as
// database.ErrNotFound, and it must reach the NOT_FOUND arm rather than the
// *ConstraintError extraction that would report it as BAD_REFERENCE.
// BAD_REFERENCE would tell the caller the row exists and is merely out of
// reach, which is the cross-tenant existence oracle the visibility read was
// added to close.
//
// Source order is the mechanism, so source order is what is asserted. Both
// arms match a wrapped error, so a reordering would silently change the code
// a cross-tenant connect returns while every containment check above still
// passed.
func TestMapErrorToGQL_NestedVisibilityMissStaysNotFound(t *testing.T) {
	out := renderAPIErrors(t)

	notFound := strings.Index(out, "errors.Is(err, database.ErrNotFound)")
	constraint := strings.Index(out, "errors.As(err, &ce)")
	if notFound < 0 || constraint < 0 {
		t.Fatalf("mapErrorToGQL is missing the ErrNotFound arm (%d) or the ConstraintError arm (%d)", notFound, constraint)
	}
	if notFound > constraint {
		t.Errorf("the ErrNotFound arm must precede the *ConstraintError extraction, so a nested connect visibility miss maps to NOT_FOUND rather than BAD_REFERENCE; got ErrNotFound at %d, ConstraintError at %d", notFound, constraint)
	}

	// The nested sentinels sit between the two, so the same reordering would
	// also be caught from the other side.
	nested := strings.Index(out, "errors.Is(err, database.ErrAlreadyRelated)")
	if nested < 0 || nested < notFound || nested > constraint {
		t.Errorf("the nested-sentinel arms belong between the ErrNotFound arm (%d) and the *ConstraintError extraction (%d); got %d", notFound, constraint, nested)
	}
}

// TestMapErrorToGQL_NestedFailureNamesItsEdge pins PRD §26.5.5's structural
// attribution. The edge is read off *database.NestedMutationError.Edge — never
// parsed back out of the message — and it is attached in mapErrorToGQL itself,
// after the code is chosen, so a nested failure carries `extensions.path`
// whatever code it maps to: a duplicate child is CONFLICT and a missing FK
// target BAD_REFERENCE, and the caller needs the edge for both.
//
// The E2E example asserts the value on the wire per sentinel; this is the
// local gate on the shape, since that one is skipped under -short.
func TestMapErrorToGQL_NestedFailureNamesItsEdge(t *testing.T) {
	out := renderAPIErrors(t)

	mustContain(t, out, "var nme *database.NestedMutationError")
	mustContain(t, out, "errors.As(err, &nme)")
	mustContain(t, out, `gqlErr.Extensions["path"] = []string{nme.Edge}`)

	mapper := strings.Index(out, "func mapErrorToGQL(")
	path := strings.Index(out, `gqlErr.Extensions["path"]`)
	codes := strings.Index(out, "func gqlErrorForCode(")
	if mapper < 0 || codes < 0 || path < mapper || path > codes {
		t.Errorf("the path must be attached inside mapErrorToGQL (%d), which every resolver calls, rather than in one code arm; got path at %d, gqlErrorForCode at %d", mapper, path, codes)
	}
	if strings.Contains(out, "strings.") {
		t.Errorf("the mapper must not parse the edge out of the error message:\n%s", out)
	}
}
