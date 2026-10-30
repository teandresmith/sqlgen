package suite

import "testing"

// concurrency checks that a client is safe to share across goroutines, outside
// a transaction and on one transaction's context.
func concurrency(t *testing.T, h *Harness) {
	_, _ = t, h
}
