package gen_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// gnwEmptySelectionProbe runs inside a generated package. GetMany with a
// non-nil FieldOptions that selects nothing returns nil, nil and hands the
// querier no statement; the same call with a nil FieldOptions, and with one
// that selects a column, does reach it, so the querier can see a statement.
const gnwEmptySelectionProbe = `package db

import (
	"errors"
	"testing"
)

func TestProbe_GetManyEmptySelection(t *testing.T) {
	t.Run("nothing selected returns nil, nil without a statement", func(t *testing.T) {
		q := &probeQuerier{}
		got, err := New(q).Notes().GetMany(t.Context(), &GetNotesInput{}, func(co *CallOptions[NoteFieldOptions]) {
			co.FieldOptions = &NoteFieldOptions{}
		})
		if err != nil || got != nil {
			t.Errorf("GetMany with an empty FieldOptions = (%v, %v), want (nil, nil)", got, err)
		}
		if stmts := q.statements(); len(stmts) != 0 {
			t.Errorf("GetMany with an empty FieldOptions issued %d statements, want none: %v", len(stmts), stmts)
		}
	})

	controls := []struct {
		name string
		fo   *NoteFieldOptions
	}{
		{name: "nil FieldOptions queries", fo: nil},
		{name: "a selected column queries", fo: &NoteFieldOptions{Body: true}},
	}
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			q := &probeQuerier{}
			_, err := New(q).Notes().GetMany(t.Context(), &GetNotesInput{}, func(co *CallOptions[NoteFieldOptions]) {
				co.FieldOptions = c.fo
			})
			if !errors.Is(err, errProbe) {
				t.Errorf("GetMany error = %v, want the recorded statement's error", err)
			}
			if stmts := q.statements(); len(stmts) != 1 {
				t.Errorf("GetMany issued %d statements, want 1: %v", len(stmts), stmts)
			}
		})
	}
}
`

// TestGenerate_GetManyEmptySelectionReturnsEarly generates a table client on
// every dialect and runs its GetMany against a recording querier: a non-nil
// FieldOptions that selects no column returns nil, nil and issues no
// statement.
func TestGenerate_GetManyEmptySelectionReturnsEarly(t *testing.T) {
	gnwRunGeneratedProbe(t, []gnwProbePackage{
		{
			dir:     "pg",
			dialect: config.DialectPostgres,
			ddl:     `CREATE TABLE notes (id bigserial PRIMARY KEY, body text NOT NULL);`,
		},
		{
			dir:     "my",
			dialect: config.DialectMySQL,
			ddl:     "CREATE TABLE notes (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, body TEXT NOT NULL);",
		},
		{
			dir:     "lite",
			dialect: config.DialectSQLite,
			ddl:     `CREATE TABLE notes (id INTEGER PRIMARY KEY AUTOINCREMENT, body TEXT NOT NULL);`,
		},
	}, gnwEmptySelectionProbe)
}
