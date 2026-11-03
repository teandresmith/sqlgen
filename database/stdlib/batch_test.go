package stdlib_test

import (
	"reflect"
	"testing"

	stdlibadapter "github.com/teandresmith/sqlgen/database/stdlib"
)

// TestAdapterHasNoSendBatch pins that the database/sql adapter offers no
// SendBatch: database/sql has no pipelining, so generated UpdateMany runs its
// statements one by one in a transaction when the querier lacks it.
func TestAdapterHasNoSendBatch(t *testing.T) {
	q := stdlibadapter.New(nil)
	if m, ok := reflect.TypeOf(q).MethodByName("SendBatch"); ok {
		t.Errorf("stdlib.New() = %T, which has method SendBatch %v; want none", q, m.Type)
	}
}
