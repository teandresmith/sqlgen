package tests

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestNumericWidths is the numeric-width round-trip. It is the only place in the
// repo where a Go numeric width gqlgen does not bind natively is written and
// read back through a live gqlgen handler against a real PostgreSQL.
//
// Two failure shapes are pinned, and neither was reachable before the
// numeric_widths table existed:
//
//   - READ. gqlgen binds `Int` to int / int32 / int64 and `Float` to float64,
//     comparing basic kinds EXACTLY. A row-struct field of any other width
//     matched nothing, and the binder error was swallowed into a
//     `panic("not implemented")` field resolver — so `small` and `ratio` would
//     panic on selection rather than fail to compile. A panic surfaces here as
//     a GraphQL error, which gqlExecData fails on.
//   - WRITE. gqlgen types a generated list field as `[]int` / `[]float64`
//     against a model carrying the column's native width, which no Go
//     conversion bridges. sqlgen now dictates the field's Go type, so the
//     values below have to survive the wire as the widths they were sent as.
//
// The values are chosen to be width-revealing rather than round numbers: each
// integer sits at or near its type's boundary, so a wrong binding truncates or
// wraps visibly instead of passing by luck.
func TestNumericWidths(t *testing.T) {
	truncateAll(t)

	const (
		wantSmall  = 32767            // math.MaxInt16 — wraps to -32768 at int16+1
		wantSmallN = -32768           // math.MinInt16
		wantBig    = 9007199254740993 // 2^53+1 — survives int64, not float64
	)

	var create struct {
		CreateNumericWidth struct {
			ID      string    `json:"id"`
			Small   int       `json:"small"`
			SmallN  *int      `json:"smallN"`
			Ratio   float64   `json:"ratio"`
			RatioN  *float64  `json:"ratioN"`
			Smalls  []int     `json:"smalls"`
			Scores  []int     `json:"scores"`
			Bigs    []int64   `json:"bigs"`
			Ratios  []float64 `json:"ratios"`
			ScoresN []int     `json:"scoresN"`
		} `json:"createNumericWidth"`
	}
	gqlExecData(t, `
		mutation ($small: Int!, $smallN: Int, $ratio: Float!, $ratioN: Float,
		          $smalls: [Int!]!, $scores: [Int!]!, $bigs: [Int!]!,
		          $ratios: [Float!]!, $scoresN: [Int!], $createdAt: Time!) {
			createNumericWidth(input: {
				small: $small, smallN: $smallN, ratio: $ratio, ratioN: $ratioN,
				smalls: $smalls, scores: $scores, bigs: $bigs,
				ratios: $ratios, scoresN: $scoresN, createdAt: $createdAt
			}) { id small smallN ratio ratioN smalls scores bigs ratios scoresN }
		}
	`, map[string]any{
		"small":     wantSmall,
		"smallN":    wantSmallN,
		"ratio":     0.5, // exactly representable in float32, so no precision noise
		"ratioN":    -0.25,
		"smalls":    []int{-32768, 0, 32767},
		"scores":    []int{-2147483648, 7, 2147483647},
		"bigs":      []int64{wantBig, -1},
		"ratios":    []float64{0.5, -1.5},
		"scoresN":   []int{1, 2, 3},
		"createdAt": "2026-01-02T03:04:05Z",
	}, &create)

	got := create.CreateNumericWidth
	if got.Small != wantSmall {
		t.Errorf("small = %d, want %d", got.Small, wantSmall)
	}
	if got.SmallN == nil || *got.SmallN != wantSmallN {
		t.Errorf("smallN = %v, want %d", got.SmallN, wantSmallN)
	}
	if got.Ratio != 0.5 {
		t.Errorf("ratio = %v, want 0.5", got.Ratio)
	}
	if got.RatioN == nil || *got.RatioN != -0.25 {
		t.Errorf("ratioN = %v, want -0.25", got.RatioN)
	}
	if diff := cmp.Diff([]int{-32768, 0, 32767}, got.Smalls); diff != "" {
		t.Errorf("smalls mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]int{-2147483648, 7, 2147483647}, got.Scores); diff != "" {
		t.Errorf("scores mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]int64{wantBig, -1}, got.Bigs); diff != "" {
		t.Errorf("bigs mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]float64{0.5, -1.5}, got.Ratios); diff != "" {
		t.Errorf("ratios mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]int{1, 2, 3}, got.ScoresN); diff != "" {
		t.Errorf("scoresN mismatch (-want +got):\n%s", diff)
	}

	// Read back through a separate query so the MARSHAL side runs on a value
	// loaded from PostgreSQL rather than one carried over from the mutation —
	// the read path is the half that produced the panic resolver.
	var read struct {
		NumericWidth struct {
			Small  int       `json:"small"`
			Ratio  float64   `json:"ratio"`
			Smalls []int     `json:"smalls"`
			Bigs   []int64   `json:"bigs"`
			Ratios []float64 `json:"ratios"`
		} `json:"numericWidth"`
	}
	gqlExecData(t, `
		query ($id: UUID!) { numericWidth(id: $id) { small ratio smalls bigs ratios } }
	`, map[string]any{"id": got.ID}, &read)

	if read.NumericWidth.Small != wantSmall {
		t.Errorf("re-read small = %d, want %d", read.NumericWidth.Small, wantSmall)
	}
	if read.NumericWidth.Ratio != 0.5 {
		t.Errorf("re-read ratio = %v, want 0.5", read.NumericWidth.Ratio)
	}
	if diff := cmp.Diff([]int{-32768, 0, 32767}, read.NumericWidth.Smalls); diff != "" {
		t.Errorf("re-read smalls mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]int64{wantBig, -1}, read.NumericWidth.Bigs); diff != "" {
		t.Errorf("re-read bigs mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]float64{0.5, -1.5}, read.NumericWidth.Ratios); diff != "" {
		t.Errorf("re-read ratios mismatch (-want +got):\n%s", diff)
	}
}

// TestNumericWidths_OverflowIsRejected pins the one behavioural gain of
// anchoring on gqlgen's own bundled marshalers rather than emitting our own:
// graphql.UnmarshalInt16 carries a NumberOverflowError, so a value outside the
// column's domain is refused at the boundary. The equivalent SCALAR path still
// goes through an `int16(...)` cast, which wraps silently — that
// asymmetry is real and is why the array element is the one asserted here.
func TestNumericWidths_OverflowIsRejected(t *testing.T) {
	truncateAll(t)

	resp := gqlExec(t, `
		mutation ($smalls: [Int!]!, $createdAt: Time!) {
			createNumericWidth(input: {
				small: 1, ratio: 1.0, smalls: $smalls,
				scores: [], bigs: [], ratios: [], createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{
		"smalls":    []int{32768}, // int16 max + 1
		"createdAt": "2026-01-02T03:04:05Z",
	}, nil)

	if len(resp.Errors) == 0 {
		t.Fatal("expected an error for an int16-overflowing array element, got none")
	}
	// Assert on the message, not merely on "something failed" — every other way
	// this mutation could break also produces an error, and a bare length check
	// would keep passing through all of them. `graphql.UnmarshalInt16` reports
	// via NumberOverflowError, whose text names the value and the bit size.
	joined := strings.ToLower(fmt.Sprint(resp.Errors))
	if !strings.Contains(joined, "32768") || !strings.Contains(joined, "overflow") {
		t.Errorf("errors = %+v, want an overflow error naming the out-of-range value 32768", resp.Errors)
	}
	// And the row must not exist: a rejected element means a rejected mutation.
	var count struct {
		NumericWidthList struct {
			TotalCount int `json:"totalCount"`
		} `json:"numericWidthList"`
	}
	gqlExecData(t, `query { numericWidthList { totalCount } }`, nil, &count)
	if count.NumericWidthList.TotalCount != 0 {
		t.Errorf("totalCount = %d, want 0 — the overflowing mutation must not have written a row",
			count.NumericWidthList.TotalCount)
	}
}
