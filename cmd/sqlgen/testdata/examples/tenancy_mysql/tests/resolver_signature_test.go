package tests

import (
	"testing"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_mysql/models"
)

// pin compiles only when its argument has exactly type T, so each call states a
// signature the build checks.
func pin[T any](T) {}

// TestWithTenantResolver_TakesTheTenantColumnType pins the generated option to a
// resolver of the tenant column's Go type, and the model and create-input tenant
// fields to that same type. A resolver returning any other type does not build.
func TestWithTenantResolver_TakesTheTenantColumnType(_ *testing.T) {
	pin[func(tenancy.TenantResolver[string]) models.ClientOption](models.WithTenantResolver)
	pin[string](models.Article{}.WorkspaceID)
	pin[omittable.Value[string]](models.CreateArticleInput{}.WorkspaceID)
}
