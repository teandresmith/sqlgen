package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/mcp"
)

// emitMCPProjectConfigs is the pipeline caller for the MCP.md §6.6 merge: it
// loops over manifest.mcp.project_configs and upserts (or removes) the
// sqlgen-managed server entry in each target. Emission resolves on when
// manifest.enabled and mcp.emit_project_config are both true; otherwise the
// run removes any previously-emitted entries so flipping the toggle off (or
// disabling the manifest entirely) cleans up on the next generation (§3.4
// opt-out).
//
// A per-target failure is reported but does not abort the remaining targets;
// the aggregated error makes the overall run exit non-zero when any target
// failed. Warnings (e.g. the unmanaged-collision warning) are returned for
// the caller to print.
func emitMCPProjectConfigs(cfg *config.RootConfig, version string) ([]string, error) {
	m := cfg.Generation.Manifest
	enabled := m != nil && m.Enabled != nil && *m.Enabled

	// Resolve the §3.4 settings, falling back to the documented defaults so
	// the removal path works even when the manifest block (and thus the
	// defaults pass) is absent or disabled.
	serverKey := "sqlgen"
	targets := []string{".mcp.json"}
	commandTemplate := ""
	emit := enabled
	if m != nil && m.MCP != nil {
		if m.MCP.ServerKey != "" {
			serverKey = m.MCP.ServerKey
		}
		if len(m.MCP.ProjectConfigs) > 0 {
			targets = m.MCP.ProjectConfigs
		}
		commandTemplate = m.MCP.CommandTemplate
		if m.MCP.EmitProjectConfig != nil {
			emit = enabled && *m.MCP.EmitProjectConfig
		}
	}
	action := mcp.MergeRemove
	if emit {
		action = mcp.MergeUpsert
	}

	var warnings []string
	var errs []error
	for _, target := range targets {
		w, err := mcp.Merge(mcp.MergeOptions{
			ConfigPath:      target,
			ModuleRoot:      ".",
			ServerKey:       serverKey,
			ManifestPath:    manifestIdentityPath(m, cfg.Output.Dir),
			CommandTemplate: commandTemplate,
			Version:         version,
			Action:          action,
		})
		warnings = append(warnings, w...)
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp project config %s: %w", target, err))
		}
	}
	return warnings, errors.Join(errs...)
}

// manifestIdentityPath returns the module-root-relative path of the JSON
// manifest the emitted entry serves, mirroring EmitJSON's placement:
// <output.dir>/<markdown_dir>/<json_filename> (defaults manifest /
// manifest_gen.json). Mirroring gen's apiImportDir,
// SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE overrides the directory used for the
// entry's *identity* only — when the E2E golden harness redirects writes to a
// temp directory, the committed .mcp.json keeps its stable manifest path
// instead of churning to /tmp on every compare run.
func manifestIdentityPath(m *config.ManifestConfig, outputDir string) string {
	if override := os.Getenv("SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE"); override != "" {
		outputDir = override
	}
	manifestDir := "manifest"
	jsonName := "manifest_gen.json"
	if m != nil && m.MarkdownDir != "" {
		manifestDir = m.MarkdownDir
	}
	if m != nil && m.JSONFilename != "" {
		jsonName = m.JSONFilename
	}
	return filepath.Join(outputDir, manifestDir, jsonName)
}
