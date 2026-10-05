package gotype_test

import (
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidstd"
)

// The three uuidgen_compile_*_test.go files each bind one UUID library to the
// local name `uuid` — legal because import names are file-scoped, and the only
// way to spell all three libraries' calls the way a generated file spells them,
// since every one of them binds `uuid`.
//
// Each file is half compile pin, half string pin. The slice literals are
// compiled against the real package, so a spelling that does not exist
// (google's Must against the standard library, gofrs's NewV4 against google)
// fails the build rather than a golden diff, and the slices' element types are
// what make the value/string split real: a string form in the []uuid.UUID
// literal would not compile either. The equality checks that follow tie those
// compiled expressions to the strings the generator actually emits.
//
// Edit the two halves together. They are deliberately textually identical, and
// a change to one alone is the bug this file exists to catch.

func TestStdlibGenerationExpressionsCompile(t *testing.T) {
	values := []uuid.UUID{
		uuid.NewV4(),
		uuid.NewV7(),
	}
	texts := []string{
		uuid.NewV4().String(),
		uuid.NewV7().String(),
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
	parsed, err := uuid.Parse("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatalf("parse call returned an error on a valid UUID: %v", err)
	}
	if got := parsed.String(); got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("parse round-trip produced %q, want the input back", got)
	}

	integ := gotype.UUIDIntegrationFor(uuidstd.ImportPath)
	pins := []struct {
		field string
		got   string
		want  string
	}{
		{"V4", integ.V4, "uuid.NewV4()"},
		{"V7", integ.V7, "uuid.NewV7()"},
		{"V4String", integ.V4String, "uuid.NewV4().String()"},
		{"V7String", integ.V7String, "uuid.NewV7().String()"},
		{"ParseFunc", integ.ParseFunc, "uuid.Parse"},
	}
	for _, pin := range pins {
		if pin.got != pin.want {
			t.Errorf("UUIDIntegrationFor(%q).%s = %q, want %q — the expression compiled above", uuidstd.ImportPath, pin.field, pin.got, pin.want)
		}
	}
}
