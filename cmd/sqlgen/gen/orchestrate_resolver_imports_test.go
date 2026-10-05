package gen

import "testing"

// TestComputeResolverFileFeatures_GetOnlyTriggersErrorsImport pins the
// errors-import gating: the Get resolver branch collapses ErrNotFound → (nil, nil)
// via errors.Is, so a config that enables Get without any delete-class
// operation still needs the `errors` import in the rendered file's preamble.
// A gate keyed solely on HardDelete / SoftDelete / Restore would make a
// Get-only-no-delete config emit `errors.Is(err, models.ErrNotFound)`
// against an import block missing "errors" — unparseable Go.
func TestComputeResolverFileFeatures_GetOnlyTriggersErrorsImport(t *testing.T) {
	tests := []struct {
		name string
		ops  ResolvedOperations
		want bool
	}{
		{
			name: "get-only-no-delete still needs errors",
			ops:  ResolvedOperations{Get: true},
			want: true,
		},
		{
			name: "hard-delete-only still needs errors",
			ops:  ResolvedOperations{HardDelete: true},
			want: true,
		},
		{
			name: "soft-delete-only still needs errors",
			ops:  ResolvedOperations{SoftDelete: true},
			want: true,
		},
		{
			name: "restore-only still needs errors",
			ops:  ResolvedOperations{Restore: true},
			want: true,
		},
		{
			name: "get plus delete still needs errors",
			ops:  ResolvedOperations{Get: true, HardDelete: true},
			want: true,
		},
		{
			name: "create-only does not need errors",
			ops:  ResolvedOperations{Create: true},
			want: false,
		},
		{
			name: "connection-only does not need errors",
			ops:  ResolvedOperations{Connection: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiCtx := &APIContext{
				Tables: []APITableContext{
					{Operations: tt.ops},
				},
			}
			got := computeResolverFileFeatures(apiCtx)
			if got.NeedsErrorsImport != tt.want {
				t.Errorf("NeedsErrorsImport: got %v, want %v (ops=%+v)", got.NeedsErrorsImport, tt.want, tt.ops)
			}
		})
	}
}
