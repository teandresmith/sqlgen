// Package tests runs code generated under source: both against the database
// it describes: the base schema with migrations/ applied.
package tests

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	_ "modernc.org/sqlite"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/introspect_sqlite/models"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	dir, err := os.MkdirTemp("", "introspect_sqlite")
	if err != nil {
		panic(fmt.Sprintf("creating temp dir: %v", err))
	}
	// The pragma applies to every pooled connection, so foreign keys are
	// enforced whichever one a query takes.
	testDB, err = sql.Open("sqlite", "file:"+filepath.Join(dir, "app.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		panic(fmt.Sprintf("opening database: %v", err))
	}

	ctx := context.Background()
	for _, path := range setupFiles() {
		script, err := os.ReadFile(path) //nolint:gosec // test reads the example's own setup files
		if err != nil {
			panic(fmt.Sprintf("reading %s: %v", path, err))
		}
		if _, err := testDB.ExecContext(ctx, string(script)); err != nil {
			panic(fmt.Sprintf("applying %s: %v", path, err))
		}
	}

	code := m.Run()

	_ = testDB.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// setupFiles lists, in order, the files that build the migrated database:
// schema.sql and views/, then migrations/.
func setupFiles() []string {
	files := []string{"../schema.sql"}
	files = append(files, sortedGlob("../views/*.sql")...)
	return append(files, sortedGlob("../migrations/*.sql")...)
}

func sortedGlob(pattern string) []string {
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		panic(fmt.Sprintf("globbing %s: %v", pattern, err))
	}
	slices.Sort(files)
	return files
}

func newClient() *models.Client {
	return models.New(dbstdlib.New(testDB))
}

// newAccount creates an account with only the required fields.
func newAccount(t *testing.T, client *models.Client, email string) *models.Account {
	t.Helper()
	ctx := context.Background()
	account, err := client.Accounts().Create(ctx, &models.CreateAccountInput{
		Email:       email,
		DisplayName: "Account " + email,
	})
	if err != nil {
		t.Fatalf("Create account: %v", err)
	}
	t.Cleanup(func() { _ = client.Accounts().HardDelete(ctx, account.ID) })
	return account
}

// newProject creates a project owned by account.
func newProject(t *testing.T, client *models.Client, account *models.Account, slug string) *models.Project {
	t.Helper()
	ctx := context.Background()
	project, err := client.Projects().Create(ctx, &models.CreateProjectInput{
		AccountID: account.ID,
		Name:      "Project " + slug,
		Slug:      slug,
	})
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}
	t.Cleanup(func() { _ = client.Projects().HardDelete(ctx, project.ID) })
	return project
}

// newTask creates a task in project.
func newTask(t *testing.T, client *models.Client, project *models.Project, input *models.CreateTaskInput) *models.Task {
	t.Helper()
	ctx := context.Background()
	input.ProjectID = project.ID
	task, err := client.Tasks().Create(ctx, input)
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}
	t.Cleanup(func() { _ = client.Tasks().HardDelete(ctx, task.ID) })
	return task
}
