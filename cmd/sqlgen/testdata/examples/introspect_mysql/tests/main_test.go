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

	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/introspect_mysql/models"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	ctx := context.Background()
	container, err := mysql.Run(
		ctx,
		"mysql:8.0",
		mysql.WithDatabase("sqlgen_e2e"),
		mysql.WithUsername("test"),
		mysql.WithPassword("test"),
	)
	if err != nil {
		panic(fmt.Sprintf("starting mysql container: %v", err))
	}
	// multiStatements lets each setup file run as one Exec.
	connStr, err := container.ConnectionString(ctx, "parseTime=true", "multiStatements=true")
	if err != nil {
		panic(fmt.Sprintf("getting connection string: %v", err))
	}
	testDB, err = sql.Open("mysql", connStr)
	if err != nil {
		panic(fmt.Sprintf("opening database: %v", err))
	}

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
	_ = container.Terminate(ctx)
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
		Summary:   "Summary of " + slug,
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
