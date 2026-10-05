package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestInitConfigContent_validatesAgainstSchema closes the loop between the two
// halves of editor support: `sqlgen init` writes a modeline pointing at the
// config schema, so the config it writes must itself satisfy that schema.
// Without this, a change to the starter template could ship a file that a
// user's editor flags as invalid the moment they open it.
func TestInitConfigContent_validatesAgainstSchema(t *testing.T) {
	sch, err := jsonschema.CompileString("embedded://config/schema/v1.json", string(config.SchemaV1()))
	if err != nil {
		t.Fatalf("CompileString(embedded schema) = %v, want nil", err)
	}

	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			content := initConfigContent(dialect)

			if !strings.HasPrefix(content, config.SchemaModeline+"\n") {
				t.Errorf("initConfigContent(%q) does not start with the schema modeline;\ngot first line: %q",
					dialect, strings.SplitN(content, "\n", 2)[0])
			}

			var doc any
			if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
				t.Fatalf("yaml.Unmarshal(initConfigContent(%q)) = %v, want nil", dialect, err)
			}
			encoded, err := json.Marshal(doc)
			if err != nil {
				t.Fatalf("json.Marshal = %v, want nil", err)
			}
			var normalized any
			if err := json.Unmarshal(encoded, &normalized); err != nil {
				t.Fatalf("json.Unmarshal = %v, want nil", err)
			}

			if err := sch.Validate(normalized); err != nil {
				t.Errorf("Validate(initConfigContent(%q)) = %v, want nil", dialect, err)
			}
		})
	}
}
