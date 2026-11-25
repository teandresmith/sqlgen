package roundtrip

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestReadResults(t *testing.T) {
	write := func(t *testing.T, dir, name string, r Result) {
		t.Helper()
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sqlite := Result{Suite: SuiteCore, Dialect: "sqlite", Driver: "stdlib", Rows: []Row{{Group: "Reads", Op: "Get", Shape: "plain table", Statements: 1, Rounds: 1}}}
	mysql := Result{Suite: SuiteFeatures, Dialect: "mysql", Driver: "stdlib"}

	t.Run("reads every result, sorted", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, ResultFile(sqlite.Suite, sqlite.Dialect, sqlite.Driver), sqlite)
		write(t, dir, ResultFile(mysql.Suite, mysql.Dialect, mysql.Driver), mysql)
		if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := ReadResults(dir)
		if err != nil {
			t.Fatalf("ReadResults() error = %v", err)
		}
		if diff := cmp.Diff([]Result{sqlite, mysql}, got); diff != "" {
			t.Errorf("ReadResults() (-want +got):\n%s", diff)
		}
	})

	t.Run("a result under another target's name", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, ResultFile(SuiteCore, "mysql", "stdlib"), sqlite)
		_, err := ReadResults(dir)
		if err == nil || !strings.Contains(err.Error(), "holds the core result for sqlite/stdlib") {
			t.Errorf("ReadResults() error = %v, want it to name the mismatch", err)
		}
	})
}
