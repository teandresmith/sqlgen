package postgres

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// TestBuiltinTypeNameCoversGrammarSpellings pins that every name the parser
// writes for a type the grammar qualifies with pg_catalog is a
// builtinTypeName, so typeName never records it in searchPathTypes and a
// same-named type in another schema never qualifies it. The spellings reach
// every type the grammar names with SystemTypeName (record aside, which no
// column can have).
func TestBuiltinTypeNameCoversGrammarSpellings(t *testing.T) {
	spellings := []string{
		"smallint", "int", "integer", "bigint", "real", "float(10)", "float", "float(30)",
		"double precision", "decimal", "dec(5)", "numeric(10,2)", "boolean", "bit", "bit(3)",
		"bit varying", "bit varying(4)", "char(3)", "character", "national character(3)", "nchar(2)",
		"varchar(3)", "character varying", "national character varying(4)", "timestamp(3)",
		"timestamp without time zone", "timestamp with time zone", "timestamp(3) with time zone",
		"time", "time(2) without time zone", "time with time zone", "time(2) with time zone",
		"interval", "interval year to month", "interval(3)", "json", "integer[]", "bit varying(4)[]",
	}
	internal := make(map[string]bool)
	for _, spelling := range spellings {
		tree, err := pg_query.Parse("CREATE TABLE t (c " + spelling + ")")
		if err != nil {
			t.Fatalf("parsing %q: %v", spelling, err)
		}
		tn := tree.GetStmts()[0].GetStmt().GetCreateStmt().GetTableElts()[0].GetColumnDef().GetTypeName()
		names := extractStringList(tn.GetNames())
		if len(names) != 2 || names[0] != "pg_catalog" {
			t.Errorf("%q: grammar names %v, want a pg_catalog type", spelling, names)
			continue
		}
		internal[names[1]] = true
		written := typeNameToString(tn)
		if !builtinTypeName(typeBaseName(written)) {
			t.Errorf("%q: the parser writes %q, whose name is not a builtinTypeName", spelling, written)
		}
	}
	want := map[string]bool{
		"int2": true, "int4": true, "int8": true, "float4": true, "float8": true, "numeric": true,
		"bool": true, "bit": true, "varbit": true, "bpchar": true, "varchar": true, "timestamp": true,
		"timestamptz": true, "time": true, "timetz": true, "interval": true, "json": true,
	}
	if diff := cmp.Diff(want, internal); diff != "" {
		t.Errorf("pg_catalog types the spellings reach (-want +got):\n%s", diff)
	}
	for name, written := range catalogSpellings {
		if !builtinTypeName(written) {
			t.Errorf("catalogSpellings[%q] = %q, not a builtinTypeName", name, written)
		}
	}
}
