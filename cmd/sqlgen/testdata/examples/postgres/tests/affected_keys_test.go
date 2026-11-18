package tests

import (
	"context"
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// affectedPKsRecorder returns a client whose mutation hook keeps the
// AffectedPKs of the last mutation, and a func that returns them.
func affectedPKsRecorder() (*models.Client, func() []any) {
	var last []any
	record := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			res, err := next(ctx, m)
			last = m.AffectedPKs
			return res, err
		}
	}
	return models.New(dbpgx.New(testPool), models.WithMutationHook(record)), func() []any { return last }
}

// A BYTEA or INET key is a slice in Go, so the batch keys its written-key set
// by each key's rendering rather than by the key itself. The written keys are
// recorded in the order they were listed; a key matching no row is left out.
func TestAffectedKeys_BatchRecordsNonComparableKeys(t *testing.T) {
	ctx := context.Background()
	client, affected := affectedPKsRecorder()

	t.Run("binary keys", func(t *testing.T) {
		digests := [][]byte{{0x0a, 0x01}, {0x0a, 0x02}}
		for i, digest := range digests {
			if _, err := client.Fingerprints().Create(ctx, &models.CreateFingerprintInput{Digest: digest, Label: []string{"ak-fp-a", "ak-fp-b"}[i]}); err != nil {
				t.Fatalf("Create fingerprint %x: %v", digest, err)
			}
			t.Cleanup(func() { _ = client.Fingerprints().HardDelete(ctx, digest) })
		}
		missing := []byte{0x0a, 0xff}

		if err := client.Fingerprints().HardDeleteMany(ctx, [][]byte{digests[1], missing, digests[0]}); err != nil {
			t.Fatalf("HardDeleteMany: %v", err)
		}
		if diff := cmp.Diff([]any{digests[1], digests[0]}, affected()); diff != "" {
			t.Errorf("AffectedPKs (-want +got):\n%s", diff)
		}
	})

	t.Run("network keys", func(t *testing.T) {
		addrs := []net.IP{net.ParseIP("10.30.0.1"), net.ParseIP("2001:db8::30")}
		for i, addr := range addrs {
			if _, err := client.Hosts().Create(ctx, &models.CreateHostInput{Addr: addr, Label: []string{"ak-host-a", "ak-host-b"}[i]}); err != nil {
				t.Fatalf("Create host %s: %v", addr, err)
			}
			t.Cleanup(func() { _ = client.Hosts().HardDelete(ctx, addr) })
		}

		if err := client.Hosts().HardDeleteMany(ctx, []net.IP{addrs[1], addrs[0]}); err != nil {
			t.Fatalf("HardDeleteMany: %v", err)
		}
		if diff := cmp.Diff([]any{addrs[1], addrs[0]}, affected()); diff != "" {
			t.Errorf("AffectedPKs (-want +got):\n%s", diff)
		}
	})
}
