// Package featuressuite is the conformance suite for the generated client with
// tenancy, cache and events enabled (docs/design/CONFORMANCE_VARIANTS.md). Each
// Features module generates the Features schema into a package at the module
// path this suite imports and calls Run with a Harness wired to its database.
// Its tree sits under one subtest, Features, so a registry citation reads
// `conformance: Features/<area>/<behavior>`.
package featuressuite

import (
	"context"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models"
)

// Harness is the per-dialect wiring the suite runs against.
type Harness struct {
	// Dialect is postgres, mysql or sqlite; Driver is pgx or stdlib. They label
	// failures only: no test branches on them.
	Dialect string
	Driver  string
	// Database names the server and its version. Run logs it for the
	// conformance report.
	Database string
	// Client is built on the dialect's querier with no options.
	Client *models.Client
}

// Run runs the Features suite against h.
func Run(t *testing.T, h *Harness) {
	// make conformance reads this line into the report header; keep its prefix.
	t.Logf("conformance harness: database %s", h.Database)
	t.Run("Features", func(t *testing.T) {
		t.Run("harness", func(t *testing.T) {
			n, err := h.Client.Notes().Count(context.Background(), nil)
			if err != nil || n != 0 {
				t.Fatalf("Notes.Count() = %d, %v, want 0, nil", n, err)
			}
		})
	})
}
