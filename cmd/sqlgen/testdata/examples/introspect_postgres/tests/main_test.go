// Package tests runs code generated under source: both against the database
// it describes: the base schema with migrations/ applied.
package tests

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/introspect_postgres/models"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	ctx := context.Background()
	container, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("sqlgen_e2e"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		panic(fmt.Sprintf("starting postgres container: %v", err))
	}
	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(fmt.Sprintf("getting connection string: %v", err))
	}
	testPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		panic(fmt.Sprintf("creating pool: %v", err))
	}

	for _, path := range setupFiles() {
		sql, err := os.ReadFile(path) //nolint:gosec // test reads the example's own setup files
		if err != nil {
			panic(fmt.Sprintf("reading %s: %v", path, err))
		}
		if _, err := testPool.Exec(ctx, string(sql)); err != nil {
			panic(fmt.Sprintf("applying %s: %v", path, err))
		}
	}

	code := m.Run()

	testPool.Close()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

// setupFiles lists, in order, the files that build the migrated database:
// schema.sql, views/ and after_views.sql, then migrations/.
func setupFiles() []string {
	files := []string{"../schema.sql"}
	files = append(files, sortedGlob("../views/*.sql")...)
	files = append(files, "../after_views.sql")
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
	return models.New(dbpgx.New(testPool))
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
