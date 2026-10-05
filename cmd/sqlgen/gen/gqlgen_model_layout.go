package gen

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// GqlgenModelImportAlias is the sqlgen-controlled Go import alias used to
// qualify gqlgen-emitted input type references in seed files and per-table
// translators. The alias is sqlgen-controlled rather than derived
// from gqlgen.yml's `model.package` so consumers cannot rename their runtime
// models package into a value that collides with this spelling — config-load
// validation guards against that case explicitly.
const GqlgenModelImportAlias = "gqlmodel"

// readGqlgenModelInfo reads gqlgen.yml at gqlgenConfigPath, parses just the
// `model:` block, and returns the consumer-side import path + alias for
// use in the rendered seed / translator file preambles. Returns empty
// strings (no error) when:
//
//   - gqlgenConfigPath is empty (config doesn't enable graphql)
//   - the file is missing on disk (consumer hasn't run `sqlgen graphql init`)
//   - modulePath is empty (consumer's go.mod can't be located)
//
// Each fail-soft case mirrors the existing ModelsImportPath path: when the
// metadata can't be derived, the import line is dropped and the file emits
// without the gqlgen reference. The translator templates only emit
// gqlgen-qualified type references when GqlgenModelAlias is non-empty,
// keeping the two ends in lockstep.
func readGqlgenModelInfo(gqlgenConfigPath, modulePath string) (string, string) {
	if gqlgenConfigPath == "" || modulePath == "" {
		return "", ""
	}
	data, err := os.ReadFile(filepath.Clean(gqlgenConfigPath))
	if err != nil {
		return "", ""
	}
	var doc struct {
		Model struct {
			Filename string `yaml:"filename"`
			Package  string `yaml:"package"`
		} `yaml:"model"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", ""
	}
	filename := doc.Model.Filename
	if filename == "" {
		filename = "graph/model/models_gen.go"
	}
	dir := filepath.ToSlash(filepath.Dir(filename))
	dir = strings.TrimPrefix(dir, "./")
	dir = strings.TrimPrefix(dir, "/")
	if dir == "" || dir == "." {
		return modulePath, GqlgenModelImportAlias
	}
	return modulePath + "/" + dir, GqlgenModelImportAlias
}
