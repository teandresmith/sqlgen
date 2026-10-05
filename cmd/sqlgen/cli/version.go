package cli

import (
	"runtime/debug"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// Version is the sqlgen release without its leading "v" ("0.3.0"), or "dev".
// Release builds set it through ldflags; a `go install …@vX.Y.Z` build has
// no ldflags, so init reads the release from the module version Go records in
// the binary.
var Version = "dev"

func init() {
	Version = resolveVersion(Version, debug.ReadBuildInfo)
}

// resolveVersion returns v unless it is still "dev" and the binary records a
// tagged release of the main module. A build from a checkout records a
// pseudo-version, or a version ending in "+dirty"; neither names a release,
// so both keep "dev".
func resolveVersion(v string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if v != "dev" {
		return v
	}
	info, ok := readBuildInfo()
	if !ok {
		return v
	}
	mv := info.Main.Version
	if !semver.IsValid(mv) || module.IsPseudoVersion(mv) || semver.Build(mv) != "" {
		return v
	}
	return strings.TrimPrefix(mv, "v")
}
