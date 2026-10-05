package cli

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name    string
		v       string
		mainVer string
		noInfo  bool
		want    string
	}{
		{name: "ldflags version wins", v: "0.3.0", mainVer: "v0.4.0", want: "0.3.0"},
		{name: "go install of a release", v: "dev", mainVer: "v0.3.0", want: "0.3.0"},
		{name: "go install of a prerelease", v: "dev", mainVer: "v0.3.0-rc.1", want: "0.3.0-rc.1"},
		{name: "checkout build records a pseudo-version", v: "dev", mainVer: "v0.0.0-20261005144417-b4ea58f75953", want: "dev"},
		{name: "pseudo-version after a release tag", v: "dev", mainVer: "v0.3.1-0.20261005144417-b4ea58f75953", want: "dev"},
		{name: "dirty tree at a tag", v: "dev", mainVer: "v0.3.0+dirty", want: "dev"},
		{name: "go run", v: "dev", mainVer: "(devel)", want: "dev"},
		{name: "no build info", v: "dev", noInfo: true, want: "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := func() (*debug.BuildInfo, bool) {
				if tt.noInfo {
					return nil, false
				}
				return &debug.BuildInfo{Main: debug.Module{Version: tt.mainVer}}, true
			}
			if got := resolveVersion(tt.v, read); got != tt.want {
				t.Errorf("resolveVersion(%q) with main version %q = %q, want %q", tt.v, tt.mainVer, got, tt.want)
			}
		})
	}
}
