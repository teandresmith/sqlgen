package gotype_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// A domain whose base is a built-in name keeps that built-in base when another
// schema declares a type of the same name: the base is read in the domain's
// own schema, so BaseSQLType and SUM widening agree with Resolve.
func TestResolver_DomainOverBuiltinIgnoresOtherSchemaType(t *testing.T) {
	r := gotype.NewResolver("postgres", true, nil)
	r.RegisterDomain("app", "integer", "text")
	r.RegisterDomain("public", "d", "integer")
	scoped := r.ForSchema("public")

	if got := scoped.Resolve("d", false, "", nil, nil).Name; got != "int32" {
		t.Errorf("Resolve(d) = %q, want int32", got)
	}
	if got := scoped.BaseSQLType("d"); got != "integer" {
		t.Errorf("BaseSQLType(d) = %q, want integer", got)
	}
	if got := scoped.ResolveAggregate("SUM", "d", false, "total", nil, nil).Name; got != "int64" {
		t.Errorf("SUM over d = %q, want int64 (integer widens to bigint)", got)
	}
}
