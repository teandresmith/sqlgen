package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// fxJunctionYAML declares the products ↔ tags m2m edge through product_tags,
// with no junction key overrides, on top of tgfYAML.
func fxJunctionYAML() string {
	return tgfYAML + `tables:
  products:
    relationships:
      - name: Tags
        type: m2m
        table: tags
        junction: product_tags
`
}

// TestGenerate_M2MJunctionKeyInferenceNeedsSingleColumnPK pins that a side
// whose table has a composite primary key cannot infer its junction column:
// with no override it is an error naming the relationship and the key to
// declare.
func TestGenerate_M2MJunctionKeyInferenceNeedsSingleColumnPK(t *testing.T) {
	const compositeProducts = `CREATE TABLE products (workspace_id uuid NOT NULL, id uuid NOT NULL, name text NOT NULL, PRIMARY KEY (workspace_id, id));
CREATE TABLE tags (id uuid PRIMARY KEY, label text NOT NULL);
CREATE TABLE product_tags (product_key uuid NOT NULL, tag_id uuid NOT NULL, PRIMARY KEY (product_key, tag_id));`
	const compositeTags = `CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);
CREATE TABLE tags (workspace_id uuid NOT NULL, id uuid NOT NULL, label text NOT NULL, PRIMARY KEY (workspace_id, id));
CREATE TABLE product_tags (product_id uuid NOT NULL, tag_key uuid NOT NULL, PRIMARY KEY (product_id, tag_key));`
	tests := []struct {
		name    string
		ddl     string
		wantErr []string
	}{
		{
			name:    "this table composite, no override",
			ddl:     compositeProducts,
			wantErr: []string{`"Tags"`, "products", "workspace_id, id", "declare its junction key columns in junction_local_fk"},
		},
		{
			name:    "related table composite, no override",
			ddl:     compositeTags,
			wantErr: []string{`"Tags"`, "tags", "workspace_id, id", "declare its junction key columns in junction_reference_fk"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tgfTryGenerate(t, tt.ddl, fxJunctionYAML())
			if err == nil {
				t.Fatalf("Generate() error = nil, want one containing %q", tt.wantErr)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Generate() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

// TestBuildTableContexts_M2MJunctionKeysMustDiffer pins that a declared m2m
// entry whose two junction keys resolve to the same columns is an error: on a
// self-referential entry with only `junction:` both keys infer product_id, and
// an explicit equal pair is the same defect. With one or both keys declared so
// the two differ, the edge builds and keeps them apart.
func TestBuildTableContexts_M2MJunctionKeysMustDiffer(t *testing.T) {
	const ddl = `CREATE TABLE products (id bigint PRIMARY KEY, name text NOT NULL);
CREATE TABLE product_links (product_id bigint NOT NULL, related_product_id bigint NOT NULL, PRIMARY KEY (product_id, related_product_id));`
	tests := []struct {
		name          string
		local, ref    config.KeyColumns
		wantErr       []string
		wantLocal     string
		wantReference string
	}{
		{
			name:    "both keys inferred",
			wantErr: []string{"products.relationships", `"Related"`, "same product_links columns (product_id)", "junction_local_fk", "junction_reference_fk"},
		},
		{
			name:          "both keys declared and distinct",
			local:         config.KeyColumns{"product_id"},
			ref:           config.KeyColumns{"related_product_id"},
			wantLocal:     "product_id",
			wantReference: "related_product_id",
		},
		{
			name:    "both keys declared and equal",
			local:   config.KeyColumns{"related_product_id"},
			ref:     config.KeyColumns{"related_product_id"},
			wantErr: []string{`"Related"`, "same product_links columns (related_product_id)"},
		},
		{
			name:          "one key declared distinct from the other's inferred column",
			ref:           config.KeyColumns{"related_product_id"},
			wantLocal:     "product_id",
			wantReference: "related_product_id",
		},
		{
			name:    "one key declared equal to the other's inferred column",
			ref:     config.KeyColumns{"product_id"},
			wantErr: []string{`"Related"`, "same product_links columns (product_id)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dialect := range allDialects {
				t.Run(string(dialect), func(t *testing.T) {
					schema := parseDDLForTest(t, dialect, ddl)
					parser.DetectRelationships(schema)
					in := testInput(schema)
					in.Config.Input.Dialect = dialect
					in.Config.Tables["products"] = config.TableConfig{Relationships: []config.TableRelationship{{
						Name: "Related", Type: "m2m", Table: "products", Junction: "product_links",
						JunctionLocalFK: tt.local, JunctionReferenceFK: tt.ref,
					}}}
					contexts, err := gen.BuildTableContexts(in, nil)
					if tt.wantErr != nil {
						if err == nil {
							t.Fatalf("BuildTableContexts() error = nil, want one containing %q", tt.wantErr)
						}
						for _, want := range tt.wantErr {
							if !strings.Contains(err.Error(), want) {
								t.Errorf("BuildTableContexts() error = %q, want it to contain %q", err, want)
							}
						}
						return
					}
					if err != nil {
						t.Fatalf("BuildTableContexts() error: %v", err)
					}
					products, ok := tableContextByName(contexts, "products")
					if !ok {
						t.Fatal("no products context")
					}
					for _, r := range products.Relationships {
						if r.FieldName != "Related" {
							continue
						}
						if r.JunctionLocalFK != tt.wantLocal || r.JunctionReferenceFK != tt.wantReference {
							t.Errorf("Related junction keys = (%q, %q), want (%q, %q)", r.JunctionLocalFK, r.JunctionReferenceFK, tt.wantLocal, tt.wantReference)
						}
						return
					}
					t.Error("products has no Related relationship")
				})
			}
		})
	}
}

// TestGenerate_M2MInferredJunctionKeyIsALinkColumn pins that an inferred
// junction column is checked as a link column like a declared one: with
// `exclude_columns` removing product_tags.product_id from the model, the edge
// that infers it is an error naming the relationship and the column.
func TestGenerate_M2MInferredJunctionKeyIsALinkColumn(t *testing.T) {
	const ddl = `CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);
CREATE TABLE tags (id uuid PRIMARY KEY, label text NOT NULL);
CREATE TABLE product_tags (id uuid PRIMARY KEY, product_id uuid NOT NULL, tag_id uuid NOT NULL);`
	_, err := tgfTryGenerate(t, ddl, fxJunctionYAML()+`  product_tags:
    exclude_columns: [product_id]
`)
	if err == nil {
		t.Fatal("Generate() error = nil, want the link-column error for the inferred product_tags.product_id")
	}
	for _, want := range []string{`"Tags"`, "product_tags.product_id", "exclude_columns"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Generate() error = %q, want it to contain %q", err, want)
		}
	}
}
