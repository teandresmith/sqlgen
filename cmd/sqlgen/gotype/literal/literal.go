// Package literal holds the registry of Go type literals sqlgen can place in a
// generated file without being told their import: builtins and the handful of
// standard-library and sqlgen types whose import path is fixed.
//
// It is a leaf so both gotype, which resolves literals through it, and config,
// which validates the config surfaces that name a Go type, can consult the one
// registry — gotype imports config, so config cannot import gotype.
package literal

import "strings"

// Info is a known literal's import path (empty for a builtin) and zero value.
type Info struct {
	Import string
	Zero   string
}

// known maps each Go type literal sqlgen resolves on its own to its import
// path and zero value.
var known = map[string]Info{
	"string":           {"", `""`},
	"int":              {"", "0"},
	"int8":             {"", "0"},
	"int16":            {"", "0"},
	"int32":            {"", "0"},
	"int64":            {"", "0"},
	"uint":             {"", "0"},
	"uint8":            {"", "0"},
	"uint16":           {"", "0"},
	"uint32":           {"", "0"},
	"uint64":           {"", "0"},
	"float32":          {"", "0"},
	"float64":          {"", "0"},
	"bool":             {"", "false"},
	"byte":             {"", "0"},
	"rune":             {"", "0"},
	"time.Time":        {"time", "time.Time{}"},
	"time.Duration":    {"time", "0"},
	"any":              {"", "nil"},
	"net.IP":           {"net", "nil"},
	"net.IPNet":        {"net", "net.IPNet{}"},
	"net.HardwareAddr": {"net", "nil"},
	"json.RawMessage":  {"encoding/json", "nil"},
	"types.JSON":       {"github.com/teandresmith/sqlgen/types", "nil"},
}

// Lookup returns the registry entry for goType and whether it is known.
func Lookup(goType string) (Info, bool) {
	info, ok := known[goType]
	return info, ok
}

// NeedsExplicitImport reports whether a Go type literal names a package the
// registry cannot supply on its own. Same-package types (`Address`) and
// builtins carry no qualifier; `time.Time`, `json.RawMessage` and the rest of
// the registry carry one but resolve their own import (PRD §4.7).
func NeedsExplicitImport(goType string) bool {
	if !strings.Contains(goType, ".") {
		return false
	}
	_, ok := known[goType]
	return !ok
}
