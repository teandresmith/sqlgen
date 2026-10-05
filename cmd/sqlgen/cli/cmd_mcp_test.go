package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestMCPServe_HelpListsFlags(t *testing.T) {
	stdout, _, err := executeCommand("mcp", "serve", "--help")
	if err != nil {
		t.Fatalf("mcp serve --help returned error: %v", err)
	}
	for _, flag := range []string{"--manifest", "--stdio", "--http", "--watch", "--no-watch", "--log", "--log-level"} {
		if !strings.Contains(stdout, flag) {
			t.Errorf("mcp serve --help output missing flag %q", flag)
		}
	}
}

func TestMCPServe_RefusesNonLoopbackHTTP(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{name: "wildcard bind", addr: "0.0.0.0:8080"},
		{name: "external ip", addr: "10.0.0.5:8080"},
		{name: "hostname", addr: "db.internal:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// --stdio=false so the command never enters the serve loop; the
			// non-loopback bind is refused at flag-parse time.
			_, _, err := executeCommand("mcp", "serve", "--stdio=false", "--http", tt.addr)
			if err == nil {
				t.Fatalf("mcp serve --http %s = nil, want error", tt.addr)
			}
			var ee *exitError
			if !errors.As(err, &ee) || ee.code != ExitConfig {
				t.Errorf("mcp serve --http %s error = %v, want *exitError with code ExitConfig", tt.addr, err)
			}
		})
	}
}

func TestMCPServe_RejectsInvalidLogLevel(t *testing.T) {
	_, _, err := executeCommand("mcp", "serve", "--stdio=false", "--log-level", "trace")
	if err == nil {
		t.Fatal("mcp serve --log-level trace = nil, want error")
	}
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != ExitConfig {
		t.Errorf("mcp serve --log-level trace error = %v, want *exitError with code ExitConfig", err)
	}
}

func TestMCPServe_WatchAndNoWatchMutuallyExclusive(t *testing.T) {
	_, _, err := executeCommand("mcp", "serve", "--stdio=false", "--watch", "--no-watch")
	if err == nil {
		t.Fatal("mcp serve --watch --no-watch = nil, want mutually-exclusive error")
	}
}
