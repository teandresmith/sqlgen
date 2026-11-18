package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func TestKeyClass_loadsAcrossComparators(t *testing.T) {
	ctx := context.Background()
	if _, err := testDB.ExecContext(ctx, `DELETE FROM key_class_real_children; DELETE FROM key_class_children; DELETE FROM key_class_parents;
		INSERT INTO key_class_parents (id) VALUES (5), (7), (1000000);
		INSERT INTO key_class_children (parent_id) VALUES ('5'), ('1000000');
		INSERT INTO key_class_real_children (parent_id) VALUES (5), (1000000);`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	parents, err := newClient().KeyClassParents().GetMany(ctx, &models.GetKeyClassParentsInput{}, func(o *models.CallOptions[models.KeyClassParentFieldOptions]) {
		o.FieldOptions = &models.KeyClassParentFieldOptions{
			ID:                   true,
			KeyClassChildren:     &models.KeyClassChildRelationshipOptions{FieldOptions: &models.KeyClassChildFieldOptions{ID: true}},
			KeyClassRealChildren: &models.KeyClassRealChildRelationshipOptions{FieldOptions: &models.KeyClassRealChildFieldOptions{ID: true}},
		}
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := map[int64][2]int{}
	for _, p := range parents {
		got[p.ID] = [2]int{len(p.KeyClassChildren), len(p.KeyClassRealChildren)}
	}
	// A float FK buckets 1000000 as its integer parent does, not as 1e+06.
	want := map[int64][2]int{5: {1, 1}, 7: {0, 0}, 1000000: {1, 1}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("children by parent (-want +got):\n%s", diff)
	}
}
