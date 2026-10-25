package gen_test

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestFuncMap_returnsPopulatedMap(t *testing.T) {
	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	if fm == nil {
		t.Fatal("FuncMap() returned nil")
	}

	// The functions the template engine's function table lists. Templates
	// may use further helpers; those are internals, not part of the table.
	expected := []string{
		"toPascalCase", "toCamelCase", "structNamePlural", "toSingular", "safeGoIdent",
		"goType", "isNullableType", "comparatorType", "zeroValue",
		"softDeleteSwitch", "softDeleteValue",
		"fkStringExpr", "fkNullGuard", "isM2M",
		"createInputFieldType", "updateInputFieldType",
		"placeholder", "quoteIdentifier", "supportsReturning",
	}
	for _, name := range expected {
		if fm[name] == nil {
			t.Errorf("FuncMap() missing function %q", name)
		}
	}

	// toSnakeCase reads a snake_case name back out of a Go identifier, which
	// is a guess, so no template may call it; contexts carry SnakeName
	// pre-computed. The function table excludes toPlural and pkAutoGenType too.
	for _, name := range []string{"toPlural", "toSnakeCase", "pkAutoGenType"} {
		if fm[name] != nil {
			t.Errorf("FuncMap() registers %q, which must not be registered", name)
		}
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

func TestFuncLoadChunkSize(t *testing.T) {
	tests := []struct {
		name       string
		dialect    sql.Dialect
		keyColumns int
		want       int
	}{
		{"postgres single key", sql.NewPostgresDialect(), 1, 32767},
		{"postgres pair", sql.NewPostgresDialect(), 2, 16383},
		{"mysql single key", sql.NewMySQLDialect(), 1, 32767},
		{"sqlite single key", sql.NewSQLiteDialect(), 1, 16383},
		{"sqlite triple", sql.NewSQLiteDialect(), 3, 5461},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loadChunkSize := gen.FuncMap(tt.dialect)["loadChunkSize"].(func(int) int)
			if got := loadChunkSize(tt.keyColumns); got != tt.want {
				t.Errorf("loadChunkSize(%d) = %d, want %d", tt.keyColumns, got, tt.want)
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

// TestFuncSoftDeleteFilterOverride pins the literal per comparator a soft
// delete column resolves to, one row per integer width. Each row's compiled
// field is the want text written as Go, so a literal that does not type-check
// (an untyped new(0) against Number[int8]) fails the build. Edit the two
// together.
func TestFuncSoftDeleteFilterOverride(t *testing.T) {
	tests := []struct {
		ctype    string
		want     string
		compiled any
	}{
		{"*comparator.Number[int8]", "&comparator.Number[int8]{Neq: new(int8(0))}", &comparator.Number[int8]{Neq: new(int8(0))}},
		{"*comparator.Number[int16]", "&comparator.Number[int16]{Neq: new(int16(0))}", &comparator.Number[int16]{Neq: new(int16(0))}},
		{"*comparator.Number[int32]", "&comparator.Number[int32]{Neq: new(int32(0))}", &comparator.Number[int32]{Neq: new(int32(0))}},
		{"*comparator.Number[int64]", "&comparator.Number[int64]{Neq: new(int64(0))}", &comparator.Number[int64]{Neq: new(int64(0))}},
		{"*comparator.Number[uint8]", "&comparator.Number[uint8]{Neq: new(uint8(0))}", &comparator.Number[uint8]{Neq: new(uint8(0))}},
		{"*comparator.Number[uint16]", "&comparator.Number[uint16]{Neq: new(uint16(0))}", &comparator.Number[uint16]{Neq: new(uint16(0))}},
		{"*comparator.Number[uint32]", "&comparator.Number[uint32]{Neq: new(uint32(0))}", &comparator.Number[uint32]{Neq: new(uint32(0))}},
		{"*comparator.NullableNumber[int8]", "&comparator.NullableNumber[int8]{Null: new(false)}", &comparator.NullableNumber[int8]{Null: new(false)}},
		{"*comparator.Bool", "&comparator.Bool{Eq: new(true)}", &comparator.Bool{Eq: new(true)}},
		{"*comparator.NullableBool", "&comparator.NullableBool{Null: new(false)}", &comparator.NullableBool{Null: new(false)}},
		{"*comparator.NullableTime", "&comparator.NullableTime{Null: new(false)}", &comparator.NullableTime{Null: new(false)}},
	}

	fm := gen.FuncMap(sql.NewPostgresDialect())
	override := fm["softDeleteFilterOverride"].(func(gen.FilterFieldContext) string)

	for _, tt := range tests {
		t.Run(tt.ctype, func(t *testing.T) {
			got := override(gen.FilterFieldContext{ComparatorType: tt.ctype, IsSoftDeleteColumn: true})
			if got != tt.want {
				t.Errorf("softDeleteFilterOverride(%q) = %q, want %q", tt.ctype, got, tt.want)
			}
			if typ := fmt.Sprintf("%T", tt.compiled); typ != tt.ctype {
				t.Errorf("compiled literal has type %s, want %s", typ, tt.ctype)
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

// TestFuncMap_FunctionsArePureAndDeterministic calls every registered
// template function through reflection with generated arguments. A pure
// function returns the same result for the same arguments, leaves its
// arguments as it found them, and returns the same result whatever else
// ran in between (another dialect's function map, other functions).
func TestFuncMap_FunctionsArePureAndDeterministic(t *testing.T) {
	dialects := []sql.Dialect{sql.NewPostgresDialect(), sql.NewMySQLDialect(), sql.NewSQLiteDialect()}
	fills := []struct {
		name  string
		empty bool
	}{
		{name: "zero arguments", empty: true},
		{name: "populated arguments", empty: false},
	}

	t.Run("the same arguments give the same result", func(t *testing.T) {
		for _, d := range dialects {
			fm := gen.FuncMap(d)
			for _, name := range sortedFuncNames(fm) {
				for _, f := range fills {
					first := callTemplateFunc(fm[name], newFuncArgs(fm[name], f.empty))
					second := callTemplateFunc(fm[name], newFuncArgs(fm[name], f.empty))
					if diff := cmp.Diff(first, second, cmp.Exporter(func(reflect.Type) bool { return true })); diff != "" {
						t.Errorf("%s dialect: %s(%s) differs between two calls (-first +second):\n%s", d.Name(), name, f.name, diff)
					}
				}
			}
		}
	})

	t.Run("arguments are not modified", func(t *testing.T) {
		for _, d := range dialects {
			fm := gen.FuncMap(d)
			for _, name := range sortedFuncNames(fm) {
				for _, f := range fills {
					args := newFuncArgs(fm[name], f.empty)
					callTemplateFunc(fm[name], args)
					want := newFuncArgs(fm[name], f.empty)
					if diff := cmp.Diff(valuesToAny(want), valuesToAny(args), cmp.Exporter(func(reflect.Type) bool { return true })); diff != "" {
						t.Errorf("%s dialect: %s(%s) modified its arguments (-before +after):\n%s", d.Name(), name, f.name, diff)
					}
				}
			}
		}
	})

	t.Run("results do not depend on other function maps or earlier calls", func(t *testing.T) {
		for i, d := range dialects {
			fm := gen.FuncMap(d)
			before := map[string][]any{}
			for _, name := range sortedFuncNames(fm) {
				before[name] = callTemplateFunc(fm[name], newFuncArgs(fm[name], false))
			}

			// Build and exercise every other dialect's map, then a fresh map
			// for this dialect, and call this dialect's functions again in
			// reverse order.
			for j, other := range dialects {
				if j == i {
					continue
				}
				ofm := gen.FuncMap(other)
				for _, name := range sortedFuncNames(ofm) {
					callTemplateFunc(ofm[name], newFuncArgs(ofm[name], false))
				}
			}
			again := gen.FuncMap(d)
			names := sortedFuncNames(again)
			slices.Reverse(names)
			for _, name := range names {
				after := callTemplateFunc(again[name], newFuncArgs(again[name], false))
				if diff := cmp.Diff(before[name], after, cmp.Exporter(func(reflect.Type) bool { return true })); diff != "" {
					t.Errorf("%s dialect: %s result changed after other function maps ran (-before +after):\n%s", d.Name(), name, diff)
				}
			}
		}
	})
}

func sortedFuncNames(fm map[string]any) []string {
	names := make([]string, 0, len(fm))
	for name := range fm {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// callTemplateFunc calls fn with args and returns its results. A panic is
// returned as a result, so a function that panics must panic the same way
// every time.
func callTemplateFunc(fn any, args []reflect.Value) (results []any) {
	defer func() {
		if r := recover(); r != nil {
			results = []any{fmt.Sprintf("panic: %v", r)}
		}
	}()
	v := reflect.ValueOf(fn)
	if v.Type().IsVariadic() {
		return valuesToAny(v.CallSlice(args))
	}
	return valuesToAny(v.Call(args))
}

func valuesToAny(vs []reflect.Value) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v.Interface()
	}
	return out
}

// newFuncArgs builds a fresh, deterministic argument list for fn. empty
// gives every parameter its zero value; otherwise strings, numbers, bools,
// slices, maps, pointers and nested struct fields are populated.
func newFuncArgs(fn any, empty bool) []reflect.Value {
	ft := reflect.TypeOf(fn)
	f := &argFiller{}
	args := make([]reflect.Value, ft.NumIn())
	for i := range args {
		v := reflect.New(ft.In(i)).Elem()
		if !empty {
			f.fill(v, 0)
		}
		args[i] = v
	}
	return args
}

type argFiller struct{ n int }

// fillerStrings is in descending order, so a populated []string arrives
// unsorted and a function that sorts its argument in place is caught.
var fillerStrings = []string{"uuid.UUID", "user_id", "string", "sql.NullString", "posts", "int32", "*int64"}

func (f *argFiller) next() int {
	f.n++
	return f.n
}

func (f *argFiller) fill(v reflect.Value, depth int) {
	if depth > 4 || !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(fillerStrings[f.next()%len(fillerStrings)])
	case reflect.Bool:
		v.SetBool(f.next()%2 == 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(f.next()%3 + 1))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint([]uint64{1, 2, 3}[f.next()%3])
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(f.next()))
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		f.fill(p.Elem(), depth+1)
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := range 2 {
			f.fill(s.Index(i), depth+1)
		}
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k := reflect.New(v.Type().Key()).Elem()
		e := reflect.New(v.Type().Elem()).Elem()
		f.fill(k, depth+1)
		f.fill(e, depth+1)
		m.SetMapIndex(k, e)
		v.Set(m)
	case reflect.Struct:
		for i := range v.NumField() {
			f.fill(v.Field(i), depth+1)
		}
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf(fillerStrings[f.next()%len(fillerStrings)]))
		}
	default:
		// No registered function takes these kinds; they keep their zero value.
	}
}
