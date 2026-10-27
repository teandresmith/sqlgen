package manifest_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgofrs"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgoogle"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// TestBuild_EntityExamplesFollowIncludeExamples pins PRD §30.2 / §30.4.2:
// include_examples defaults to true and then carries a "canonical example per
// entity" as examples.read / examples.write, each an array of Go code lines;
// include_examples: false strips the per-entity examples.
func TestBuild_EntityExamplesFollowIncludeExamples(t *testing.T) {
	tests := []struct {
		name            string
		includeExamples *bool
		wantExamples    bool
	}{
		{name: "default includes examples", includeExamples: nil, wantExamples: true},
		{name: "explicit true includes examples", includeExamples: new(true), wantExamples: true},
		{name: "explicit false strips examples", includeExamples: new(false), wantExamples: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Generation.Manifest.IncludeExamples = tt.includeExamples
			schema := &parser.Schema{
				Tables: []parser.Table{
					{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "email", Type: "text"}}},
				},
			}
			doc, err := manifest.Build(testBuildInput(t, schema, cfg))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			e := findEntity(t, doc, "User")

			if !tt.wantExamples {
				if e.Examples != nil {
					t.Errorf("include_examples=false: examples = %+v, want absent (nil)", *e.Examples)
				}
				return
			}
			if e.Examples == nil {
				t.Fatalf("include_examples default/true: examples = nil, want read and write arrays")
			}
			if len(e.Examples.Read) == 0 {
				t.Errorf("examples.read is empty, want at least one Go line (canonical read example)")
			}
			if len(e.Examples.Write) == 0 {
				t.Errorf("examples.write is empty, want at least one Go line (canonical write example)")
			}
		})
	}
}

// TestBuild_EntityExampleLines pins the example lines themselves (PRD
// §30.4.2): each is a line of Go built from the entity's real method set,
// input fields and filter comparators, qualified with output.package and
// reached through conventions.client_entry_points. A table's read is a Get by
// primary key (the <Entity>PK struct for a composite key) and a filtered
// GetMany; its write is a Create with the required create-input fields and an
// omittable Update. A view has read lines only — Get only under @pk — and an
// empty write.
func TestBuild_EntityExampleLines(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "email", Type: "text"},
				{Name: "login_count", Type: "bigint"},
				{Name: "nickname", Type: "text", Nullable: true},
			}},
			{Name: "order_items", Columns: []parser.Column{
				{Name: "order_id", Type: "uuid", PrimaryKey: true},
				{Name: "line", Type: "integer", PrimaryKey: true},
				{Name: "quantity", Type: "integer"},
			}},
		},
		Views: []parser.View{{
			Name:    "active_users",
			SQL:     "SELECT id, email FROM users",
			Columns: []parser.Column{{Name: "id", Type: "uuid"}, {Name: "email", Type: "text"}},
		}},
	}
	doc, err := manifest.Build(testBuildInput(t, schema, baseConfig()))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	tests := []struct {
		entity string
		want   manifest.Examples
	}{
		{
			entity: "User",
			want: manifest.Examples{
				Read: []string{
					`row, err := client.Users().Get(ctx, uuid.NewV4())`,
					`rows, err := client.Users().GetMany(ctx, &models.GetUsersInput{Filter: &models.UserFilter{Email: &comparator.String{Eq: new("example")}}})`,
				},
				Write: []string{
					`created, err := client.Users().Create(ctx, &models.CreateUserInput{Email: "example", LoginCount: 1})`,
					`updated, err := client.Users().Update(ctx, uuid.NewV4(), &models.UpdateUserInput{Email: omittable.Set("example")})`,
				},
			},
		},
		{
			entity: "OrderItem",
			want: manifest.Examples{
				Read: []string{
					`row, err := client.OrderItems().Get(ctx, models.OrderItemPK{OrderID: uuid.NewV4(), Line: 1})`,
					`rows, err := client.OrderItems().GetMany(ctx, &models.GetOrderItemsInput{Filter: &models.OrderItemFilter{Quantity: &comparator.Number[int32]{Eq: new(int32(1))}}})`,
				},
				Write: []string{
					`created, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{OrderID: uuid.NewV4(), Line: 1, Quantity: 1})`,
					`updated, err := client.OrderItems().Update(ctx, models.OrderItemPK{OrderID: uuid.NewV4(), Line: 1}, &models.UpdateOrderItemInput{Quantity: omittable.Set(int32(1))})`,
				},
			},
		},
		{
			entity: "ActiveUser",
			want: manifest.Examples{
				Read: []string{
					`rows, err := client.ActiveUser().GetMany(ctx, &models.GetActiveUsersInput{Filter: &models.ActiveUserFilter{Email: &comparator.String{Eq: new("example")}}})`,
				},
				Write: []string{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.entity, func(t *testing.T) {
			e := findEntity(t, doc, tt.entity)
			if e.Examples == nil {
				t.Fatalf("examples = nil, want read and write lines")
			}
			if diff := cmp.Diff(tt.want, *e.Examples); diff != "" {
				t.Errorf("%s examples mismatch (-want +got):\n%s", tt.entity, diff)
			}
		})
	}
}

// TestBuild_EntityExampleUUIDConstructor pins that a uuid.UUID value in an
// example line is spelled with the constructor of the UUID library the package
// resolved (PRD §7.4 "Generating UUID values"): the three libraries disagree —
// only google exports New, gofrs returns (UUID, error) and has no Must-free
// form — so one fixed spelling compiles against one library only. The zero
// integration stands for the standard-library default.
func TestBuild_EntityExampleUUIDConstructor(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]config.TypeOverride
		integ    gotype.UUIDIntegration
		wantGet  string
	}{
		{
			name:    "standard library default",
			wantGet: `row, err := client.Users().Get(ctx, uuid.NewV4())`,
		},
		{
			name:     "google",
			override: map[string]config.TypeOverride{"uuid": uuidgoogle.Override()},
			integ:    gotype.UUIDIntegrationFor(uuidgoogle.ImportPath),
			wantGet:  `row, err := client.Users().Get(ctx, uuid.New())`,
		},
		{
			name:     "gofrs",
			override: map[string]config.TypeOverride{"uuid": uuidgofrs.Override()},
			integ:    gotype.UUIDIntegrationFor(uuidgofrs.ImportPath),
			wantGet:  `row, err := client.Users().Get(ctx, uuid.Must(uuid.NewV4()))`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Overrides.Types = tt.override
			schema := &parser.Schema{Tables: []parser.Table{{Name: "users", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "email", Type: "text"},
			}}}}
			in := testBuildInput(t, schema, cfg)
			in.UUID = tt.integ
			doc, err := manifest.Build(in)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			e := findEntity(t, doc, "User")
			if e.Examples == nil || len(e.Examples.Read) == 0 {
				t.Fatalf("examples.read is empty, want a Get line")
			}
			if got := e.Examples.Read[0]; got != tt.wantGet {
				t.Errorf("Get line = %q, want %q", got, tt.wantGet)
			}
		})
	}
}

// TestBuild_EntityExampleIDFilterLiteral pins the comparator.ID filter value:
// the family compares through a string the database parses as the column's
// own type, so the example is a string that column accepts — a well-formed
// UUID for a uuid column, plain text for a text key.
func TestBuild_EntityExampleIDFilterLiteral(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{
		{Name: "accounts", Columns: []parser.Column{
			{Name: "code", Type: "text", PrimaryKey: true},
		}},
		{Name: "events", Columns: []parser.Column{
			{Name: "id", Type: "bigint", PrimaryKey: true},
			{Name: "entity_id", Type: "uuid"},
		}},
	}}
	doc, err := manifest.Build(testBuildInput(t, schema, baseConfig()))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	tests := []struct {
		entity string
		want   string
	}{
		{entity: "Event", want: `rows, err := client.Events().GetMany(ctx, &models.GetEventsInput{Filter: &models.EventFilter{EntityID: &comparator.ID{Eq: new("123e4567-e89b-12d3-a456-426614174000")}}})`},
		{entity: "Account", want: `rows, err := client.Accounts().GetMany(ctx, &models.GetAccountsInput{Filter: &models.AccountFilter{Code: &comparator.ID{Eq: new("example")}}})`},
	}
	for _, tt := range tests {
		t.Run(tt.entity, func(t *testing.T) {
			e := findEntity(t, doc, tt.entity)
			if e.Examples == nil {
				t.Fatalf("examples = nil")
			}
			found := false
			for _, line := range e.Examples.Read {
				found = found || line == tt.want
			}
			if !found {
				t.Errorf("examples.read = %q, want a line %q", e.Examples.Read, tt.want)
			}
		})
	}
}
