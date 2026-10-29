package gen_test

import (
	"strings"
	"testing"
)

// Close closes, in order, the querier and whichever of the cache and the
// event publisher the client is configured with, and joins their errors.
func TestUnifiedClientTemplate_closeClosesConfiguredCacheAndPublisher(t *testing.T) {
	const (
		querierClose   = "if closer, ok := c.querier.(io.Closer); ok {"
		cacheClose     = "if c.opts.cache != nil {\n\t\terrs = append(errs, c.opts.cache.Close())"
		publisherClose = "if closer, ok := c.opts.eventPublisher.(io.Closer); ok {"
		join           = "return errors.Join(errs...)"
	)
	tests := []struct {
		name          string
		cache, events bool
	}{
		{name: "neither configured", cache: false, events: false},
		{name: "cache only", cache: true, events: false},
		{name: "events only", cache: false, events: true},
		{name: "cache and events", cache: true, events: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testUnifiedClientContext_tablesAndViews()
			ctx.CacheEnabled = tt.cache
			ctx.EventsEnabled = tt.events
			out := executeUnifiedClientTemplate(t, ctx)

			start := strings.Index(out, "func (c *Client) Close() error {")
			if start < 0 {
				t.Fatalf("no Close method in output:\n%s", out)
			}
			body := out[start:]
			if end := strings.Index(body, "\n}\n"); end >= 0 {
				body = body[:end]
			}

			want := []string{querierClose}
			if tt.cache {
				want = append(want, cacheClose)
			}
			if tt.events {
				want = append(want, publisherClose)
			}
			want = append(want, join)
			at := 0
			for _, w := range want {
				i := strings.Index(body[at:], w)
				if i < 0 {
					t.Fatalf("Close body lacks %q after offset %d:\n%s", w, at, body)
				}
				at += i + len(w)
			}
			if got := strings.Contains(body, "c.opts.cache"); got != tt.cache {
				t.Errorf("Close touches the cache = %v, want %v:\n%s", got, tt.cache, body)
			}
			if got := strings.Contains(body, "c.opts.eventPublisher"); got != tt.events {
				t.Errorf("Close touches the event publisher = %v, want %v:\n%s", got, tt.events, body)
			}
		})
	}
}
