package suite

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
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

	t.Run("records only the keys it wrote as affected", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "d")
		missing := d.ID + 1000
		client, affected := affectedPKsRecorder(h)
		if _, err := client.Documents().SoftDeleteMany(ctx, []int64{missing, d.ID}); err != nil {
			t.Fatalf("SoftDeleteMany([missing, d]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]any{d.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of SoftDeleteMany([missing, d]) (-want +got):\n%s", diff)
		}
		if _, err := client.Documents().SoftDeleteMany(ctx, []int64{missing}); err != nil {
			t.Fatalf("SoftDeleteMany([missing]) error = %v, want nil", err)
		}
		if got := affected(); len(got) != 0 {
			t.Errorf("AffectedPKs of a SoftDeleteMany matching no row = %v, want none", got)
		}
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

	t.Run("records only the keys it wrote as affected", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "d")
		if _, err := h.Client.Documents().SoftDelete(ctx, d.ID); err != nil {
			t.Fatalf("SoftDelete(d) error = %v", err)
		}
		missing := d.ID + 1000
		client, affected := affectedPKsRecorder(h)
		if _, err := client.Documents().RestoreMany(ctx, []int64{missing, d.ID}); err != nil {
			t.Fatalf("RestoreMany([missing, d]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]any{d.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of RestoreMany([missing, d]) (-want +got):\n%s", diff)
		}
		if _, err := client.Documents().RestoreMany(ctx, []int64{missing}); err != nil {
			t.Fatalf("RestoreMany([missing]) error = %v, want nil", err)
		}
		if got := affected(); len(got) != 0 {
			t.Errorf("AffectedPKs of a RestoreMany matching no row = %v, want none", got)
		}
	})
}
