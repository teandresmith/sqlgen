package tests

// A many-to-many loader scans the junction's keys into their own Go types;
// pgx scans neither BYTEA nor INET into a string.

import (
	"context"
	"net"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

func TestM2MLoad_BinaryAndNetworkKeys(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	addrs := []net.IP{net.ParseIP("10.20.0.1"), net.ParseIP("2001:db8::20")}
	for i, addr := range addrs {
		if _, err := client.Hosts().Create(ctx, &models.CreateHostInput{Addr: addr, Label: []string{"host-a", "host-b"}[i]}); err != nil {
			t.Fatalf("Create host %s: %v", addr, err)
		}
		t.Cleanup(func() { _ = client.Hosts().HardDelete(ctx, addr) })
	}
	digests := [][]byte{{0x00, 0xff}, {0x41}, {0x7f, 0x80, 0x00}}
	for i, digest := range digests {
		if _, err := client.Fingerprints().Create(ctx, &models.CreateFingerprintInput{Digest: digest, Label: []string{"fp-a", "fp-b", "fp-c"}[i]}); err != nil {
			t.Fatalf("Create fingerprint %x: %v", digest, err)
		}
		t.Cleanup(func() { _ = client.Fingerprints().HardDelete(ctx, digest) })
	}

	// host-a holds fp-a and fp-b, host-b holds fp-b; fp-c has no host.
	links := []models.CreateHostFingerprintInput{
		{HostAddr: addrs[0], FingerprintDigest: digests[0]},
		{HostAddr: addrs[0], FingerprintDigest: digests[1]},
		{HostAddr: addrs[1], FingerprintDigest: digests[1]},
	}
	for _, in := range links {
		if _, err := client.HostFingerprints().Create(ctx, &in); err != nil {
			t.Fatalf("Create host fingerprint: %v", err)
		}
		// Registered after the hosts and fingerprints, so it runs before them.
		t.Cleanup(func() {
			_ = client.HostFingerprints().HardDelete(ctx, models.HostFingerprintPK{HostAddr: in.HostAddr, FingerprintDigest: in.FingerprintDigest})
		})
	}

	t.Run("host fingerprints by binary key", func(t *testing.T) {
		hosts, err := client.Hosts().GetMany(ctx, &models.GetHostsInput{
			Filter: &models.HostFilter{Addr: &comparator.Opaque[net.IP]{In: addrs}},
		}, func(o *models.CallOptions[models.HostFieldOptions]) {
			o.FieldOptions = &models.HostFieldOptions{
				Label:        true,
				Fingerprints: &models.FingerprintRelationshipOptions{FieldOptions: &models.FingerprintFieldOptions{Label: true}},
			}
		})
		if err != nil {
			t.Fatalf("GetMany hosts with Fingerprints: %v", err)
		}
		want := map[string][]string{"host-a": {"fp-a", "fp-b"}, "host-b": {"fp-b"}}
		if len(hosts) != len(want) {
			t.Fatalf("GetMany returned %d hosts, want %d", len(hosts), len(want))
		}
		for _, h := range hosts {
			var got []string
			for _, f := range h.Fingerprints {
				got = append(got, f.Label)
			}
			slices.Sort(got)
			if !slices.Equal(got, want[h.Label]) {
				t.Errorf("%s: Fingerprints = %v, want %v", h.Label, got, want[h.Label])
			}
		}
	})

	t.Run("fingerprint hosts by network key", func(t *testing.T) {
		fingerprints, err := client.Fingerprints().GetMany(ctx, &models.GetFingerprintsInput{
			Filter: &models.FingerprintFilter{Digest: &comparator.Opaque[[]byte]{In: digests}},
		}, func(o *models.CallOptions[models.FingerprintFieldOptions]) {
			o.FieldOptions = &models.FingerprintFieldOptions{
				Label: true,
				Hosts: &models.HostRelationshipOptions{FieldOptions: &models.HostFieldOptions{Label: true}},
			}
		})
		if err != nil {
			t.Fatalf("GetMany fingerprints with Hosts: %v", err)
		}
		want := map[string][]string{"fp-a": {"host-a"}, "fp-b": {"host-a", "host-b"}, "fp-c": nil}
		if len(fingerprints) != len(want) {
			t.Fatalf("GetMany returned %d fingerprints, want %d", len(fingerprints), len(want))
		}
		for _, f := range fingerprints {
			var got []string
			for _, h := range f.Hosts {
				got = append(got, h.Label)
			}
			slices.Sort(got)
			if !slices.Equal(got, want[f.Label]) {
				t.Errorf("%s: Hosts = %v, want %v", f.Label, got, want[f.Label])
			}
		}
	})
}
