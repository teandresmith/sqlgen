package mock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/database/mock"
)

func TestQuerier_ExecFn(t *testing.T) {
	var capturedSQL string
	var capturedArgs []any

	m := mock.New()
	m.ExecFn = func(ctx context.Context, sql string, arguments ...any) (database.Result, error) {
		capturedSQL = sql
		capturedArgs = arguments
		return &mock.Result{
			RowsAffectedFn: func() (int64, error) { return 3, nil },
		}, nil
	}

	ctx := context.Background()
	result, err := m.Exec(ctx, "INSERT INTO users (name) VALUES ($1)", "alice")
	if err != nil {
		t.Fatalf("Exec() unexpected error: %v", err)
	}

	if capturedSQL != "INSERT INTO users (name) VALUES ($1)" {
		t.Errorf("Exec() sql = %q, want %q", capturedSQL, "INSERT INTO users (name) VALUES ($1)")
	}
	if diff := cmp.Diff([]any{"alice"}, capturedArgs); diff != "" {
		t.Errorf("Exec() args mismatch (-want +got):\n%s", diff)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected() unexpected error: %v", err)
	}
	if rows != 3 {
		t.Errorf("RowsAffected() = %d, want 3", rows)
	}
}

func TestQuerier_QueryFn(t *testing.T) {
	var capturedSQL string
	var capturedArgs []any

	callCount := 0
	m := mock.New()
	m.QueryFn = func(ctx context.Context, sql string, args ...any) (database.Rows, error) {
		capturedSQL = sql
		capturedArgs = args
		return &mock.Rows{
			NextFn: func() bool {
				callCount++
				return callCount <= 2
			},
			ScanFn: func(dest ...any) error {
				*dest[0].(*int) = callCount
				*dest[1].(*string) = "user"
				return nil
			},
			ColumnsFn: func() ([]string, error) {
				return []string{"id", "name"}, nil
			},
		}, nil
	}

	ctx := context.Background()
	rows, err := m.Query(ctx, "SELECT id, name FROM users WHERE active = $1", true)
	if err != nil {
		t.Fatalf("Query() unexpected error: %v", err)
	}

	if capturedSQL != "SELECT id, name FROM users WHERE active = $1" {
		t.Errorf("Query() sql = %q, want %q", capturedSQL, "SELECT id, name FROM users WHERE active = $1")
	}
	if diff := cmp.Diff([]any{true}, capturedArgs); diff != "" {
		t.Errorf("Query() args mismatch (-want +got):\n%s", diff)
	}

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("Columns() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]string{"id", "name"}, cols); diff != "" {
		t.Errorf("Columns() mismatch (-want +got):\n%s", diff)
	}

	var ids []int
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("Scan() unexpected error: %v", err)
		}
		ids = append(ids, id)
	}
	if diff := cmp.Diff([]int{1, 2}, ids); diff != "" {
		t.Errorf("iterated ids mismatch (-want +got):\n%s", diff)
	}
}

func TestQuerier_QueryRowFn(t *testing.T) {
	var capturedSQL string
	var capturedArgs []any

	m := mock.New()
	m.QueryRowFn = func(ctx context.Context, sql string, args ...any) database.Row {
		capturedSQL = sql
		capturedArgs = args
		return mock.NewRow(42, "alice", "alice@example.com")
	}

	ctx := context.Background()
	row := m.QueryRow(ctx, "SELECT id, name, email FROM users WHERE id = $1", 42)

	if capturedSQL != "SELECT id, name, email FROM users WHERE id = $1" {
		t.Errorf("QueryRow() sql = %q, want %q", capturedSQL, "SELECT id, name, email FROM users WHERE id = $1")
	}
	if diff := cmp.Diff([]any{42}, capturedArgs); diff != "" {
		t.Errorf("QueryRow() args mismatch (-want +got):\n%s", diff)
	}

	var id int
	var name, email string
	if err := row.Scan(&id, &name, &email); err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if id != 42 {
		t.Errorf("Scan() id = %d, want 42", id)
	}
	if name != "alice" {
		t.Errorf("Scan() name = %q, want %q", name, "alice")
	}
	if email != "alice@example.com" {
		t.Errorf("Scan() email = %q, want %q", email, "alice@example.com")
	}
}

func TestQuerier_BeginFn(t *testing.T) {
	var capturedName string

	m := mock.New()
	m.BeginFn = func(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
		capturedName = name
		return nil, nil
	}

	ctx := context.Background()
	_, err := m.Begin(ctx, "test_tx")
	if err != nil {
		t.Fatalf("Begin() unexpected error: %v", err)
	}

	if capturedName != "test_tx" {
		t.Errorf("Begin() name = %q, want %q", capturedName, "test_tx")
	}
}

// WithSession hands fn the mock itself: a mock has no pool, so it already is
// the one session there is, and generated code that pins works against it.
func TestQuerier_WithSession(t *testing.T) {
	m := mock.New()
	errFn := errors.New("fn failed")
	var got database.Querier
	err := m.WithSession(context.Background(), func(session database.Querier) error {
		got = session
		return errFn
	})
	if got != m {
		t.Errorf("WithSession() passed fn %v, want the mock itself", got)
	}
	if !errors.Is(err, errFn) {
		t.Errorf("WithSession() error = %v, want fn's error", err)
	}
}

func TestQuerier_DefaultBehavior(t *testing.T) {
	m := mock.New()
	ctx := context.Background()

	t.Run("Exec returns empty result", func(t *testing.T) {
		result, err := m.Exec(ctx, "INSERT INTO users DEFAULT VALUES")
		if err != nil {
			t.Errorf("Exec() error = %v, want nil", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			t.Errorf("RowsAffected() error = %v, want nil", err)
		}
		if rows != 0 {
			t.Errorf("RowsAffected() = %d, want 0", rows)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Errorf("LastInsertId() error = %v, want nil", err)
		}
		if id != 0 {
			t.Errorf("LastInsertId() = %d, want 0", id)
		}
	})

	t.Run("Query returns empty rows", func(t *testing.T) {
		rows, err := m.Query(ctx, "SELECT 1")
		if err != nil {
			t.Errorf("Query() error = %v, want nil", err)
		}
		if rows.Next() {
			t.Error("Next() = true, want false")
		}
		if err := rows.Err(); err != nil {
			t.Errorf("Err() = %v, want nil", err)
		}
		if err := rows.Close(); err != nil {
			t.Errorf("Close() = %v, want nil", err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Errorf("Columns() error = %v, want nil", err)
		}
		if cols != nil {
			t.Errorf("Columns() = %v, want nil", cols)
		}
	})

	t.Run("QueryRow returns empty row", func(t *testing.T) {
		row := m.QueryRow(ctx, "SELECT 1")
		if err := row.Scan(); err != nil {
			t.Errorf("Scan() error = %v, want nil", err)
		}
	})

	t.Run("Begin returns nil", func(t *testing.T) {
		tx, err := m.Begin(ctx, "test")
		if err != nil {
			t.Errorf("Begin() error = %v, want nil", err)
		}
		if tx != nil {
			t.Errorf("Begin() tx = %v, want nil", tx)
		}
	})
}

func TestResult_Configured(t *testing.T) {
	r := &mock.Result{
		RowsAffectedFn: func() (int64, error) { return 5, nil },
		LastInsertIdFn: func() (int64, error) { return 42, nil },
	}

	rows, err := r.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected() unexpected error: %v", err)
	}
	if rows != 5 {
		t.Errorf("RowsAffected() = %d, want 5", rows)
	}

	id, err := r.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() unexpected error: %v", err)
	}
	if id != 42 {
		t.Errorf("LastInsertId() = %d, want 42", id)
	}
}

func TestResult_Errors(t *testing.T) {
	errRows := errors.New("rows affected error")
	errInsert := errors.New("last insert id error")

	r := &mock.Result{
		RowsAffectedFn: func() (int64, error) { return 0, errRows },
		LastInsertIdFn: func() (int64, error) { return 0, errInsert },
	}

	_, err := r.RowsAffected()
	if !errors.Is(err, errRows) {
		t.Errorf("RowsAffected() error = %v, want %v", err, errRows)
	}

	_, err = r.LastInsertId()
	if !errors.Is(err, errInsert) {
		t.Errorf("LastInsertId() error = %v, want %v", err, errInsert)
	}
}

func TestRows_Iteration(t *testing.T) {
	data := []struct {
		id   int
		name string
	}{
		{1, "alice"},
		{2, "bob"},
		{3, "carol"},
	}
	idx := -1

	r := &mock.Rows{
		NextFn: func() bool {
			idx++
			return idx < len(data)
		},
		ScanFn: func(dest ...any) error {
			*dest[0].(*int) = data[idx].id
			*dest[1].(*string) = data[idx].name
			return nil
		},
		ColumnsFn: func() ([]string, error) {
			return []string{"id", "name"}, nil
		},
	}

	cols, err := r.Columns()
	if err != nil {
		t.Fatalf("Columns() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]string{"id", "name"}, cols); diff != "" {
		t.Errorf("Columns() mismatch (-want +got):\n%s", diff)
	}

	var gotIDs []int
	var gotNames []string
	for r.Next() {
		var id int
		var name string
		if err := r.Scan(&id, &name); err != nil {
			t.Fatalf("Scan() unexpected error: %v", err)
		}
		gotIDs = append(gotIDs, id)
		gotNames = append(gotNames, name)
	}

	if diff := cmp.Diff([]int{1, 2, 3}, gotIDs); diff != "" {
		t.Errorf("iterated ids mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"alice", "bob", "carol"}, gotNames); diff != "" {
		t.Errorf("iterated names mismatch (-want +got):\n%s", diff)
	}
}

func TestRow_ScanFn(t *testing.T) {
	r := &mock.Row{
		ScanFn: func(dest ...any) error {
			*dest[0].(*int) = 99
			return nil
		},
	}

	var id int
	if err := r.Scan(&id); err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if id != 99 {
		t.Errorf("Scan() id = %d, want 99", id)
	}
}

func TestRow_ScanFn_OverridesValues(t *testing.T) {
	r := &mock.Row{
		Values: []any{1, "should not be used"},
		ScanFn: func(dest ...any) error {
			*dest[0].(*int) = 999
			return nil
		},
	}

	var id int
	if err := r.Scan(&id); err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if id != 999 {
		t.Errorf("Scan() id = %d, want 999 (ScanFn should override Values)", id)
	}
}

func TestNewRow(t *testing.T) {
	row := mock.NewRow(42, "alice", true)

	var id int
	var name string
	var active bool
	if err := row.Scan(&id, &name, &active); err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if id != 42 {
		t.Errorf("Scan() id = %d, want 42", id)
	}
	if name != "alice" {
		t.Errorf("Scan() name = %q, want %q", name, "alice")
	}
	if !active {
		t.Error("Scan() active = false, want true")
	}
}
