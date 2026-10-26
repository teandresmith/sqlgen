package cli

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// TestBuildTypeRegistry_FollowsStructName pins PRD §8.5 "`sqlgen lint`
// resolves the override the same way, so its type registry keys on the names
// the package actually declares": with `tables.user_ips.struct_name:
// NetworkAddress` the package declares `NetworkAddress` and
// `TableNetworkAddresses`, and nothing spelled from the SQL name.
func TestBuildTypeRegistry_FollowsStructName(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{{
		Name:   "user_ips",
		Schema: "public",
		Columns: []parser.Column{
			{Name: "id", Type: "serial", PrimaryKey: true},
			{Name: "address", Type: "text"},
		},
	}}}
	cfg := &config.RootConfig{Tables: map[string]config.TableConfig{
		"user_ips": {StructName: "NetworkAddress"},
	}}

	reg, err := buildTypeRegistry(schema, cfg)
	if err != nil {
		t.Fatalf("buildTypeRegistry() error: %v", err)
	}

	t.Run("the table constant follows the struct_name", func(t *testing.T) {
		info, ok := reg.byConstant["TableNetworkAddresses"]
		if !ok {
			t.Fatalf("registry has no entry for TableNetworkAddresses; constants = %v", lsnKeys(reg.byConstant))
		}
		if info.SQLName != "user_ips" {
			t.Errorf("TableNetworkAddresses entry SQLName = %q, want %q", info.SQLName, "user_ips")
		}
		if !info.TypeNames["NetworkAddress"] {
			t.Errorf("TableNetworkAddresses entry TypeNames = %v, want NetworkAddress among them", info.TypeNames)
		}
	})
	t.Run("the struct type follows the struct_name", func(t *testing.T) {
		if _, ok := reg.byType["NetworkAddress"]; !ok {
			t.Errorf("registry has no type entry for NetworkAddress; types = %v", lsnKeys(reg.byType))
		}
	})
	t.Run("no name is spelled from the SQL table name", func(t *testing.T) {
		if _, ok := reg.byConstant["TableUserIPSes"]; ok {
			t.Error("registry has an entry for TableUserIPSes, which the package does not declare")
		}
		if _, ok := reg.byType["UserIPS"]; ok {
			t.Error("registry has a type entry for UserIPS, which the package does not declare")
		}
	})
}

func lsnKeys(m map[string]*tableInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
