package gen

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestCanonicalAcronyms_GqlgenDivergentSpellings pins the Go field names for
// every column shape found diverging between sqlgen's spelling and gqlgen's
// templates.ToGo. These names are now authoritative rather than predictive:
// APIGoFieldOverrides hands each one to gqlgen as a fieldName override, so the
// generated struct carries exactly what this test asserts.
//
// The "was" values are recorded per case rather than asserted — this test
// guards the current spelling; the end-to-end guarantee that gqlgen agrees
// lives in the graphql example, which compiles these columns against real
// gqlgen (PRD §8.5).
func TestCanonicalAcronyms_GqlgenDivergentSpellings(t *testing.T) {
	tests := []struct {
		name   string
		column string
		want   string
		was    string // sqlgen's former spelling
	}{
		// sqlgen uppercased, gqlgen did not.
		{name: "mac", column: "mac", want: "MAC", was: "MAC"},
		{name: "mac_addr", column: "mac_addr", want: "MACAddr", was: "MACAddr"},
		{name: "pk_val", column: "pk_val", want: "PKVal", was: "PKVal"},
		{name: "fk_val", column: "fk_val", want: "FKVal", was: "FKVal"},
		{name: "io_count", column: "io_count", want: "IOCount", was: "IOCount"},
		{name: "os_name", column: "os_name", want: "OSName", was: "OSName"},
		{name: "ssl_mode", column: "ssl_mode", want: "SSLMode", was: "SSLMode"},

		// gqlgen uppercased, sqlgen did not — these are the spellings that move.
		{name: "ascii_art", column: "ascii_art", want: "ASCIIArt", was: "AsciiArt"},
		{name: "csv_data", column: "csv_data", want: "CSVData", was: "CsvData"},
		{name: "xml_body", column: "xml_body", want: "XMLBody", was: "XmlBody"},
		{name: "tcp_port", column: "tcp_port", want: "TCPPort", was: "TcpPort"},
		{name: "tls_version", column: "tls_version", want: "TLSVersion", was: "TlsVersion"},
		{name: "css_class", column: "css_class", want: "CSSClass", was: "CssClass"},
		{name: "guid", column: "guid", want: "GUID", was: "Guid"},
		// UID is deliberately NOT in the canonical set — it would add an
		// eleventh UI/UID prefix pair. Listed to pin the exclusion, not the
		// uppercasing.
		{name: "uid", column: "uid", want: "Uid", was: "Uid"},
		{name: "vm_size", column: "vm_size", want: "VMSize", was: "VmSize"},
		{name: "aws_region", column: "aws_region", want: "AWSRegion", was: "AwsRegion"},
		{name: "rpc_name", column: "rpc_name", want: "RPCName", was: "RpcName"},
		{name: "xsrf_token", column: "xsrf_token", want: "XSRFToken", was: "XsrfToken"},

		// sqlgen additions absent from both upstream lists (PRD §8.5).
		{name: "gpu_count", column: "gpu_count", want: "GPUCount", was: "GpuCount"},
		{name: "sku_code", column: "sku_code", want: "SKUCode", was: "SkuCode"},

		// Digit boundaries never form an acronym — gqlgen's word walker splits
		// at lower→digit unconditionally, so UTF8 cannot match and HTTP can.
		{name: "utf8_body", column: "utf8_body", want: "Utf8Body", was: "UTF8Body"},
		{name: "http2_flag", column: "http2_flag", want: "HTTP2Flag", was: "Http2Flag"},

		// An acronym directly after a digit-terminated word. sqlgen has always
		// spelled these correctly; gqlgen could not follow, because the emitted
		// camelCase field name ("line2ID") leaves its word walker no seam. The
		// names below are dictated to gqlgen rather than predicted, which is
		// what makes them safe.
		{name: "line2_id", column: "line2_id", want: "Line2ID", was: "Line2ID"},
		{name: "address2_id", column: "address2_id", want: "Address2ID", was: "Address2ID"},
		{name: "s3_url", column: "s3_url", want: "S3URL", was: "S3URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FieldName(tt.column); got != tt.want {
				t.Errorf("FieldName(%q) = %q, want %q (formerly %q)", tt.column, got, tt.want, tt.was)
			}
		})
	}
}

// TestCanonicalAcronyms_UppercaseAndSorted pins the two shape invariants of
// the canonical set. Sorted: the list is hand-maintained, and sorting keeps
// duplicates and prefix pairs (UI/UID) adjacent where review can see them.
// Uppercase: identCaser matches a whole word case-folded to upper, so a
// mixed-case entry could never fire (PRD §8.5).
func TestCanonicalAcronyms_UppercaseAndSorted(t *testing.T) {
	got := canonicalAcronyms

	if !slices.IsSorted(got) {
		t.Error("canonicalAcronyms is not sorted")
	}

	seen := make(map[string]bool, len(got))
	for _, a := range got {
		if a != strings.ToUpper(a) {
			t.Errorf("acronym %q is not uppercase", a)
		}
		if a == "" {
			t.Error("acronym set contains an empty entry")
		}
		if seen[a] {
			t.Errorf("acronym %q appears more than once", a)
		}
		seen[a] = true
	}
}

// TestCanonicalAcronyms_SnakeCaseInverse pins the snake_case spelling of every
// acronym shape that reaches it, through the helper that actually spells
// generated filenames, JSON tags and enum identifiers: toSnakeName, which reads
// the SQL name. That direction is exact — the SQL name still carries the word
// boundaries the PascalCase form drops (PRD §8.5).
//
// The PascalCase round trip is asserted alongside it, but only for the columns
// where a reading can succeed. It cannot for a column carrying an acronym that
// extends another ("osi_layer" → OSILayer, which is also the PascalCase form of
// "osi_layer" and of nothing else, but "OSIDValue" is the form of both
// "os_id_value" and "osi_dvalue"), nor for a single-letter word ("a_vpn_b" →
// AVPNB). Those live in TestToSnakeName, which asserts the exact direction only.
func TestCanonicalAcronyms_SnakeCaseInverse(t *testing.T) {
	columns := []string{
		"csv_data", "ascii_art", "tcp_port", "mac_addr", "sku_code",
		"gpu_count", "os_name", "io_count", "utf8_body", "http2_flag",
		"user_id", "created_at", "api_url",

		// One column per prefix pair. Every one of these spelled itself
		// differently when read back from PascalCase ("osi_layer" → "os_ilayer").
		"dslam_port", "https_url", "idf_room", "ids_alert", "ips_mode",
		"nrzi_mode", "osi_layer", "ospf_area", "pcm_rate", "snap_header",

		// UI/UID and SLA/SLARP — excluded from the set, spelled correctly here
		// either way.
		"uid_maps", "user_uid", "slarp_id", "sla_days",
	}

	for _, column := range columns {
		t.Run(column, func(t *testing.T) {
			if got := toSnakeName(column); got != column {
				t.Errorf("toSnakeName(%q) = %q, want %q", column, got, column)
			}
			pascal := toPascalCase(column)
			if got := toSnakeCase(pascal); got != column {
				t.Errorf("round trip %q → %q → %q, want %q", column, pascal, got, column)
			}
		})
	}
}

// TestCanonicalAcronyms_PrefixPairs pins the ten entries that extend another
// entry in the set. A prefix pair is what makes the PascalCase form unreadable
// — "OSIDValue" is the form of both "os_id_value" and "osi_dvalue" — which is
// why every generated name that has a SQL name to read spells itself from that
// instead (PRD §8.5).
//
// The list is asserted rather than merely counted so that adding an entry which
// extends an existing one is a deliberate, visible change: the pair shows up
// here and in the doc comment on canonicalAcronyms, not as a silently
// misspelled filename.
func TestCanonicalAcronyms_PrefixPairs(t *testing.T) {
	want := []string{
		"DSL/DSLAM", "HTTP/HTTPS", "ID/IDF", "ID/IDS", "IP/IPS",
		"NRZ/NRZI", "OS/OSI", "OS/OSPF", "PC/PCM", "SNA/SNAP",
	}

	var got []string
	for _, a := range canonicalAcronyms {
		for i := 1; i < len(a); i++ {
			if canonicalAcronymSet[a[:i]] {
				got = append(got, a[:i]+"/"+a)
			}
		}
	}
	slices.Sort(got)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("prefix pairs mismatch (-want +got):\n%s", diff)
	}
}
