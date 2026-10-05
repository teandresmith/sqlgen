package introspect

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestChooseMatviewPK(t *testing.T) {
	tests := []struct {
		name    string
		indexes []matviewUniqueIndex
		want    []string
	}{
		{
			name: "single index",
			indexes: []matviewUniqueIndex{
				{name: "mv_id_idx", columns: []string{"id"}},
			},
			want: []string{"id"},
		},
		{
			name: "fewest columns wins",
			indexes: []matviewUniqueIndex{
				{name: "a_two_col_idx", columns: []string{"tenant_id", "user_id"}},
				{name: "z_single_col_idx", columns: []string{"id"}},
			},
			want: []string{"id"},
		},
		{
			name: "column-count tie broken by index name ascending",
			indexes: []matviewUniqueIndex{
				{name: "mv_email_idx", columns: []string{"email"}},
				{name: "mv_account_idx", columns: []string{"account_id"}},
			},
			want: []string{"account_id"},
		},
		{
			name: "multi-column winner keeps column order",
			indexes: []matviewUniqueIndex{
				{name: "mv_three_idx", columns: []string{"a", "b", "c"}},
				{name: "mv_pair_idx", columns: []string{"tenant_id", "user_id"}},
			},
			want: []string{"tenant_id", "user_id"},
		},
		{
			name: "selection independent of input order",
			indexes: []matviewUniqueIndex{
				{name: "z_single_col_idx", columns: []string{"id"}},
				{name: "a_two_col_idx", columns: []string{"tenant_id", "user_id"}},
			},
			want: []string{"id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chooseMatviewPK(tt.indexes)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("chooseMatviewPK() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
