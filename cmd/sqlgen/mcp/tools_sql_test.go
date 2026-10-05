package mcp

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestShowSQL(t *testing.T) {
	d := fixtureDeps()

	t.Run("resolves the active dialect", func(t *testing.T) {
		got, err := showSQL(d, ShowSQLInput{Entity: "User", Method: "Get"})
		if err != nil {
			t.Fatalf("showSQL: %v", err)
		}
		want := ShowSQLOutput{
			SQL:     "SELECT * FROM users WHERE id = $1",
			Params:  []SQLParam{{Name: "id", Type: "string"}},
			Dialect: "postgres",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("output mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("dialect override without a matching body yields -32005", func(t *testing.T) {
		_, err := showSQL(d, ShowSQLInput{Entity: "User", Method: "Get", Dialect: "mysql"})
		assertCode(t, err, CodeMethodSQLUnavailable)
	})

	t.Run("method without sql_bodies yields -32005", func(t *testing.T) {
		_, err := showSQL(d, ShowSQLInput{Entity: "User", Method: "Upsert"})
		assertCode(t, err, CodeMethodSQLUnavailable)
	})

	t.Run("unknown method yields -32006 with suggestions", func(t *testing.T) {
		_, err := showSQL(d, ShowSQLInput{Entity: "User", Method: "Ge"})
		assertNotFound(t, err, CodeMethodNotFound, []string{"Get"})
	})

	t.Run("unknown entity yields -32003", func(t *testing.T) {
		_, err := showSQL(d, ShowSQLInput{Entity: "Usr", Method: "Get"})
		assertNotFound(t, err, CodeEntityNotFound, []string{"User"})
	})
}
