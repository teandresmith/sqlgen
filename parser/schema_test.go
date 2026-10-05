package parser_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/parser"
)

func TestSchemaModelConstruction(t *testing.T) {
	s := parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{
						Name:       "id",
						Type:       "bigint",
						PrimaryKey: true,
					},
					{
						Name:     "email",
						Type:     "text",
						Unique:   true,
						Nullable: false,
					},
					{
						Name:     "name",
						Type:     "text",
						Nullable: true,
						Default:  "'anonymous'",
					},
					{
						Name:        "company_id",
						Type:        "bigint",
						Nullable:    true,
						FKReference: &parser.FKReference{Table: "companies", Schema: "public", Column: "id"},
					},
					{
						Name:          "serial_col",
						Type:          "integer",
						AutoIncrement: true,
					},
					{
						Name:    "bio",
						Type:    "text",
						Comment: "User biography",
					},
				},
				Constraints: []parser.Constraint{
					{
						Name:    "users_pkey",
						Type:    parser.PrimaryKey,
						Columns: []string{"id"},
					},
					{
						Name:             "users_company_id_fkey",
						Type:             parser.ForeignKey,
						Columns:          []string{"company_id"},
						ReferenceTable:   "companies",
						ReferenceSchema:  "public",
						ReferenceColumns: []string{"id"},
					},
					{
						Name:    "users_email_key",
						Type:    parser.Unique,
						Columns: []string{"email"},
					},
					{
						Name:            "users_name_check",
						Type:            parser.Check,
						CheckExpression: "length(name) > 0",
					},
					{
						Name:    "users_name_idx",
						Type:    parser.Index,
						Columns: []string{"name"},
					},
				},
				Comment: "All registered users",
			},
		},
		Enums: []parser.Enum{
			{
				Name:   "status",
				Schema: "public",
				Values: []string{"active", "inactive", "banned"},
			},
		},
		CompositeTypes: []parser.CompositeType{
			{
				Name:   "address",
				Schema: "public",
				Attributes: []parser.Attribute{
					{Name: "street", Type: "text"},
					{Name: "city", Type: "text"},
					{Name: "zip", Type: "text"},
				},
			},
		},
		DomainTypes: []parser.DomainType{
			{
				Name:     "email_address",
				Schema:   "public",
				BaseType: "text",
				Constraints: []parser.Constraint{
					{
						Name:            "email_check",
						Type:            parser.Check,
						CheckExpression: "VALUE ~ '^.+@.+$'",
					},
				},
			},
		},
		Views: []parser.View{
			{
				Name:   "active_users",
				Schema: "public",
				SQL:    "SELECT id, name FROM users WHERE status = 'active'",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint"},
					{Name: "name", Type: "text", Nullable: true},
				},
			},
		},
		Relationships: []parser.Relationship{
			{
				Name:        "Company",
				Type:        parser.OneToOne,
				SourceTable: "users",
				TargetTable: "companies",
				FKColumn:    "company_id",
			},
			{
				Name:                "Tags",
				Type:                parser.ManyToMany,
				SourceTable:         "products",
				TargetTable:         "tags",
				JunctionTable:       "product_tags",
				JunctionLocalFK:     "product_id",
				JunctionReferenceFK: "tag_id",
			},
			{
				Name:        "Orders",
				Type:        parser.OneToMany,
				SourceTable: "users",
				TargetTable: "orders",
				FKColumn:    "user_id",
				Filter:      "status = 'active'",
				Sort: []parser.RelationshipSort{
					{Column: "created_at", Direction: "desc"},
				},
			},
		},
	}

	// Verify table fields.
	if len(s.Tables) != 1 {
		t.Fatalf("len(Tables) = %d, want 1", len(s.Tables))
	}
	tbl := s.Tables[0]
	if tbl.Name != "users" {
		t.Errorf("Table.Name = %q, want %q", tbl.Name, "users")
	}
	if tbl.Schema != "public" {
		t.Errorf("Table.Schema = %q, want %q", tbl.Schema, "public")
	}
	if tbl.Comment != "All registered users" {
		t.Errorf("Table.Comment = %q, want %q", tbl.Comment, "All registered users")
	}
	if len(tbl.Columns) != 6 {
		t.Fatalf("len(Columns) = %d, want 6", len(tbl.Columns))
	}

	// Verify column fields.
	col := tbl.Columns[0]
	if col.Name != "id" || col.Type != "bigint" || !col.PrimaryKey {
		t.Errorf("Column[0] = {Name:%q Type:%q PK:%v}, want {Name:\"id\" Type:\"bigint\" PK:true}", col.Name, col.Type, col.PrimaryKey)
	}
	col = tbl.Columns[1]
	if !col.Unique || col.Nullable {
		t.Errorf("Column[1] Unique=%v Nullable=%v, want Unique=true Nullable=false", col.Unique, col.Nullable)
	}
	col = tbl.Columns[2]
	if col.Default != "'anonymous'" {
		t.Errorf("Column[2].Default = %q, want %q", col.Default, "'anonymous'")
	}
	col = tbl.Columns[3]
	if col.FKReference == nil {
		t.Fatalf("Column[3].FKReference = nil, want non-nil")
	}
	if col.FKReference.Table != "companies" || col.FKReference.Schema != "public" || col.FKReference.Column != "id" {
		t.Errorf("Column[3].FKReference = %+v, want {Table:companies Schema:public Column:id}", col.FKReference)
	}
	col = tbl.Columns[4]
	if !col.AutoIncrement {
		t.Errorf("Column[4].AutoIncrement = false, want true")
	}
	col = tbl.Columns[5]
	if col.Comment != "User biography" {
		t.Errorf("Column[5].Comment = %q, want %q", col.Comment, "User biography")
	}

	// Verify constraint types.
	if len(tbl.Constraints) != 5 {
		t.Fatalf("len(Constraints) = %d, want 5", len(tbl.Constraints))
	}
	wantTypes := []parser.ConstraintType{parser.PrimaryKey, parser.ForeignKey, parser.Unique, parser.Check, parser.Index}
	for i, ct := range wantTypes {
		if tbl.Constraints[i].Type != ct {
			t.Errorf("Constraint[%d].Type = %v, want %v", i, tbl.Constraints[i].Type, ct)
		}
	}

	// Verify enum.
	if len(s.Enums) != 1 {
		t.Fatalf("len(Enums) = %d, want 1", len(s.Enums))
	}
	if diff := cmp.Diff([]string{"active", "inactive", "banned"}, s.Enums[0].Values); diff != "" {
		t.Errorf("Enum.Values mismatch (-want +got):\n%s", diff)
	}

	// Verify composite type.
	if len(s.CompositeTypes) != 1 {
		t.Fatalf("len(CompositeTypes) = %d, want 1", len(s.CompositeTypes))
	}
	if s.CompositeTypes[0].Name != "address" {
		t.Errorf("CompositeType.Name = %q, want %q", s.CompositeTypes[0].Name, "address")
	}
	if len(s.CompositeTypes[0].Attributes) != 3 {
		t.Errorf("len(CompositeType.Attributes) = %d, want 3", len(s.CompositeTypes[0].Attributes))
	}

	// Verify domain type.
	if len(s.DomainTypes) != 1 {
		t.Fatalf("len(DomainTypes) = %d, want 1", len(s.DomainTypes))
	}
	if s.DomainTypes[0].BaseType != "text" {
		t.Errorf("DomainType.BaseType = %q, want %q", s.DomainTypes[0].BaseType, "text")
	}

	// Verify view.
	if len(s.Views) != 1 {
		t.Fatalf("len(Views) = %d, want 1", len(s.Views))
	}
	if s.Views[0].Name != "active_users" {
		t.Errorf("View.Name = %q, want %q", s.Views[0].Name, "active_users")
	}
	if len(s.Views[0].Columns) != 2 {
		t.Errorf("len(View.Columns) = %d, want 2", len(s.Views[0].Columns))
	}

	// Verify relationships.
	if len(s.Relationships) != 3 {
		t.Fatalf("len(Relationships) = %d, want 3", len(s.Relationships))
	}
	if s.Relationships[0].Type != parser.OneToOne {
		t.Errorf("Relationship[0].Type = %v, want OneToOne", s.Relationships[0].Type)
	}
	if s.Relationships[1].Type != parser.ManyToMany {
		t.Errorf("Relationship[1].Type = %v, want ManyToMany", s.Relationships[1].Type)
	}
	if s.Relationships[1].JunctionTable != "product_tags" {
		t.Errorf("Relationship[1].JunctionTable = %q, want %q", s.Relationships[1].JunctionTable, "product_tags")
	}
	if s.Relationships[2].Filter != "status = 'active'" {
		t.Errorf("Relationship[2].Filter = %q, want %q", s.Relationships[2].Filter, "status = 'active'")
	}
	if len(s.Relationships[2].Sort) != 1 || s.Relationships[2].Sort[0].Column != "created_at" {
		t.Errorf("Relationship[2].Sort = %+v, want [{Column:created_at Direction:desc}]", s.Relationships[2].Sort)
	}
}

func TestSchemaSortConsistentOrdering(t *testing.T) {
	makeSchema := func() parser.Schema {
		return parser.Schema{
			Tables: []parser.Table{
				{Name: "zebra", Schema: "public"},
				{Name: "alpha", Schema: "public"},
				{Name: "alpha", Schema: "analytics"},
				{Name: "middle", Schema: "public"},
			},
			Enums: []parser.Enum{
				{Name: "status", Schema: "public"},
				{Name: "color", Schema: "public"},
				{Name: "color", Schema: "analytics"},
			},
			CompositeTypes: []parser.CompositeType{
				{Name: "point", Schema: "public"},
				{Name: "address", Schema: "public"},
				{Name: "address", Schema: "geo"},
			},
			DomainTypes: []parser.DomainType{
				{Name: "zip_code", Schema: "public"},
				{Name: "email", Schema: "public"},
			},
			Views: []parser.View{
				{Name: "z_view", Schema: "public"},
				{Name: "a_view", Schema: "public"},
				{Name: "a_view", Schema: "reporting"},
			},
			Relationships: []parser.Relationship{
				{Name: "Tags"},
				{Name: "Company"},
				{Name: "Orders"},
			},
		}
	}

	s1 := makeSchema()
	s1.Sort()

	s2 := makeSchema()
	s2.Sort()

	if diff := cmp.Diff(s1, s2); diff != "" {
		t.Errorf("Sort() produced different results on identical input (-first +second):\n%s", diff)
	}

	// Verify tables sorted by schema then name.
	wantTableOrder := []struct{ schema, name string }{
		{"analytics", "alpha"},
		{"public", "alpha"},
		{"public", "middle"},
		{"public", "zebra"},
	}
	for i, want := range wantTableOrder {
		if s1.Tables[i].Schema != want.schema || s1.Tables[i].Name != want.name {
			t.Errorf("Tables[%d] = {%q, %q}, want {%q, %q}", i, s1.Tables[i].Schema, s1.Tables[i].Name, want.schema, want.name)
		}
	}

	// Verify enums sorted by schema then name.
	wantEnumOrder := []struct{ schema, name string }{
		{"analytics", "color"},
		{"public", "color"},
		{"public", "status"},
	}
	for i, want := range wantEnumOrder {
		if s1.Enums[i].Schema != want.schema || s1.Enums[i].Name != want.name {
			t.Errorf("Enums[%d] = {%q, %q}, want {%q, %q}", i, s1.Enums[i].Schema, s1.Enums[i].Name, want.schema, want.name)
		}
	}

	// Verify composite types sorted by schema then name.
	wantCompositeOrder := []struct{ schema, name string }{
		{"geo", "address"},
		{"public", "address"},
		{"public", "point"},
	}
	for i, want := range wantCompositeOrder {
		if s1.CompositeTypes[i].Schema != want.schema || s1.CompositeTypes[i].Name != want.name {
			t.Errorf("CompositeTypes[%d] = {%q, %q}, want {%q, %q}", i, s1.CompositeTypes[i].Schema, s1.CompositeTypes[i].Name, want.schema, want.name)
		}
	}

	// Verify domain types sorted.
	if s1.DomainTypes[0].Name != "email" || s1.DomainTypes[1].Name != "zip_code" {
		t.Errorf("DomainTypes order = [%q, %q], want [\"email\", \"zip_code\"]", s1.DomainTypes[0].Name, s1.DomainTypes[1].Name)
	}

	// Verify views sorted by schema then name.
	wantViewOrder := []struct{ schema, name string }{
		{"public", "a_view"},
		{"public", "z_view"},
		{"reporting", "a_view"},
	}
	for i, want := range wantViewOrder {
		if s1.Views[i].Schema != want.schema || s1.Views[i].Name != want.name {
			t.Errorf("Views[%d] = {%q, %q}, want {%q, %q}", i, s1.Views[i].Schema, s1.Views[i].Name, want.schema, want.name)
		}
	}

	// Verify relationships sorted by name.
	wantRelOrder := []string{"Company", "Orders", "Tags"}
	for i, want := range wantRelOrder {
		if s1.Relationships[i].Name != want {
			t.Errorf("Relationships[%d].Name = %q, want %q", i, s1.Relationships[i].Name, want)
		}
	}
}

func TestSchemaSortEmptySchema(t *testing.T) {
	s := parser.Schema{}
	s.Sort() // must not panic

	if len(s.Tables) != 0 {
		t.Errorf("len(Tables) = %d after Sort(), want 0", len(s.Tables))
	}
	if len(s.Enums) != 0 {
		t.Errorf("len(Enums) = %d after Sort(), want 0", len(s.Enums))
	}
	if len(s.CompositeTypes) != 0 {
		t.Errorf("len(CompositeTypes) = %d after Sort(), want 0", len(s.CompositeTypes))
	}
	if len(s.DomainTypes) != 0 {
		t.Errorf("len(DomainTypes) = %d after Sort(), want 0", len(s.DomainTypes))
	}
	if len(s.Views) != 0 {
		t.Errorf("len(Views) = %d after Sort(), want 0", len(s.Views))
	}
	if len(s.Relationships) != 0 {
		t.Errorf("len(Relationships) = %d after Sort(), want 0", len(s.Relationships))
	}
}

func TestSchemaSortColumnsPreserveInsertionOrder(t *testing.T) {
	s := parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint"},
					{Name: "zebra_col", Type: "text"},
					{Name: "alpha_col", Type: "text"},
				},
			},
		},
	}

	s.Sort()

	wantOrder := []string{"id", "zebra_col", "alpha_col"}
	for i, want := range wantOrder {
		if s.Tables[0].Columns[i].Name != want {
			t.Errorf("Columns[%d].Name = %q after Sort(), want %q (insertion order preserved)", i, s.Tables[0].Columns[i].Name, want)
		}
	}
}

func TestRelationshipTypeStrings(t *testing.T) {
	tests := []struct {
		rt   parser.RelationshipType
		want string
	}{
		{parser.OneToOne, "OneToOne"},
		{parser.OneToMany, "OneToMany"},
		{parser.ManyToMany, "ManyToMany"},
		{parser.RelationshipType(0), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.rt.String(); got != tt.want {
				t.Errorf("RelationshipType(%d).String() = %q, want %q", tt.rt, got, tt.want)
			}
		})
	}
}

func TestConstraintTypeStrings(t *testing.T) {
	tests := []struct {
		ct   parser.ConstraintType
		want string
	}{
		{parser.PrimaryKey, "PRIMARY KEY"},
		{parser.ForeignKey, "FOREIGN KEY"},
		{parser.Unique, "UNIQUE"},
		{parser.Check, "CHECK"},
		{parser.Index, "INDEX"},
		{parser.ConstraintType(0), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.ct.String(); got != tt.want {
				t.Errorf("ConstraintType(%d).String() = %q, want %q", tt.ct, got, tt.want)
			}
		})
	}
}

func TestCheckTableDropRefs(t *testing.T) {
	tables := []parser.Table{
		{
			Name:   "users",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "bigint"},
			},
		},
		{
			Name:   "orders",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "bigint"},
				{
					Name: "user_id",
					Type: "bigint",
					FKReference: &parser.FKReference{
						Table:  "users",
						Schema: "public",
						Column: "id",
					},
				},
			},
		},
		{
			Name:   "products",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "bigint"},
			},
		},
	}

	tests := []struct {
		name    string
		schema  string
		table   string
		wantErr bool
	}{
		{
			name:    "drop table with FK reference → error",
			schema:  "public",
			table:   "users",
			wantErr: true,
		},
		{
			name:    "drop table with no references → OK",
			schema:  "public",
			table:   "products",
			wantErr: false,
		},
		{
			name:    "drop nonexistent table → OK",
			schema:  "public",
			table:   "nonexistent",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parser.CheckTableDropRefs(tables, tt.schema, tt.table)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckTableDropRefs(%s.%s) error = %v, wantErr %v", tt.schema, tt.table, err, tt.wantErr)
			}
		})
	}
}

func TestCheckTableDropRefs_ConstraintLevel(t *testing.T) {
	tables := []parser.Table{
		{
			Name:   "categories",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "bigint"},
			},
		},
		{
			Name:   "products",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "bigint"},
				{Name: "category_id", Type: "bigint"},
			},
			Constraints: []parser.Constraint{
				{
					Name:             "fk_category",
					Type:             parser.ForeignKey,
					Columns:          []string{"category_id"},
					ReferenceTable:   "categories",
					ReferenceSchema:  "public",
					ReferenceColumns: []string{"id"},
				},
			},
		},
	}

	err := parser.CheckTableDropRefs(tables, "public", "categories")
	if err == nil {
		t.Fatal("CheckTableDropRefs() expected error for table-level FK constraint, got nil")
	}
}

func TestUnresolvedForeignKeys(t *testing.T) {
	docs := parser.Table{Schema: "public", Name: "documents"}
	tests := []struct {
		name   string
		tables []parser.Table
		want   []string
	}{
		{
			name: "every target parsed",
			tables: []parser.Table{docs, {
				Schema: "audit", Name: "labels",
				Columns: []parser.Column{{Name: "document_id", FKReference: &parser.FKReference{Schema: "public", Table: "documents", Column: "id"}}},
			}},
		},
		{
			name: "column FK and its constraint into a missing table report once",
			tables: []parser.Table{docs, {
				Schema: "audit", Name: "labels",
				Columns:     []parser.Column{{Name: "document_id", FKReference: &parser.FKReference{Schema: "audit", Table: "documents", Column: "id"}}},
				Constraints: []parser.Constraint{{Type: parser.ForeignKey, Columns: []string{"document_id"}, ReferenceSchema: "audit", ReferenceTable: "documents"}},
			}},
			want: []string{"foreign key audit.labels(document_id) references audit.documents, which no parsed table defines"},
		},
		{
			name: "composite constraint and a second table, sorted",
			tables: []parser.Table{
				{
					Schema: "public", Name: "z_refs",
					Columns: []parser.Column{{Name: "ghost_id", FKReference: &parser.FKReference{Schema: "public", Table: "ghosts", Column: "id"}}},
				},
				{
					Schema: "public", Name: "a_refs",
					Constraints: []parser.Constraint{{Type: parser.ForeignKey, Columns: []string{"a", "b"}, ReferenceSchema: "public", ReferenceTable: "pairs"}},
				},
			},
			want: []string{
				"foreign key public.a_refs(a, b) references public.pairs, which no parsed table defines",
				"foreign key public.z_refs(ghost_id) references public.ghosts, which no parsed table defines",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, err := range parser.UnresolvedForeignKeys(tt.tables) {
				got = append(got, err.Error())
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("UnresolvedForeignKeys() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCheckTypeDropRefs(t *testing.T) {
	tables := []parser.Table{
		{
			Name:   "users",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "bigint"},
				{Name: "status", Type: "status"},
				{Name: "role", Type: "public.role"},
			},
		},
	}

	tests := []struct {
		name     string
		schema   string
		typeName string
		wantErr  bool
	}{
		{
			name:     "drop type used by column (bare name match) → error",
			schema:   "public",
			typeName: "status",
			wantErr:  true,
		},
		{
			name:     "drop type used by column (qualified name match) → error",
			schema:   "public",
			typeName: "role",
			wantErr:  true,
		},
		{
			name:     "drop type not used by any column → OK",
			schema:   "public",
			typeName: "unused_type",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parser.CheckTypeDropRefs(tables, tt.schema, tt.typeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckTypeDropRefs(%s.%s) error = %v, wantErr %v", tt.schema, tt.typeName, err, tt.wantErr)
			}
		})
	}
}
