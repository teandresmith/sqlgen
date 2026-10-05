package manifest

import "errors"

// Sentinel errors exported for callers of the runtime-embedded manifest.
var (
	// ErrEntityNotFound is returned by the generated Client.ManifestEntity when
	// no entity matches the requested table name.
	ErrEntityNotFound = errors.New("manifest: entity not found")
	// ErrParseManifest wraps any failure to parse the embedded manifest bytes.
	// The generated init() panics with this so a corrupt embed surfaces at
	// package-load time rather than on first use.
	ErrParseManifest = errors.New("manifest: parse failure (corrupt embed)")
)
