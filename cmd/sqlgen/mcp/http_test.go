package mcp_test

import (
	"testing"

	sqlgenmcp "github.com/teandresmith/sqlgen/cmd/sqlgen/mcp"
)

func TestResolveHTTPAddr(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		want    string
		wantErr bool
	}{
		{name: "bare port", flag: "8080", want: "127.0.0.1:8080"},
		{name: "empty host defaults to loopback", flag: ":8080", want: "127.0.0.1:8080"},
		{name: "explicit loopback ipv4", flag: "127.0.0.1:8080", want: "127.0.0.1:8080"},
		{name: "loopback ipv4 in 127 block", flag: "127.0.0.2:9000", want: "127.0.0.2:9000"},
		{name: "loopback ipv6", flag: "[::1]:8080", want: "[::1]:8080"},

		{name: "wildcard bind refused", flag: "0.0.0.0:8080", wantErr: true},
		{name: "wildcard ipv6 refused", flag: "[::]:8080", wantErr: true},
		{name: "external ip refused", flag: "192.168.1.10:8080", wantErr: true},
		{name: "public ip refused", flag: "8.8.8.8:8080", wantErr: true},
		{name: "hostname refused", flag: "example.com:8080", wantErr: true},
		{name: "localhost hostname refused", flag: "localhost:8080", wantErr: true},

		{name: "empty flag refused", flag: "", wantErr: true},
		{name: "non-numeric port refused", flag: "127.0.0.1:abc", wantErr: true},
		{name: "port zero refused", flag: "0", wantErr: true},
		{name: "port out of range refused", flag: "70000", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sqlgenmcp.ResolveHTTPAddr(tt.flag)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveHTTPAddr(%q) = %q, want error", tt.flag, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveHTTPAddr(%q) unexpected error: %v", tt.flag, err)
			}
			if got != tt.want {
				t.Errorf("ResolveHTTPAddr(%q) = %q, want %q", tt.flag, got, tt.want)
			}
		})
	}
}
