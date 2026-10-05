package gotype_test

import (
	"testing"

	uuid "github.com/google/uuid"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgoogle"
)

// See uuidgen_compile_stdlib_test.go for what these three files are.
//
// google is the only integration that exports New and the only one that
// exports NewString, which is why its v4 cells are the only unwrapped ones and
// why its string form is a different call rather than its value form with
// .String() appended.

func TestGoogleGenerationExpressionsCompile(t *testing.T) {
	values := []uuid.UUID{
		uuid.New(),
		uuid.Must(uuid.NewV7()),
	}
	texts := []string{
		uuid.NewString(),
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
	parsed, err := uuid.Parse("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatalf("parse call returned an error on a valid UUID: %v", err)
	}
	if got := parsed.String(); got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("parse round-trip produced %q, want the input back", got)
	}

	integ := gotype.UUIDIntegrationFor(uuidgoogle.ImportPath)
	pins := []struct {
		field string
		got   string
		want  string
	}{
		{"V4", integ.V4, "uuid.New()"},
		{"V7", integ.V7, "uuid.Must(uuid.NewV7())"},
		{"V4String", integ.V4String, "uuid.NewString()"},
		{"V7String", integ.V7String, "uuid.Must(uuid.NewV7()).String()"},
		{"ParseFunc", integ.ParseFunc, "uuid.Parse"},
	}
	for _, pin := range pins {
		if pin.got != pin.want {
			t.Errorf("UUIDIntegrationFor(%q).%s = %q, want %q — the expression compiled above", uuidgoogle.ImportPath, pin.field, pin.got, pin.want)
		}
	}
}
