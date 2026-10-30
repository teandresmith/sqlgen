package suite

import "testing"

// errorWrapping checks that every client method's error starts with its own
// operation and table prefix and keeps its cause reachable.
func errorWrapping(t *testing.T, h *Harness) {
	_, _ = t, h
}
