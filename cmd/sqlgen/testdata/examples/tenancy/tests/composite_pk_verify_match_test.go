package tests

import (
	"reflect"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// When the tenant column is part of the primary key, the PK carries the tenant
// and the update input does not: a row's tenant is not a mutable attribute.
func TestCompositePK_UpdateInputOmitsTheTenantColumn(t *testing.T) {
	if _, ok := reflect.TypeFor[models.OrderItemPK]().FieldByName("WorkspaceID"); !ok {
		t.Error("OrderItemPK has no WorkspaceID field, want the tenant column in the PK struct")
	}
	if f, ok := reflect.TypeFor[models.UpdateOrderItemInput]().FieldByName("WorkspaceID"); ok {
		t.Errorf("UpdateOrderItemInput has field %s, want no tenant column", f.Name)
	}
}
