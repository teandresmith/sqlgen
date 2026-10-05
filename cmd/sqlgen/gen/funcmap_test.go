package gen_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/sql"
)

func TestFuncMap_returnsPopulatedMap(t *testing.T) {
	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	if fm == nil {
		t.Fatal("FuncMap() returned nil")
	}

	// Verify all expected function categories are registered.
	expected := []string{
		// Naming — toSnakeCase is deliberately absent: it reads a snake_case
		// name back out of a Go identifier, which is a guess, and no template
		// needs it (PRD §8.5). Contexts carry SnakeName pre-computed.
		"toPascalCase", "toCamelCase", "structNamePlural", "toSingular", "safeGoIdent",
		// Types
		"goType", "isNullableType", "comparatorType", "zeroValue",
		// Soft Delete
		"softDeleteSwitch", "softDeleteValue", "softDeleteCondition",
		"softDeleteRestoreCondition", "softDeleteTypeConst", "softDeleteRestoreGoValue",
		"softDeleteFilterOverride",
		"softDeleteSubqueryPredicate", "softDeleteSubqueryArg",
		// PK Filtering
		"pkFilterExpr", "pkFilterInExpr", "pkFilterInExprStr", "pkIsStringType",
		// Relationships
		"fkStringExpr", "fkToString", "fkToStringByGoType", "fkNullGuard", "fkLoadGuard", "fkLoadKey", "isO2O", "isM2M", "reverseO2OTargets",
		// Inputs
		"createInputFieldType", "updateInputFieldType",
		// Tags
		"formatTagPairs",
		// Imports
		"uniqueImports",
		// Dialect
		"placeholder", "quoteIdentifier", "supportsReturning",
		// Cache / tenancy predicates
		"anyTenanted",
		// LockMode guard (PRD §9.6a)
		"lockGuardCtx",
		// API / GraphQL helpers
		// `comparatorFor` was deleted — a column's comparator input
		// is resolved once into APIFieldContext.Filter (PRD §26.5.3) and
		// the schema template reads it there.
		"screamingSnakeCase", "gqlEnumIdent", "hasAnyRead", "hasAnyMutation", "pkConvert",
		"pkSchemaArgs", "pkGoArgs", "pkPassArgs",
		"inputCoerceBare", "inputCoerceDeref",
	}

	for _, name := range expected {
		if fm[name] == nil {
			t.Errorf("FuncMap() missing function %q", name)
		}
	}

	if len(fm) != len(expected) {
		t.Errorf("FuncMap() has %d entries, want %d", len(fm), len(expected))
	}
}

func TestFuncIsNullableType(t *testing.T) {
	tests := []struct {
		name   string
		goType string
		want   bool
	}{
		{name: "pointer type", goType: "*int32", want: true},
		{name: "slice type", goType: "[]string", want: true},
		{name: "map type", goType: "map[string]any", want: true},
		{name: "sql.NullString", goType: "sql.NullString", want: true},
		{name: "sql.NullInt64", goType: "sql.NullInt64", want: true},
		{name: "bare string", goType: "string", want: false},
		{name: "bare int", goType: "int64", want: false},
		{name: "bare bool", goType: "bool", want: false},
		{name: "struct type", goType: "uuid.UUID", want: false},
	}

	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	isNullable := fm["isNullableType"].(func(string) bool)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNullable(tt.goType)
			if got != tt.want {
				t.Errorf("isNullableType(%q) = %v, want %v", tt.goType, got, tt.want)
			}
		})
	}
}

func TestFuncSoftDeleteSwitch(t *testing.T) {
	tests := []struct {
		strategy config.SoftDeleteType
		want     string
	}{
		{config.SoftDeleteTimestamp, "CURRENT_TIMESTAMP"},
		{config.SoftDeleteBool, "TRUE"},
		{config.SoftDeleteInteger, "1"},
	}

	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	softDeleteSwitch := fm["softDeleteSwitch"].(func(config.SoftDeleteType) string)

	for _, tt := range tests {
		t.Run(string(tt.strategy), func(t *testing.T) {
			got := softDeleteSwitch(tt.strategy)
			if got != tt.want {
				t.Errorf("softDeleteSwitch(%q) = %q, want %q", tt.strategy, got, tt.want)
			}
		})
	}
}

func TestFuncSoftDeleteValue(t *testing.T) {
	tests := []struct {
		strategy config.SoftDeleteType
		want     string
	}{
		{config.SoftDeleteTimestamp, "NULL"},
		{config.SoftDeleteBool, "FALSE"},
		{config.SoftDeleteInteger, "0"},
	}

	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	softDeleteValue := fm["softDeleteValue"].(func(config.SoftDeleteType) string)

	for _, tt := range tests {
		t.Run(string(tt.strategy), func(t *testing.T) {
			got := softDeleteValue(tt.strategy)
			if got != tt.want {
				t.Errorf("softDeleteValue(%q) = %q, want %q", tt.strategy, got, tt.want)
			}
		})
	}
}

func TestFuncPlaceholder(t *testing.T) {
	tests := []struct {
		name    string
		dialect sql.Dialect
		pos     int
		want    string
	}{
		{"postgres pos 1", sql.NewPostgresDialect(), 1, "$1"},
		{"postgres pos 3", sql.NewPostgresDialect(), 3, "$3"},
		{"mysql pos 1", sql.NewMySQLDialect(), 1, "?"},
		{"sqlite pos 1", sql.NewSQLiteDialect(), 1, "?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := gen.FuncMap(tt.dialect)
			placeholder := fm["placeholder"].(func(int) string)
			got := placeholder(tt.pos)
			if got != tt.want {
				t.Errorf("placeholder(%d) = %q, want %q", tt.pos, got, tt.want)
			}
		})
	}
}

func TestFuncSupportsReturning(t *testing.T) {
	tests := []struct {
		name    string
		dialect sql.Dialect
		want    bool
	}{
		{"postgres", sql.NewPostgresDialect(), true},
		{"mysql", sql.NewMySQLDialect(), false},
		{"sqlite", sql.NewSQLiteDialect(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := gen.FuncMap(tt.dialect)
			supportsReturning := fm["supportsReturning"].(func() bool)
			got := supportsReturning()
			if got != tt.want {
				t.Errorf("supportsReturning() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFuncIsM2M(t *testing.T) {
	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	isM2M := fm["isM2M"].(func(gen.RelationshipContext) bool)

	tests := []struct {
		name string
		ctx  gen.RelationshipContext
		want bool
	}{
		{
			name: "many to many",
			ctx:  gen.RelationshipContext{Type: 3}, // parser.ManyToMany = 3
			want: true,
		},
		{
			name: "one to one",
			ctx:  gen.RelationshipContext{Type: 1}, // parser.OneToOne = 1
			want: false,
		},
		{
			name: "one to many",
			ctx:  gen.RelationshipContext{Type: 2}, // parser.OneToMany = 2
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isM2M(tt.ctx)
			if got != tt.want {
				t.Errorf("isM2M(%v) = %v, want %v", tt.ctx.Type, got, tt.want)
			}
		})
	}
}

func TestFuncSoftDeleteRestoreCondition(t *testing.T) {
	tests := []struct {
		name string
		sd   gen.SoftDeleteContext
		want string
	}{
		{
			name: "timestamp",
			sd:   gen.SoftDeleteContext{Column: "deleted_at", FieldName: "DeletedAt", Strategy: "timestamp"},
			want: `sql.Where(c.dialect.QuoteIdentifier("deleted_at")).IsNotNull()`,
		},
		{
			name: "bool",
			sd:   gen.SoftDeleteContext{Column: "is_deleted", FieldName: "IsDeleted", Strategy: "bool"},
			want: `sql.Where(c.dialect.QuoteIdentifier("is_deleted")).Eq(true)`,
		},
		{
			name: "integer",
			sd:   gen.SoftDeleteContext{Column: "deleted", FieldName: "Deleted", Strategy: "integer"},
			want: `sql.Where(c.dialect.QuoteIdentifier("deleted")).Eq(1)`,
		},
	}

	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	restoreCond := fm["softDeleteRestoreCondition"].(func(gen.SoftDeleteContext) string)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := restoreCond(tt.sd)
			if got != tt.want {
				t.Errorf("softDeleteRestoreCondition(%+v) = %q, want %q", tt.sd, got, tt.want)
			}
		})
	}
}

func TestFuncSoftDeleteRestoreGoValue(t *testing.T) {
	tests := []struct {
		strategy config.SoftDeleteType
		want     string
	}{
		{config.SoftDeleteTimestamp, "nil"},
		{config.SoftDeleteBool, "false"},
		{config.SoftDeleteInteger, "0"},
	}

	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	restoreGoValue := fm["softDeleteRestoreGoValue"].(func(config.SoftDeleteType) string)

	for _, tt := range tests {
		t.Run(string(tt.strategy), func(t *testing.T) {
			got := restoreGoValue(tt.strategy)
			if got != tt.want {
				t.Errorf("softDeleteRestoreGoValue(%q) = %q, want %q", tt.strategy, got, tt.want)
			}
		})
	}
}

// TestFuncFKToStringByGoType pins the FK conversion table — picking the
// FK→string expression from the target's Go type string instead of the
// parent's PK column. Required by the M2M relationship loader (relationship
// loaders for cross-PK-type M2M, e.g. users(uuid.UUID) ↔ categories(int64),
// would otherwise emit `t.ID.String()` on int64).
func TestFuncFKToStringByGoType(t *testing.T) {
	tests := []struct {
		name    string
		goType  string
		varExpr string
		want    string
	}{
		{name: "plain string PK", goType: "string", varExpr: "t.ID", want: "t.ID"},
		{name: "uuid.UUID via Stringer", goType: "uuid.UUID", varExpr: "t.ID", want: "t.ID.String()"},
		{name: "uuid.UUID gofrs via Stringer", goType: "github.com/gofrs/uuid/v5.UUID", varExpr: "t.ID", want: "t.ID.String()"},
		{name: "int64 via fmt.Sprint", goType: "int64", varExpr: "t.ID", want: "fmt.Sprint(t.ID)"},
		{name: "int32 via fmt.Sprint", goType: "int32", varExpr: "c.ID", want: "fmt.Sprint(c.ID)"},
		{name: "time.Time via Stringer", goType: "time.Time", varExpr: "t.CreatedAt", want: "t.CreatedAt.String()"},
		{name: "ksuid.KSUID via fmt.Sprint", goType: "ksuid.KSUID", varExpr: "t.ID", want: "fmt.Sprint(t.ID)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.FKToStringByGoTypeForTest(tt.goType, tt.varExpr)
			if got != tt.want {
				t.Errorf("FKToStringByGoTypeForTest(%q, %q) = %q, want %q", tt.goType, tt.varExpr, got, tt.want)
			}
		})
	}
}

// TestFuncFKToStringByGoType_M2MCrossPKType pins the specific generated
// shape this guards: the M2M target lookup must convert the target's PK
// using the target's own Go type, not the parent's. Regressing this would
// emit `t.ID.String()` on int64 (compile error) or `fmt.Sprint(t.ID)` on
// uuid.UUID (works at runtime via Stringer interface but uses the wrong
// helper — an inconsistency the test forbids by anchoring both sides).
func TestFuncFKToStringByGoType_M2MCrossPKType(t *testing.T) {
	// users → categories M2M (parent uuid.UUID, target int64): must NOT use
	// .String() on int64.
	got := gen.FKToStringByGoTypeForTest("int64", "t.ID")
	if got == "t.ID.String()" {
		t.Errorf("M2M target (int64) emitted .String() — would not compile")
	}
	if got != "fmt.Sprint(t.ID)" {
		t.Errorf("M2M target (int64): got %q, want %q", got, "fmt.Sprint(t.ID)")
	}

	// categories → users M2M (parent int64, target uuid.UUID): must use
	// .String() on uuid.UUID, not fmt.Sprint (consistent helper choice
	// across both directions).
	got = gen.FKToStringByGoTypeForTest("uuid.UUID", "t.ID")
	if got != "t.ID.String()" {
		t.Errorf("M2M target (uuid.UUID): got %q, want %q", got, "t.ID.String()")
	}
}

// TestFuncFKLoadGuard pins the guard-expression table — the O2M
// loader emits `if !(<guard>) { continue }` to skip null FK rows before
// stringifying. Resolver is nil; only static known wrappers + pointer/bare
// fallthroughs apply.
func TestFuncFKLoadGuard(t *testing.T) {
	tests := []struct {
		name           string
		fkColumnGoType string
		varExpr        string
		want           string
	}{
		{name: "bare uuid.UUID — no guard", fkColumnGoType: "uuid.UUID", varExpr: "r.UserID", want: ""},
		{name: "bare int64 — no guard", fkColumnGoType: "int64", varExpr: "r.ID", want: ""},
		{name: "bare string — no guard", fkColumnGoType: "string", varExpr: "r.Code", want: ""},
		{name: "*uuid.UUID — nil guard", fkColumnGoType: "*uuid.UUID", varExpr: "r.UserID", want: "r.UserID != nil"},
		{name: "*int64 — nil guard", fkColumnGoType: "*int64", varExpr: "r.ParentID", want: "r.ParentID != nil"},
		{name: "uuid.NullUUID — Valid guard", fkColumnGoType: "uuid.NullUUID", varExpr: "r.UserID", want: "r.UserID.Valid"},
		{name: "decimal.NullDecimal — Valid guard", fkColumnGoType: "decimal.NullDecimal", varExpr: "r.Amount", want: "r.Amount.Valid"},
		{name: "sql.NullString — Valid guard", fkColumnGoType: "sql.NullString", varExpr: "r.Slug", want: "r.Slug.Valid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.FKLoadGuardForTest(nil, tt.fkColumnGoType, tt.varExpr)
			if got != tt.want {
				t.Errorf("FKLoadGuardForTest(nil, %q, %q) = %q, want %q", tt.fkColumnGoType, tt.varExpr, got, tt.want)
			}
		})
	}
}

// TestFuncFKLoadKey pins the bucket-key expression table — the O2M
// loader unwraps a possibly-null FK before stringifying for parent/child
// bucket lookup. Resolver is nil; only static known wrappers + pointer/bare
// fallthroughs apply.
func TestFuncFKLoadKey(t *testing.T) {
	tests := []struct {
		name           string
		fkColumnGoType string
		varExpr        string
		want           string
	}{
		{name: "bare string — pass-through", fkColumnGoType: "string", varExpr: "r.Slug", want: "r.Slug"},
		{name: "bare int64 — Sprint", fkColumnGoType: "int64", varExpr: "r.ID", want: "fmt.Sprint(r.ID)"},
		{name: "bare uuid.UUID — Stringer", fkColumnGoType: "uuid.UUID", varExpr: "r.UserID", want: "r.UserID.String()"},
		{name: "*string — deref pass-through", fkColumnGoType: "*string", varExpr: "r.Slug", want: "(*r.Slug)"},
		{name: "*int64 — deref Sprint", fkColumnGoType: "*int64", varExpr: "r.ParentID", want: "fmt.Sprint((*r.ParentID))"},
		{name: "*uuid.UUID — deref Stringer", fkColumnGoType: "*uuid.UUID", varExpr: "r.UserID", want: "(*r.UserID).String()"},
		{name: "uuid.NullUUID — unwrap + Stringer", fkColumnGoType: "uuid.NullUUID", varExpr: "r.UserID", want: "r.UserID.UUID.String()"},
		{name: "decimal.NullDecimal — unwrap + Stringer", fkColumnGoType: "decimal.NullDecimal", varExpr: "r.Amount", want: "r.Amount.Decimal.String()"},
		{name: "types.NullDateTime — unwrap + Stringer", fkColumnGoType: "types.NullDateTime", varExpr: "r.At", want: "r.At.Time.String()"},
		{name: "sql.NullString — unwrap pass-through", fkColumnGoType: "sql.NullString", varExpr: "r.Slug", want: "r.Slug.String"},
		{name: "sql.NullInt64 — unwrap + Sprint", fkColumnGoType: "sql.NullInt64", varExpr: "r.Count", want: "fmt.Sprint(r.Count.Int64)"},
		{name: "sql.NullTime — unwrap + Stringer", fkColumnGoType: "sql.NullTime", varExpr: "r.At", want: "r.At.Time.String()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.FKLoadKeyForTest(nil, tt.fkColumnGoType, tt.varExpr)
			if got != tt.want {
				t.Errorf("FKLoadKeyForTest(nil, %q, %q) = %q, want %q", tt.fkColumnGoType, tt.varExpr, got, tt.want)
			}
		})
	}
}

// TestFuncFKLoadKey_UserDeclaredWrapper exercises the resolver-aware path
// (the user-facing surface): a user-declared wrapper with a
// custom underlying field name and valid_field name.
func TestFuncFKLoadKey_UserDeclaredWrapper(t *testing.T) {
	r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
		"text": {
			Type:   "myapp.OptionalString",
			Import: "myapp/pkg/optional",
			Nullable: config.NullableVariant{
				Type:            "myapp.NullableString",
				Import:          "myapp/pkg/optional",
				UnderlyingField: "Value",
				ValidField:      "Set",
			},
		},
	})

	gotGuard := gen.FKLoadGuardForTest(r, "myapp.NullableString", "r.Slug")
	if gotGuard != "r.Slug.Set" {
		t.Errorf("guard = %q, want %q", gotGuard, "r.Slug.Set")
	}

	gotKey := gen.FKLoadKeyForTest(r, "myapp.NullableString", "r.Slug")
	if gotKey != "fmt.Sprint(r.Slug.Value)" {
		t.Errorf("key = %q, want %q", gotKey, "fmt.Sprint(r.Slug.Value)")
	}
}
