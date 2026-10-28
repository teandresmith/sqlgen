package cli

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// rdwQualified returns "schema.name" for each element.
func rdwQualified[T any](xs []T, key func(T) (string, string)) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		s, n := key(x)
		out = append(out, s+"."+n)
	}
	return out
}

// The schema parseSchema hands to generation is sorted by schema, then name,
// though the files declare it in another order, and each table keeps its
// columns in declared order.
func TestParseSchema_SortedBeforeGeneration(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		in := config.InputConfig{Dialect: config.DialectPostgres}
		schema := tdfParseFiles(t, in, map[string]string{
			"schema.sql": `CREATE SCHEMA a;
CREATE SCHEMA b;
CREATE TYPE b.a_mood AS ENUM ('x');
CREATE TYPE a.z_mood AS ENUM ('x');
CREATE TYPE b.a_point AS (x int);
CREATE TYPE a.z_point AS (x int);
CREATE DOMAIN b.a_code AS text;
CREATE DOMAIN a.z_code AS text;
CREATE TABLE b.a (zeta int, alpha int, mid int);
CREATE TABLE a.z (y int, b int);
CREATE TABLE b.c (k int);
CREATE TABLE a.m (q int, p int);`,
		}, nil)
		if diff := cmp.Diff([]string{"a.m", "a.z", "b.a", "b.c"}, tdfTableNames(schema)); diff != "" {
			t.Errorf("tables mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"a.z_mood", "b.a_mood"}, rdwQualified(schema.Enums, func(e parser.Enum) (string, string) { return e.Schema, e.Name })); diff != "" {
			t.Errorf("enums mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"a.z_point", "b.a_point"}, rdwQualified(schema.CompositeTypes, func(c parser.CompositeType) (string, string) { return c.Schema, c.Name })); diff != "" {
			t.Errorf("composite types mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"a.z_code", "b.a_code"}, rdwQualified(schema.DomainTypes, func(d parser.DomainType) (string, string) { return d.Schema, d.Name })); diff != "" {
			t.Errorf("domain types mismatch (-want +got):\n%s", diff)
		}
		cols := map[string][]string{}
		for _, tb := range schema.Tables {
			for _, c := range tb.Columns {
				cols[tb.Schema+"."+tb.Name] = append(cols[tb.Schema+"."+tb.Name], c.Name)
			}
		}
		want := map[string][]string{"b.a": {"zeta", "alpha", "mid"}, "a.z": {"y", "b"}, "b.c": {"k"}, "a.m": {"q", "p"}}
		if diff := cmp.Diff(want, cols); diff != "" {
			t.Errorf("columns mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("mysql", func(t *testing.T) {
		in := config.InputConfig{Dialect: config.DialectMySQL}
		schema := tdfParseFiles(t, in, map[string]string{
			"schema.sql": `CREATE TABLE zoo (tags SET('x','y'), mood ENUM('x','y'));
CREATE TABLE apes (tags SET('x','y'), mood ENUM('x','y'));`,
		}, nil)
		if diff := cmp.Diff([]string{".apes", ".zoo"}, tdfTableNames(schema)); diff != "" {
			t.Errorf("tables mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{".apes__tags_set", ".zoo__tags_set"}, rdwQualified(schema.Sets, func(s parser.Set) (string, string) { return s.Schema, s.Name })); diff != "" {
			t.Errorf("sets mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{".apes__mood_enum", ".zoo__mood_enum"}, rdwQualified(schema.Enums, func(e parser.Enum) (string, string) { return e.Schema, e.Name })); diff != "" {
			t.Errorf("enums mismatch (-want +got):\n%s", diff)
		}
	})
}
