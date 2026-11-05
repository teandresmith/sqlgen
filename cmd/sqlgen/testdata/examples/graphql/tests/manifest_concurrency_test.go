package tests

import (
	"sync"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// TestManifest_ConcurrentReads reads the embedded manifest from many goroutines
// at once; under -race a write anywhere on the read path fails it.
func TestManifest_ConcurrentReads(t *testing.T) {
	client := &models.Client{}
	index := client.Manifest().Entities
	if len(index) == 0 {
		t.Fatal("manifest has no entities")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 16*len(index))
	for range 8 {
		wg.Go(func() {
			for _, ix := range client.Manifest().Entities {
				key := ix.Table
				if ix.Schema != "" {
					key = ix.Schema + "." + ix.Table
				}
				if _, err := client.ManifestEntity(key); err != nil {
					errs <- err
				}
				// Every table here is in one schema, so the bare name resolves
				// too, through the lookup's bare-name scan.
				if _, err := client.ManifestEntity(ix.Table); err != nil {
					errs <- err
				}
				_ = client.ManifestVersion()
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("ManifestEntity during concurrent reads: %v", err)
	}
}
