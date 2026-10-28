package gen_test

import (
	"strings"
	"testing"
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
