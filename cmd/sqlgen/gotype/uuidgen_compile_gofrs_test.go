package gotype_test

import (
	"testing"

	uuid "github.com/gofrs/uuid/v5"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgofrs"
)

// See uuidgen_compile_stdlib_test.go for what these three files are.
//
// This one closes a break rather than guarding against one. No example
// exercises uuidgofrs, so nothing in the tree ever compiled a gofrs-bound
// generated package, and the calls the generator emitted for one —
// google's uuid.New() and uuid.NewString(), neither of which gofrs has — could
// not build. Both of gofrs's constructors return (UUID, error), so every cell
// goes through the package's own Must.

func TestGofrsGenerationExpressionsCompile(t *testing.T) {
	values := []uuid.UUID{
		uuid.Must(uuid.NewV4()),
		uuid.Must(uuid.NewV7()),
	}
	texts := []string{
		uuid.Must(uuid.NewV4()).String(),
		uuid.Must(uuid.NewV7()).String(),
	}
	// The two literals above are the test: they compile only if the
	// expressions exist and return the declared element type. Rendering every
	// one keeps them live and checks the property each form shares.
	for _, v := range values {
		if got := len(v.String()); got != 36 {
			t.Errorf("value form rendered %q, length %d, want the 36-character canonical form", v.String(), got)
		}
	}
	for _, text := range texts {
		if len(text) != 36 {
			t.Errorf("string form produced %q, length %d, want the 36-character canonical form", text, len(text))
		}
	}

	// The parse call the GraphQL scalar unmarshalers emit (PRD §7.4 "Parsing
	// a UUID from a string"). Like the expressions above, this line IS the
	// test: it compiles only if the function exists with this name and returns
	// (uuid.UUID, error).
	parsed, err := uuid.FromString("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatalf("parse call returned an error on a valid UUID: %v", err)
	}
	if got := parsed.String(); got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("parse round-trip produced %q, want the input back", got)
	}

	integ := gotype.UUIDIntegrationFor(uuidgofrs.ImportPath)
	pins := []struct {
		field string
		got   string
		want  string
	}{
		{"V4", integ.V4, "uuid.Must(uuid.NewV4())"},
		{"V7", integ.V7, "uuid.Must(uuid.NewV7())"},
		{"V4String", integ.V4String, "uuid.Must(uuid.NewV4()).String()"},
		{"V7String", integ.V7String, "uuid.Must(uuid.NewV7()).String()"},
		{"ParseFunc", integ.ParseFunc, "uuid.FromString"},
	}
	for _, pin := range pins {
		if pin.got != pin.want {
			t.Errorf("UUIDIntegrationFor(%q).%s = %q, want %q — the expression compiled above", uuidgofrs.ImportPath, pin.field, pin.got, pin.want)
		}
	}
}
