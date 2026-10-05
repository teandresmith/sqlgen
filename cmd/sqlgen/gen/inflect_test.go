package gen

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// frozenNameSchema carries one table per shape a frozen last word takes: an
// acronym the suffix trim doubled, an acronym the generic "-s" rule truncated,
// an ordinary noun the same rule truncated, and an acronym that is not the last
// word and therefore still inflects.
func frozenNameSchema() *parser.Schema {
	table := func(name string) parser.Table {
		return parser.Table{
			Name: name,
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
				{Name: "name", Type: "text", Nullable: false},
			},
		}
	}
	return &parser.Schema{
		Tables: []parser.Table{
			table("user_ips"),
			table("client_os"),
			table("lens"),
			table("dns_records"),
			table("products"),
		},
	}
}

func TestSplitLastWord(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantPrefix string
		wantWord   string
	}{
		{name: "single word", input: "users", wantPrefix: "", wantWord: "users"},
		{name: "underscore", input: "user_ips", wantPrefix: "user_", wantWord: "ips"},
		{name: "several underscores", input: "product_tag_labels", wantPrefix: "product_tag_", wantWord: "labels"},
		{name: "hyphen", input: "user-ips", wantPrefix: "user-", wantWord: "ips"},
		{name: "pascal case", input: "OrderItem", wantPrefix: "Order", wantWord: "Item"},
		{name: "capital run stays whole", input: "UserIPS", wantPrefix: "User", wantWord: "IPS"},
		{name: "trailing acronym", input: "ProductSKU", wantPrefix: "Product", wantWord: "SKU"},
		{name: "capital run and its lowercase tail are one word", input: "OSILayer", wantPrefix: "", wantWord: "OSILayer"},
		{name: "capital run after a lowercase word splits", input: "UserIDS", wantPrefix: "User", wantWord: "IDS"},
		{name: "dot is carried into the word", input: "asset.primary", wantPrefix: "", wantWord: "asset.primary"},
		{name: "slash splits", input: "read/write", wantPrefix: "read/", wantWord: "write"},
		{name: "trailing delimiter leaves no word", input: "user_", wantPrefix: "user_", wantWord: ""},
		{name: "empty", input: "", wantPrefix: "", wantWord: ""},
		// A byte that is not valid UTF-8 ranges as utf8.RuneError, one byte
		// wide, while utf8.RuneLen(utf8.RuneError) is three — advancing by the
		// latter walked past the end of the string and panicked. The parser
		// validates no encoding, so a name from a `.sql` file or an
		// introspected schema can carry one.
		{name: "invalid utf-8 byte ends the word", input: "users\xff", wantPrefix: "users\xff", wantWord: ""},
		{name: "invalid utf-8 byte inside a name", input: "user\xffs", wantPrefix: "user\xff", wantWord: "s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, word := splitLastWord(tt.input)
			if prefix != tt.wantPrefix || word != tt.wantWord {
				t.Errorf("splitLastWord(%q) = (%q, %q), want (%q, %q)",
					tt.input, prefix, word, tt.wantPrefix, tt.wantWord)
			}
			if prefix+word != tt.input {
				t.Errorf("splitLastWord(%q) lost bytes: prefix+word = %q", tt.input, prefix+word)
			}
		})
	}
}

// TestIsInflectionFrozen pins the two sources of the freeze and the boundary
// between them: only the LAST word decides, so `dns_records` still inflects.
func TestIsInflectionFrozen(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "canonical acronym lower", input: "ips", want: true},
		{name: "canonical acronym upper", input: "IPS", want: true},
		{name: "canonical acronym mixed", input: "Ips", want: true},
		{name: "non-plural s noun", input: "lens", want: true},
		{name: "non-plural s noun capitalized", input: "Canvas", want: true},
		{name: "ordinary plural", input: "records", want: false},
		{name: "ordinary singular", input: "record", want: false},
		{name: "acronym-shaped but not canonical", input: "zzz", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInflectionFrozen(tt.input); got != tt.want {
				t.Errorf("isInflectionFrozen(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestNonPluralSNouns_SortedAndWellFormed pins the shape of the list so an
// unsorted insert or a duplicate is a visible change. The trailing-"s" check is
// a necessary condition, not the full one: whether an entry is actually
// truncated by the generic rule was measured when it was added, and is not
// re-derivable here now that the library it was measured against is gone.
func TestNonPluralSNouns_SortedAndWellFormed(t *testing.T) {
	if !slices.IsSorted(nonPluralSNouns) {
		t.Errorf("nonPluralSNouns is not sorted: %v", nonPluralSNouns)
	}
	seen := make(map[string]bool, len(nonPluralSNouns))
	for _, n := range nonPluralSNouns {
		if seen[n] {
			t.Errorf("nonPluralSNouns has a duplicate entry %q", n)
		}
		seen[n] = true
		if n != strings.ToLower(n) {
			t.Errorf("nonPluralSNouns entry %q is not lower case", n)
		}
		if !strings.HasSuffix(n, "s") {
			t.Errorf("nonPluralSNouns entry %q does not end in %q — the generic rule never reaches it", n, "s")
		}
	}
}

// TestInflectDictionary_NoDuplicates pins the ported tables as duplicate-free.
// The source raised a panic here; newInflector takes the first entry instead,
// so without this test a bad edit would silently drop a word.
func TestInflectDictionary_NoDuplicates(t *testing.T) {
	singulars := make(map[string]bool, len(inflectDictionary))
	plurals := make(map[string]bool, len(inflectDictionary))
	for _, wd := range inflectDictionary {
		// The source panicked on a missing plural too. Without a plural and
		// without the uncountable flag, loadRules inserts a rule with an empty
		// suffix, which strings.HasSuffix matches for every word — silently
		// freezing all pluralization rather than dropping one word.
		if wd.plural == "" && !wd.uncountable {
			t.Errorf("inflectDictionary entry %q has no plural and is not uncountable", wd.singular)
		}
		if singulars[wd.singular] {
			t.Errorf("inflectDictionary has two entries for singular %q", wd.singular)
		}
		singulars[wd.singular] = true

		if wd.unidirectional {
			continue
		}
		plural := wd.plural
		if wd.uncountable && plural == "" {
			plural = wd.singular
		}
		if plurals[plural] {
			t.Errorf("inflectDictionary has two entries for plural %q — mark one unidirectional", plural)
		}
		plurals[plural] = true
		if wd.alternative != "" {
			if plurals[wd.alternative] {
				t.Errorf("inflectDictionary has two entries for plural %q", wd.alternative)
			}
			plurals[wd.alternative] = true
		}
	}
}

// TestInflectionHasNoProcessGlobalDependency pins the invariant the owned
// inflector exists for. The library it replaced loaded `inflections.json` and
// `acronyms.json` from the process working directory in an `init()`, which made
// every generated struct name and file stem steerable — and, on an entry that
// collided with its own table, crashed the binary before `main` ran. Because
// that load happened at init, no in-process test can reproduce it; the durable
// guard is that the dependency is gone (PRD §8.5).
func TestInflectionHasNoProcessGlobalDependency(t *testing.T) {
	mod, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	if strings.Contains(string(mod), "gobuffalo/flect") {
		t.Error("cmd/sqlgen depends on gobuffalo/flect again — its init() reads inflections.json " +
			"from the working directory, which re-exposes generated names to process-global state")
	}
}

// TestStructNameAndSnakeName_FrozenWords pins the two identifiers a frozen word
// reaches: the Go struct name and the file stem the generated `.go`,
// `.graphqls` and resolver seed all share. Previously `user_ips` produced
// `UserIPSIPS` in `user_ips_ips_gen.go` and `lens` produced `Len` in
// `len_gen.go`.
func TestStructNameAndSnakeName_FrozenWords(t *testing.T) {
	tests := []struct {
		name          string
		table         string
		wantStruct    string
		wantSnake     string
		wantTableCons string
	}{
		{name: "acronym doubling", table: "user_ips", wantStruct: "UserIPS", wantSnake: "user_ips", wantTableCons: "TableUserIPSes"},
		{name: "acronym truncation", table: "client_os", wantStruct: "ClientOS", wantSnake: "client_os", wantTableCons: "TableClientOSes"},
		{name: "bare acronym", table: "tls", wantStruct: "TLS", wantSnake: "tls", wantTableCons: "TableTLSes"},
		{name: "non-plural s noun", table: "lens", wantStruct: "Lens", wantSnake: "lens", wantTableCons: "TableLenses"},
		{name: "acronym is not the last word", table: "dns_records", wantStruct: "DNSRecord", wantSnake: "dns_record", wantTableCons: "TableDNSRecords"},
		{name: "acronym-final singular pluralizes", table: "user_ip", wantStruct: "UserIP", wantSnake: "user_ip", wantTableCons: "TableUserIPs"},
		{name: "ordinary plural is unaffected", table: "products", wantStruct: "Product", wantSnake: "product", wantTableCons: "TableProducts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StructName(tt.table, "", nil); got != tt.wantStruct {
				t.Errorf("StructName(%q) = %q, want %q", tt.table, got, tt.wantStruct)
			}
			if got := SnakeName(tt.table, "", nil); got != tt.wantSnake {
				t.Errorf("SnakeName(%q) = %q, want %q", tt.table, got, tt.wantSnake)
			}
			if got := TableConstantName(tt.table, "", "", nil); got != tt.wantTableCons {
				t.Errorf("TableConstantName(%q) = %q, want %q", tt.table, got, tt.wantTableCons)
			}
		})
	}
}

// TestStructNamePlural_NeverEqualsInput is the invariant that keeps the
// generated GraphQL schema valid and the resolver package compiling: the
// single-row query and the Connection query land in one `extend type Query`
// block, and their two resolver methods on one receiver, so a plural equal to
// its singular emits a duplicate SDL field and a duplicate Go method.
//
// Two independent routes break it, which is why the assertion is over every
// word rather than a list of the ones known to have failed: a frozen word,
// where no rule may rewrite the stem, and an **uncountable** noun, whose
// plural *is* its singular. Stating the invariant this way is what keeps a
// third route from reopening it.
func TestStructNamePlural_NeverEqualsInput(t *testing.T) {
	names := make([]string, 0, 8+2*len(canonicalAcronyms)+2*len(nonPluralSNouns)+3*len(inflectDictionary))
	names = append(names, "UserIPS", "ClientOS", "TLS", "Lens", "ProductSKU", "UserIP", "User", "Product")
	for _, a := range canonicalAcronyms {
		names = append(names, a, "Tbl"+a)
	}
	for _, n := range nonPluralSNouns {
		names = append(names, capitalizeFirst(n), "Tbl"+capitalizeFirst(n))
	}
	for _, wd := range inflectDictionary {
		names = append(names, capitalizeFirst(wd.singular), capitalizeFirst(wd.plural), "Tbl"+capitalizeFirst(wd.singular))
	}

	for _, n := range names {
		if n == "" {
			continue
		}
		if got := StructNamePlural(n); got == n {
			t.Errorf("StructNamePlural(%q) = %q — the single-row and Connection query names would collide", n, got)
		}
	}
}

// TestStructNamePlural_KeepsOrdinaryPlurals pins that the never-equals
// guarantee is a fallback, not a rewrite: every name inflection can already
// pluralize keeps the word English gives it.
func TestStructNamePlural_KeepsOrdinaryPlurals(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "regular", input: "Product", want: "Products"},
		{name: "y to ies", input: "Category", want: "Categories"},
		{name: "irregular", input: "Person", want: "People"},
		{name: "latin", input: "Matrix", want: "Matrices"},
		{name: "sibilant", input: "Address", want: "Addresses"},
		{name: "frozen acronym", input: "UserIPS", want: "UserIPSes"},
		{name: "uncountable takes the marker", input: "Media", want: "Medias"},
		{name: "uncountable ending in a sibilant", input: "Series", want: "Serieses"},
		{name: "uncountable ending in sh", input: "Fish", want: "Fishes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StructNamePlural(tt.input); got != tt.want {
				t.Errorf("StructNamePlural(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestToPlural_KeepsAlreadyPluralWords pins the guarantee StructNamePlural
// deliberately does NOT share. Relationship field naming pluralizes a
// snake-case SQL name and depends on an already-plural name passing through
// untouched, so a schema whose tables are named `reviews` and `tags` stays
// byte-stable (`relationshipToContext`, gen/context_table.go). Widening the
// never-equals rule to toPlural would spell `reviewses`.
func TestToPlural_KeepsAlreadyPluralWords(t *testing.T) {
	for _, name := range []string{"reviews", "tags", "developer_assets", "media", "series"} {
		if got := toPlural(name); got != name {
			t.Errorf("toPlural(%q) = %q, want it unchanged", name, got)
		}
	}
}

// TestTableConstantName_MatchesTableNameFile pins the tablename file against
// the per-table contexts it is built from: the file must declare exactly the
// constants the table clients reference, and nothing else.
//
// Two properties, both load-bearing. The `struct_name` case — the constant
// follows the override, so a renamed table has to move the constant
// here too, or the file and the clients disagree and the package will not
// compile. And the *filtering* case — a table the contexts dropped gets no
// constant, because there is no client, model, cache entry or hook to register
// against it, and a constant for one put entities in the namespace that no
// resolved-name check could see (PRD §6.4, §9.4b).
func TestTableConstantName_MatchesTableNameFile(t *testing.T) {
	schema := frozenNameSchema()
	// A PK-less table BuildTableContexts drops, and an excluded one.
	schema.Tables = append(
		schema.Tables,
		parser.Table{Name: "pkless", Columns: []parser.Column{{Name: "label", Type: "text"}}},
		parser.Table{Name: "audit_trail", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
	)
	// The PostgreSQL parser assigns every table a schema (PRD §5.5).
	for i := range schema.Tables {
		schema.Tables[i].Schema = "public"
	}

	cfg := &config.RootConfig{
		ExcludeTables: []string{"audit_*"},
		Tables:        map[string]config.TableConfig{"products": {StructName: "Item"}},
	}
	cfg.Input.Dialect = config.DialectPostgres
	cfg.Output.Driver = config.DriverPgx
	cfg.Output.Package = "db"
	cfg.Generation.QueryLimit = new(1000)
	cfg.Generation.BatchSize = new(200)
	cfg.Generation.PageSize = new(100)
	cfg.Generation.UUIDVersion = "v4"
	cfg.Overrides.UsePointers = new(true)

	input, collisions := newContextBuildInput(schema, cfg)
	tables, err := BuildTableContexts(input, collisions)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	ctx := buildTableNameFileContext(tables, nil, "db")

	if len(ctx.Tables) != len(tables) {
		t.Fatalf("buildTableNameFileContext returned %d tables, want %d (one per built context)",
			len(ctx.Tables), len(tables))
	}
	for i, entry := range ctx.Tables {
		tc := tables[i]
		if entry.ConstantName != tc.TableNameConstant {
			t.Errorf("table %q: tablename file constant = %q, want %q (the context's)",
				tc.TableName, entry.ConstantName, tc.TableNameConstant)
		}
		if entry.SQLName != tc.TableName {
			t.Errorf("table %q: tablename file SQL name = %q, want the SQL name", tc.TableName, entry.SQLName)
		}
		if want := "public." + tc.TableName; entry.Value != want {
			t.Errorf("table %q: tablename file value = %q, want %q (PostgreSQL qualifies it, PRD §8.5)",
				tc.TableName, entry.Value, want)
		}
		if !strings.HasPrefix(entry.ConstantName, "Table") {
			t.Errorf("table %q: constant %q is not the whole name — the template emits it verbatim",
				tc.TableName, entry.ConstantName)
		}
	}

	declared := make(map[string]string, len(ctx.Tables))
	for _, entry := range ctx.Tables {
		declared[entry.SQLName] = entry.ConstantName
	}
	if got := declared["products"]; got != "TableItems" {
		t.Errorf("table %q with struct_name Item: constant = %q, want %q — the override has to "+
			"reach the constant or a renamed table cannot be separated from a colliding one",
			"products", got, "TableItems")
	}
	for _, dropped := range []string{"pkless", "audit_trail"} {
		if got, ok := declared[dropped]; ok {
			t.Errorf("table %q generates nothing but the tablename file still declares %q; "+
				"a constant with no client behind it is dead weight and hides a namespace collision",
				dropped, got)
		}
	}
}

// TestTableNameFile_ValuesDistinguishSchemas pins the constant's value, not
// its identifier. The runtime identifies a table by the value alone:
// hook.ForMutation / ForTable compare m.Table, and the cache facade switches on
// it. With `audit.orders` beside `public.orders` both constants were valued
// "orders", so the cache facade failed to compile (`duplicate case`) and a hook
// scoped to one table fired on — or failed — the other.
func TestTableNameFile_ValuesDistinguishSchemas(t *testing.T) {
	orders := func(schema string) parser.Table {
		return parser.Table{
			Name:   "orders",
			Schema: schema,
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "note", Type: "text", Nullable: true},
			},
		}
	}
	tests := []struct {
		name    string
		dialect config.Dialect
		tables  []parser.Table
		views   []ViewContext
		want    map[string]string // constant identifier → value
	}{
		{
			name:    "postgres qualifies every value, and two schemas stay apart",
			dialect: config.DialectPostgres,
			tables:  []parser.Table{orders("audit"), orders("public")},
			views: []ViewContext{
				{TableNameConstant: "TableOrderTotals", ViewName: "order_totals", Schema: "public"},
			},
			want: map[string]string{
				"TableAuditOrders":  "audit.orders",
				"TablePublicOrders": "public.orders",
				"TableOrderTotals":  "public.order_totals",
			},
		},
		{
			name:    "a table with no schema keeps the bare name",
			dialect: config.DialectSQLite,
			tables: []parser.Table{{
				Name:    "users",
				Columns: []parser.Column{{Name: "id", Type: "integer", PrimaryKey: true}},
			}},
			views: []ViewContext{
				{TableNameConstant: "TableUserTotals", ViewName: "user_totals"},
			},
			want: map[string]string{
				"TableUsers":      "users",
				"TableUserTotals": "user_totals",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.RootConfig{}
			cfg.Input.Dialect = tt.dialect
			cfg.Output.Driver = config.DriverStdlib
			if tt.dialect == config.DialectPostgres {
				cfg.Output.Driver = config.DriverPgx
			}
			cfg.Output.Package = "db"
			cfg.Generation.QueryLimit = new(1000)
			cfg.Generation.BatchSize = new(200)
			cfg.Generation.PageSize = new(100)
			cfg.Generation.UUIDVersion = "v4"
			cfg.Overrides.UsePointers = new(true)

			input, collisions := newContextBuildInput(&parser.Schema{Tables: tt.tables}, cfg)
			tables, err := BuildTableContexts(input, collisions)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			ctx := buildTableNameFileContext(tables, tt.views, "db")

			got := make(map[string]string)
			for _, entry := range slices.Concat(ctx.Tables, ctx.Views) {
				got[entry.ConstantName] = entry.Value
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("constant values mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
