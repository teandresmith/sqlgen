package gen

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestResolveAccess pins the PRD §32.2 role → capability matrix: every role
// resolves to exactly its row, and empty / unknown values fall back to
// public. roleCapabilityTable is the single source of truth the templates
// consume — a drift here silently reshapes every external surface.
func TestResolveAccess(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		wantRole string
		wantCaps accessCapabilities
	}{
		{
			name:     "public",
			role:     config.AccessPublic,
			wantRole: "public",
			wantCaps: accessCapabilities{
				APIReadable: true, APIWritable: true, APIFilterable: true, APISortable: true,
				EventRedacted: false, ManifestVisibility: "public",
			},
		},
		{
			name:     "read_only",
			role:     config.AccessReadOnly,
			wantRole: "read_only",
			wantCaps: accessCapabilities{
				APIReadable: true, APIWritable: false, APIFilterable: true, APISortable: true,
				EventRedacted: false, ManifestVisibility: "read_only",
			},
		},
		{
			name:     "write_only",
			role:     config.AccessWriteOnly,
			wantRole: "write_only",
			wantCaps: accessCapabilities{
				APIReadable: false, APIWritable: true, APIFilterable: false, APISortable: false,
				EventRedacted: true, ManifestVisibility: "write_only",
			},
		},
		{
			name:     "hidden",
			role:     config.AccessHidden,
			wantRole: "hidden",
			wantCaps: accessCapabilities{
				APIReadable: false, APIWritable: false, APIFilterable: false, APISortable: false,
				EventRedacted: false, ManifestVisibility: "hidden",
			},
		},
		{
			name:     "internal",
			role:     config.AccessInternal,
			wantRole: "internal",
			wantCaps: accessCapabilities{
				APIReadable: false, APIWritable: false, APIFilterable: false, APISortable: false,
				EventRedacted: true, ManifestVisibility: "internal",
			},
		},
		{
			name:     "empty resolves to public",
			role:     "",
			wantRole: "public",
			wantCaps: accessCapabilities{
				APIReadable: true, APIWritable: true, APIFilterable: true, APISortable: true,
				EventRedacted: false, ManifestVisibility: "public",
			},
		},
		{
			name:     "unknown resolves to public",
			role:     "confidential",
			wantRole: "public",
			wantCaps: accessCapabilities{
				APIReadable: true, APIWritable: true, APIFilterable: true, APISortable: true,
				EventRedacted: false, ManifestVisibility: "public",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRole, gotCaps := resolveAccess(tt.role)
			if gotRole != tt.wantRole {
				t.Errorf("resolveAccess(%q) role = %q, want %q", tt.role, gotRole, tt.wantRole)
			}
			if diff := cmp.Diff(tt.wantCaps, gotCaps); diff != "" {
				t.Errorf("resolveAccess(%q) capabilities mismatch (-want +got):\n%s", tt.role, diff)
			}
		})
	}
}
