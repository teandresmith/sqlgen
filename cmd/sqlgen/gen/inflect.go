package gen

import (
	"strings"
	"unicode"
)

// English inflection — the singular and plural forms of a SQL name.
//
// sqlgen owns this engine rather than calling one. The library it replaced
// (gobuffalo/flect v1.0.3) reads `inflections.json` and `acronyms.json` from
// the process working directory in an `init()`, which made every generated
// struct name and file stem steerable by a stray file in the consumer's
// project: an `inflections.json` holding `{"widget":"users"}` renamed table
// `users` to struct `Widget` in `widget_gen.go`, and one holding an entry that
// collided with flect's own table panicked before `main` ran. Owning the
// tables closes both (PRD §8.5).
//
// The dictionary and suffix tables below are ported verbatim from that library
// so every name that inflected correctly before still inflects to the same
// word — the port is what makes "no golden churn" a property of the data
// rather than a claim about English. What is NOT ported is its `Ident`
// machinery: `Ident.ReplaceSuffix` trims an uppercased acronym off a lowercase
// original, the trim silently fails, and the singular is appended to the whole
// input instead of replacing its suffix ("user_ips" → "user_ipsIPS"). This
// engine splices the last word by index, where that failure cannot occur.
//
//	Ported data: github.com/gobuffalo/flect v1.0.3 (plural_rules.go)
//	The MIT License (MIT) — Copyright (c) 2019 Mark Bates
//
// Acronyms are frozen (PRD §8.5). If the last word of a name is a canonical
// acronym or a known non-plural "-s" noun, no inflection rule may touch it:
// `singularize` returns the name unchanged, because an acronym is not an
// English plural, and `pluralize` appends a marker rather than rewriting a
// stem. The plural direction cannot also freeze — `QueryName` and
// `QueryNamePlural` land in the same `extend type Query` block and
// `StructName` / `StructNamePlural` name two methods on one resolver receiver,
// so a frozen plural emits a duplicate SDL field and a duplicate Go method for
// any acronym-final table.

// nonPluralSNouns are English nouns that end in "s" without being plurals.
// The generic "-s" singular rule strips their final letter exactly as it does
// an acronym's — "lens" resolved to struct `Len` in `len_gen.go` — so they take
// the same freeze. Every entry was verified to be corrupted before it was
// added; the list is a judgment call rather than a closed set, and
// `tables.<t>.struct_name` remains the escape hatch for a noun it misses
// (PRD §8.5).
//
// A frozen word pluralizes by appending, so the classical plural of the Greek
// and Latin forms here is not produced ("emphasis" → "emphasises", not
// "emphases"). That trade is deliberate: the singular spells the struct name
// and the file stem, the plural only a constant and a query name.
//
// "corps" is deliberately absent: appending would spell "corpses", which is a
// different word's plural rather than merely a clumsy one.
//
// Kept sorted so a duplicate is visible; pinned by TestNonPluralSNouns_Sorted.
var nonPluralSNouns = []string{
	"asbestos", "atlas", "bias", "canvas", "chaos", "cosmos", "dais", "emphasis",
	"ethos", "gas", "iris", "kudos", "lens", "metropolis", "pathos", "polis",
	"praxis", "tennis", "trellis",
}

// frozenInflection is the set no inflection rule may rewrite: every canonical
// acronym plus every non-plural "-s" noun, upper-cased so the lookup folds
// case the way the acronym set already does (acronyms.go).
var frozenInflection = buildFrozenInflection()

func buildFrozenInflection() map[string]bool {
	set := make(map[string]bool, len(canonicalAcronyms)+len(nonPluralSNouns))
	for _, a := range canonicalAcronyms {
		set[a] = true
	}
	for _, n := range nonPluralSNouns {
		set[strings.ToUpper(n)] = true
	}
	return set
}

// isInflectionFrozen reports whether word is one no inflection rule may touch.
func isInflectionFrozen(word string) bool {
	return frozenInflection[strings.ToUpper(word)]
}

// splitLastWord returns s split immediately before its final word, so that
// prefix+word is s byte for byte. Only the last word inflects; the prefix is
// carried through untouched.
//
// The boundaries are the ones splitIdentForSnake already uses (acronyms.go) —
// delimiters via isSnakeDelimiter, a capital that follows a non-capital, and a
// symbol, which is dropped. Punctuation is carried into the word, which is what
// keeps "asset.primary" whole. Deliberately absent is a split inside a run of
// capitals: the acronym freeze has to see the whole trailing run to recognize
// it, and splitting "UserIPS" into "IPS" is the point.
func splitLastWord(s string) (prefix, word string) {
	// start marks where a word begins, never where one ended: the index a
	// boundary rune occupies is the only thing in hand, and its width is not
	// recoverable from the rune itself — a byte that is not valid UTF-8 ranges
	// as utf8.RuneError, one byte wide, while utf8.RuneLen(utf8.RuneError) is
	// three. Adding that width to reach the next rune walked past the end of
	// the string and panicked in the slice below.
	start, inWord := 0, false
	var prev rune
	for i, c := range s {
		switch {
		case isSnakeDelimiter(c):
			inWord = false
		case unicode.IsUpper(c) && !unicode.IsUpper(prev):
			start, inWord = i, true
		case unicode.IsLetter(c) || unicode.IsDigit(c) || unicode.IsPunct(c) || c == '`':
			if !inWord {
				start, inWord = i, true
			}
		default:
			// A symbol, a control rune, or a byte that is not valid UTF-8 —
			// all end the word and none belongs to the next one.
			inWord = false
		}
		prev = c
	}
	if !inWord {
		return s, ""
	}
	return s[:start], s[start:]
}

// appendPluralMarker spells the plural of a word no rule may rewrite by adding
// the English marker and nothing else: "es" after a sibilant, "s" otherwise.
//
// It is the fallback for the two cases where inflection cannot produce a
// distinct plural — a frozen word (an acronym is not an English plural) and an
// uncountable noun, whose plural is its singular. Both still need a plural
// identifier that differs from the singular one, because the two land in the
// same namespace (PRD §8.5).
func appendPluralMarker(s string) string {
	lower := strings.ToLower(s)
	for _, sibilant := range []string{"s", "x", "z", "ch", "sh"} {
		if strings.HasSuffix(lower, sibilant) {
			return s + "es"
		}
	}
	return s + "s"
}

// capitalizeFirst title-cases the first rune and leaves the rest alone. It
// carries the case of a dictionary word onto its replacement, so "People"
// singularizes to "Person" rather than "person".
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToTitle(r[0])
	return string(r)
}

// inflectWord is one entry of the ported dictionary. The field names match the
// source so the literal below transfers verbatim.
//
//   - alternative: a second accepted plural, read only in the plural→singular
//     direction.
//   - unidirectional: the plural must not map back to this singular, because
//     another entry owns it ("medium" → "media", but "media" → "media").
//   - uncountable: singular and plural are the same word.
//   - exact: the pair is matched as a whole word only, never as the tail of a
//     compound.
type inflectWord struct {
	singular       string
	plural         string
	alternative    string
	unidirectional bool
	uncountable    bool
	exact          bool
}

// inflectDictionary is the explicit word list — irregulars, uncountables, and
// the Latin and Greek forms no suffix rule can describe. It has priority over
// every rule below it.
var inflectDictionary = []inflectWord{
	// identicals https://en.wikipedia.org/wiki/English_plurals#Nouns_with_identical_singular_and_plural
	{singular: "aircraft", plural: "aircraft"},
	{singular: "beef", plural: "beef", alternative: "beefs"},
	{singular: "bison", plural: "bison"},
	{singular: "blues", plural: "blues", unidirectional: true},
	{singular: "chassis", plural: "chassis"},
	{singular: "deer", plural: "deer"},
	{singular: "fish", plural: "fish", alternative: "fishes"},
	{singular: "moose", plural: "moose"},
	{singular: "police", plural: "police"},
	{singular: "salmon", plural: "salmon", alternative: "salmons"},
	{singular: "series", plural: "series"},
	{singular: "sheep", plural: "sheep"},
	{singular: "shrimp", plural: "shrimp", alternative: "shrimps"},
	{singular: "species", plural: "species"},
	{singular: "swine", plural: "swine", alternative: "swines"},
	{singular: "trout", plural: "trout", alternative: "trouts"},
	{singular: "tuna", plural: "tuna", alternative: "tunas"},
	{singular: "you", plural: "you"},
	// -en https://en.wikipedia.org/wiki/English_plurals#Plurals_in_-(e)n
	{singular: "child", plural: "children"},
	{singular: "ox", plural: "oxen", exact: true},
	// apophonic https://en.wikipedia.org/wiki/English_plurals#Apophonic_plurals
	{singular: "foot", plural: "feet"},
	{singular: "goose", plural: "geese"},
	{singular: "man", plural: "men"},
	{singular: "human", plural: "humans"}, // not humen
	{singular: "louse", plural: "lice", exact: true},
	{singular: "mouse", plural: "mice"},
	{singular: "tooth", plural: "teeth"},
	{singular: "woman", plural: "women"},
	// misc https://en.wikipedia.org/wiki/English_plurals#Miscellaneous_irregular_plurals
	{singular: "die", plural: "dice", exact: true},
	{singular: "person", plural: "people"},

	// Words from French that end in -u add an x; in addition to eau to eaux rule
	{singular: "adieu", plural: "adieux", alternative: "adieus"},
	{singular: "fabliau", plural: "fabliaux"},
	{singular: "bureau", plural: "bureaus", alternative: "bureaux"}, // popular

	// Words from Greek that end in -on change -on to -a; in addition to hedron rule
	{singular: "criterion", plural: "criteria"},
	{singular: "ganglion", plural: "ganglia", alternative: "ganglions"},
	{singular: "lexicon", plural: "lexica", alternative: "lexicons"},
	{singular: "mitochondrion", plural: "mitochondria", alternative: "mitochondrions"},
	{singular: "noumenon", plural: "noumena"},
	{singular: "phenomenon", plural: "phenomena"},
	{singular: "taxon", plural: "taxa"},

	// Words from Latin that end in -um change -um to -a; in addition to some rules
	{singular: "media", plural: "media"}, // popular case: media -> media
	{singular: "medium", plural: "media", alternative: "mediums", unidirectional: true},
	{singular: "stadium", plural: "stadiums", alternative: "stadia"},
	{singular: "aquarium", plural: "aquaria", alternative: "aquariums"},
	{singular: "auditorium", plural: "auditoria", alternative: "auditoriums"},
	{singular: "symposium", plural: "symposia", alternative: "symposiums"},
	{singular: "curriculum", plural: "curriculums", alternative: "curricula"}, // ulum
	{singular: "quota", plural: "quotas"},

	// Words from Latin that end in -us change -us to -i or -era
	{singular: "alumnus", plural: "alumni", alternative: "alumnuses"}, // -i
	{singular: "bacillus", plural: "bacilli"},
	{singular: "cactus", plural: "cacti", alternative: "cactuses"},
	{singular: "coccus", plural: "cocci"},
	{singular: "focus", plural: "foci", alternative: "focuses"},
	{singular: "locus", plural: "loci", alternative: "locuses"},
	{singular: "nucleus", plural: "nuclei", alternative: "nucleuses"},
	{singular: "octopus", plural: "octupuses", alternative: "octopi"},
	{singular: "radius", plural: "radii", alternative: "radiuses"},
	{singular: "syllabus", plural: "syllabi"},
	{singular: "corpus", plural: "corpora", alternative: "corpuses"}, // -ra
	{singular: "genus", plural: "genera"},

	// Words from Latin that end in -a change -a to -ae
	{singular: "alumna", plural: "alumnae"},
	{singular: "vertebra", plural: "vertebrae"},
	{singular: "differentia", plural: "differentiae"}, // -tia
	{singular: "minutia", plural: "minutiae"},
	{singular: "vita", plural: "vitae"},   // -ita
	{singular: "larva", plural: "larvae"}, // -va
	{singular: "postcava", plural: "postcavae"},
	{singular: "praecava", plural: "praecavae"},
	{singular: "uva", plural: "uvae"},

	// Words from Latin that end in -ex change -ex to -ices
	{singular: "apex", plural: "apices", alternative: "apexes"},
	{singular: "codex", plural: "codices", alternative: "codexes"},
	{singular: "index", plural: "indices", alternative: "indexes"},
	{singular: "latex", plural: "latices", alternative: "latexes"},
	{singular: "vertex", plural: "vertices", alternative: "vertexes"},
	{singular: "vortex", plural: "vortices", alternative: "vortexes"},

	// Words from Latin that end in -ix change -ix to -ices (eg, matrix becomes matrices)
	{singular: "appendix", plural: "appendices", alternative: "appendixes"},
	{singular: "radix", plural: "radices", alternative: "radixes"},
	{singular: "helix", plural: "helices", alternative: "helixes"},

	// Words from Latin that end in -is change -is to -es
	{singular: "axis", plural: "axes", exact: true},
	{singular: "crisis", plural: "crises"},
	{singular: "ellipsis", plural: "ellipses", unidirectional: true}, // ellipse
	{singular: "genesis", plural: "geneses"},
	{singular: "oasis", plural: "oases"},
	{singular: "thesis", plural: "theses"},
	{singular: "testis", plural: "testes"},
	{singular: "base", plural: "bases"}, // popular case
	{singular: "basis", plural: "bases", unidirectional: true},

	{singular: "alias", plural: "aliases", exact: true}, // no alia, no aliasis
	{singular: "vedalia", plural: "vedalias"},           // no vedalium, no vedaliases

	// Words that end in -ch, -o, -s, -sh, -x, -z (can be conflict with the others)
	{singular: "use", plural: "uses", exact: true}, // us vs use
	{singular: "abuse", plural: "abuses"},
	{singular: "cause", plural: "causes"},
	{singular: "clause", plural: "clauses"},
	{singular: "cruse", plural: "cruses"},
	{singular: "excuse", plural: "excuses"},
	{singular: "fuse", plural: "fuses"},
	{singular: "house", plural: "houses"},
	{singular: "misuse", plural: "misuses"},
	{singular: "muse", plural: "muses"},
	{singular: "pause", plural: "pauses"},
	{singular: "ache", plural: "aches"},
	{singular: "topaz", plural: "topazes"},
	{singular: "buffalo", plural: "buffaloes", alternative: "buffalos"},
	{singular: "potato", plural: "potatoes"},
	{singular: "tomato", plural: "tomatoes"},

	// uncountables
	{singular: "equipment", uncountable: true},
	{singular: "information", uncountable: true},
	{singular: "jeans", uncountable: true},
	{singular: "money", uncountable: true},
	{singular: "news", uncountable: true},
	{singular: "rice", uncountable: true},

	// exceptions: -f to -ves, not -fe
	{singular: "dwarf", plural: "dwarfs", alternative: "dwarves"},
	{singular: "hoof", plural: "hoofs", alternative: "hooves"},
	{singular: "thief", plural: "thieves"},
	// exceptions: instead of -f(e) to -ves
	{singular: "chive", plural: "chives"},
	{singular: "hive", plural: "hives"},
	{singular: "move", plural: "moves"},

	// exceptions: instead of -y to -ies
	{singular: "movie", plural: "movies"},
	{singular: "cookie", plural: "cookies"},

	// exceptions: instead of -um to -a
	{singular: "pretorium", plural: "pretoriums"},
	{singular: "agenda", plural: "agendas"}, // instead of plural of agendum
	// exceptions: instead of -um to -a (chemical element names)

	// Words from Latin that end in -a change -a to -ae
	{singular: "formula", plural: "formulas", alternative: "formulae"}, // also -um/-a

	// exceptions: instead of -o to -oes
	{singular: "shoe", plural: "shoes"},
	{singular: "toe", plural: "toes", exact: true},
	{singular: "graffiti", plural: "graffiti"},

	// abbreviations
	{singular: "ID", plural: "IDs", exact: true},
}

// inflectSuffix is one bidirectional suffix rule.
type inflectSuffix struct {
	singular string
	plural   string
}

// inflectSuffixes are the suffix rules, in priority order — NOT alphabetical.
// The first match inflects, so reordering this list changes generated
// identifiers.
var inflectSuffixes = []inflectSuffix{
	// https://en.wiktionary.org/wiki/Appendix:English_irregular_nouns#Rules
	// Words that end in -f or -fe change -f or -fe to -ves
	{"tive", "tives"}, // exception
	{"eaf", "eaves"},
	{"oaf", "oaves"},
	{"afe", "aves"},
	{"arf", "arves"},
	{"rfe", "rves"},
	{"rf", "rves"},
	{"lf", "lves"},
	{"fe", "ves"}, // previously '[a-eg-km-z]fe' TODO: regex support

	// Words that end in -y preceded by a consonant change -y to -ies
	{"ay", "ays"},
	{"ey", "eys"},
	{"oy", "oys"},
	{"quy", "quies"},
	{"uy", "uys"},
	{"y", "ies"}, // '[^aeiou]y'

	// Words from French that end in -u add an x (eg, château becomes châteaux)
	{"eau", "eaux"}, // it seems like 'eau' is the most popular form of this rule

	// Words from Latin that end in -a change -a to -ae; before -on to -a and -um to -a
	{"bula", "bulae"},
	{"dula", "bulae"},
	{"lula", "bulae"},
	{"nula", "bulae"},
	{"vula", "bulae"},

	// Words from Greek that end in -on change -on to -a (eg, polyhedron becomes polyhedra)
	// https://en.wiktionary.org/wiki/Category:English_irregular_plurals_ending_in_"-a"
	{"hedron", "hedra"},

	// Words from Latin that end in -um change -um to -a (eg, minimum becomes minima)
	// https://en.wiktionary.org/wiki/Category:English_irregular_plurals_ending_in_"-a"
	{"ium", "ia"}, // some exceptions especially chemical element names
	{"seum", "seums"},
	{"eum", "ea"},
	{"oum", "oa"},
	{"stracum", "straca"},
	{"dum", "da"},
	{"elum", "ela"},
	{"ilum", "ila"},
	{"olum", "ola"},
	{"ulum", "ula"},
	{"llum", "lla"},
	{"ylum", "yla"},
	{"imum", "ima"},
	{"ernum", "erna"},
	{"gnum", "gna"},
	{"brum", "bra"},
	{"crum", "cra"},
	{"terum", "tera"},
	{"serum", "sera"},
	{"trum", "tra"},
	{"antum", "anta"},
	{"atum", "ata"},
	{"entum", "enta"},
	{"etum", "eta"},
	{"itum", "ita"},
	{"otum", "ota"},
	{"utum", "uta"},
	{"ctum", "cta"},
	{"ovum", "ova"},

	// Words from Latin that end in -us change -us to -i or -era
	// not easy to make a simple rule. just add them all to the dictionary

	// Words from Latin that end in -ex change -ex to -ices (eg, vortex becomes vortices)
	// Words from Latin that end in -ix change -ix to -ices (eg, matrix becomes matrices)
	//    for example, -dix, -dex, and -dice will have the same plural form so
	//    making a simple rule is not possible for them
	{"trix", "trices"}, // ignore a few words end in trice

	// Words from Latin that end in -is change -is to -es (eg, thesis becomes theses)
	// -sis and -se has the same plural -ses so making a rule is not easy too.
	{"iasis", "iases"},
	{"mesis", "meses"},
	{"kinesis", "kineses"},
	{"resis", "reses"},
	{"gnosis", "gnoses"}, // e.g. diagnosis
	{"opsis", "opses"},   // e.g. synopsis
	{"ysis", "yses"},     // e.g. analysis

	// Words that end in -ch, -o, -s, -sh, -x, -z
	{"ouse", "ouses"},
	{"lause", "lauses"},
	{"us", "uses"}, // use/uses is in the dictionary

	{"ch", "ches"},
	{"io", "ios"},
	{"sh", "shes"},
	{"ss", "sses"},
	{"ez", "ezzes"},
	{"iz", "izzes"},
	{"tz", "tzes"},
	{"zz", "zzes"},
	{"ano", "anos"},
	{"lo", "los"},
	{"to", "tos"},
	{"oo", "oos"},
	{"o", "oes"},
	{"x", "xes"},

	// for abbreviations
	{"S", "Ses"},

	// excluded rules: seems rare
	// Words from Hebrew that add -im or -ot (eg, cherub becomes cherubim)
	// - cherub (cherubs or cherubim), seraph (seraphs or seraphim)
	// Words from Greek that end in -ma change -ma to -mata
	// - The most of words end in -ma are in this category but it looks like
	//   just adding -s is more popular.
	// Words from Latin that end in -nx change -nx to -nges
	// - The most of words end in -nx are in this category but it looks like
	//   just adding -es is more popular. (sphinxes)

	// excluded rules: don't care at least for now:
	// Words that end in -ful that add an s after the -ful
	// Words that end in -s or -ese denoting a national of a particular country
	// Symbols or letters, which often add -'s
}

// inflectRule pairs a suffix with the function that rewrites a word carrying
// it. A rule whose fn is keepWord marks a word already in the target form.
type inflectRule struct {
	suffix string
	fn     func(string) string
}

// replaceSuffixFunc returns a rule function that swaps suffix for repl.
func replaceSuffixFunc(suffix, repl string) func(string) string {
	return func(s string) string {
		return s[:len(s)-len(suffix)] + repl
	}
}

// keepWord is the rule function for a word already in the target form.
func keepWord(s string) string { return s }

// inflector holds the two lookup maps and the two ordered rule lists.
type inflector struct {
	singleToPlural map[string]string
	pluralToSingle map[string]string
	pluralRules    []inflectRule
	singularRules  []inflectRule
}

// englishInflector is the single instance. It is built once from frozen tables
// and is never mutated afterwards, which is the property the library it
// replaced could not offer.
var englishInflector = newInflector()

// newInflector builds the maps and rule lists from inflectDictionary and
// inflectSuffixes.
func newInflector() *inflector {
	in := &inflector{
		singleToPlural: make(map[string]string, len(inflectDictionary)),
		pluralToSingle: make(map[string]string, len(inflectDictionary)),
	}
	in.loadDictionary()
	in.loadRules()
	return in
}

// loadDictionary fills the two whole-word maps, which outrank every rule.
//
// The first entry for a word wins. The tables are frozen and duplicate-free —
// TestInflectDictionary_NoDuplicates pins that — so this only decides what a
// future bad edit does, and losing one word beats the panic the source raised
// here, which took the whole binary down before main.
func (in *inflector) loadDictionary() {
	for _, wd := range inflectDictionary {
		plural := wd.plural
		if wd.uncountable && plural == "" {
			plural = wd.singular
		}
		if _, ok := in.singleToPlural[wd.singular]; !ok {
			in.singleToPlural[wd.singular] = plural
		}
		if wd.unidirectional {
			continue
		}
		if _, ok := in.pluralToSingle[plural]; !ok {
			in.pluralToSingle[plural] = wd.singular
		}
		if wd.alternative == "" {
			continue
		}
		if _, ok := in.pluralToSingle[wd.alternative]; !ok {
			in.pluralToSingle[wd.alternative] = wd.singular
		}
	}
}

// loadRules fills the two ordered rule lists — the suffix rules first, then
// the dictionary words as compound-word suffixes.
//
// Rules are prepended, so the earlier a suffix appears in inflectSuffixes the
// higher its priority, and the dictionary — walked last — outranks every
// suffix rule. An `exact` word is whole-word only and contributes no rule.
func (in *inflector) loadRules() {
	for i := len(inflectSuffixes) - 1; i >= 0; i-- {
		in.insertPluralRule(inflectSuffixes[i].singular, inflectSuffixes[i].plural)
		in.insertSingularRule(inflectSuffixes[i].plural, inflectSuffixes[i].singular)
	}

	for _, wd := range inflectDictionary {
		if wd.exact {
			continue
		}
		plural := wd.plural
		if wd.uncountable && plural == "" {
			plural = wd.singular
		}
		in.insertPluralRule(wd.singular, plural)
		if wd.unidirectional {
			continue
		}
		in.insertSingularRule(plural, wd.singular)
		if wd.alternative != "" {
			in.insertSingularRule(wd.alternative, wd.singular)
		}
	}
}

// insertPluralRule puts a singular→plural rewrite at the front of the plural
// rules, behind a keepWord entry for the plural form itself.
func (in *inflector) insertPluralRule(suffix, repl string) {
	in.pluralRules = append([]inflectRule{
		{suffix: repl, fn: keepWord},
		{suffix: suffix, fn: replaceSuffixFunc(suffix, repl)},
	}, in.pluralRules...)
}

// insertSingularRule puts a plural→singular rewrite at the front of the
// singular rules, behind a keepWord entry for the singular form itself.
func (in *inflector) insertSingularRule(suffix, repl string) {
	in.singularRules = append([]inflectRule{
		{suffix: repl, fn: keepWord},
		{suffix: suffix, fn: replaceSuffixFunc(suffix, repl)},
	}, in.singularRules...)
}

// singularize returns the singular form of s, inflecting only its last word.
func (in *inflector) singularize(s string) string {
	prefix, word := splitLastWord(s)
	if word == "" {
		return s
	}
	if isInflectionFrozen(word) {
		return s
	}
	if singular, ok := in.pluralToSingle[s]; ok {
		return singular
	}
	if _, ok := in.singleToPlural[s]; ok {
		return s
	}

	lower := strings.ToLower(word)
	if singular, ok := in.pluralToSingle[lower]; ok {
		if word == capitalizeFirst(word) {
			singular = capitalizeFirst(singular)
		}
		return prefix + singular
	}
	if _, ok := in.singleToPlural[lower]; ok {
		return s
	}

	for _, r := range in.singularRules {
		if strings.HasSuffix(word, r.suffix) {
			return prefix + r.fn(word)
		}
	}
	return prefix + strings.TrimSuffix(word, "s")
}

// pluralize returns the plural form of s, inflecting only its last word.
func (in *inflector) pluralize(s string) string {
	prefix, word := splitLastWord(s)
	if word == "" {
		return s
	}
	if isInflectionFrozen(word) {
		return appendPluralMarker(s)
	}
	if plural, ok := in.singleToPlural[s]; ok {
		return plural
	}
	if _, ok := in.pluralToSingle[s]; ok {
		return s
	}

	lower := strings.ToLower(word)
	if _, ok := in.pluralToSingle[lower]; ok {
		return s
	}
	if plural, ok := in.singleToPlural[lower]; ok {
		if word == capitalizeFirst(word) {
			plural = capitalizeFirst(plural)
		}
		return prefix + plural
	}

	for _, r := range in.pluralRules {
		if strings.HasSuffix(word, r.suffix) {
			return prefix + r.fn(word)
		}
	}
	if strings.HasSuffix(lower, "s") {
		return s
	}
	return s + "s"
}
