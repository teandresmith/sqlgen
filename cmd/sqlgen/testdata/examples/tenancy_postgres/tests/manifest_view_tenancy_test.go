package tests

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/manifest"
)

// The manifest leg of view tenancy (PRD §30.4.2). This example is the only
// one in the tree that pairs `tenancy.enabled: true` with a view, so its
// emitted artifacts are what pin the read-only-entity features.tenancy shape.
//
// Schema validation of these same artifacts against the frozen schema/v1.json
// lives in cmd/sqlgen/cli/manifest_validate_e2e_test.go, where the embedded
// schema and the jsonschema library are importable.

func readEntity(t *testing.T, name string) (manifest.Entity, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile("../models/manifest/entities/" + name + ".json")
	if err != nil {
		t.Fatalf("reading %s entity: %v", name, err)
	}
	var typed manifest.Entity
	if err := json.Unmarshal(raw, &typed); err != nil {
		t.Fatalf("decoding %s entity: %v", name, err)
	}
	var loose map[string]any
	if err := json.Unmarshal(raw, &loose); err != nil {
		t.Fatalf("decoding %s entity loosely: %v", name, err)
	}
	return typed, loose
}

// TestManifest_TenantedViewReportsTenancy asserts the emitted view entities
// carry features.tenancy. Reporting null here would tell every manifest
// consumer — the MCP server, an agent, a drift check — that a scoped view's
// rows are globally visible.
func TestManifest_TenantedViewReportsTenancy(t *testing.T) {
	for _, name := range []string{"article_stat", "workspace_line_total"} {
		t.Run(name, func(t *testing.T) {
			entity, loose := readEntity(t, name)

			if entity.Kind != "view" {
				t.Fatalf("kind = %q, want view", entity.Kind)
			}
			if entity.Features.Tenancy == nil {
				t.Fatalf("features.tenancy = nil, want populated")
			}
			if got := entity.Features.Tenancy.Column; got != "workspace_id" {
				t.Errorf("tenancy.column = %q, want workspace_id", got)
			}
			// "verify-match" denotes the §29.4.2 / §29.7 mutation-input check,
			// which has no read-only analogue — even for workspace_line_total,
			// whose @pk annotation names the tenant column.
			if got := entity.Features.Tenancy.Mode; got != "auto-filter" {
				t.Errorf("tenancy.mode = %q, want auto-filter", got)
			}
			if got := entity.Features.Tenancy.MissingResolverError; got != "tenancy.ErrMissing" {
				t.Errorf("tenancy.missing_resolver_error = %q, want tenancy.ErrMissing", got)
			}

			// omitempty must remove the key entirely, not emit "".
			features, ok := loose["features"].(map[string]any)
			if !ok {
				t.Fatalf("features is not an object")
			}
			tenancyBlock, ok := features["tenancy"].(map[string]any)
			if !ok {
				t.Fatalf("features.tenancy is not an object")
			}
			if _, present := tenancyBlock["mismatch_error"]; present {
				t.Errorf("view entity carries a mismatch_error key; it names an error a read-only entity can never return")
			}

			// A view emits no events and has no soft-delete column; cache stays
			// null pending its own spec pass (§30.4.2).
			if entity.Features.Events != nil || entity.Features.SoftDelete != nil || entity.Features.Cache != nil {
				t.Errorf("view features = %+v, want events/soft_delete/cache all nil", entity.Features)
			}

			// §30.4.2: errors[] lists the sentinels the generated body can
			// actually reach. Every view read resolves the tenant and this
			// example runs required:true, so all five carry ErrMissing — and
			// none carries ErrMismatch, which is a mutation rule. The E2E
			// suite proves the same five methods really do return it.
			for _, m := range entity.Methods.Query {
				if !slices.Contains(m.Errors, "tenancy.ErrMissing") {
					t.Errorf("%s.errors = %v, want it to include tenancy.ErrMissing", m.Name, m.Errors)
				}
				if slices.Contains(m.Errors, "tenancy.ErrMismatch") {
					t.Errorf("%s.errors advertises tenancy.ErrMismatch, which a read-only entity can never return", m.Name)
				}
			}
			// Refresh is never scoped and never resolves (§29.2.5).
			for _, m := range entity.Methods.Mutation {
				if slices.Contains(m.Errors, "tenancy.ErrMissing") {
					t.Errorf("%s.errors = %v; matview refresh resolves no tenant", m.Name, m.Errors)
				}
			}
		})
	}
}

// TestManifest_TenantedTableStillCarriesMismatchError is the converse: adding
// omitempty must not silently drop the field on the table shape, where it is
// always populated.
func TestManifest_TenantedTableStillCarriesMismatchError(t *testing.T) {
	entity, loose := readEntity(t, "article")

	if entity.Features.Tenancy == nil {
		t.Fatalf("table features.tenancy = nil, want populated")
	}
	if got := entity.Features.Tenancy.MismatchError; got != "tenancy.ErrMismatch" {
		t.Errorf("table tenancy.mismatch_error = %q, want tenancy.ErrMismatch", got)
	}
	features := loose["features"].(map[string]any)
	tenancyBlock := features["tenancy"].(map[string]any)
	if _, present := tenancyBlock["mismatch_error"]; !present {
		t.Errorf("table entity lost its mismatch_error key")
	}
}
