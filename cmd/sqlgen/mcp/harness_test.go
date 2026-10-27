package mcp

import (
	"net"
	"testing"
	"time"
)

// freeLoopbackAddr reserves an ephemeral loopback port and returns its address.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("release reserved port: %v", err)
	}
	return addr
}

// waitForListen blocks until addr accepts a TCP connection or the deadline
// passes, confirming a transport bound before the test talks to it or cancels
// it.
func waitForListen(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("http transport did not begin listening on %s within 2s", addr)
}

// FreeLoopbackAddr exposes freeLoopbackAddr to the external mcp_test package,
// so both test packages share one helper.
func FreeLoopbackAddr(t *testing.T) string {
	t.Helper()
	return freeLoopbackAddr(t)
}

// WaitForListen exposes waitForListen to the external mcp_test package.
func WaitForListen(t *testing.T, addr string) {
	t.Helper()
	waitForListen(t, addr)
}
