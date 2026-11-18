package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestTinyint1_OnlyZeroAndOneScanIntoBool reads users rows whose is_active,
// a BOOLEAN column MySQL stores as tinyint(1) and sqlgen types as bool, holds
// 0, 1 and other values the column accepts. 0 and 1 read back as false and
// true; any other value fails the read instead of becoming either.
func TestTinyint1_OnlyZeroAndOneScanIntoBool(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	insert := func(t *testing.T, email string, isActive int) int64 {
		t.Helper()
		res, err := testDB.ExecContext(ctx,
			`INSERT INTO users (name, email, is_active, balance) VALUES (?, ?, ?, 0)`,
			"tinyint1", email, isActive)
		if err != nil {
			t.Fatalf("insert is_active = %d: %v", isActive, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("last insert id: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testDB.ExecContext(context.Background(), `DELETE FROM users WHERE id = ?`, id)
		})
		return id
	}

	t.Run("0 and 1 scan", func(t *testing.T) {
		for _, tc := range []struct {
			stored int
			want   bool
		}{{0, false}, {1, true}} {
			id := insert(t, fmt.Sprintf("tinyint1-ok-%d@example.com", tc.stored), tc.stored)
			got, err := client.Users().Get(ctx, id)
			if err != nil {
				t.Fatalf("Get with is_active = %d: %v", tc.stored, err)
			}
			if got.IsActive != tc.want {
				t.Errorf("is_active = %d read back as %v, want %v", tc.stored, got.IsActive, tc.want)
			}
		}
	})

	t.Run("other values do not scan", func(t *testing.T) {
		for _, stored := range []int{2, -1, 127} {
			id := insert(t, fmt.Sprintf("tinyint1-bad-%d@example.com", stored), stored)
			got, err := client.Users().Get(ctx, id)
			if err == nil {
				t.Errorf("Get with is_active = %d: got IsActive = %v, want a scan error", stored, got.IsActive)
				continue
			}
			if !strings.Contains(err.Error(), "is_active") {
				t.Errorf("Get with is_active = %d: error %q does not name the column", stored, err)
			}
		}
	})
}
