package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

func TestConnectionPaginationPassthrough(t *testing.T) {
	// PRD §26.5.3 — Relay Connection args (first / after / last / before)
	// map directly to ConnectionInput, no translator function.
	// Q.Products takes a pre-translated *<pkg>.<Table>Filter directly
	// from the seed (no translation in Q); the seed calls translateProductFilter.
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	// Pass-through field assignments in Q.Products body.
	mustContain(t, out, "First:  first,")
	mustContain(t, out, "After:  after,")
	mustContain(t, out, "Last:   last,")
	mustContain(t, out, "Before: before,")
	// Filter is a direct pass-through of the model-side type (already translated).
	mustContain(t, out, "Filter: filter,")

	// Seed delegations call the translator before passing into Q.
	mustContain(t, seeds, "translateProductFilter(filter)")

	// No mystery translator symbol — connection args remain pass-through.
	forbidden := []string{
		"translatePagination",
		"translateConnectionPagination",
		"translateProductPagination",
	}
	for _, sym := range forbidden {
		if strings.Contains(out, sym) || strings.Contains(seeds, sym) {
			t.Errorf("translator symbol %q must not exist (PRD §26.5.3 — pagination is pass-through)\n%s\n%s", sym, out, seeds)
		}
	}
}

func TestListPaginationPassthrough(t *testing.T) {
	// PRD §26.5.3 — `<table>List` args (limit / offset) map directly to the
	// List method's input. Q.ProductList takes a pre-translated
	// *<pkg>.<Table>Filter from the seed.
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	mustContain(t, out, "in := models.PaginateInput[models.ProductFilter]{")
	mustContain(t, out, "Filter: filter,")
	mustContain(t, out, "if limit != nil {")
	mustContain(t, out, "in.Limit = *limit")
	mustContain(t, out, "if offset != nil {")
	mustContain(t, out, "in.Offset = *offset")

	// Seed delegations translate the filter argument.
	mustContain(t, seeds, "translateProductFilter(filter)")

	if strings.Contains(out, "translateListPagination") || strings.Contains(seeds, "translateListPagination") {
		t.Errorf("translateListPagination must not exist (PRD §26.5.3 — list pagination is pass-through)")
	}
}
