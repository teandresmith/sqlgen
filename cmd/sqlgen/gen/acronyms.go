package gen

import (
	"slices"
	"strings"
	"unicode"

	"github.com/ettle/strcase"
)

// canonicalAcronyms is the single acronym set the generator recognizes, per
// PRD §8.5 "Acronym Detection". It is deliberately sqlgen-owned rather than
// inherited from a casing library's built-in defaults, and equally
// deliberately not shared with gqlgen: the Go names gqlgen uses for the types
// it generates are dictated field by field (APIGoFieldOverrides →
// `models.<T>.fields.<f>.fieldName`, §26.5.6 "Go field naming") rather than
// inferred from a list both engines consult. Sharing a list was tried and is
// insufficient — two engines can agree on every acronym and still disagree,
// because camelization is lossy at a digit boundary: "line2_id" emits the
// GraphQL field "line2ID", which gives gqlgen's word walker no seam before the
// acronym.
//
// The set is frozen as the union of three sources, deduplicated:
//
//   - gobuffalo/flect v1.0.3's 144 base acronyms — sqlgen's historical
//     behavior, preserved so no column that uppercases today stops doing so.
//   - gqlgen v0.17.90's 47 templates.CommonInitialisms — adopted for their own
//     sake: ASCII, CSV, TCP, TLS, XML, GUID, VM and friends are worth
//     uppercasing regardless of who else recognizes them.
//   - sqlgen's own additions: SSL, TTL, PK, FK, UI, IO, EOF, OS, RAM
//     (historical), plus GPU and SKU (documented as expected elsewhere in the
//     PRD but absent from both upstream lists).
//
// flect's five mixed-case entries (Mbps, MoCA, WiFi, gbps, kbps) are excluded:
// identCaser matches a whole word case-folded to upper, so a mixed-case entry
// could never fire — "wifi_ssid" resolves to WifiSSID (PRD §8.5). flect's own
// lookup has the same shape (baseAcronyms[strings.ToUpper(word)]).
//
// UID and SLA are excluded. Both would extend an entry already in the set —
// UI/UID and SLA/SLARP — joining the ten prefix pairs the set already carries
// (DSL/DSLAM, HTTP/HTTPS, ID/IDF, ID/IDS, IP/IPS, NRZ/NRZI, OS/OSI, OS/OSPF,
// PC/PCM, SNA/SNAP). A prefix pair is unreadable in the inverse direction:
// "OSIDValue" is the PascalCase form of both "os_id_value" and "osi_dvalue",
// and no matching policy can tell them apart. The inverse is gone from
// every path that has a SQL name to read instead (toSnakeName below), which
// leaves only the `struct_name` config override exposed — so the exclusion is
// now a judgment call about that one path rather than a hard constraint.
// Re-admitting either is a PRD change like any other set edit.
//
// No entry is excluded on gqlgen's account. gqlgen never consults this set; it
// receives whatever spelling the set produces, field by field. Membership is
// therefore decided purely by sqlgen-side consequences — the caser fold and
// the prefix-pair reading described above.
//
// Adding or removing an entry is a PRD change, not an implementation choice —
// an entry still moves generated identifiers on both sides of the gqlgen
// boundary at once, since the spelling produced here is the one dictated
// across it. Kept sorted and uppercase so duplicates and prefix pairs (ID/IDS,
// OS/OSI) sit adjacent where review can see them; both shapes are pinned by
// TestCanonicalAcronyms_UppercaseAndSorted, and the ten pairs themselves by
// TestCanonicalAcronyms_PrefixPairs.
var canonicalAcronyms = []string{
	"ACK", "ACL", "ADSL", "AES", "ANSI", "API", "ARP", "ASCII",
	"ATM", "AWS", "BGP", "BSS", "CCITT", "CHAP", "CIDR", "CIR",
	"CLI", "CMOS", "CPE", "CPU", "CRC", "CRT", "CSMA", "CSS",
	"CSV", "DCE", "DEC", "DES", "DHCP", "DMI", "DNS", "DRAM",
	"DSL", "DSLAM", "DTE", "EHA", "EIA", "EIGRP", "EOF", "ESS",
	"FCC", "FCS", "FDDI", "FK", "FTP", "GBIC", "GCP", "GEPOF",
	"GPU", "GUID", "HDLC", "HTML", "HTTP", "HTTPS", "IANA", "ICMP",
	"ID", "IDF", "IDS", "IEEE", "IETF", "IMAP", "IO", "IP",
	"IPS", "ISDN", "ISP", "JSON", "JWT", "KVK", "LACP", "LAN",
	"LAPB", "LAPF", "LHS", "LLC", "MAC", "MC", "MDF", "MIB",
	"MPLS", "MTU", "NAC", "NAT", "NBMA", "NIC", "NRZ", "NRZI",
	"NVRAM", "OK", "OS", "OSI", "OSPF", "OUI", "PAP", "PAT",
	"PC", "PCM", "PDF", "PDU", "PGP", "PIM", "PK", "POP3",
	"POTS", "PPP", "PPTP", "PTT", "PVST", "QPS", "QR", "RAM",
	"RARP", "RCP", "RFC", "RHS", "RIP", "RLL", "ROM", "RPC",
	"RSTP", "RTP", "SDLC", "SFD", "SFP", "SKU", "SLARP", "SLIP",
	"SMTP", "SNA", "SNAP", "SNMP", "SOF", "SQL", "SRAM", "SSH",
	"SSID", "SSL", "STP", "SVG", "SYN", "TCP", "TDM", "TFTP",
	"TIA", "TLS", "TOFU", "TTL", "UDP", "UI", "URI", "URL",
	"USB", "UTF8", "UTP", "UUID", "VC", "VLAN", "VLSM", "VM",
	"VPN", "W3C", "WAN", "WEP", "WPA", "WWW", "XML", "XMPP",
	"XSRF", "XSS",
}

// identCaser produces sqlgen's Go identifier spellings (PascalCase,
// camelCase). Its split rules descend from gqlgen's golint-derived word
// walker: break on delimiters, on case transitions, at acronym boundaries, and
// before a digit. The digit rule is why an acronym containing a digit never
// matches — "utf8_body" resolves to "Utf8Body", not "UTF8Body", because "utf"
// and "8" are separate words.
//
// The rules are frozen, but as sqlgen's own rather than as a parity
// requirement: the split decides the published GraphQL field name ("userID"
// and "userId" are different fields on the wire) and every generated Go
// identifier. Parity is no longer what holds them in place — gqlgen is told
// the Go names field by field (§26.5.6 "Go field naming"), and where it still
// binds by a name it derives itself, on a bound row type, it compares
// case-insensitively (equalFieldName = EqualFold after stripping underscores,
// codegen/util.go), which a split difference cannot break (PRD §8.5 "Word
// splitting").
//
// goInitialisms is false: the canonical set is passed as the complete override
// map, so golint's list contributes nothing beyond what §8.5 already folds in.
var identCaser = strcase.NewCaser(false, canonicalAcronymSet, strcase.NewSplitFn(
	casingDelimiters,
	strcase.SplitCase,
	strcase.SplitAcronym,
	strcase.SplitBeforeNumber,
))

// casingDelimiters are the runes that separate words for identifier casing.
// Deliberately excludes '.': see normalizeForCasing.
var casingDelimiters = []rune{'_', '-', ' '}

// normalizeForCasing rewrites a raw schema name into the shape identCaser
// expects, reproducing flect's two distinct treatments of punctuation.
//
// This matters for schema-declared enum values, which reach toPascalCase as
// raw SQL literals and can carry a dot: "asset.primary" must resolve to
// "Assetprimary", giving the GraphQL enum identifier ASSETPRIMARY (PRD §13.7,
// §26.5.2). That identifier is the consumer-visible wire format — the value a
// GraphQL client sends and receives — so it is frozen. Treating '.' as a
// delimiter instead would silently re-spell it as ASSET_PRIMARY and break
// every existing query; leaving the rune in place would emit it into the Go
// constant name (`...EnumAsset.primary`), which does not compile. Filtering is
// the only option that does neither.
//
// Every other non-alphanumeric rune IS a word boundary in flect — "read/write"
// pascalizes to "ReadWrite", not "Readwrite" — so those are rewritten to '_'
// rather than dropped. Getting this wrong is the same class of mistake as the
// dot: it silently re-spells the GraphQL enum identifier a client sends
// (READ_WRITE → READWRITE).
//
// Hyphen, underscore and space are already delimiters and pass through, so
// "asset-primary" continues to split into ASSET_PRIMARY as it does today.
func normalizeForCasing(s string) string {
	if !strings.ContainsFunc(s, needsNormalizing) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case !needsNormalizing(r):
			b.WriteRune(r)
		case r == '.':
			// Filtered without splitting — see above.
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// needsNormalizing reports whether r is rewritten by normalizeForCasing.
func needsNormalizing(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return false
	}
	return !slices.Contains(casingDelimiters, r)
}

// canonicalAcronymSet is canonicalAcronyms as the set form strcase expects,
// built once and shared by identCaser and the inverse splitter so the two
// directions cannot consult different lists.
var canonicalAcronymSet = acronymSet()

// acronymSet returns canonicalAcronyms as the set form strcase expects.
func acronymSet() map[string]bool {
	set := make(map[string]bool, len(canonicalAcronyms))
	for _, a := range canonicalAcronyms {
		set[a] = true
	}
	return set
}

// snakeCaser produces the snake_case spelling of a SQL name. It is identCaser
// with two deliberate differences, and it reads the SQL name rather than the
// PascalCase form, so the word boundaries are still observable and nothing has
// to be recovered:
//
//   - No acronym set. The words are lowercased, so uppercasing them first and
//     lowercasing them again would only be a way to get "api_url" wrong.
//   - No SplitBeforeNumber. The digit rule exists to keep an acronym from
//     matching across a digit ("utf8_body" → Utf8Body, PRD §8.5); as a snake
//     boundary it would emit "utf_8_body" and "line_2_id", which are not the
//     names the SQL side used. flect never split at a digit either, so
//     dropping the rule is what keeps this byte-identical to the previous
//     spelling for every name that was already correct.
//
// The delimiter list and the two case rules are shared with identCaser, which
// is what makes toSnakeName the exact parallel of toPascalCase rather than an
// independent guess at the same answer (PRD §8.5 "One canonical list, one
// splitter").
var snakeCaser = strcase.NewCaser(false, nil, strcase.NewSplitFn(
	casingDelimiters,
	strcase.SplitCase,
	strcase.SplitAcronym,
))

// isSnakeDelimiter reports whether c ends a word in the inverse direction. The
// set is wider than casingDelimiters by ':' and '/' because those were
// delimiters in the flect splitter this replaced, and toSnakeCase's remaining
// caller passes through a user-supplied string (PRD §8.5).
func isSnakeDelimiter(c rune) bool {
	return c == '_' || c == '-' || c == ':' || c == '/' || unicode.IsSpace(c)
}

// splitIdentForSnake splits a PascalCase or camelCase Go identifier into the
// words toSnakeCase joins with underscores.
//
// A run of capitals carries no observable word boundary — "APIURL" has no seam
// — so the split is list-driven: at each position it takes the LONGEST
// canonical acronym that fits. The previous implementation (flect.Underscore)
// took the shortest, which silently misspelled every identifier containing an
// acronym that extends another: "OSILayer" underscored to "os_ilayer" and
// "HTTPSURL" to "http_surl".
//
// Longest-match is not an inverse either, and no policy is: "OSIDValue" is the
// PascalCase form of both "os_id_value" and "osi_dvalue", and "AVPNB" of both
// "a_vpn_b" and "avpnb". It is the best available reading of a string that has
// lost its seams — which is why this function is also kept off every
// path that still has the SQL name to read instead. What is left is the
// `struct_name` / `views.<v>.struct_name` config override, where the user hands
// the generator a Go identifier and there is no SQL name to consult.
func splitIdentForSnake(s string) []string {
	r := []rune(strings.TrimSpace(s))
	parts := make([]string, 0, 8)
	word := make([]rune, 0, len(r))

	flush := func() {
		if len(word) > 0 {
			parts = append(parts, string(word))
			word = word[:0]
		}
	}

	var prev rune
	for i, c := range r {
		switch {
		case isSnakeDelimiter(c):
			flush()
		case unicode.IsUpper(c) && !unicode.IsUpper(prev):
			flush()
			word = append(word, c)
		case unicode.IsUpper(c) && endsLongestAcronym(r, i-len(word), i):
			flush()
			word = append(word, c)
		case unicode.IsLetter(c) || unicode.IsDigit(c) || unicode.IsPunct(c) || c == '`':
			word = append(word, c)
		default:
			// Symbols split and are dropped; punctuation above is carried into
			// the word and filtered when the parts are joined, which is what
			// keeps "asset.primary" one word (PRD §8.5).
			flush()
		}
		prev = c
	}
	flush()

	return parts
}

// endsLongestAcronym reports whether r[start:i] is the longest canonical
// acronym that fits in the run of capitals beginning at start, i.e. whether the
// word ends at i.
//
// The run is capped one rune short when it is followed by a lowercase letter:
// that final capital heads the next word, not this acronym. Without the cap
// "IDSlot" would read as "IDS" + "lot" rather than "ID" + "Slot".
func endsLongestAcronym(r []rune, start, i int) bool {
	if !canonicalAcronymSet[strings.ToUpper(string(r[start:i]))] {
		return false
	}

	k := start
	for k < len(r) && unicode.IsUpper(r[k]) {
		k++
	}
	end := k
	if k < len(r) && unicode.IsLower(r[k]) {
		end = k - 1
	}

	for j := i + 1; j <= end; j++ {
		if canonicalAcronymSet[strings.ToUpper(string(r[start:j]))] {
			return false
		}
	}

	return true
}
