package gen

import "testing"

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// PRD 8.5 examples
		{name: "user_id", input: "user_id", want: "UserID"},
		{name: "ip_address", input: "ip_address", want: "IPAddress"},
		{name: "api_url", input: "api_url", want: "APIURL"},
		{name: "http_status", input: "http_status", want: "HTTPStatus"},
		{name: "json_data", input: "json_data", want: "JSONData"},
		{name: "ssl_enabled", input: "ssl_enabled", want: "SSLEnabled"},
		{name: "cpu_usage", input: "cpu_usage", want: "CPUUsage"},
		{name: "ttl_seconds", input: "ttl_seconds", want: "TTLSeconds"},

		// Acronym at start
		{name: "id_value", input: "id_value", want: "IDValue"},

		// Acronym in middle
		{name: "get_http_response", input: "get_http_response", want: "GetHTTPResponse"},
		{name: "user_api_key", input: "user_api_key", want: "UserAPIKey"},

		// Acronym at end
		{name: "primary_id", input: "primary_id", want: "PrimaryID"},
		{name: "base_url", input: "base_url", want: "BaseURL"},
		{name: "request_uri", input: "request_uri", want: "RequestURI"},

		// Non-acronym words
		{name: "created_at", input: "created_at", want: "CreatedAt"},
		{name: "first_name", input: "first_name", want: "FirstName"},
		{name: "order_items", input: "order_items", want: "OrderItems"},

		// Single word
		{name: "name", input: "name", want: "Name"},
		{name: "id", input: "id", want: "ID"},

		// Empty
		{name: "empty", input: "", want: ""},

		// Already PascalCase input
		{name: "already pascal", input: "UserID", want: "UserID"},

		// Digit-leading — §8.5 "Digit-Leading Handling"
		{name: "digit-leading mixed", input: "2010_revenue", want: "Col2010Revenue"},
		{name: "digit-leading numeric only", input: "2010", want: "Col2010"},
		{name: "digit-leading single digit", input: "1st_place", want: "Col1stPlace"},
		{name: "digit-leading already-prefixed input", input: "col_2010_revenue", want: "Col2010Revenue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toPascalCase(tt.input)
			if got != tt.want {
				t.Errorf("toPascalCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "user_id", input: "user_id", want: "userID"},
		{name: "ip_address", input: "ip_address", want: "ipAddress"},
		{name: "created_at", input: "created_at", want: "createdAt"},
		{name: "http_status", input: "http_status", want: "httpStatus"},
		{name: "api_url", input: "api_url", want: "apiURL"},
		{name: "single word", input: "name", want: "name"},
		{name: "empty", input: "", want: ""},
		{name: "first word acronym", input: "id_value", want: "idValue"},

		// Digit-leading — §8.5 "Digit-Leading Handling"
		{name: "digit-leading mixed", input: "2010_revenue", want: "col2010Revenue"},
		{name: "digit-leading numeric only", input: "2010", want: "col2010"},
		{name: "digit-leading single digit", input: "1st_place", want: "col1stPlace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toCamelCase(tt.input)
			if got != tt.want {
				t.Errorf("toCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple pascal", input: "CreatedAt", want: "created_at"},
		{name: "trailing acronym", input: "UserID", want: "user_id"},
		{name: "leading acronym", input: "IPAddress", want: "ip_address"},
		{name: "leading long acronym", input: "HTTPStatus", want: "http_status"},
		{name: "no uppercase", input: "name", want: "name"},
		{name: "empty", input: "", want: ""},
		{name: "already snake", input: "created_at", want: "created_at"},
		{name: "json data", input: "JSONData", want: "json_data"},

		// Longest-match over the canonical set. flect took the shortest and
		// misspelled every identifier carrying an acronym that extends another.
		// One case per prefix pair, plus the boundary shapes that keep
		// longest-match honest.
		{name: "pair DSL/DSLAM", input: "DSLAMPort", want: "dslam_port"},
		{name: "pair HTTP/HTTPS", input: "HTTPSURL", want: "https_url"},
		{name: "pair ID/IDF", input: "IDFRoom", want: "idf_room"},
		{name: "pair ID/IDS", input: "IDSAlert", want: "ids_alert"},
		{name: "pair IP/IPS", input: "IPSMode", want: "ips_mode"},
		{name: "pair NRZ/NRZI", input: "NRZIMode", want: "nrzi_mode"},
		{name: "pair OS/OSI", input: "OSILayer", want: "osi_layer"},
		{name: "pair OS/OSPF", input: "OSPFArea", want: "ospf_area"},
		{name: "pair PC/PCM", input: "PCMRate", want: "pcm_rate"},
		{name: "pair SNA/SNAP", input: "SNAPHeader", want: "snap_header"},

		// The shorter member of a pair still wins when the capital that would
		// complete the longer one heads the next word.
		{name: "shorter member wins before lowercase", input: "IDSlot", want: "id_slot"},
		{name: "unextended acronym", input: "OSName", want: "os_name"},
		{name: "acronym then acronym", input: "APIURL", want: "api_url"},

		// Digit boundaries end a run of capitals without splitting it.
		{name: "digit inside acronym", input: "Utf8Body", want: "utf8_body"},
		{name: "acronym then digit", input: "HTTP2Flag", want: "http2_flag"},
		{name: "acronym after digit", input: "Line2ID", want: "line2_id"},
		{name: "digit-leading guard form", input: "Col2010Revenue", want: "col2010_revenue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toSnakeCase(tt.input)
			if got != tt.want {
				t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSnakeCaseRoundTrip(t *testing.T) {
	// snake_case → PascalCase → snake_case should produce the original.
	tests := []string{
		"user_id",
		"ip_address",
		"http_status",
		"json_data",
		"cpu_usage",
		"created_at",
		"first_name",
		"order_items",
		"api_url",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			pascal := toPascalCase(input)
			got := toSnakeCase(pascal)
			if got != input {
				t.Errorf("roundtrip %q → %q → %q, want %q", input, pascal, got, input)
			}
		})
	}
}

func TestToSingular(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "regular -s", input: "users", want: "user"},
		{name: "regular -s products", input: "products", want: "product"},
		{name: "ies to y", input: "companies", want: "company"},
		{name: "ies to y categories", input: "categories", want: "category"},
		{name: "sses to ss", input: "addresses", want: "address"},
		{name: "uses to us", input: "statuses", want: "status"},
		{name: "shes to sh", input: "dishes", want: "dish"},
		{name: "ches to ch", input: "watches", want: "watch"},
		{name: "xes to x", input: "boxes", want: "box"},
		{name: "oes to o", input: "heroes", want: "hero"},
		{name: "ses to se", input: "responses", want: "response"},
		{name: "ves to f", input: "wolves", want: "wolf"},
		{name: "already singular", input: "user", want: "user"},
		{name: "singular ss", input: "boss", want: "boss"},
		{name: "empty", input: "", want: ""},
		{name: "uncountable", input: "sheep", want: "sheep"},
		{name: "irregular", input: "people", want: "person"},
		{name: "order_items", input: "order_items", want: "order_item"},
		{name: "already singular -us", input: "status", want: "status"},
		{name: "already singular -us campus", input: "campus", want: "campus"},
		{name: "already singular -us focus", input: "focus", want: "focus"},
		{name: "already singular -us radius", input: "radius", want: "radius"},
		{name: "already singular -is basis", input: "basis", want: "basis"},
		{name: "already singular -is thesis", input: "thesis", want: "thesis"},

		// A frozen last word never inflects. The generic "-s" rule
		// truncated an acronym that was never a plural, and the library's
		// suffix trim appended the singular to the whole input instead of
		// replacing it (PRD §8.5).
		{name: "acronym doubling ips", input: "user_ips", want: "user_ips"},
		{name: "acronym doubling dns", input: "dns", want: "dns"},
		{name: "acronym truncation os", input: "client_os", want: "client_os"},
		{name: "acronym truncation tls", input: "tls", want: "tls"},
		{name: "acronym pascal case", input: "UserIPS", want: "UserIPS"},
		{name: "non-plural s noun lens", input: "lens", want: "lens"},
		{name: "non-plural s noun gas", input: "user_gas", want: "user_gas"},
		{name: "non-plural s noun canvas", input: "canvas", want: "canvas"},
		{name: "acronym-final but not last word", input: "dns_records", want: "dns_record"},
		{name: "acronym prefix pair ip stays plural", input: "user_ips", want: "user_ips"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toSingular(tt.input)
			if got != tt.want {
				t.Errorf("toSingular(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToPlural(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "regular", input: "user", want: "users"},
		{name: "regular product", input: "product", want: "products"},
		{name: "y to ies", input: "company", want: "companies"},
		{name: "y to ies category", input: "category", want: "categories"},
		{name: "vowel y", input: "day", want: "days"},
		{name: "ss to sses", input: "address", want: "addresses"},
		{name: "s to ses", input: "status", want: "statuses"},
		{name: "sh to shes", input: "dish", want: "dishes"},
		{name: "ch to ches", input: "watch", want: "watches"},
		{name: "x to xes", input: "box", want: "boxes"},
		{name: "o to oes", input: "hero", want: "heroes"},
		{name: "f to ves", input: "wolf", want: "wolves"},
		{name: "empty", input: "", want: ""},
		{name: "uncountable", input: "sheep", want: "sheep"},
		{name: "irregular", input: "person", want: "people"},

		// A frozen last word takes an appended marker rather than a
		// rewritten stem, and never returns its input: QueryName and
		// QueryNamePlural share one `extend type Query` block (PRD §8.5).
		{name: "acronym appends s", input: "user_ip", want: "user_ips"},
		{name: "acronym ending in s appends es", input: "user_ips", want: "user_ipses"},
		{name: "acronym doubling dns", input: "dns", want: "dnses"},
		{name: "acronym -f is not -ves", input: "xsrf", want: "xsrfs"},
		{name: "acronym pascal case", input: "UserIPS", want: "UserIPSes"},
		{name: "acronym pascal case sku", input: "ProductSKU", want: "ProductSKUs"},
		{name: "non-plural s noun lens", input: "lens", want: "lenses"},
		{name: "non-plural s noun gas", input: "gas", want: "gases"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toPlural(tt.input)
			if got != tt.want {
				t.Errorf("toPlural(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStructName(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		schema     string
		collisions map[string]bool
		want       string
	}{
		// No collisions — no prefix regardless of schema
		{name: "products no collision", table: "products", schema: "public", collisions: nil, want: "Product"},
		{name: "order_items no collision", table: "order_items", schema: "public", collisions: nil, want: "OrderItem"},
		{name: "users no collision", table: "users", schema: "", collisions: nil, want: "User"},
		{name: "companies no collision", table: "companies", schema: "public", collisions: nil, want: "Company"},
		{name: "statuses no collision", table: "statuses", schema: "public", collisions: nil, want: "Status"},
		{name: "addresses no collision", table: "addresses", schema: "public", collisions: nil, want: "Address"},

		// Colliding name — prefix with schema
		{name: "public users collide", table: "users", schema: "public", collisions: map[string]bool{"users": true}, want: "PublicUser"},
		{name: "billing users collide", table: "users", schema: "billing", collisions: map[string]bool{"users": true}, want: "BillingUser"},

		// Non-colliding name in multi-schema — no prefix
		{name: "products no collide multi", table: "products", schema: "public", collisions: map[string]bool{"users": true}, want: "Product"},

		// Colliding name with empty schema — no prefix
		{name: "collide no schema", table: "users", schema: "", collisions: map[string]bool{"users": true}, want: "User"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StructName(tt.table, tt.schema, tt.collisions)
			if got != tt.want {
				t.Errorf("StructName(%q, %q, collisions) = %q, want %q", tt.table, tt.schema, got, tt.want)
			}
		})
	}
}

// TestToSnakeName pins the snake_case spelling derived from the SQL name. It
// is exact where toSnakeCase can only guess, because the SQL name still carries
// the word boundaries the PascalCase form drops — one case per prefix pair, and
// the single-letter shape no inverse can recover (PRD §8.5).
func TestToSnakeName(t *testing.T) {
	// Every one of these is its own snake_case spelling: the helper must be a
	// no-op on a name the schema already spells in snake_case.
	identity := []string{
		"user_id", "created_at", "api_url", "csv_data", "ascii_art", "tcp_port",
		"mac_addr", "sku_code", "gpu_count", "os_name", "io_count", "utf8_body",
		"http2_flag", "line2_id", "s3_url",

		// One per prefix pair. Each of these round-tripped through PascalCase
		// as a different name when read back out of the PascalCase form
		// ("osi_layer" → "os_ilayer").
		"dslam_port", "https_url", "idf_room", "ids_alert", "ips_mode",
		"nrzi_mode", "osi_layer", "ospf_area", "pcm_rate", "snap_header",

		// A single-letter word leaves no seam in the PascalCase form at all
		// ("a_vpn_b" → "AVPNB"), so no inverse could recover it either.
		"a_vpn_b", "x_id_y",

		// UI/UID and SLA/SLARP: excluded from the set, spelled correctly here
		// whether or not that ever changes.
		"uid_map", "user_uid", "slarp_id", "sla_days",
	}
	for _, name := range identity {
		t.Run(name, func(t *testing.T) {
			if got := toSnakeName(name); got != name {
				t.Errorf("toSnakeName(%q) = %q, want %q", name, got, name)
			}
		})
	}

	shaped := []struct {
		name  string
		input string
		want  string
	}{
		{name: "camel source", input: "orderItems", want: "order_items"},
		{name: "pascal source", input: "OrderItems", want: "order_items"},
		{name: "hyphenated source", input: "order-items", want: "order_items"},
		{name: "dot filtered without splitting", input: "asset.primary", want: "assetprimary"},
		{name: "other punctuation splits", input: "read/write", want: "read_write"},
		{name: "all-caps source has no seam", input: "APIURL", want: "apiurl"},
		{name: "no digit-leading guard here", input: "2010_revenue", want: "2010_revenue"},
		{name: "empty", input: "", want: ""},
	}
	for _, tt := range shaped {
		t.Run(tt.name, func(t *testing.T) {
			if got := toSnakeName(tt.input); got != tt.want {
				t.Errorf("toSnakeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestScreamingSnakeCase pins the sort-enum value spelling. It shares
// toSnakeName with gqlEnumIdent but carries a different digit-leading prefix
// ("col_", not "col"), and both are frozen wire identifiers (PRD §8.5) — so
// each needs its own pin, including on the extending-acronym and single-letter
// shapes.
func TestScreamingSnakeCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "snake", input: "created_at", want: "CREATED_AT"},
		{name: "acronym", input: "api_url", want: "API_URL"},
		{name: "camel source", input: "createdAt", want: "CREATED_AT"},

		// Prefix pairs: spelled through the PascalCase form these come out
		// wrong ("https_url" → HTTP_SURL).
		{name: "extending acronym", input: "https_url", want: "HTTPS_URL"},
		{name: "extending acronym ids", input: "ids_alert", want: "IDS_ALERT"},
		{name: "single-letter word", input: "a_vpn_b", want: "A_VPN_B"},

		// The "col_" guard, which gqlEnumIdent deliberately does not share.
		{name: "digit-leading", input: "2010_revenue", want: "COL_2010_REVENUE"},
		{name: "digit inside", input: "utf8_body", want: "UTF8_BODY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := screamingSnakeCase(tt.input); got != tt.want {
				t.Errorf("screamingSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestSnakeName mirrors TestStructName: the same inputs, asserting the file
// stem that accompanies each struct. The two must stay in step — the stem names
// the file the struct is generated into (PRD §8.5).
func TestSnakeName(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		schema     string
		collisions map[string]bool
		want       string
	}{
		// No collisions — no prefix regardless of schema
		{name: "products no collision", table: "products", schema: "public", collisions: nil, want: "product"},
		{name: "order_items no collision", table: "order_items", schema: "public", collisions: nil, want: "order_item"},
		{name: "users no collision", table: "users", schema: "", collisions: nil, want: "user"},
		{name: "companies no collision", table: "companies", schema: "public", collisions: nil, want: "company"},
		{name: "statuses no collision", table: "statuses", schema: "public", collisions: nil, want: "status"},
		{name: "addresses no collision", table: "addresses", schema: "public", collisions: nil, want: "address"},

		// Colliding name — prefix with schema
		{name: "public users collide", table: "users", schema: "public", collisions: map[string]bool{"users": true}, want: "public_user"},
		{name: "billing users collide", table: "users", schema: "billing", collisions: map[string]bool{"users": true}, want: "billing_user"},

		// Non-colliding name in multi-schema — no prefix
		{name: "products no collide multi", table: "products", schema: "public", collisions: map[string]bool{"users": true}, want: "product"},

		// Colliding name with empty schema — no prefix
		{name: "collide no schema", table: "users", schema: "", collisions: map[string]bool{"users": true}, want: "user"},

		// Prefix-pair tables: the stem the PascalCase form could not spell.
		{name: "extending acronym", table: "osi_layers", schema: "", collisions: nil, want: "osi_layer"},
		{name: "extending acronym collide", table: "osi_layers", schema: "audit", collisions: map[string]bool{"osi_layers": true}, want: "audit_osi_layer"},
		{name: "https table", table: "https_urls", schema: "", collisions: nil, want: "https_url"},

		// Digit-leading names keep the "col" guard StructName applies, so the
		// stem still matches the struct it names.
		{name: "digit-leading table", table: "2010_revenues", schema: "", collisions: nil, want: "col2010_revenue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SnakeName(tt.table, tt.schema, tt.collisions)
			if got != tt.want {
				t.Errorf("SnakeName(%q, %q, collisions) = %q, want %q", tt.table, tt.schema, got, tt.want)
			}
		})
	}
}

// TestSnakeNameFor pins the one path that still has to read a Go identifier:
// a `struct_name` config override has no SQL name behind it.
func TestSnakeNameFor(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		schema     string
		override   string
		collisions map[string]bool
		want       string
	}{
		{name: "no override uses the SQL name", table: "osi_layers", override: "", want: "osi_layer"},
		{name: "override is read back", table: "osi_layers", override: "OSILayer", want: "osi_layer"},
		{name: "override wins over the SQL name", table: "products", override: "Widget", want: "widget"},
		{name: "override ignores the collision prefix", table: "users", schema: "public", override: "AuditUser", collisions: map[string]bool{"users": true}, want: "audit_user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snakeNameFor(tt.table, tt.schema, tt.override, tt.collisions)
			if got != tt.want {
				t.Errorf("snakeNameFor(%q, %q, %q, collisions) = %q, want %q",
					tt.table, tt.schema, tt.override, got, tt.want)
			}
		})
	}
}

func TestFieldName(t *testing.T) {
	tests := []struct {
		name   string
		column string
		want   string
	}{
		{name: "created_at", column: "created_at", want: "CreatedAt"},
		{name: "user_id", column: "user_id", want: "UserID"},
		{name: "ip_address", column: "ip_address", want: "IPAddress"},
		{name: "name", column: "name", want: "Name"},
		{name: "api_url", column: "api_url", want: "APIURL"},

		// Digit-leading — §8.5 "Digit-Leading Handling"
		{name: "digit-leading 2010_revenue", column: "2010_revenue", want: "Col2010Revenue"},
		{name: "digit-leading 2010", column: "2010", want: "Col2010"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FieldName(tt.column)
			if got != tt.want {
				t.Errorf("FieldName(%q) = %q, want %q", tt.column, got, tt.want)
			}
		})
	}
}

// TestGQLEnumIdent pins the GraphQL enum identifier derivation against every
// shape that flows through `buildAPIEnumContext` (schema-side) and
// `templates/enum.go.tmpl::MarshalGQL/UnmarshalGQL` (runtime-side). The single
// helper backs both, so a divergence between the schema declaration and the
// wire format becomes structurally impossible — the goldens and these cases
// together pin that contract.
//
// Prior to consolidating the helper, the schema-side derivation was
// `strings.ToUpper(toPascalCase(v))` while the template-side derivation was
// `toPascalCase | screamingSnakeCase`. The two agree for dotted values
// (`asset.primary` → `ASSETPRIMARY`, what flect.Pascalize flattens) and bare
// words (`spv` → `SPV`), but diverge for snake-cased enum values: the schema
// emitted `MULTIWORDVALUE` while MarshalGQL wrote `MULTI_WORD_VALUE`, which
// gqlgen would have rejected at runtime. The §13.7 fixture happened to use
// only dotted values, masking the gap.
// TestSafeGoIdent covers the public contract documented in
// PRD §8.5 (Reserved Word Handling): pass-through for non-reserved names, "Val" suffix
// for every keyword, predeclared identifier, and generator-reserved local.
// The exhaustive sweep (every entry of goReservedIdents) is in
// TestSafeGoIdent_AllReservedEscape; this test pins the user-facing
// behavior with representative cases.
func TestSafeGoIdent(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		// Pass-through — typical column names already camelCased
		{name: "pass-through plain", in: "name", want: "name"},
		{name: "pass-through camel acronym", in: "userID", want: "userID"},
		{name: "pass-through createdAt", in: "createdAt", want: "createdAt"},
		{name: "pass-through status", in: "status", want: "status"},
		{name: "pass-through empty", in: "", want: ""},

		// Keywords — sample
		{name: "keyword type", in: "type", want: "typeVal"},
		{name: "keyword range", in: "range", want: "rangeVal"},
		{name: "keyword case", in: "case", want: "caseVal"},
		{name: "keyword func", in: "func", want: "funcVal"},
		{name: "keyword default", in: "default", want: "defaultVal"},
		{name: "keyword select", in: "select", want: "selectVal"},
		{name: "keyword map", in: "map", want: "mapVal"},
		{name: "keyword chan", in: "chan", want: "chanVal"},
		{name: "keyword return", in: "return", want: "returnVal"},
		{name: "keyword interface", in: "interface", want: "interfaceVal"},
		{name: "keyword package", in: "package", want: "packageVal"},

		// Predeclared types and constants
		{name: "predeclared any", in: "any", want: "anyVal"},
		{name: "predeclared comparable", in: "comparable", want: "comparableVal"},
		{name: "predeclared error", in: "error", want: "errorVal"},
		{name: "predeclared string", in: "string", want: "stringVal"},
		{name: "predeclared int", in: "int", want: "intVal"},
		{name: "predeclared bool", in: "bool", want: "boolVal"},
		{name: "predeclared nil", in: "nil", want: "nilVal"},
		{name: "predeclared true", in: "true", want: "trueVal"},
		{name: "predeclared false", in: "false", want: "falseVal"},
		{name: "predeclared iota", in: "iota", want: "iotaVal"},

		// Predeclared functions (incl. Go 1.21+ min/max/clear)
		{name: "builtin new", in: "new", want: "newVal"},
		{name: "builtin make", in: "make", want: "makeVal"},
		{name: "builtin len", in: "len", want: "lenVal"},
		{name: "builtin cap", in: "cap", want: "capVal"},
		{name: "builtin append", in: "append", want: "appendVal"},
		{name: "builtin copy", in: "copy", want: "copyVal"},
		{name: "builtin delete", in: "delete", want: "deleteVal"},
		{name: "builtin min", in: "min", want: "minVal"},
		{name: "builtin max", in: "max", want: "maxVal"},
		{name: "builtin clear", in: "clear", want: "clearVal"},

		// Generator-reserved locals
		{name: "reserved ctx", in: "ctx", want: "ctxVal"},
		{name: "reserved err", in: "err", want: "errVal"},
		{name: "reserved v", in: "v", want: "vVal"},
		{name: "reserved ok", in: "ok", want: "okVal"},

		// Capitalized variants are not reserved — PascalCase exposed names
		// never collide, so the helper passes them through unchanged.
		{name: "pascal Type", in: "Type", want: "Type"},
		{name: "pascal Range", in: "Range", want: "Range"},
		{name: "pascal Error", in: "Error", want: "Error"},

		// Digit-leading — §8.5 "Digit-Leading Handling"
		{name: "digit-leading camel form", in: "2010Revenue", want: "col2010Revenue"},
		{name: "digit-leading numeric only", in: "2010", want: "col2010"},
		// Idempotent: already-prefixed input passes through unchanged.
		{name: "digit-leading already prefixed", in: "col2010Revenue", want: "col2010Revenue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeGoIdent(tt.in)
			if got != tt.want {
				t.Errorf("safeGoIdent(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSafeGoIdent_AllReservedEscape walks every entry of goReservedIdents and
// asserts:
//
//  1. Every reserved name is rewritten (no passthrough).
//  2. The escaped form is itself NOT reserved — i.e., the "Val" suffix is a
//     safe landing zone for every current keyword and predeclared identifier.
//     This pins the choice of suffix: if Go ever adds (say) `typeVal` as a
//     predeclared identifier, this test fails loudly and forces a suffix
//     review.
func TestSafeGoIdent_AllReservedEscape(t *testing.T) {
	for name := range goReservedIdents {
		t.Run(name, func(t *testing.T) {
			escaped := safeGoIdent(name)
			if escaped == name {
				t.Errorf("safeGoIdent(%q) returned input unchanged; expected escape", name)
			}
			if goReservedIdents[escaped] {
				t.Errorf("safeGoIdent(%q) = %q, which is itself reserved — suffix collision", name, escaped)
			}
		})
	}
}

func TestGQLEnumIdent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "dotted", input: "asset.primary", want: "ASSETPRIMARY"},
		{name: "dotted multi-segment", input: "foo.bar.baz", want: "FOOBARBAZ"},
		{name: "bare", input: "spv", want: "SPV"},
		{name: "snake", input: "multi_word_value", want: "MULTI_WORD_VALUE"},
		{name: "snake with acronym", input: "http_status", want: "HTTP_STATUS"},
		{name: "pascal-cased input", input: "AssetPrimary", want: "ASSET_PRIMARY"},
		{name: "camel-cased input", input: "assetPrimary", want: "ASSET_PRIMARY"},
		{name: "hyphenated", input: "asset-primary", want: "ASSET_PRIMARY"},
		{name: "single-letter", input: "x", want: "X"},

		// The identifier is spelled from the declared value, not read back out
		// of its PascalCase form, which would publish HTTP_SURL.
		{name: "extending acronym", input: "https_url", want: "HTTPS_URL"},
		{name: "extending acronym ids", input: "ids_alert", want: "IDS_ALERT"},
		{name: "single-letter word", input: "a_vpn_b", want: "A_VPN_B"},
		{name: "digit-leading keeps the col guard", input: "2010_revenue", want: "COL2010_REVENUE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gqlEnumIdent(tt.input)
			if got != tt.want {
				t.Errorf("gqlEnumIdent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
