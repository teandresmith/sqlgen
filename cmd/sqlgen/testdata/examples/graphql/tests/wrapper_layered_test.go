package tests

// TestWrapperLayered_PreservesConsumerEntries pins PRD §26.5.6's
// "consumer owns gqlgen.yml end-to-end" guarantee: the wrapper's `Merge`
// step adds sqlgen-required entries (schema glob, per-table model bindings)
// to an *in-memory* config it passes to gqlgen via a temp file, and never
// mutates the on-disk file regardless of how layered the consumer's
// configuration is.
//
// The regression this guards: a future refactor that "helpfully" rewrites
// the on-disk gqlgen.yml — collapsing custom directives, dropping
// hand-bound model entries, or otherwise clobbering consumer-owned state.
// PRD §26.5.6: "the on-disk gqlgen.yml is never modified".
//
// Mechanism: stage the example, pre-seed gqlgen.yml with a custom
// `directives:` block + a hand-bound entry under `models:`, replace
// `gqlgen_bin` with a tiny shell stub that captures `--config <path>` to a
// known capture file then exits 0 (so we read the *merged* config gqlgen
// would have seen without paying the real gqlgen subprocess cost), then
// run `sqlgen graphql gen` and assert:
//
//	1. gqlgen.yml byte-equal pre/post (sha1).
//	2. The captured merged config carries BOTH the consumer's pre-seeded
//	   entries (directive name, hand-bound type) AND the sqlgen-emitted
//	   entries (managed-table model bindings, schema glob).

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWrapperLayered_PreservesConsumerEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wrapper layered test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("shell-based gqlgen stub is POSIX-only")
	}

	stage := stageGraphQLExample(t)
	gqlgenYML := filepath.Join(stage, "gqlgen.yml")

	// Pre-seed the consumer's gqlgen.yml with a custom `directives:` block
	// and a hand-bound entry under `models:`. The directive uses a
	// distinctive name (`@consumerOwnedDirective`) so the captured-config
	// assertion can grep for it; the hand-bound model entry binds a
	// hypothetical scalar `ConsumerOwnedScalar` to a fake Go type. Neither
	// reflects anything in the schema — the test only verifies these
	// consumer-authored entries survive the merge round trip.
	consumerYML := string(readFile(t, gqlgenYML)) + "\n" + consumerLayeredYAML + "\n"
	if err := os.WriteFile(gqlgenYML, []byte(consumerYML), 0o600); err != nil {
		t.Fatalf("seeding consumer gqlgen.yml: %v", err)
	}
	preSum := sha256.Sum256([]byte(consumerYML))

	// Stub gqlgen binary: a shell script that captures the file passed to
	// `--config <path>` and exits 0. Sqlgen's wrapper exec's whatever
	// `api.graphql.gqlgen_bin` resolves to; splitting on whitespace means
	// `sh <stub>` invokes the script with the wrapper-supplied args.
	capturePath := filepath.Join(stage, "captured_gqlgen_config.yml")
	stubPath := filepath.Join(stage, "stub_gqlgen.sh")
	stub := `#!/bin/sh
# Stub gqlgen binary for TestWrapperLayered. Captures the --config <path>
# argument's file contents to CAPTURE_FILE, then exits 0. Real gqlgen runs
# are out of scope — this test only inspects what sqlgen *passed* to the
# subprocess, not what gqlgen would have produced.
while [ $# -gt 0 ]; do
    case "$1" in
        --config)
            shift
            cp "$1" "$CAPTURE_FILE"
            ;;
    esac
    shift
done
exit 0
`
	if err := os.WriteFile(stubPath, []byte(stub), 0o700); err != nil { //nolint:gosec // stub script must be executable
		t.Fatalf("writing stub gqlgen script: %v", err)
	}

	// Rewrite the staged sqlgen.yml so the wrapper invokes our stub
	// instead of `go run github.com/99designs/gqlgen`. The stub captures
	// the merged temp config to `capturePath` and exits 0 — no real
	// gqlgen invocation happens, so the seeded resolver files / generated
	// artifacts from the staged example stay byte-equal pre/post.
	stagedSqlgenYML := filepath.Join(stage, "sqlgen.yml")
	cfg := string(readFile(t, stagedSqlgenYML))
	cfg = strings.ReplaceAll(cfg, "gqlgen_bin: go run github.com/99designs/gqlgen", "gqlgen_bin: sh "+stubPath)
	if err := os.WriteFile(stagedSqlgenYML, []byte(cfg), 0o600); err != nil {
		t.Fatalf("rewriting staged sqlgen.yml: %v", err)
	}

	// The stub reads CAPTURE_FILE from its env. Exporting via the runSqlgen
	// child env is the simplest plumbing — sqlgen inherits the test env and
	// forwards it (with GOWORK=off appended) to the gqlgen subprocess.
	t.Setenv("CAPTURE_FILE", capturePath)

	out, err := runSqlgen(t, stage, "graphql", "gen", "--config", "./sqlgen.yml")
	if err != nil {
		t.Fatalf("sqlgen graphql gen failed: %v\noutput:\n%s", err, out)
	}

	// Assertion 1: on-disk gqlgen.yml byte-equal pre/post (the wrapper
	// never touches the consumer's file).
	postYML := readFile(t, gqlgenYML)
	postSum := sha256.Sum256(postYML)
	if preSum != postSum {
		t.Errorf("gqlgen.yml SHA changed across `sqlgen graphql gen`:\npre:  %x\npost: %x\nWrapper must NOT modify the on-disk file (PRD §26.5.6).", preSum, postSum)
	}
	if string(postYML) != consumerYML {
		t.Errorf("gqlgen.yml content changed pre/post:\npre:\n%s\npost:\n%s", consumerYML, postYML)
	}

	// Assertion 2: the captured merged temp config carries BOTH the
	// consumer's hand-authored entries AND sqlgen's emitted entries.
	if _, err := os.Stat(capturePath); err != nil {
		t.Fatalf("stub did not capture the merged config (capture file missing): %v\nsqlgen output:\n%s", err, out)
	}
	merged := string(readFile(t, capturePath))

	// Consumer entries survived the merge: the custom directive block name
	// and the hand-bound model entry both appear in the merged config.
	consumerMustHave := []string{
		"consumerOwnedDirective", // from `directives:` block
		"ConsumerOwnedScalar",    // from `models:` block (hand-bound type name)
		"github.com/example/consumer/handbound.Type",
	}
	for _, want := range consumerMustHave {
		if !strings.Contains(merged, want) {
			t.Errorf("consumer-authored entry %q missing from merged temp config:\n%s", want, merged)
		}
	}

	// Sqlgen entries are merged in: per-table model bindings (the example
	// has 8 managed tables — every one should be present) plus the schema
	// glob the wrapper emits. The User/Product entries pin the per-table
	// model binding; PageInfo pins the shared envelope binding required by
	// §26.5.6 "Envelope binding".
	sqlgenMustHave := []string{
		"UserConnection",
		"ProductConnection",
		"PageInfo",
		"models/graph/*.graphqls",
	}
	for _, want := range sqlgenMustHave {
		if !strings.Contains(merged, want) {
			t.Errorf("sqlgen-emitted entry %q missing from merged temp config:\n%s", want, merged)
		}
	}
}

// consumerLayeredYAML is the bonus gqlgen.yml block appended to the
// example's checked-in gqlgen.yml at test start. The directive declaration
// is structurally valid (gqlgen accepts arbitrary directive names under
// `directives:`); the model entry pins a fake scalar binding so the
// post-merge inspection has a distinctive consumer-authored fingerprint to
// grep for.
//
// The directive block is intentionally short — its only job is to give the
// merge step a non-`models:` / non-`schema:` key the wrapper must preserve.
const consumerLayeredYAML = `
# Pre-seeded by TestWrapperLayered to verify the wrapper preserves
# consumer-authored entries across the merge round trip.
directives:
  consumerOwnedDirective:
    skip_runtime: true

models:
  ConsumerOwnedScalar:
    model: github.com/example/consumer/handbound.Type
`
