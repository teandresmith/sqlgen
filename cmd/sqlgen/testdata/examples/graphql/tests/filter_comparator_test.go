package tests

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The JSONB and Slice comparator families over the real gqlgen server, against
// a real PostgreSQL.
//
// `filterProbes.doc` and `.tags` once advertised `StringComparator`. That input
// has no translator entry behind a JSONB or array column, so the model filter
// received nil: the server answered every `{doc: {hasKey: "region"}}` query
// with the UNFILTERED set while the client believed it had filtered.
//
// Every assertion below is a STRICT SUBSET of the seeded rows, so the old
// unfiltered answer fails each one — a test that merely checked the query
// succeeds would have passed before the fix.

// filterProbeSeed is the four-row fixture every case in this file filters
// over. Each row is chosen so that at least one operator separates it from
// every other row.
type filterProbeSeed struct {
	usEast   string // doc has env+region, tags alpha+beta
	euWest   string // doc has env+region, tags beta+gamma
	bare     string // doc has env only, tags EMPTY
	punctual string // tags carry a comma — the array-literal quoting surface
}

func seedFilterProbes(t *testing.T) filterProbeSeed {
	t.Helper()
	truncateAll(t)

	mk := func(doc map[string]any, tags []string, tagsN []string, flags []bool, weights []float64) string {
		var out struct {
			CreateFilterProbe struct {
				ID string `json:"id"`
			} `json:"createFilterProbe"`
		}
		gqlExecData(t, `
			mutation Seed($doc: JSON!, $tags: [String!]!, $tagsN: [String!], $flags: [Boolean!]!, $weights: [Float!]!, $createdAt: Time!) {
				createFilterProbe(input: {
					doc: $doc, tags: $tags, tagsN: $tagsN,
					flags: $flags, weights: $weights, createdAt: $createdAt
				}) { id }
			}
		`, map[string]any{
			"doc": doc, "tags": tags, "tagsN": tagsN,
			"flags": flags, "weights": weights, "createdAt": fixedTimestamp,
		}, &out)
		if out.CreateFilterProbe.ID == "" {
			t.Fatal("seeding filter probe: empty ID")
		}
		return out.CreateFilterProbe.ID
	}

	return filterProbeSeed{
		usEast: mk(map[string]any{"env": "prod", "region": "us"},
			[]string{"alpha", "beta"}, []string{"kept"}, []bool{true}, []float64{1.5}),
		euWest: mk(map[string]any{"env": "staging", "region": "eu"},
			[]string{"beta", "gamma"}, nil, []bool{false, true}, []float64{2.5}),
		bare: mk(map[string]any{"env": "prod"},
			[]string{}, nil, []bool{}, []float64{}),
		punctual: mk(map[string]any{"env": "prod", "note": "x"},
			[]string{"with,comma", "quote\"d"}, nil, []bool{true}, []float64{3.5}),
	}
}

// filterProbeIDs runs filterProbeList with the given filter argument and
// returns the matched ids, sorted so assertions are order-independent (the
// list takes no sort argument here).
//
// `varDefs` is the operation's variable-definition list, spelled per case
// rather than shared: GraphQL rejects a document declaring a variable it never
// uses, so one union of every case's variables would fail validation on all of
// them.
func filterProbeIDs(t *testing.T, varDefs, filter string, vars map[string]any) []string {
	t.Helper()
	var out struct {
		FilterProbeList struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"filterProbeList"`
	}
	gqlExecData(t, `
		query Probe`+varDefs+` {
			filterProbeList(filter: `+filter+`) { items { id } }
		}
	`, vars, &out)
	ids := make([]string, 0, len(out.FilterProbeList.Items))
	for _, it := range out.FilterProbeList.Items {
		ids = append(ids, it.ID)
	}
	slices.Sort(ids)
	return ids
}

func wantIDs(ids ...string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

// TestJSONBComparator_NarrowsOverHTTP exercises every JSONB operator PRD §26.4
// projects, against a real jsonb column. Each expectation is a proper subset
// of the four seeded rows.
func TestJSONBComparator_NarrowsOverHTTP(t *testing.T) {
	s := seedFilterProbes(t)

	// The unfiltered baseline. Every case below must differ from it, which is
	// precisely what a missing translator would return.
	if got, want := filterProbeIDs(t, "", `{}`, nil), wantIDs(s.usEast, s.euWest, s.bare, s.punctual); !cmp.Equal(got, want) {
		t.Fatalf("unfiltered baseline mismatch (-want +got):\n%s", cmp.Diff(want, got))
	}

	tests := []struct {
		name    string
		varDefs string
		filter  string
		vars    map[string]any
		want    []string
	}{
		{
			// `doc ? 'region'` — only the two rows carrying that key.
			name:   "hasKey",
			filter: `{doc: {hasKey: "region"}}`,
			want:   wantIDs(s.usEast, s.euWest),
		},
		{
			// `doc ?| ARRAY['region','note']`.
			name:   "hasAnyKey",
			filter: `{doc: {hasAnyKey: ["region", "note"]}}`,
			want:   wantIDs(s.usEast, s.euWest, s.punctual),
		},
		{
			// `doc ?& ARRAY['env','region']` — both keys, not either.
			name:   "hasAllKeys",
			filter: `{doc: {hasAllKeys: ["env", "region"]}}`,
			want:   wantIDs(s.usEast, s.euWest),
		},
		{
			// `doc @> '{"env":"prod"}'` — containment, not equality, so the
			// two-key and three-key prod rows both match.
			name:    "contains",
			varDefs: `($v: JSON)`,
			filter:  `{doc: {contains: $v}}`,
			vars:    map[string]any{"v": map[string]any{"env": "prod"}},
			want:    wantIDs(s.usEast, s.bare, s.punctual),
		},
		{
			// `doc <@ '{"env":"prod","region":"us","extra":1}'` — the reverse
			// direction, so only documents whose every pair is present match.
			name:    "containedBy",
			varDefs: `($v: JSON)`,
			filter:  `{doc: {containedBy: $v}}`,
			vars:    map[string]any{"v": map[string]any{"env": "prod", "region": "us", "extra": 1}},
			want:    wantIDs(s.usEast, s.bare),
		},
		{
			// `doc @? '$.region'` — a jsonpath, which is a different operator
			// from hasKey even though this example's paths agree.
			name:   "pathExists",
			filter: `{doc: {pathExists: "$.region"}}`,
			want:   wantIDs(s.usEast, s.euWest),
		},
		{
			// The recursion path: a document predicate inside `and`.
			name:    "nested in and",
			varDefs: `($v: JSON)`,
			filter:  `{and: [{doc: {hasKey: "region"}}, {doc: {contains: $v}}]}`,
			vars:    map[string]any{"v": map[string]any{"env": "prod"}},
			want:    wantIDs(s.usEast),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterProbeIDs(t, tt.varDefs, tt.filter, tt.vars)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s (-want +got):\n%s", tt.filter, diff)
			}
		})
	}
}

// TestSliceComparator_NarrowsOverHTTP exercises every array operator against a
// real `text[]` column.
//
// The `with,comma` case is the one with teeth. `sliceToArrayLiteral` renders
// the operand as a PostgreSQL array text literal, and an unquoted element
// containing a comma once split into two on the server — so the predicate
// matched a DIFFERENT set and returned silently wrong rows. That defect was
// invisible while the only arrays anything filtered on held enum members;
// `containsAny: [String!]` puts caller-supplied text on the surface, which is
// why it is pinned here rather than only in the comparator unit test.
func TestSliceComparator_NarrowsOverHTTP(t *testing.T) {
	s := seedFilterProbes(t)

	tests := []struct {
		name    string
		varDefs string
		filter  string
		vars    map[string]any
		want    []string
	}{
		{
			// `tags && '{alpha}'` — overlap.
			name:   "containsAny",
			filter: `{tags: {containsAny: ["alpha"]}}`,
			want:   wantIDs(s.usEast),
		},
		{
			// `tags @> '{beta,gamma}'` — superset, so the row holding only
			// `beta` is excluded and the assertion separates any from all.
			name:   "containsAll",
			filter: `{tags: {containsAll: ["beta", "gamma"]}}`,
			want:   wantIDs(s.euWest),
		},
		{
			// `tags <@ '{alpha,beta,gamma}'` — the empty array is a subset of
			// everything, which is why the bare row is in the answer.
			name:   "containedBy",
			filter: `{tags: {containedBy: ["alpha", "beta", "gamma"]}}`,
			want:   wantIDs(s.usEast, s.euWest, s.bare),
		},
		{
			name:   "isEmpty true",
			filter: `{tags: {isEmpty: true}}`,
			want:   wantIDs(s.bare),
		},
		{
			name:   "isEmpty false",
			filter: `{tags: {isEmpty: false}}`,
			want:   wantIDs(s.usEast, s.euWest, s.punctual),
		},
		{
			// An unquoted comma used to split this into `with` and
			// `comma`, matching nothing while erroring on nothing.
			name:   "element containing a comma",
			filter: `{tags: {containsAny: ["with,comma"]}}`,
			want:   wantIDs(s.punctual),
		},
		{
			// The other half of the same grammar: a double quote used to fail
			// outright with `malformed array literal`.
			name:    "element containing a quote",
			varDefs: `($q: String!)`,
			filter:  `{tags: {containsAny: [$q]}}`,
			vars:    map[string]any{"q": `quote"d`},
			want:    wantIDs(s.punctual),
		},
		{
			// A non-String element type, so the projection is exercised on
			// more than the identity-copy arm.
			name:   "boolean elements",
			filter: `{flags: {containsAll: [true, false]}}`,
			want:   wantIDs(s.euWest),
		},
		{
			// Float elements go through the same path with a `float64`
			// parameter rather than a string one.
			name:   "float elements",
			filter: `{weights: {containsAny: [2.5, 3.5]}}`,
			want:   wantIDs(s.euWest, s.punctual),
		},
		{
			// PRD §26.4 Rule 2 on an array column: only the nullable twin
			// declares isNull, and it reaches IS NULL / IS NOT NULL.
			name:   "nullable isNull",
			filter: `{tagsN: {isNull: true}}`,
			want:   wantIDs(s.euWest, s.bare, s.punctual),
		},
		{
			name:   "nullable isNull false",
			filter: `{tagsN: {isNull: false}}`,
			want:   wantIDs(s.usEast),
		},
		{
			// The recursion path, and the interaction between two families in
			// one filter.
			name:   "nested in and with a document predicate",
			filter: `{and: [{tags: {containsAny: ["beta"]}}, {doc: {hasKey: "region"}}]}`,
			want:   wantIDs(s.usEast, s.euWest),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterProbeIDs(t, tt.varDefs, tt.filter, tt.vars)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s (-want +got):\n%s", tt.filter, diff)
			}
		})
	}
}

// TestFilterComparator_SchemaRejectsBadOperands pins the parse-time half of
// the projection — the benefit that motivated typing the operands at all
// rather than keeping StringComparator and parsing at runtime. Every one of
// these documents once parsed cleanly and returned 200 with the unfiltered
// set.
func TestFilterComparator_SchemaRejectsBadOperands(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantMsg string
	}{
		{
			// Rule 2: `doc` is NOT NULL, so its input declares no isNull.
			name:    "isNull on a NOT NULL column",
			query:   `query { filterProbeList(filter: {doc: {isNull: true}}) { items { id } } }`,
			wantMsg: `"isNull" is not defined`,
		},
		{
			// The text operators StringComparator carried are gone: a document
			// column no longer advertises `like`.
			name:    "text operator on a document column",
			query:   `query { filterProbeList(filter: {doc: {like: "%prod%"}}) { items { id } } }`,
			wantMsg: `"like" is not defined`,
		},
		{
			// `containsAny` takes a list of the ELEMENT type; a document is
			// not one.
			name:    "document operand on an array comparator",
			query:   `query { filterProbeList(filter: {tags: {containsAny: {env: "prod"}}}) { items { id } } }`,
			wantMsg: `not defined by type "String"`,
		},
		{
			// `isEmpty` is a Boolean predicate, not an element operand.
			name:    "element list for isEmpty",
			query:   `query { filterProbeList(filter: {tags: {isEmpty: ["alpha"]}}) { items { id } } }`,
			wantMsg: "Boolean cannot represent",
		},
		{
			// Element types are checked too — `flags` operands are Boolean,
			// which is the monomorphization earning its keep.
			name:    "wrong element type",
			query:   `query { filterProbeList(filter: {flags: {containsAny: ["true"]}}) { items { id } } }`,
			wantMsg: "Boolean cannot represent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gqlExpectValidationError(t, tt.query, tt.wantMsg)
		})
	}
}

// --- the JSON family on PostgreSQL ----------------------------------------

// jsonProfileSeed is the four-profile fixture TestJSONComparator_NarrowsOverHTTP
// filters over. `profiles.settings` is `json` (not `jsonb`) and O2O with users
// through a UNIQUE FK, so each row needs its own user.
type jsonProfileSeed struct {
	darkUS string // {"theme":"dark","region":"us"}
	darkEU string // {"theme":"dark","region":"eu"}
	light  string // {"theme":"light"} — has theme, no region
	unset  string // settings IS NULL
}

func seedJSONProfiles(t *testing.T) jsonProfileSeed {
	t.Helper()
	truncateAll(t)

	mk := func(slug string, settings map[string]any) string {
		user := seedUser(t, slug+"@example.com", slug)
		var out struct {
			CreateProfile struct {
				ID string `json:"id"`
			} `json:"createProfile"`
		}
		gqlExecData(t, `
			mutation Seed($userID: UUID!, $settings: JSON, $createdAt: Time!) {
				createProfile(input: {
					userID: $userID, bio: "b", avatarURL: "a",
					settings: $settings, createdAt: $createdAt
				}) { id }
			}
		`, map[string]any{
			"userID": user.ID, "settings": settings, "createdAt": fixedTimestamp,
		}, &out)
		if out.CreateProfile.ID == "" {
			t.Fatalf("seeding profile %s: empty ID", slug)
		}
		return out.CreateProfile.ID
	}

	return jsonProfileSeed{
		darkUS: mk("json-dark-us", map[string]any{"theme": "dark", "region": "us"}),
		darkEU: mk("json-dark-eu", map[string]any{"theme": "dark", "region": "eu"}),
		light:  mk("json-light", map[string]any{"theme": "light"}),
		unset:  mk("json-unset", nil),
	}
}

// profileIDs runs profileList with the given filter argument and returns the
// matched ids, sorted so assertions are order-independent.
func profileIDs(t *testing.T, varDefs, filter string, vars map[string]any) []string {
	t.Helper()
	var out struct {
		ProfileList struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"profileList"`
	}
	gqlExecData(t, `
		query Probe`+varDefs+` {
			profileList(filter: `+filter+`) { items { id } }
		}
	`, vars, &out)
	ids := make([]string, 0, len(out.ProfileList.Items))
	for _, it := range out.ProfileList.Items {
		ids = append(ids, it.ID)
	}
	slices.Sort(ids)
	return ids
}

// TestJSONComparator_NarrowsOverHTTP exercises the JSON family against a real
// PostgreSQL `json` column — the branch of comparator.JSON.Parse the MySQL
// example cannot reach.
//
// This is the json-cast regression test, and every `contains` / `hasKey` case
// here FAILS LOUDLY without the jsonb cast rather than returning a wrong set:
// `settings @> $1` on a `json` column raises `operator does not exist: json @>
// unknown` (SQLSTATE 42883), which surfaces as a GraphQL error and trips
// gqlExecData. Without the cast there could be no PostgreSQL coverage of this
// family at all — precisely because it could not run.
//
// `hasKey` deliberately passes a BARE KEY. PostgreSQL's `?` matches a key name;
// MySQL's JSON_CONTAINS_PATH wants a `$.`-prefixed path, and the sibling MySQL
// test spells it that way. That divergence is PRD §11.2's stated limit of the
// shared operand, and these two tests are what pin both halves of it.
func TestJSONComparator_NarrowsOverHTTP(t *testing.T) {
	s := seedJSONProfiles(t)

	// The unfiltered baseline. Every case below is a proper subset of it.
	if got, want := profileIDs(t, "", `{}`, nil), wantIDs(s.darkUS, s.darkEU, s.light, s.unset); !cmp.Equal(got, want) {
		t.Fatalf("unfiltered baseline mismatch (-want +got):\n%s", cmp.Diff(want, got))
	}

	tests := []struct {
		name    string
		varDefs string
		filter  string
		vars    map[string]any
		want    []string
	}{
		{
			// `settings::jsonb @> '{"theme":"dark"}'` — containment, so the
			// two-key rows match on a one-key document.
			name:    "contains",
			varDefs: `($v: JSON)`,
			filter:  `{settings: {contains: $v}}`,
			vars:    map[string]any{"v": map[string]any{"theme": "dark"}},
			want:    wantIDs(s.darkUS, s.darkEU),
		},
		{
			// Containment is not equality in the other direction either: a
			// two-pair document narrows to the single row carrying both.
			name:    "contains two pairs",
			varDefs: `($v: JSON)`,
			filter:  `{settings: {contains: $v}}`,
			vars:    map[string]any{"v": map[string]any{"theme": "dark", "region": "us"}},
			want:    wantIDs(s.darkUS),
		},
		{
			// `settings::jsonb ? 'region'` — a BARE key, not a `$.` path.
			name:   "hasKey",
			filter: `{settings: {hasKey: "region"}}`,
			want:   wantIDs(s.darkUS, s.darkEU),
		},
		{
			// The NULL row is excluded because `NULL ? 'theme'` is NULL, not
			// false — worth pinning separately from the isNull case.
			name:   "hasKey present on every non-null row",
			filter: `{settings: {hasKey: "theme"}}`,
			want:   wantIDs(s.darkUS, s.darkEU, s.light),
		},
		{
			// A key no row carries, so the answer is empty rather than a
			// subset — the shape an accept-and-ignore field cannot produce.
			name:   "hasKey with no matches",
			filter: `{settings: {hasKey: "absent"}}`,
			want:   []string{},
		},
		{
			// `isNull` tests the column itself and takes NO cast, which is
			// why it was the one operator that already worked (PRD §11.2).
			name:   "isNull true",
			filter: `{settings: {isNull: true}}`,
			want:   wantIDs(s.unset),
		},
		{
			name:   "isNull false",
			filter: `{settings: {isNull: false}}`,
			want:   wantIDs(s.darkUS, s.darkEU, s.light),
		},
		{
			// Both document operators in one comparator — the AND the model
			// filter builds from a single struct, and a strict narrowing of
			// each operator's own answer.
			name:    "contains and hasKey together",
			varDefs: `($v: JSON)`,
			filter:  `{settings: {contains: $v, hasKey: "theme"}}`,
			vars:    map[string]any{"v": map[string]any{"region": "us"}},
			want:    wantIDs(s.darkUS),
		},
		{
			// The recursion path: a document predicate inside `and`.
			name:    "nested in and",
			varDefs: `($v: JSON)`,
			filter:  `{and: [{settings: {hasKey: "region"}}, {settings: {contains: $v}}]}`,
			vars:    map[string]any{"v": map[string]any{"theme": "dark", "region": "eu"}},
			want:    wantIDs(s.darkEU),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := profileIDs(t, tt.varDefs, tt.filter, tt.vars)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s (-want +got):\n%s", tt.filter, diff)
			}
		})
	}
}
