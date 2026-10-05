package gen

import "github.com/teandresmith/sqlgen/cmd/sqlgen/config"

// accessCapabilities is the per-surface exposure derived from a column's
// access role — the template-facing decomposition of the PRD §32.2 matrix.
// Templates and context builders branch on these booleans, never on the
// role name itself.
type accessCapabilities struct {
	// APIReadable: the column appears in the GraphQL object type (API out).
	APIReadable bool
	// APIWritable: the column appears in the API create/update inputs.
	APIWritable bool
	// APIFilterable: the column appears in the API filter input (subject to
	// the column type having a comparator — access only removes, never adds).
	APIFilterable bool
	// APISortable: the column appears in the API sort enum.
	APISortable bool
	// EventRedacted: the column is cleared from the published Event.Input clone.
	EventRedacted bool
	// ManifestVisibility: the access marker emitted on the manifest column.
	ManifestVisibility string
}

// roleCapabilityTable is the single source of truth for the PRD §32.2
// role → capability matrix.
var roleCapabilityTable = map[string]accessCapabilities{
	config.AccessPublic: {
		APIReadable: true, APIWritable: true, APIFilterable: true, APISortable: true,
		EventRedacted: false, ManifestVisibility: config.AccessPublic,
	},
	config.AccessReadOnly: {
		APIReadable: true, APIWritable: false, APIFilterable: true, APISortable: true,
		EventRedacted: false, ManifestVisibility: config.AccessReadOnly,
	},
	config.AccessWriteOnly: {
		APIReadable: false, APIWritable: true, APIFilterable: false, APISortable: false,
		EventRedacted: true, ManifestVisibility: config.AccessWriteOnly,
	},
	config.AccessHidden: {
		APIReadable: false, APIWritable: false, APIFilterable: false, APISortable: false,
		EventRedacted: false, ManifestVisibility: config.AccessHidden,
	},
	config.AccessInternal: {
		APIReadable: false, APIWritable: false, APIFilterable: false, APISortable: false,
		EventRedacted: true, ManifestVisibility: config.AccessInternal,
	},
}

// resolveAccess normalizes a configured access role and returns it with its
// capability set. Empty and unknown values resolve to public — config
// validation rejects unknown roles before generation reaches this point, so
// the fallback only serves direct (test / programmatic) construction.
func resolveAccess(role string) (string, accessCapabilities) {
	if caps, ok := roleCapabilityTable[role]; ok {
		return role, caps
	}
	return config.AccessPublic, roleCapabilityTable[config.AccessPublic]
}

// columnAccessCapabilities returns the column's resolved capability set. A
// ColumnContext with an empty Access role (a hand-built fixture that skipped
// the access resolution pass) resolves to public — otherwise the zero-valued
// capability booleans would silently drop the column from every API surface.
// Production ColumnContexts always carry a
// resolved role, so this reads the stored booleans verbatim for them.
func columnAccessCapabilities(col ColumnContext) accessCapabilities {
	if col.Access == "" {
		_, caps := resolveAccess("")
		return caps
	}
	return accessCapabilities{
		APIReadable:        col.APIReadable,
		APIWritable:        col.APIWritable,
		APIFilterable:      col.APIFilterable,
		APISortable:        col.APISortable,
		EventRedacted:      col.EventRedacted,
		ManifestVisibility: col.ManifestVisibility,
	}
}
