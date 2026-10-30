package tests

import (
	"context"
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/database"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// Every error a view client method returns starts with that method's own
// prefix exactly once, and its cause stays reachable. order_totals is a
// materialized view, so it has every view method, Refresh included.
func TestViewErrors_CarryTheMethodPrefix(t *testing.T) {
	client := newClient()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	assertPrefix := func(t *testing.T, err error, prefix string, cause error) {
		t.Helper()
		if err == nil {
			t.Fatalf("err = nil, want an error starting %q", prefix)
		}
		if !strings.HasPrefix(err.Error(), prefix) || strings.Count(err.Error(), prefix) != 1 {
			t.Errorf("err = %q, want it to start with %q exactly once", err, prefix)
		}
		if !errors.Is(err, cause) {
			t.Errorf("err = %q, does not wrap %v", err, cause)
		}
	}

	t.Run("Get prefixes a missing key with get and the singular", func(t *testing.T) {
		_, err := client.OrderTotal().Get(context.Background(), uuid.New())
		assertPrefix(t, err, "get order_total: ", database.ErrNotFound)
	})
	t.Run("Get prefixes a database failure with get and the singular", func(t *testing.T) {
		_, err := client.OrderTotal().Get(canceled, uuid.New())
		assertPrefix(t, err, "get order_total: ", context.Canceled)
	})
	t.Run("GetMany prefixes a database failure with get many and the view", func(t *testing.T) {
		_, err := client.OrderTotal().GetMany(canceled, nil)
		assertPrefix(t, err, "get many order_totals: ", context.Canceled)
	})
	t.Run("Count prefixes a database failure with count and the view", func(t *testing.T) {
		_, err := client.OrderTotal().Count(canceled, nil)
		assertPrefix(t, err, "count order_totals: ", context.Canceled)
	})
	t.Run("Paginate prefixes a database failure with paginate and the view", func(t *testing.T) {
		_, err := client.OrderTotal().Paginate(canceled, models.PaginateInput[models.OrderTotalFilter]{Limit: 1})
		assertPrefix(t, err, "paginate order_totals: ", context.Canceled)
	})
	t.Run("Connection prefixes an invalid cursor with connection and the view", func(t *testing.T) {
		_, err := client.OrderTotal().Connection(context.Background(), models.ConnectionInput[models.OrderTotalFilter]{First: new(1), After: new("not a cursor")})
		assertPrefix(t, err, "connection order_totals: ", database.ErrInvalidCursor)
	})
	t.Run("Refresh prefixes a database failure with refresh and the view", func(t *testing.T) {
		err := client.OrderTotal().Refresh(canceled)
		assertPrefix(t, err, "refresh order_totals: ", context.Canceled)
	})
	t.Run("RefreshConcurrently prefixes a database failure with refresh concurrently and the view", func(t *testing.T) {
		err := client.OrderTotal().RefreshConcurrently(canceled)
		assertPrefix(t, err, "refresh concurrently order_totals: ", context.Canceled)
	})
	t.Run("RefreshConcurrently prefixes its refusal inside a transaction with refresh concurrently and the view", func(t *testing.T) {
		var err error
		txErr := database.WithTransaction(context.Background(), dbpgx.New(testPool), "refresh_in_tx", func(txCtx context.Context) error {
			err = client.OrderTotal().RefreshConcurrently(txCtx)
			return nil
		})
		if txErr != nil {
			t.Fatalf("transaction: %v", txErr)
		}
		assertPrefix(t, err, "refresh concurrently order_totals: ", database.ErrRefreshConcurrentlyInTx)
	})
}
