package suite

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// concurrency checks that a client is safe to share across goroutines, outside
// a transaction and on one transaction's context. The suite runs under -race,
// and a barrier in a query hook holds every call until all of them are in
// flight, so the calls do overlap.
func concurrency(t *testing.T, h *Harness) {
	ctx := context.Background()
	const n = 8

	t.Run("one client serves overlapping calls from many goroutines", func(t *testing.T) {
		h.Reset(t)
		authors := seedAuthorsWithEdges(t, h, n)
		barrier, overlapped := queryBarrier(n, readOf(hook.OpGet, models.TableAuthors))
		c := models.New(h.Base, models.WithQueryHook(barrier))
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() { errs[i] = getAuthorThenPost(ctx, c, authors[i], i) })
		}
		wg.Wait()
		if !overlapped() {
			t.Fatalf("the %d calls never overlapped", n)
		}
		if err := errors.Join(errs...); err != nil {
			t.Fatal(err)
		}
		assertRowCount(t, h, "posts", 2*n)
	})

	t.Run("overlapping calls on one transaction's context all run and commit", func(t *testing.T) {
		h.Reset(t)
		authors := seedAuthorsWithEdges(t, h, n)
		barrier, overlapped := queryBarrier(n, readOf(hook.OpGet, models.TableAuthors))
		c := models.New(h.Base, models.WithQueryHook(barrier))
		err := database.WithTransaction(ctx, h.Base, "fan_out", func(txCtx context.Context) error {
			errs := make([]error, n)
			var wg sync.WaitGroup
			for i := range n {
				wg.Go(func() { errs[i] = getAuthorThenPost(txCtx, c, authors[i], i) })
			}
			wg.Wait()
			return errors.Join(errs...)
		})
		if !overlapped() {
			t.Fatalf("the %d calls never overlapped", n)
		}
		if err != nil {
			t.Fatalf("transaction error = %v", err)
		}
		assertRowCount(t, h, "posts", 2*n)
	})
}

// getAuthorThenPost reads the author with both fanned-out relationships,
// checks it came back with its own post and tag, then creates a post with no
// author. It returns what went wrong rather than failing the test, since it
// runs on its own goroutine.
func getAuthorThenPost(ctx context.Context, c *models.Client, want authorEdges, i int) error {
	a, err := c.Authors().Get(ctx, want.author, withPostsAndTags)
	if err != nil {
		return fmt.Errorf("call %d: Get: %w", i, err)
	}
	if a.ID != want.author || len(a.Posts) != 1 || a.Posts[0].ID != want.post || len(a.Tags) != 1 || a.Tags[0].ID != want.tag {
		return fmt.Errorf("call %d: Get = %+v, want author %d with post %d and tag %d", i, a, want.author, want.post, want.tag)
	}
	if _, err := c.Posts().Create(ctx, &models.CreatePostInput{Title: fmt.Sprintf("post %d", i)}); err != nil {
		return fmt.Errorf("call %d: Create: %w", i, err)
	}
	return nil
}

// authorEdges is one seeded author and the keys of its post and its tag.
type authorEdges struct{ author, post, tag int64 }

// seedAuthorsWithEdges creates n authors, each with a post and a tag of its
// own, so each concurrent call has a distinct expected result.
func seedAuthorsWithEdges(t *testing.T, h *Harness, n int) []authorEdges {
	t.Helper()
	out := make([]authorEdges, n)
	for i := range out {
		a := createAuthor(t, h, fmt.Sprintf("author %d", i))
		p := createPost(t, h, fmt.Sprintf("post %d", i), new(a.ID))
		tag := createTag(t, h, fmt.Sprintf("tag %d", i))
		linkAuthorTag(t, h, a.ID, tag.ID)
		out[i] = authorEdges{author: a.ID, post: p.ID, tag: tag.ID}
	}
	return out
}
