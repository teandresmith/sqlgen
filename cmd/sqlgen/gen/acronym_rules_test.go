package gen

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// arrPRDCanonicalSet is the canonical acronym block from PRD §8.5 "Acronym
// Detection", copied verbatim.
const arrPRDCanonicalSet = `
ACK ACL ADSL AES ANSI API ARP ASCII ATM AWS BGP BSS CCITT CHAP CIDR CIR CLI CMOS CPE
CPU CRC CRT CSMA CSS CSV DCE DEC DES DHCP DMI DNS DRAM DSL DSLAM DTE EHA EIA EIGRP EOF
ESS FCC FCS FDDI FK FTP GBIC GCP GEPOF GPU GUID HDLC HTML HTTP HTTPS IANA ICMP ID IDF
IDS IEEE IETF IMAP IO IP IPS ISDN ISP JSON JWT KVK LACP LAN LAPB LAPF LHS LLC MAC MC
MDF MIB MPLS MTU NAC NAT NBMA NIC NRZ NRZI NVRAM OK OS OSI OSPF OUI PAP PAT PC PCM PDF
PDU PGP PIM PK POP3 POTS PPP PPTP PTT PVST QPS QR RAM RARP RCP RFC RHS RIP RLL ROM RPC
RSTP RTP SDLC SFD SFP SKU SLARP SLIP SMTP SNA SNAP SNMP SOF SQL SRAM SSH SSID SSL STP
SVG SYN TCP TDM TFTP TIA TLS TOFU TTL UDP UI URI URL USB UTF8 UTP UUID VC VLAN VLSM VM
VPN W3C WAN WEP WPA WWW XML XMPP XSRF XSS
`

// TestCanonicalAcronyms_ExactSet pins PRD §8.5: the canonical set is exactly
// the 170 entries the PRD lists — membership and count.
func TestCanonicalAcronyms_ExactSet(t *testing.T) {
	want := strings.Fields(arrPRDCanonicalSet)
	slices.Sort(want)
	got := slices.Clone(canonicalAcronyms)
	slices.Sort(got)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("canonicalAcronyms differs from the PRD §8.5 canonical set (-prd +code):\n%s", diff)
	}
	if len(got) != 170 {
		t.Errorf("len(canonicalAcronyms) = %d, want 170", len(got))
	}
}

// TestCanonicalAcronyms_ExcludedEntries pins PRD §8.5: the five mixed-case
// flect entries and SLA are not in the set, so a word spelled like them is
// cased as an ordinary word.
func TestCanonicalAcronyms_ExcludedEntries(t *testing.T) {
	tests := []struct {
		name   string
		column string
		want   string
	}{
		{name: "WiFi is excluded", column: "wifi_ssid", want: "WifiSSID"},
		{name: "Mbps is excluded", column: "mbps_rate", want: "MbpsRate"},
		{name: "MoCA is excluded", column: "moca_link", want: "MocaLink"},
		{name: "gbps is excluded", column: "gbps_cap", want: "GbpsCap"},
		{name: "kbps is excluded", column: "kbps_cap", want: "KbpsCap"},
		{name: "SLA is excluded", column: "sla_days", want: "SlaDays"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FieldName(tt.column); got != tt.want {
				t.Errorf("FieldName(%q) = %q, want %q", tt.column, got, tt.want)
			}
		})
	}
}

// TestStructName_NoIDSException pins PRD §8.5 "Singular and plural": there is
// no exception for IDS — `user_ids` is not read as the plural of `user_id`.
func TestStructName_NoIDSException(t *testing.T) {
	if got := StructName("user_ids", "", nil); got != "UserIDS" {
		t.Errorf("StructName(%q) = %q, want %q", "user_ids", got, "UserIDS")
	}
	if got := SnakeName("user_ids", "", nil); got != "user_ids" {
		t.Errorf("SnakeName(%q) = %q, want %q", "user_ids", got, "user_ids")
	}
}

// TestWordSplitting_Delimiters pins PRD §8.5 "Word splitting": words break on
// `_`, `-` and space; every other non-alphanumeric rune except `.` is a word
// boundary; `.` is filtered without splitting. Both the Go identifier and the
// GraphQL enum identifier of a declared value follow the same split.
func TestWordSplitting_Delimiters(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantPasc  string
		wantIdent string
	}{
		{name: "underscore splits", input: "read_write", wantPasc: "ReadWrite", wantIdent: "READ_WRITE"},
		{name: "hyphen splits", input: "read-write", wantPasc: "ReadWrite", wantIdent: "READ_WRITE"},
		{name: "space splits", input: "read write", wantPasc: "ReadWrite", wantIdent: "READ_WRITE"},
		{name: "slash splits", input: "read/write", wantPasc: "ReadWrite", wantIdent: "READ_WRITE"},
		{name: "dot is filtered without splitting", input: "asset.primary", wantPasc: "Assetprimary", wantIdent: "ASSETPRIMARY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toPascalCase(tt.input); got != tt.wantPasc {
				t.Errorf("toPascalCase(%q) = %q, want %q", tt.input, got, tt.wantPasc)
			}
			if got := gqlEnumIdent(tt.input); got != tt.wantIdent {
				t.Errorf("gqlEnumIdent(%q) = %q, want %q", tt.input, got, tt.wantIdent)
			}
		})
	}
}

// arrSplitterIdents are the package-level declarations that make up the
// acronym set and the word-splitting rule (PRD §8.5 "One canonical list, one
// splitter").
var arrSplitterIdents = []string{
	"canonicalAcronyms",
	"canonicalAcronymSet",
	"identCaser",
	"snakeCaser",
	"casingDelimiters",
	"normalizeForCasing",
	"splitIdentForSnake",
}

// arrCasingEntryPoints are the four naming.go functions every naming site
// reaches the splitter through (PRD §8.5).
var arrCasingEntryPoints = []string{"toPascalCase", "toCamelCase", "toSnakeName", "toSnakeCase"}

// arrParsePackage parses every non-test Go file of package gen.
func arrParsePackage(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing package files: %v", err)
	}
	fset := token.NewFileSet()
	files := make(map[string]*ast.File)
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", p, err)
		}
		files[p] = f
	}
	if len(files) == 0 {
		t.Fatal("no non-test Go files found in the package directory")
	}
	return fset, files
}

// TestAcronymRules_DefinedOnceInAcronymsGo pins PRD §8.5 "One canonical list,
// one splitter": the acronym set and the word-splitting rule are defined once,
// in acronyms.go, and no other file brings in a casing library of its own.
func TestAcronymRules_DefinedOnceInAcronymsGo(t *testing.T) {
	_, files := arrParsePackage(t)

	t.Run("the set and the splitter are declared in acronyms.go", func(t *testing.T) {
		declaredIn := map[string][]string{}
		for path, f := range files {
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil {
						declaredIn[d.Name.Name] = append(declaredIn[d.Name.Name], path)
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						if vs, ok := spec.(*ast.ValueSpec); ok {
							for _, n := range vs.Names {
								declaredIn[n.Name] = append(declaredIn[n.Name], path)
							}
						}
					}
				}
			}
		}
		for _, name := range arrSplitterIdents {
			if got := declaredIn[name]; !slices.Equal(got, []string{"acronyms.go"}) {
				t.Errorf("%s declared in %v, want only acronyms.go", name, got)
			}
		}
	})

	t.Run("only acronyms.go imports a casing library", func(t *testing.T) {
		for path, f := range files {
			for _, imp := range f.Imports {
				ip, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatalf("%s: bad import path %s", path, imp.Path.Value)
				}
				isCasingLib := strings.Contains(ip, "strcase") || strings.Contains(ip, "flect") ||
					strings.Contains(ip, "inflect") || strings.Contains(ip, "camelcase")
				if isCasingLib && path != "acronyms.go" {
					t.Errorf("%s imports casing library %q; the splitter is defined once, in acronyms.go", path, ip)
				}
			}
		}
	})

	t.Run("only acronyms.go constructs a splitter", func(t *testing.T) {
		for path, f := range files {
			if path == "acronyms.go" {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "strcase" {
					t.Errorf("%s references strcase.%s outside acronyms.go", path, sel.Sel.Name)
				}
				return true
			})
		}
	})
}

// TestAcronymRules_ReachedThroughNamingHelpers pins PRD §8.5: every naming site
// reaches the splitter through toPascalCase / toCamelCase / toSnakeName /
// toSnakeCase in naming.go. Outside acronyms.go, the splitter declarations are
// referenced only from the bodies of those four functions.
//
// The inflection engine (inflect.go) reads canonicalAcronyms, and only that
// declaration: PRD §8.5 freezes a final canonical acronym against
// singularizing, which needs the set's membership but not its casing.
func TestAcronymRules_ReachedThroughNamingHelpers(t *testing.T) {
	fset, files := arrParsePackage(t)
	guarded := map[string]bool{}
	for _, name := range arrSplitterIdents {
		guarded[name] = true
	}

	for path, f := range files {
		if path == "acronyms.go" {
			continue
		}
		for _, decl := range f.Decls {
			owner := ""
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil {
				owner = fd.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || !guarded[id.Name] {
					return true
				}
				switch {
				case path == "naming.go" && slices.Contains(arrCasingEntryPoints, owner):
				case path == "inflect.go" && id.Name == "canonicalAcronyms":
				default:
					t.Errorf("%s: %s referenced from %q; naming sites must reach it through %v in naming.go",
						fset.Position(id.Pos()), id.Name, owner, arrCasingEntryPoints)
				}
				return true
			})
		}
	}
}

// arrCasingChildEnv marks the re-executed test binary that reports spellings
// from a working directory carrying casing-library config files.
const arrCasingChildEnv = "SQLGEN_ARR_CASING_CHILD"

// arrCasingProbe returns the spellings whose stability the process-global
// test checks: struct names, file stems and field names, each for an input an
// acronyms.json or inflections.json in the working directory would re-spell.
func arrCasingProbe() map[string]string {
	out := map[string]string{}
	for _, in := range []string{"sla_days", "uid_value", "wifi_ssid", "user_ids", "lens", "people", "api_url"} {
		out["StructName/"+in] = StructName(in, "", nil)
		out["SnakeName/"+in] = SnakeName(in, "", nil)
		out["toPascalCase/"+in] = toPascalCase(in)
		out["toCamelCase/"+in] = toCamelCase(in)
		out["toSnakeName/"+in] = toSnakeName(in)
		out["StructNamePlural/"+in] = StructNamePlural(StructName(in, "", nil))
	}
	out["toSnakeCase/SLADays"] = toSnakeCase("SLADays")
	return out
}

// arrWriteCasingConfigFiles writes the files a casing or inflection library
// would load from the working directory.
func arrWriteCasingConfigFiles(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"acronyms.json":    `["SLA", "UID", "WIFI", "Sla", "Uid"]`,
		"inflections.json": `{"user_ids": "user_id", "lens": "len", "people": "peoples", "day": "dayz"}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
}

// TestCasing_NoProcessGlobalState pins PRD §8.5: casing and inflection do not
// depend on process-global state. An `acronyms.json` / `inflections.json` in
// the working directory leaves every spelling unchanged — both when the
// directory changes mid-process and when the binary starts there, which is
// where a library loading those files in init() would read them.
func TestCasing_NoProcessGlobalState(t *testing.T) {
	if os.Getenv(arrCasingChildEnv) == "1" {
		b, err := json.Marshal(arrCasingProbe())
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(os.Getenv(arrCasingChildEnv+"_OUT"), b, 0o600); err != nil { //nolint:gosec // test helper: the parent test names the output path
			t.Fatalf("writing child output: %v", err)
		}
		return
	}

	want := arrCasingProbe()
	dir := t.TempDir()
	arrWriteCasingConfigFiles(t, dir)

	t.Run("unchanged after moving into a directory with casing config files", func(t *testing.T) {
		t.Chdir(dir)
		if diff := cmp.Diff(want, arrCasingProbe()); diff != "" {
			t.Errorf("spellings changed with acronyms.json / inflections.json in the working directory (-before +after):\n%s", diff)
		}
	})

	t.Run("unchanged in a process started in a directory with casing config files", func(t *testing.T) {
		exe, err := os.Executable()
		if err != nil {
			t.Fatalf("os.Executable: %v", err)
		}
		outPath := filepath.Join(t.TempDir(), "child.json")
		cmd := exec.Command(exe, "-test.run=^TestCasing_NoProcessGlobalState$", "-test.count=1") //nolint:gosec // test helper: re-runs this test binary
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), arrCasingChildEnv+"=1", arrCasingChildEnv+"_OUT="+outPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child process failed: %v\n%s", err, out)
		}
		b, err := os.ReadFile(outPath) //nolint:gosec // test helper: path is the child test's output in a temp dir
		if err != nil {
			t.Fatalf("reading child output: %v", err)
		}
		var got map[string]string
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("decoding child output: %v", err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("spellings differ in a process started beside acronyms.json / inflections.json (-parent +child):\n%s", diff)
		}
	})
}
