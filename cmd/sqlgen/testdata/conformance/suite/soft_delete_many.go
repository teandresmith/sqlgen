package suite

import (
	"context"
	"testing"
)

// softDeleteMany: SoftDeleteMany marks every listed row as deleted.
func softDeleteMany(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("splits the keys into sub-batches of the table's batch size", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 5)
		h.Querier.ResetCount()
		if _, err := h.Client.Documents().SoftDeleteMany(ctx, ids); err != nil {
			t.Fatalf("SoftDeleteMany(5 keys) error = %v, want nil", err)
		}
		// The documents table sets batch_size: 2, so 5 keys are ceil(5 / 2) = 3 sub-batches.
		if updates := statementsWithPrefix(h, "UPDATE"); len(updates) != 3 {
			t.Errorf("SoftDeleteMany(5 keys) sent %d UPDATE statements, want 3: %q", len(updates), updates)
		}
		assertLiveDocuments(t, h, 0)
		assertRowCount(t, h, "documents", 5)
	})
}

// restoreMany: RestoreMany clears the soft-delete marker on every listed row.
func restoreMany(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("splits the keys into sub-batches of the table's batch size", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 5)
		if _, err := h.Client.Documents().SoftDeleteMany(ctx, ids); err != nil {
			t.Fatalf("SoftDeleteMany(5 keys) error = %v", err)
		}
		h.Querier.ResetCount()
		if _, err := h.Client.Documents().RestoreMany(ctx, ids); err != nil {
			t.Fatalf("RestoreMany(5 keys) error = %v, want nil", err)
		}
		// The documents table sets batch_size: 2, so 5 keys are ceil(5 / 2) = 3 sub-batches.
		if updates := statementsWithPrefix(h, "UPDATE"); len(updates) != 3 {
			t.Errorf("RestoreMany(5 keys) sent %d UPDATE statements, want 3: %q", len(updates), updates)
		}
		assertLiveDocuments(t, h, 5)
	})
}
