package gen_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// accessAPISchema builds the §32 mixed-role fixture from the phase-22
// acceptance criteria: password_hash internal, created_at read_only,
// new_password write_only, internal_score hidden, plus public columns.
func accessAPISchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "email", Type: "text"},
					{Name: "password_hash", Type: "text"},
					{Name: "created_at", Type: "timestamp", Default: "now()"},
					{Name: "new_password", Type: "text", Nullable: true},
					{Name: "internal_score", Type: "bigint", Nullable: true},
				},
			},
		},
	}
}

func accessAPIContext(t *testing.T) gen.APITableContext {
	t.Helper()
	in := apiTestInput(t, accessAPISchema())
	in.Config.Tables["users"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"password_hash":  {Access: "internal"},
			"created_at":     {Access: "read_only"},
			"new_password":   {Access: "write_only"},
			"internal_score": {Access: "hidden"},
		},
	}
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if apiCtx == nil || len(apiCtx.Tables) != 1 {
		t.Fatalf("BuildAPIContext tables = %v, want exactly 1", apiCtx)
	}
	return apiCtx.Tables[0]
}

// block extracts one brace-delimited schema block (e.g. `type User {...}`)
// so per-surface assertions don't false-positive on other blocks.
func block(t *testing.T, schema, header string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(header) + `\s*\{[^}]*\}`)
	m := re.FindString(schema)
	if m == "" {
		t.Fatalf("schema block %q not found in:\n%s", header, schema)
	}
	return m
}

// TestGraphQLSchema_AccessProjection pins the §32.3 GraphQL surface matrix
// for a mixed-role table: each surface exposes exactly the columns its
// capability allows, and restricted columns are absent everywhere else.
func TestGraphQLSchema_AccessProjection(t *testing.T) {
	at := accessAPIContext(t)
	schema := renderAPITableSchema(t, at)

	tests := []struct {
		surface string
		header  string
		want    []string
		absent  []string
	}{
		{
			surface: "object type (API out)",
			header:  "type User",
			want:    []string{"id:", "email:", "createdAt:"},
			absent:  []string{"passwordHash", "newPassword", "internalScore"},
		},
		{
			surface: "create input (API in)",
			header:  "input CreateUserInput",
			want:    []string{"email:", "newPassword:"},
			absent:  []string{"passwordHash", "createdAt", "internalScore"},
		},
		{
			surface: "update input (API in)",
			header:  "input UpdateUserInput",
			want:    []string{"email:", "newPassword:"},
			absent:  []string{"passwordHash", "createdAt", "internalScore"},
		},
		{
			surface: "filter input",
			header:  "input UserFilter",
			want:    []string{"email:", "createdAt:"},
			absent:  []string{"passwordHash", "newPassword", "internalScore"},
		},
		{
			surface: "sort enum",
			header:  "enum UserSortField",
			want:    []string{"ID", "EMAIL", "CREATED_AT"},
			absent:  []string{"PASSWORD_HASH", "NEW_PASSWORD", "INTERNAL_SCORE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.surface, func(t *testing.T) {
			b := block(t, schema, tt.header)
			for _, w := range tt.want {
				if !strings.Contains(b, w) {
					t.Errorf("%s missing %q:\n%s", tt.surface, w, b)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(b, a) {
					t.Errorf("%s exposes access-restricted %q:\n%s", tt.surface, a, b)
				}
			}
		})
	}

	// The _inc/_dec operators exist only for writable numerics: the hidden
	// bigint internal_score must not surface increment operators.
	if strings.Contains(schema, "internalScore_inc") || strings.Contains(schema, "internalScore_dec") {
		t.Errorf("update input emits _inc/_dec for non-writable internal_score:\n%s", schema)
	}
	for _, op := range at.UpdateOps {
		if op.SQLName == "internal_score" {
			t.Errorf("UpdateOps includes non-writable internal_score — seeds would dispatch a dead increment op")
		}
	}
}

// TestGraphQLSchema_AccessProjection_TranslatorsMatchSchema pins the
// context-side lists the filter/sort/input translators iterate — they must
// mirror the schema-side gates exactly (a schema field without a translator
// panics at runtime; a translator without a schema field references a
// non-existent gqlgen input field and fails to compile).
func TestGraphQLSchema_AccessProjection_TranslatorsMatchSchema(t *testing.T) {
	at := accessAPIContext(t)

	filterCols := make([]string, 0, len(at.FilterFields))
	for _, f := range at.FilterFields {
		filterCols = append(filterCols, f.ModelFieldName)
	}
	for _, banned := range []string{"PasswordHash", "NewPassword", "InternalScore"} {
		for _, got := range filterCols {
			if got == banned {
				t.Errorf("FilterFields includes restricted column %s", banned)
			}
		}
	}
	found := false
	for _, got := range filterCols {
		if got == "CreatedAt" {
			found = true
		}
	}
	if !found {
		t.Errorf("FilterFields = %v, want read_only CreatedAt present", filterCols)
	}

	sortCols := make(map[string]bool, len(at.SortFields))
	for _, f := range at.SortFields {
		sortCols[f.SQLColumn] = true
	}
	for _, banned := range []string{"password_hash", "new_password", "internal_score"} {
		if sortCols[banned] {
			t.Errorf("SortFields includes restricted column %s", banned)
		}
	}
	if !sortCols["created_at"] || !sortCols["id"] {
		t.Errorf("SortFields = %v, want id + created_at present", sortCols)
	}

	for _, list := range []struct {
		name   string
		fields []gen.APIInputField
	}{{"CreateInputFields", at.CreateInputFields}, {"UpdateInputFields", at.UpdateInputFields}} {
		names := make(map[string]bool, len(list.fields))
		for _, f := range list.fields {
			names[f.ModelFieldName] = true
		}
		if !names["Email"] || !names["NewPassword"] {
			t.Errorf("%s = %v, want Email + NewPassword present", list.name, names)
		}
		for _, banned := range []string{"PasswordHash", "CreatedAt", "InternalScore"} {
			if names[banned] {
				t.Errorf("%s includes non-writable column %s", list.name, banned)
			}
		}
	}
}

// TestValidateAPIWalkerCompleteness_Access pins the §26.5.2 lint amendment:
// restricted columns intentionally absent pass; a readable column without a
// case still fails (the original guard); a restricted column with a live
// case fails (dead code masking the projection).
//
// Each case carries the row identity production always resolves so
// the third direction of the lint is satisfied by the fixture rather than
// tripped by it — the §32.2 half of the walker is what these cases are about.
func TestValidateAPIWalkerCompleteness_Access(t *testing.T) {
	src := gen.TableContext{
		TableName: "users",
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", Access: "public", APIReadable: true},
			{Name: "email", FieldName: "Email", Access: "public", APIReadable: true},
			{Name: "password_hash", FieldName: "PasswordHash", Access: "internal"},
		},
	}

	tests := []struct {
		name    string
		fields  []gen.APIFieldContext
		wantErr string
	}{
		{
			name: "restricted column absent passes",
			fields: []gen.APIFieldContext{
				{SQLName: "id", GoFieldName: "ID", Readable: true},
				{SQLName: "email", GoFieldName: "Email", Readable: true},
				{SQLName: "password_hash", GoFieldName: "PasswordHash", Readable: false}, // present but case-gated off
			},
		},
		{
			name: "restricted column fully omitted also passes",
			fields: []gen.APIFieldContext{
				{SQLName: "id", GoFieldName: "ID", Readable: true},
				{SQLName: "email", GoFieldName: "Email", Readable: true},
			},
		},
		{
			name: "readable column missing fails",
			fields: []gen.APIFieldContext{
				{SQLName: "id", GoFieldName: "ID", Readable: true},
				{SQLName: "password_hash", GoFieldName: "PasswordHash", Readable: false},
			},
			wantErr: `no case for column "email"`,
		},
		{
			name: "restricted column with live case fails",
			fields: []gen.APIFieldContext{
				{SQLName: "id", GoFieldName: "ID", Readable: true},
				{SQLName: "email", GoFieldName: "Email", Readable: true},
				{SQLName: "password_hash", GoFieldName: "PasswordHash", Readable: true},
			},
			wantErr: `case for access-restricted column "password_hash"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := gen.APITableContext{StructName: "User", Fields: tt.fields, RowIdentityFields: []string{"ID"}}
			err := gen.ValidateAPIWalkerCompleteness(gen.APIEntityFromTable(src), at, nil)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateAPIWalkerCompleteness() unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateAPIWalkerCompleteness() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestGraphQLWalker_AccessProjection renders the field-options walker for the
// mixed-role table and asserts restricted columns have no case while exposed
// columns and relationships keep theirs.
func TestGraphQLWalker_AccessProjection(t *testing.T) {
	at := accessAPIContext(t)

	tmpl := loadAPISchemaTemplate(t)
	var buf strings.Builder
	ctx := struct {
		ModelsPackage string
		Tables        []gen.APITableContext
	}{ModelsPackage: "models", Tables: []gen.APITableContext{at}}
	if err := tmpl.ExecuteTemplate(&buf, "api/field-options", ctx); err != nil {
		t.Fatalf("rendering api/field-options: %v", err)
	}
	out := buf.String()

	for _, want := range []string{`case "id":`, `case "email":`, `case "createdAt":`} {
		if !strings.Contains(out, want) {
			t.Errorf("walker missing %s\n%s", want, out)
		}
	}
	for _, banned := range []string{`case "passwordHash":`, `case "newPassword":`, `case "internalScore":`} {
		if strings.Contains(out, banned) {
			t.Errorf("walker has dead case %s for access-restricted column\n%s", banned, out)
		}
	}
}
