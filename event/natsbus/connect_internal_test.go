package natsbus

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// TestConnectDialOptions pins the resilience defaults Connect dials with:
// unbounded reconnect, an 8 MiB reconnect buffer, a bounded drain on Close, and
// disconnect and reconnect callbacks.
func TestConnectDialOptions(t *testing.T) {
	o := nats.GetDefaultOptions()
	o.MaxReconnect = 0
	o.ReconnectBufSize = 0
	o.DrainTimeout = 0
	for _, opt := range connectDialOptions() {
		if err := opt(&o); err != nil {
			t.Fatalf("applying dial option: %v", err)
		}
	}
	if o.MaxReconnect != -1 {
		t.Errorf("MaxReconnect = %d, want -1 (reconnect forever)", o.MaxReconnect)
	}
	if o.ReconnectBufSize != 8*1024*1024 {
		t.Errorf("ReconnectBufSize = %d, want %d", o.ReconnectBufSize, 8*1024*1024)
	}
	if o.DrainTimeout != 30*time.Second {
		t.Errorf("DrainTimeout = %v, want 30s", o.DrainTimeout)
	}
	if o.DisconnectedErrCB == nil {
		t.Error("DisconnectedErrCB = nil, want a disconnect callback")
	}
	if o.ReconnectedCB == nil {
		t.Error("ReconnectedCB = nil, want a reconnect callback")
	}
}
